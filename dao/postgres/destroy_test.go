package postgres

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"

	"github.com/yongjohnlee80/golib/dao"
)

// EXPLICIT DESTRUCTION MUST NOT BE OVERRULED BY A WIRE THAT LOOKS FINE.
//
// The consumer that asks for it is the one holding the fact the driver cannot see: the
// SESSION on this backend is unfit, because a reset statement was refused or could not be
// proved. The wire itself is in perfect order in exactly that case — every frame went out
// and came back, nothing is queued, nothing is poisoned — so the reuse predicate says
// "recycle this" and would be right about the only thing it can measure. These cells pin
// that the predicate does not get the last word.
//
// The cells run server-free, over a net.Pipe, because the property is about the DECISION
// and not about PostgreSQL. What they need from a real pool is only that a lease exists
// at all — both teardown paths return immediately when it is gone — so they hold a
// recording lease and watch what the teardown does to it and to the socket.

// countingLease is a pool lease that records how many times it was given back. Once is
// the contract: the goroutine that nils the handle's lease owns the release, so a
// teardown that relinquished twice would be returning a member the pool had already
// handed to somebody else.
type countingLease struct {
	mu sync.Mutex
	n  int
}

func (l *countingLease) Release() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.n++
}

func (l *countingLease) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.n
}

// recordingConn is the handle's socket with the two teardown actions made visible: the
// past deadline that interrupts an in-flight read or write, and the close that destroys
// the connection. Both are what "destroyed" MEANS here, so both are counted rather than
// inferred from a later symptom.
type recordingConn struct {
	net.Conn
	mu          sync.Mutex
	interrupted bool
	closes      int
}

func (c *recordingConn) SetDeadline(t time.Time) error {
	if !t.IsZero() && !t.After(time.Now()) {
		c.mu.Lock()
		c.interrupted = true
		c.mu.Unlock()
	}
	return c.Conn.SetDeadline(t)
}

func (c *recordingConn) Close() error {
	c.mu.Lock()
	c.closes++
	c.mu.Unlock()
	return c.Conn.Close()
}

func (c *recordingConn) state() (interrupted bool, closes int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.interrupted, c.closes
}

// leasedHandle builds a quiescent, healthy, fully reusable pinned handle that holds a
// lease and a socket both cells can read back. Nothing is poisoned, nothing is queued and
// no transaction is open, which is the state the reuse predicate accepts — so anything a
// teardown destroys here, it destroyed on purpose.
func leasedHandle(t *testing.T) (*pinnedConn, *countingLease, *recordingConn) {
	t.Helper()

	srv, cli := net.Pipe()
	rec := &recordingConn{Conn: cli}
	t.Cleanup(func() { _ = srv.Close(); _ = cli.Close() })
	lease := &countingLease{}
	p := &pinnedConn{
		frontend: pgproto3.NewFrontend(srv, rec),
		netConn:  rec,
		acq:      lease,
	}
	p.mu.Lock()
	reusable := p.reusableLocked()
	p.mu.Unlock()
	if !reusable {
		t.Fatal("the fixture handle is not reusable, so nothing it refuses to recycle " +
			"proves anything about explicit destruction")
	}
	return p, lease, rec
}

// The load-bearing cell. The SAME handle, in the SAME reusable state, must be recycled by
// Discard and destroyed by Destroy — which is the whole difference between the two calls
// and the reason the second one exists.
//
// Mutating Destroy into Discard, or into a plain Release, reddens the destroy row: the
// socket stays open and the interrupt never happens. Mutating Discard into Destroy
// reddens the discard row for the mirror-image reason, so neither call can quietly become
// the other.
func TestPinnedConn_DestroyOutranksAReusableWireAndDiscardDoesNot(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		teardown      func(*pinnedConn)
		wantClosed    bool
		wantInterrupt bool
	}{
		{"Discard recycles a wire it can prove clean", (*pinnedConn).Discard, false, false},
		{"Destroy destroys that same wire anyway", func(p *pinnedConn) { p.Destroy() }, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p, lease, rec := leasedHandle(t)
			tc.teardown(p)

			if got := lease.count(); got != 1 {
				t.Fatalf("the lease was relinquished %d time(s), want exactly 1: the pool "+
					"member must always go back, however it ends", got)
			}
			interrupted, closes := rec.state()
			switch {
			case tc.wantClosed && closes == 0:
				t.Fatal("the socket is still open, so the pool can hand this backend to the " +
					"next caller — the destruction reached the driver object and stopped there")
			case !tc.wantClosed && closes != 0:
				t.Fatal("a wire that could be proved clean was destroyed anyway, which costs " +
					"a reconnect on every healthy release")
			}
			if interrupted != tc.wantInterrupt {
				t.Fatalf("in-flight I/O interrupted = %v, want %v", interrupted, tc.wantInterrupt)
			}
			// Terminal either way: the handle is finished and says so.
			if err := p.Send(context.Background(), ParseOp("", "SELECT 1", nil)); !errors.Is(err, ErrPoisoned) {
				t.Fatalf("Send after teardown = %v, want ErrPoisoned", err)
			}
		})
	}
}

// Destruction demanded while a read is PARKED ON THE SOCKET must still happen, and must
// not close the connection out from under that read.
//
// This is the case the ordering exists for. Destroy poisons first so no new operation
// starts, shortens the deadline so the parked read returns, and only then barriers on the
// wire lock — so by the time the socket is closed the reader has let go of it. Closing
// first would be a use-after-close on somebody else's goroutine, and the cell would hang
// or race rather than fail cleanly, which is why the read is parked deterministically
// instead of being timed.
func TestPinnedConn_DestroyInterruptsAParkedReceive(t *testing.T) {
	t.Parallel()

	p, lease, rec := leasedHandle(t)
	parked := make(chan struct{})
	var once sync.Once
	p.recv = func(context.Context) (pgproto3.BackendMessage, error) {
		once.Do(func() { close(parked) })
		buf := make([]byte, 1)
		if _, err := rec.Read(buf); err != nil {
			return nil, err
		}
		return &pgproto3.NoData{}, nil
	}
	// Stage the handle mid-group: bytes are on the wire and the answer has not arrived,
	// which is the only state Receive is legal from.
	p.mu.Lock()
	p.out = flushed
	p.mu.Unlock()

	recvErr := make(chan error, 1)
	go func() {
		_, err := p.Receive(context.Background())
		recvErr <- err
	}()
	select {
	case <-parked:
	case <-time.After(5 * time.Second):
		t.Fatal("Receive never reached the socket read, so the destruction below would not " +
			"be racing anything")
	}

	done := make(chan struct{})
	go func() { p.Destroy(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Destroy did not complete while a read was parked: either the read was " +
			"never interrupted or the barrier is waiting on the goroutine it interrupted")
	}
	select {
	case err := <-recvErr:
		if err == nil {
			t.Fatal("the parked Receive returned a message although the connection was destroyed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the parked Receive is still blocked after Destroy returned")
	}

	if got := lease.count(); got != 1 {
		t.Fatalf("the lease was relinquished %d time(s), want exactly 1", got)
	}
	if _, closes := rec.state(); closes == 0 {
		t.Fatal("the socket survived a destruction demanded during a parked read")
	}
	if _, err := p.Sync(context.Background()); !errors.Is(err, ErrPoisoned) {
		t.Fatalf("Sync after Destroy = %v, want ErrPoisoned", err)
	}
}

// Destroy is idempotent, and a Destroy after a Discard is too. Both matter because the
// deferred teardown a consumer writes at pin time still runs after an explicit
// destruction on the error path — that second call must be a no-op, not a second release
// of a member the pool has since given away.
func TestPinnedConn_DestroyIsIdempotentAfterItselfAndAfterDiscard(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		first func(*pinnedConn)
	}{
		{"after Destroy", func(p *pinnedConn) { p.Destroy() }},
		{"after Discard", (*pinnedConn).Discard},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p, lease, rec := leasedHandle(t)
			tc.first(p)
			closesAfterFirst := func() int { _, n := rec.state(); return n }()
			p.Destroy()
			p.Destroy()

			if got := lease.count(); got != 1 {
				t.Fatalf("the lease was relinquished %d time(s) across three teardowns, "+
					"want exactly 1", got)
			}
			if _, closes := rec.state(); closes != closesAfterFirst {
				t.Fatalf("the socket was closed %d time(s) after the first teardown and %d "+
					"time(s) after the repeats; a repeat must touch nothing", closesAfterFirst, closes)
			}
		})
	}
}

// A Destroy after a SUCCESSFUL Release must do nothing at all.
//
// Release already handed the member back, so the pool may have given it to another
// goroutine; closing the socket now would destroy a stranger's connection and the symptom
// would appear in code that never called any of this. The guarantee Destroy offers is
// therefore bounded by the lease, and a consumer that may need to destroy a backend
// decides before it releases — which is the order a release gate runs in anyway.
func TestPinnedConn_DestroyAfterReleaseTouchesNothing(t *testing.T) {
	t.Parallel()

	p, lease, rec := leasedHandle(t)
	if err := p.Release(context.Background()); err != nil {
		t.Fatalf("Release from a healthy quiescent handle = %v, want nil", err)
	}
	p.Destroy()

	if got := lease.count(); got != 1 {
		t.Fatalf("the lease was relinquished %d time(s), want exactly 1 — the Release's", got)
	}
	if interrupted, closes := rec.state(); interrupted || closes != 0 {
		t.Fatalf("Destroy touched a connection the pool already owns (interrupted=%v, "+
			"closes=%d)", interrupted, closes)
	}
}

// The probe is what a consumer actually calls, and it must answer honestly for a handle
// that does not carry the capability — a consumer on an older build of this package gets
// false and owes its own weaker teardown, never a silent nothing.
func TestDestroy_ProbeAnswersForBothShapes(t *testing.T) {
	t.Parallel()

	p, lease, _ := leasedHandle(t)
	if !Destroy(p) {
		t.Fatal("the pinned handle did not answer the destruction probe")
	}
	if got := lease.count(); got != 1 {
		t.Fatalf("the probe reported a destruction that relinquished %d lease(s), want 1", got)
	}
	if Destroy(externalPinnedNoDestroy{}) {
		t.Fatal("a PinnedConn with no Destroy answered the probe, so a consumer would " +
			"believe a destruction happened that nothing performed")
	}
}

// externalPinnedNoDestroy is the shape every consumer's own fake has: PinnedConn and
// nothing else.
type externalPinnedNoDestroy struct{}

func (externalPinnedNoDestroy) Send(context.Context, ExtendedOp) error { return nil }
func (externalPinnedNoDestroy) Flush(context.Context) error            { return nil }
func (externalPinnedNoDestroy) Receive(context.Context) (ExtendedMessage, error) {
	return ExtendedMessage{}, nil
}
func (externalPinnedNoDestroy) Sync(context.Context) (byte, error) { return 'I', nil }
func (externalPinnedNoDestroy) BeginSessionTx(context.Context, dao.TxOptions) (dao.ContextTxConn, error) {
	return nil, nil
}
func (externalPinnedNoDestroy) Release(context.Context) error { return nil }
func (externalPinnedNoDestroy) Discard()                      {}
