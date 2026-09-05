package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
// MinConns 0 with a sub-second MaxConnIdleTime makes the pool discard idle
// connections, so the second round of work forces genuinely NEW physical
// connects. The assertion is that the count GREW, with the first round's count
// recorded as the baseline; a hook that only ran at Open would leave it equal.
func TestConnectHook_RunsOnEveryPhysicalConnect(t *testing.T) {
	url := hookURL(t)
	var hooked atomic.Int64

	conn, err := OpenNamed(context.Background(), "hooktest", url,
		WithConnectHook(func(ctx context.Context, c dao.ConnectedConn) error {
			hooked.Add(1)
			// A real hook does work; do some, so the ConnectedConn surface is
			// exercised rather than merely counted.
			rows, qerr := c.QueryContext(ctx, "SELECT 1")
			if qerr != nil {
				return qerr
			}
			defer rows.Close()
			return rows.Err()
		}),
		MaxOpenConns(2),
		func(cfg *pgxpool.Config) {
			cfg.MinConns = 0
			cfg.MaxConnIdleTime = 200 * time.Millisecond
			cfg.MaxConnLifetime = 500 * time.Millisecond
		},
	)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()

	ctx := context.Background()
	exercise := func() {
		rows, qerr := conn.QueryContext(ctx, "SELECT 1")
		if qerr != nil {
			t.Fatalf("query: %v", qerr)
		}
		for rows.Next() {
		}
		_ = rows.Close()
	}

	exercise()
	first := hooked.Load()
	if first == 0 {
		t.Fatal("the hook never ran at all — the instrument is broken, not the pool")
	}

	// Let every pooled connection age out, then work again: those connects are
	// new physical ones.
	time.Sleep(900 * time.Millisecond)
	for i := 0; i < 3; i++ {
		exercise()
	}

	if got := hooked.Load(); got <= first {
		t.Errorf("hook ran %d times after recycling, %d before — it is firing at Open only, "+
			"not on every physical connect, which is the property this seam exists for",
			got, first)
	} else {
		t.Logf("hook fired %d times (baseline %d) across recycled connections", got, first)
	}
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
