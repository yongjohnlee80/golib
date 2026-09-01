package postgres

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
)

// Server-free regression cells for the PR #20 r0 review findings (MF2, MF3). Both drive
// the handle over a net.Pipe whose peer is not reading, so a write blocks deterministically
// and the window each finding names can be entered on purpose rather than raced for.
// MF1's cell needs a real pool and lives in pinned_integration_test.go.

// tuple reads the handle's state tracks under its lock. It lives in this untagged file
// so both the default and the integration build see it.
func tuple(p *pinnedConn) (outboundState, inboundState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.out, p.in
}

// writingOf reads the write-buffer claim under the handle's lock.
func writingOf(p *pinnedConn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writing
}

// gateWriter signals the instant the frontend's write reaches the socket, then delegates.
// It is what makes "cancel DURING the write" and "Send DURING the write" deterministic:
// without it a test can only race the window and would usually miss it.
type gateWriter struct {
	w       io.Writer
	entered chan struct{}
	once    sync.Once
}

func (g *gateWriter) Write(b []byte) (int, error) {
	g.once.Do(func() { close(g.entered) })
	return g.w.Write(b)
}

// blockedWireHandle builds a pinnedConn whose writes block until the test drains the peer.
// Nothing reads srv, so the first frontend flush parks inside Write.
func blockedWireHandle(t *testing.T) (*pinnedConn, net.Conn, *gateWriter) {
	t.Helper()

	srv, cli := net.Pipe()
	t.Cleanup(func() { _ = srv.Close(); _ = cli.Close() })
	gate := &gateWriter{w: cli, entered: make(chan struct{})}
	p := &pinnedConn{
		frontend: pgproto3.NewFrontend(srv, gate),
		netConn:  cli,
	}
	return p, srv, gate
}

// drainPeer reads whatever the handle writes, so a parked Write can complete.
func drainPeer(t *testing.T, srv net.Conn) {
	t.Helper()

	go func() {
		buf := make([]byte, 4096)
		for {
			_ = srv.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, err := srv.Read(buf); err != nil {
				return
			}
		}
	}()
}

// MF2 (r0): Flush must be bounded by ctx CANCELLATION, not only by ctx.Deadline. A
// cancellable context with no deadline — and a cancellation that lands before a later
// deadline — must both interrupt a write already parked on the socket. Before the fix
// writeBuffered installed a socket deadline only when ctx carried one, so these two
// shapes hung until the peer moved.
func TestPinned_MF2_FlushIsBoundedByCancellationNotOnlyDeadline(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
	}{
		{"cancellable, no deadline", func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		}},
		{"cancelled before a later deadline", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Hour)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p, _, gate := blockedWireHandle(t)
			if err := p.Send(context.Background(), ParseOp("", "SELECT 1", nil)); err != nil {
				t.Fatalf("Send: %v", err)
			}
			ctx, cancel := tc.ctx()
			defer cancel()

			flushErr := make(chan error, 1)
			go func() { flushErr <- p.Flush(ctx) }()

			// Wait until the write is genuinely parked: cancelling earlier would be
			// caught by Flush's pre-dispatch check and prove nothing about the window.
			select {
			case <-gate.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("Flush never reached the socket write")
			}
			cancel()

			select {
			case err := <-flushErr:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("Flush = %v, want context.Canceled (the raw cause, preserved)", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Flush did not observe cancellation while parked in Write: the write is not bounded by ctx")
			}

			// A cancellation mid-write is a transport-level outcome: the handle is
			// poisoned and only Discard remains legal.
			if err := p.Send(context.Background(), ParseOp("", "SELECT 2", nil)); !errors.Is(err, ErrPoisoned) {
				t.Fatalf("Send after a cancelled write = %v, want ErrPoisoned", err)
			}
		})
	}
}

// MF3 (r0): Send and Flush share pgproto3's write buffer, so they must not overlap. Flush
// claims the buffer under mu before it starts writing; a Send arriving while that write
// is parked is refused rather than appending to a wbuf that Write is reading and is about
// to reset. Before the fix the Send was admitted, its frame was dropped by the buffer
// reset, and Flush then overwrote the building state it had just produced.
func TestPinned_MF3_SendDuringFlushIsRefusedNotLost(t *testing.T) {
	t.Parallel()

	p, srv, gate := blockedWireHandle(t)
	if err := p.Send(context.Background(), ParseOp("", "SELECT 1", nil)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	flushErr := make(chan error, 1)
	go func() { flushErr <- p.Flush(context.Background()) }()
	select {
	case <-gate.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("Flush never reached the socket write")
	}

	// The load-bearing moment: Flush owns the buffer and is parked inside Write.
	if err := p.Send(context.Background(), ParseOp("", "SELECT 2", nil)); !errors.Is(err, ErrSegmentInFlight) {
		t.Fatalf("Send during a parked Flush = %v, want ErrSegmentInFlight (its frame would be erased by the buffer reset)", err)
	}
	// Sync writes to the same buffer and must be refused for the same reason.
	if _, err := p.Sync(context.Background()); !errors.Is(err, ErrSegmentInFlight) {
		t.Fatalf("Sync during a parked Flush = %v, want ErrSegmentInFlight", err)
	}
	// A second Flush likewise refuses instead of waiting on the wire.
	if err := p.Flush(context.Background()); !errors.Is(err, ErrSegmentInFlight) {
		t.Fatalf("Flush during a parked Flush = %v, want ErrSegmentInFlight", err)
	}

	drainPeer(t, srv)
	if err := <-flushErr; err != nil {
		t.Fatalf("Flush: %v", err)
	}
	// The state is exactly what the completed Flush produced — no refused Send left a
	// stale building behind it, and nothing was queued that the reset then erased.
	out, in := tuple(p)
	if out != flushed || in != noInbound {
		t.Fatalf("state after Flush = (%d, %d), want (flushed, noInbound)", out, in)
	}
	if writingOf(p) {
		t.Fatal("the write-buffer claim was not released")
	}
	// Control: with no write in flight, Send is admitted again and moves the track —
	// the refusal above is the parked write, not a permanently closed door.
	if err := p.Send(context.Background(), ParseOp("", "SELECT 3", nil)); err != nil {
		t.Fatalf("Send after Flush completed: %v", err)
	}
	if out, _ := tuple(p); out != building {
		t.Fatalf("outbound = %d after the control Send, want building", out)
	}
}

// The Send-during-RECEIVE resume path must stay open: the MF3 claim covers the write
// buffer only, and Receive does not touch it. This is the property the fix could most
// easily have broken.
func TestPinned_MF3_ResumeSendDuringReceiveStillAllowed(t *testing.T) {
	t.Parallel()

	p, _, _ := blockedWireHandle(t)
	// Stage the handle exactly as it stands mid-group: bytes on the wire, messages
	// still arriving.
	p.mu.Lock()
	p.out, p.in = flushed, receiving
	p.mu.Unlock()
	if err := p.Send(context.Background(), ExecuteOp("", 2)); err != nil {
		t.Fatalf("resume Send during receiving = %v, want it admitted", err)
	}
	out, in := tuple(p)
	if out != building || in != receiving {
		t.Fatalf("state = (%d, %d), want (building, receiving) — the inbound track must be preserved", out, in)
	}
}
