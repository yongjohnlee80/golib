package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yongjohnlee80/golib/dao"
)

// Compile-time proof that the adapter satisfies the narrow capability and
// nothing wider: a ConnectedConn is Querier+Execer, and a hook must not be
// able to Begin or Close the connection it is configuring.
var _ dao.ConnectedConn = pgxConnectedConn{}

// TestWithConnectHook_SetsAfterConnect needs no server. It pins the plumbing:
// the option installs pgx's AfterConnect, and a nil hook is a no-op rather
// than a nil-deref waiting for the first connect.
func TestWithConnectHook_SetsAfterConnect(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://u:p@localhost:1/db")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.AfterConnect != nil {
		t.Fatal("a fresh config already has AfterConnect set — this test would prove nothing")
	}
	WithConnectHook(func(context.Context, dao.ConnectedConn) error { return nil })(cfg)
	if cfg.AfterConnect == nil {
		t.Error("WithConnectHook did not install AfterConnect")
	}

	cfg2, _ := pgxpool.ParseConfig("postgres://u:p@localhost:1/db")
	WithConnectHook(nil)(cfg2)
	if cfg2.AfterConnect != nil {
		t.Error("WithConnectHook(nil) installed a hook; it must be a no-op")
	}
}

func hookURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TEST_PGURL")
	if url == "" {
		t.Skip("TEST_PGURL not set; skipping postgres connect-hook integration tests")
	}
	return url
}

// TestConnectHook_RunsOnEveryPhysicalConnect is the property the whole seam
// exists for, and the one a single-connection test would NOT establish: a
// DataConn is a pool, so the hook must fire for connections created later and
// for replacements — not once at Open.
//
// It asserts SET MEMBERSHIP, not a count. An earlier version compared the hook
// count before and after recycling and required it to grow, which lector and I
// both flagged as a thin discriminator: a hook that fired on every OTHER
// connect would satisfy "it grew". Here every connection is identified by its
// backend PID, and the assertion is that EVERY pid a query ran on was also
// seen by the hook. A missed connection is then named, not averaged away.
func TestConnectHook_RunsOnEveryPhysicalConnect(t *testing.T) {
	url := hookURL(t)

	var mu sync.Mutex
	// COUNT per backend, not a set: set membership proves every served
	// connection was hooked, but not that it was hooked exactly ONCE. A hook
	// re-run on an existing connection is a different defect and this
	// distinguishes it (lector, r0).
	hooked := map[int32]int{}

	conn, err := OpenNamed(context.Background(), "hooktest", url,
		WithConnectHook(func(ctx context.Context, c dao.ConnectedConn) error {
			// The hook does real work on the ConnectedConn surface rather than
			// merely incrementing: this is also the only exercise of
			// QueryContext/Scan through the narrow capability.
			rows, qerr := c.QueryContext(ctx, "SELECT pg_backend_pid()")
			if qerr != nil {
				return qerr
			}
			defer rows.Close()
			var pid int32
			if rows.Next() {
				if serr := rows.Scan(&pid); serr != nil {
					return serr
				}
			}
			if err := rows.Err(); err != nil {
				return err
			}
			mu.Lock()
			hooked[pid]++
			mu.Unlock()
			return nil
		}),
		MaxOpenConns(3),
		func(cfg *pgxpool.Config) {
			cfg.MinConns = 0
			cfg.MaxConnIdleTime = 200 * time.Millisecond
			cfg.MaxConnLifetime = 400 * time.Millisecond
		},
	)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	var servedMu sync.Mutex
	served := map[int32]bool{}
	// Which physical connection actually served this query.
	work := func() {
		rows, qerr := conn.QueryContext(ctx, "SELECT pg_backend_pid()")
		if qerr != nil {
			t.Fatalf("query: %v", qerr)
		}
		defer rows.Close()
		var pid int32
		if rows.Next() {
			if serr := rows.Scan(&pid); serr != nil {
				t.Fatalf("scan: %v", serr)
			}
			servedMu.Lock()
			served[pid] = true
			servedMu.Unlock()
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("rows: %v", err)
		}
	}

	// Rounds of CONCURRENT work, separated by enough idle time that the pool
	// discards and re-establishes connections. Concurrency is what forces
	// several physical connects at once rather than one reused serially, and
	// the idle gap is what forces replacements (lector, r0).
	for round := 0; round < 3; round++ {
		var wg sync.WaitGroup
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); work() }()
		}
		wg.Wait()
		time.Sleep(700 * time.Millisecond)
	}

	if len(served) < 2 {
		// The instrument must observe more than one connection or it cannot
		// distinguish "hooked every connect" from "hooked the only one".
		t.Fatalf("only %d distinct backend(s) served queries — the pool never recycled, so "+
			"this run proves nothing about later connections", len(served))
	}

	mu.Lock()
	defer mu.Unlock()
	servedMu.Lock()
	defer servedMu.Unlock()
	var missed, repeated []int32
	for pid := range served {
		switch n := hooked[pid]; {
		case n == 0:
			missed = append(missed, pid)
		case n > 1:
			repeated = append(repeated, pid)
		}
	}
	if len(missed) > 0 {
		t.Errorf("%d of %d physical connection(s) served a query WITHOUT the hook having run "+
			"on them (backend pids %v) — the hook is not firing on every physical connect, "+
			"which is the property this seam exists for", len(missed), len(served), missed)
	}
	if len(repeated) > 0 {
		t.Errorf("the hook ran MORE THAN ONCE on backend pid(s) %v — it must run once per "+
			"physical connect, not per acquire", repeated)
	}
	t.Logf("%d distinct backends served queries, each hooked exactly once (%d hook runs total)",
		len(served), len(hooked))
}

// TestConnectHook_FailingHookFailsTheConnect: a session that cannot be set up
// must never serve. The error has to reach the caller, and no query may run on
// the connection the hook rejected.
func TestConnectHook_FailingHookFailsTheConnect(t *testing.T) {
	url := hookURL(t)
	sentinel := errors.New("connect hook refused this session")
	var served atomic.Int64

	conn, err := OpenNamed(context.Background(), "hookfail", url,
		WithConnectHook(func(ctx context.Context, c dao.ConnectedConn) error {
			return sentinel
		}),
	)
	if err != nil {
		// Failing at Open is an acceptable shape too; the property is that no
		// query ever runs.
		if !strings.Contains(err.Error(), sentinel.Error()) {
			t.Fatalf("open failed but not with the hook's error: %v", err)
		}
		t.Logf("open refused, carrying the hook's error: %v", err)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, qerr := conn.QueryContext(ctx, "SELECT 1")
	if qerr == nil {
		served.Add(1)
		_ = rows.Close()
		t.Fatal("a query ran on a connection whose hook returned an error")
	}
	if !strings.Contains(qerr.Error(), sentinel.Error()) {
		t.Errorf("the hook's error did not reach the caller; got %v", qerr)
	}
}

// TestWithConnectHook_ComposesAndShortCircuits: WithConnectHook must never
// overwrite an AfterConnect that is already set — by an earlier
// WithConnectHook, or by a caller's own Option reaching pgxpool.Config. The
// prior callback runs FIRST and its error short-circuits, so a later hook
// never runs against a half-configured session. Needs no server.
func TestWithConnectHook_ComposesAndShortCircuits(t *testing.T) {
	newCfg := func(t *testing.T) *pgxpool.Config {
		t.Helper()
		cfg, err := pgxpool.ParseConfig("postgres://u:p@localhost:1/db")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		return cfg
	}

	t.Run("both run, in registration order", func(t *testing.T) {
		cfg := newCfg(t)
		var order []string
		WithConnectHook(func(context.Context, dao.ConnectedConn) error {
			order = append(order, "first")
			return nil
		})(cfg)
		WithConnectHook(func(context.Context, dao.ConnectedConn) error {
			order = append(order, "second")
			return nil
		})(cfg)
		if err := cfg.AfterConnect(context.Background(), nil); err != nil {
			t.Fatalf("AfterConnect: %v", err)
		}
		if len(order) != 2 || order[0] != "first" || order[1] != "second" {
			t.Errorf("hooks ran %v; want [first second] — the second overwrote the first, or "+
				"they ran out of registration order", order)
		}
	})

	t.Run("an earlier failure short-circuits the later hook", func(t *testing.T) {
		cfg := newCfg(t)
		boom := errors.New("first hook refused")
		ran := false
		WithConnectHook(func(context.Context, dao.ConnectedConn) error { return boom })(cfg)
		WithConnectHook(func(context.Context, dao.ConnectedConn) error {
			ran = true
			return nil
		})(cfg)
		err := cfg.AfterConnect(context.Background(), nil)
		if !errors.Is(err, boom) {
			t.Errorf("AfterConnect returned %v; want the first hook's error", err)
		}
		if ran {
			t.Error("the second hook ran after the first refused the connection — a later hook " +
				"must not run against a half-configured session")
		}
	})

	t.Run("a pre-existing AfterConnect is preserved", func(t *testing.T) {
		cfg := newCfg(t)
		priorRan := false
		cfg.AfterConnect = func(context.Context, *pgx.Conn) error { priorRan = true; return nil }
		WithConnectHook(func(context.Context, dao.ConnectedConn) error { return nil })(cfg)
		if err := cfg.AfterConnect(context.Background(), nil); err != nil {
			t.Fatalf("AfterConnect: %v", err)
		}
		if !priorRan {
			t.Error("WithConnectHook overwrote an AfterConnect the caller had already set")
		}
	})
}

// TestConnectHook_AbsentHookIsUnaffected is the negative control. Without it,
// a broken pool would make the tests above pass for the wrong reason.
func TestConnectHook_AbsentHookIsUnaffected(t *testing.T) {
	url := hookURL(t)
	conn, err := OpenNamed(context.Background(), "nohook", url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	rows, qerr := conn.QueryContext(context.Background(), "SELECT 1")
	if qerr != nil {
		t.Fatalf("a pool with no hook must behave exactly as before: %v", qerr)
	}
	_ = rows.Close()
}
