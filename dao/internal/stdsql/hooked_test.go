package stdsql

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/dao"

	_ "github.com/go-sql-driver/mysql"
	_ "modernc.org/sqlite"
)

// sqlite is the engine under test here because it is the harder of the two
// database/sql engines: it exports no connector, so it exercises the
// dsnConnector fallback. mysql's driver.DriverContext path is covered by
// TestBaseConnector_UsesTheDriversOwnConnector, which needs no server.

func dsn(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "hook.db")
}

// A TEMP table in sqlite belongs to ONE connection and is invisible to every
// other. So a temp table created by the hook and read back through the pool
// proves the hook ran on THE SAME PHYSICAL CONNECTION the pool went on to
// use — not merely that it ran.
func TestHookRunsOnTheConnectionThePoolThenUses(t *testing.T) {
	ctx := context.Background()

	db, err := OpenHooked("sqlite", dsn(t), func(ctx context.Context, c dao.ConnectedConn) error {
		_, err := c.ExecContext(ctx, `CREATE TEMP TABLE hook_marker (v INTEGER)`)
		if err != nil {
			return err
		}
		_, err = c.ExecContext(ctx, `INSERT INTO hook_marker (v) VALUES (42)`)
		return err
	})
	if err != nil {
		t.Fatalf("OpenHooked: %v", err)
	}
	defer db.Close()

	var got int
	if err := db.QueryRowContext(ctx, `SELECT v FROM hook_marker`).Scan(&got); err != nil {
		t.Fatalf("reading the hook's temp table through the pool: %v\n"+
			"a 'no such table' here means the hook ran on a different connection "+
			"than the one the pool handed out", err)
	}
	if got != 42 {
		t.Errorf("hook_marker.v = %d, want 42", got)
	}
}

// Positive control for the test above: WITHOUT a hook the temp table must not
// exist. If this passed too, the assertion above would be measuring something
// other than the hook.
func TestWithoutAHookTheMarkerIsAbsent(t *testing.T) {
	ctx := context.Background()

	db, err := OpenHooked("sqlite", dsn(t), nil)
	if err != nil {
		t.Fatalf("OpenHooked: %v", err)
	}
	defer db.Close()

	var got int
	err = db.QueryRowContext(ctx, `SELECT v FROM hook_marker`).Scan(&got)
	if err == nil {
		t.Fatal("hook_marker exists with no hook installed; the marker proves nothing")
	}
}

// The hook must run once per PHYSICAL connection: once at first open, and
// again on every reconnect. Counting calls alone would pass for a hook that
// fires twice on one connection or skips every other one, so this counts
// DISTINCT connections too — each must be hooked exactly once.
func TestHookRunsOncePerPhysicalConnection(t *testing.T) {
	ctx := context.Background()

	var mu sync.Mutex
	hookedPerConn := map[int]int{}
	var hookErrs []error // a hook that FAILS is invisible unless it is recorded
	var nextID atomic.Int64

	db, err := OpenHooked("sqlite", dsn(t), func(ctx context.Context, c dao.ConnectedConn) error {
		// Stamp this connection with an id only it can see, so the pool can
		// tell its physical connections apart the way pg_backend_pid does.
		id := int(nextID.Add(1))
		record := func(err error) error {
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				hookErrs = append(hookErrs, err)
				return err
			}
			hookedPerConn[id]++
			return nil
		}
		if _, err := c.ExecContext(ctx, `CREATE TEMP TABLE conn_id (v INTEGER)`); err != nil {
			return record(err)
		}
		if _, err := c.ExecContext(ctx, fmt.Sprintf(`INSERT INTO conn_id (v) VALUES (%d)`, id)); err != nil {
			return record(err)
		}
		return record(nil)
	})
	if err != nil {
		t.Fatalf("OpenHooked: %v", err)
	}
	defer db.Close()

	// Force the pool to churn physical connections rather than reuse one.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(0)
	db.SetConnMaxIdleTime(time.Millisecond)

	seen := map[int]int{}
	var seenMu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var id int
			if err := db.QueryRowContext(ctx, `SELECT v FROM conn_id`).Scan(&id); err != nil {
				t.Errorf("reading conn_id: %v", err)
				return
			}
			seenMu.Lock()
			seen[id]++
			seenMu.Unlock()
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(hookedPerConn) == 0 {
		t.Fatal("no connection was hooked")
	}
	// A FAILED hook is reported as itself. It used to be invisible: the failure
	// incremented an entry counter, never reached the map, and the mismatch
	// surfaced as "a connection was hooked twice" — a message for a completely
	// different fault, and one that could not be told apart from the real thing.
	if len(hookErrs) > 0 {
		t.Errorf("%d hook invocation(s) failed; the first was: %v", len(hookErrs), hookErrs[0])
	}
	for id, n := range hookedPerConn {
		if n != 1 {
			t.Errorf("connection %d was hooked %d times, want exactly 1", id, n)
		}
	}
	// Every connection the pool served had been stamped, so none escaped the
	// hook. A missing stamp would have failed the Scan above.
	seenMu.Lock()
	defer seenMu.Unlock()
	for id := range seen {
		if _, ok := hookedPerConn[id]; !ok {
			t.Errorf("the pool served connection %d, which the hook never stamped", id)
		}
	}
	t.Logf("observed %d distinct physical connections, each hooked exactly once; "+
		"the pool served %d of them across 12 concurrent queries",
		len(hookedPerConn), len(seen))
}

func TestFailingHookFailsTheConnect(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("hook says no")

	db, err := OpenHooked("sqlite", dsn(t), func(context.Context, dao.ConnectedConn) error {
		return sentinel
	})
	if err != nil {
		t.Fatalf("OpenHooked: %v", err)
	}
	defer db.Close()

	err = db.PingContext(ctx)
	if err == nil {
		t.Fatal("Ping succeeded although the hook failed")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want it to wrap %v", err, sentinel)
	}
}

// A hook that queries must work, not only one that execs — ConnectedConn
// promises Querier as well as Execer, and the lent *sql.Conn is what makes
// the stdlib's row conversion available.
func TestHookCanQuery(t *testing.T) {
	ctx := context.Background()
	var version string

	db, err := OpenHooked("sqlite", dsn(t), func(ctx context.Context, c dao.ConnectedConn) error {
		rows, err := c.QueryContext(ctx, `SELECT sqlite_version()`)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			return errors.New("no row from sqlite_version()")
		}
		if err := rows.Scan(&version); err != nil {
			return err
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("OpenHooked: %v", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if version == "" {
		t.Error("hook's query returned no version string")
	}
}

// The nil hook must be the ordinary open, not a wrapped one — callers should
// not pay for a seam they did not ask for.
func TestNilHookOpensOrdinarily(t *testing.T) {
	db, err := OpenHooked("sqlite", dsn(t), nil)
	if err != nil {
		t.Fatalf("OpenHooked: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

// mysql implements driver.DriverContext, so baseConnector must take the
// driver's OWN connector rather than the dsnConnector fallback. No server is
// needed: building a connector does not dial.
func TestBaseConnector_UsesTheDriversOwnConnector(t *testing.T) {
	// Not a skip: the driver is imported by this test file precisely so this
	// assertion always runs. A skip here would observe nothing.
	if !driverRegistered("mysql") {
		t.Fatal("mysql driver is not registered; the blank import above should guarantee it")
	}
	c, err := baseConnector("mysql", "user:pass@tcp(127.0.0.1:3306)/db")
	if err != nil {
		t.Fatalf("baseConnector: %v", err)
	}
	if _, isFallback := c.(dsnConnector); isFallback {
		t.Error("mysql went through the dsnConnector fallback despite implementing driver.DriverContext")
	}
}

// sqlite does NOT implement driver.DriverContext, so it must take the
// fallback. This is the decoy for the test above: if both engines returned
// the same shape, neither assertion would mean anything.
func TestBaseConnector_FallsBackWhenTheDriverHasNoConnector(t *testing.T) {
	c, err := baseConnector("sqlite", "file:x.db")
	if err != nil {
		t.Fatalf("baseConnector: %v", err)
	}
	if _, isFallback := c.(dsnConnector); !isFallback {
		t.Errorf("sqlite returned %T, want the dsnConnector fallback", c)
	}
}

func TestOneShotLendsExactlyOnce(t *testing.T) {
	want := stubConn{}
	o := &oneShot{conn: want}

	// First: the exact connection being lent, not merely a non-error.
	got, err := o.Connect(context.Background())
	if err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	if got != driver.Conn(want) {
		t.Fatalf("first Connect returned %#v, want the connection being lent", got)
	}

	// Second: refused. Without the first assertion above, a oneShot that
	// never lent anything would also pass this.
	if _, err := o.Connect(context.Background()); !errors.Is(err, errLendExhausted) {
		t.Errorf("second Connect error = %v, want errLendExhausted", err)
	}
}

// noCloseConn is the trick the whole approach rests on: the throwaway pool
// closes what it believes it owns, and the real connection must survive it.
func TestNoCloseConnSuppressesClose(t *testing.T) {
	rec := &recordingConn{}
	wrapped := noCloseConn{Conn: rec}

	if err := wrapped.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if rec.closed {
		t.Error("Close reached the real connection; the pool would have closed the connection it was lent")
	}

	// Decoy: closing the real connection directly MUST record, or the flag
	// above proves nothing.
	if err := rec.Close(); err != nil {
		t.Fatalf("direct Close: %v", err)
	}
	if !rec.closed {
		t.Error("the recorder never records a close; the assertion above is vacuous")
	}
}

type stubConn struct{}

func (stubConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("stub") }
func (stubConn) Close() error                        { return nil }
func (stubConn) Begin() (driver.Tx, error)           { return nil, errors.New("stub") }

type recordingConn struct {
	stubConn
	closed bool
}

func (c *recordingConn) Close() error { c.closed = true; return nil }

func driverRegistered(name string) bool {
	for _, d := range sql.Drivers() {
		if d == name {
			return true
		}
	}
	return false
}

// A5's exception, measured rather than assumed. database/sql retries a connect
// that fails with driver.ErrBadConn, so a hook returning it — or wrapping it —
// runs again. Hook authors need to know, because a hook with a side effect
// would repeat it.
func TestErrBadConnFromAHookIsRetried(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ret     error
		wantMin int64
	}{
		{"plain error is not retried", errors.New("nope"), 1},
		{"ErrBadConn is retried", driver.ErrBadConn, 2},
		{"wrapped ErrBadConn is retried", fmt.Errorf("wrap: %w", driver.ErrBadConn), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int64
			db, err := OpenHooked("sqlite", dsn(t), func(context.Context, dao.ConnectedConn) error {
				calls.Add(1)
				return tc.ret
			})
			if err != nil {
				t.Fatalf("OpenHooked: %v", err)
			}
			defer db.Close()

			if err := db.PingContext(context.Background()); err == nil {
				t.Fatal("Ping succeeded although the hook failed")
			}
			got := calls.Load()
			if tc.wantMin == 1 && got != 1 {
				t.Errorf("hook ran %d times, want exactly 1: an ordinary error must stop the connect", got)
			}
			if tc.wantMin > 1 && got < tc.wantMin {
				t.Errorf("hook ran %d times, want at least %d: database/sql retries ErrBadConn", got, tc.wantMin)
			}
			t.Logf("%s -> hook ran %d time(s)", tc.name, got)
		})
	}
}
