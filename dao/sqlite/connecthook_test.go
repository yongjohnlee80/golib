package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
)

// A1b. autodb converts plain functions to these Option types
// (core/exec/conns.go). That conversion is exactly what makes changing the
// Option type breaking rather than additive, so the connect-hook work must
// leave it compiling. This witness is the conversion form itself.
var _ = Option(func(db *sql.DB) { db.SetMaxOpenConns(1) })

func hookDSN(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "hooked.db")
}

// A3(ii). THE property that modernc.org/sqlite's global RegisterConnectionHook
// would break: two DataConns open over sqlite in ONE process, with DIFFERENT
// hooks, must each see only their own. With a process-global registration both
// hooks would fire on every connection of both, and each marker would be
// visible from both sides.
func TestTwoDataConnsWithDifferentHooksAreIsolated(t *testing.T) {
	ctx := context.Background()

	a, err := OpenHooked(ctx, "a", hookDSN(t), func(ctx context.Context, c dao.ConnectedConn) error {
		_, err := c.ExecContext(ctx, `CREATE TEMP TABLE marker_a (v INTEGER)`)
		return err
	})
	if err != nil {
		t.Fatalf("open a: %v", err)
	}
	defer a.Close()

	b, err := OpenHooked(ctx, "b", hookDSN(t), func(ctx context.Context, c dao.ConnectedConn) error {
		_, err := c.ExecContext(ctx, `CREATE TEMP TABLE marker_b (v INTEGER)`)
		return err
	})
	if err != nil {
		t.Fatalf("open b: %v", err)
	}
	defer b.Close()

	sees := func(conn dao.DataConn, table string) bool {
		rows, err := conn.QueryContext(ctx, `SELECT 1 FROM `+table)
		if err != nil {
			return false
		}
		_ = rows.Close()
		return true
	}

	// Each sees its own — without this the isolation claim below would hold
	// trivially for a hook that never ran at all.
	if !sees(a, "marker_a") {
		t.Error("connection a does not see its own hook's marker; its hook did not run")
	}
	if !sees(b, "marker_b") {
		t.Error("connection b does not see its own hook's marker; its hook did not run")
	}

	// Neither sees the other's. This is the assertion the global registration
	// would fail.
	if sees(a, "marker_b") {
		t.Error("connection a sees b's marker: the hook leaked across DataConns")
	}
	if sees(b, "marker_a") {
		t.Error("connection b sees a's marker: the hook leaked across DataConns")
	}
}

// A5, stdsql side. A hook failure stops the connect and reaches the caller;
// it is NOT retried, so a hook with a side effect runs once. The postgres side
// asserts the same property against pgx separately, because the two engines
// reach it by different machinery and averaging them into one sentence would
// hide the exception documented on OpenHooked.
func TestFailingHookStopsTheConnectAndIsNotRetried(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("hook refuses")
	calls := 0

	_, err := OpenHooked(ctx, "x", hookDSN(t), func(context.Context, dao.ConnectedConn) error {
		calls++
		return sentinel
	})
	if err == nil {
		t.Fatal("OpenHooked succeeded although the hook failed")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error = %v, want it to wrap %v", err, sentinel)
	}
	if calls != 1 {
		t.Errorf("hook ran %d times, want exactly 1 — an ordinary hook error must not be retried", calls)
	}
}
