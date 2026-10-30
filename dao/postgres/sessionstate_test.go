package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgproto3"
)

// TestPinnedConn_ASessionStateCommandIsNotRecycled: a member on which a command left session state
// (a SET, a PREPARE, a LISTEN, ...) is destroyed at Release or Discard, not handed to the pool's
// next borrower: either still gives the lease back once, and closes the socket; Release still
// succeeds. A member whose commands left none is recycled as before, the socket open.
func TestPinnedConn_ASessionStateCommandIsNotRecycled(t *testing.T) {
	t.Parallel()
	for tag, destroyed := range map[string]bool{
		"SET": true, "RESET": true, "DISCARD ALL": true, "PREPARE": true, "DEALLOCATE ALL": true,
		"LISTEN": true, "UNLISTEN": true, "DECLARE CURSOR": true,
		"SELECT 1": false, "INSERT 0 1": false, "SAVEPOINT": false, "COMMIT": false, "SETUP": false,
	} {
		for _, release := range []bool{true, false} {
			name := tag + "/Discard"
			if release {
				name = tag + "/Release"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				p, lease, rec := leasedHandle(t)
				p.recv = func(context.Context) (pgproto3.BackendMessage, error) {
					return &pgproto3.CommandComplete{CommandTag: []byte(tag)}, nil
				}
				if _, err := p.readMessage(context.Background()); err != nil {
					t.Fatal(err)
				}
				if release {
					if err := p.Release(context.Background()); err != nil {
						t.Fatalf("Release: %v", err)
					}
				} else {
					p.Discard()
				}
				if got := lease.count(); got != 1 {
					t.Fatalf("the lease was given back %d time(s), want 1", got)
				}
				if _, closes := rec.state(); (closes > 0) != destroyed {
					t.Errorf("after %q: socket closed %v, want %v", tag, closes > 0, destroyed)
				}
				want := ErrPoisoned
				if release {
					want = ErrReleased
				}
				if err := p.Send(context.Background(), ParseOp("", "SELECT 1", nil)); !errors.Is(err, want) {
					t.Errorf("Send after the teardown = %v, want %v", err, want)
				}
			})
		}
	}
}
