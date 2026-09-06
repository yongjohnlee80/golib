//go:build integration

package postgres

import (
	"context"
	"net/url"
	"os"
	"runtime"
	"testing"
	"time"
)

// recorderCount counts live registry entries.
func recorderCount() int {
	n := 0
	recorders.Range(func(_, _ any) bool { n++; return true })
	return n
}

// A connect that FAILS after the frontend is built — wrong
// password — must not leave a recorder behind. pgconn builds the frontend
// before authentication, and no pool hook fires for a connection that never
// became a pool resource, so the registry must not depend on one.
func TestParamStatus_Live_FailedConnectLeavesNoRecorder(t *testing.T) {
	u, err := url.Parse(os.Getenv("TEST_PGURL"))
	if err != nil || u.Host == "" {
		t.Skip("TEST_PGURL not set")
	}
	u.User = url.UserPassword(u.User.Username(), "definitely-the-wrong-password")
	base := recorderCount()
	for i := 0; i < 5; i++ {
		conn, err := Open(context.Background(), u.String())
		if err != nil {
			continue // a pool may refuse at Open; either way no connection survives
		}
		if _, qerr := conn.QueryContext(context.Background(), "SELECT 1"); qerr == nil {
			t.Fatal("a wrong-password connect succeeded; the cell observes nothing")
		}
		_ = conn.Close()
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if recorderCount() <= base {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("recorders grew %d → %d after five failed connects and GC; frontends of failed connects leak", base, recorderCount())
}
