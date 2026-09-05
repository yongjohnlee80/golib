package server

import (
	"context"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The drain-visibility contract (ADR-0006 §2.2).
//
// A connection is claimed with Registry.Reserve BEFORE any establishment work,
// so a drain that begins mid-establishment waits for it. Registering only after
// the session exists left the connection invisible to Drain for the whole of
// session setup — and that window contains the CALLER-SUPPLIED session factory,
// so a consumer widened it without knowing it existed.
//
// The defect these cells lock was silent, which is why they assert on the error
// as well as on the wait: Run returned nil — a clean graceful shutdown — in
// ~40µs while a connection sat unserved in the factory. Raising DrainTimeout
// did nothing, because the drain was not racing its budget; it saw an empty
// registry and correctly concluded there was nothing to wait for.

// scaffoldInFactory starts a scaffold whose session factory blocks until the
// returned release func is called, and returns once a dialled connection is
// inside that factory. Every cell below needs that exact state.
func scaffoldInFactory(t *testing.T, opts ...ScaffoldOption) (
	s *Scaffold, runErr <-chan error, cancel func(), handlerRan <-chan struct{}, release func(),
) {
	t.Helper()
	entered := make(chan struct{})
	releaseCh := make(chan struct{})
	ran := make(chan struct{})

	base := []ScaffoldOption{
		ScaffoldAddr("127.0.0.1:0"),
		ScaffoldSessionFactory(func(ctx context.Context, conn net.Conn) Session {
			close(entered)
			<-releaseCh
			return nil
		}),
	}
	s = NewScaffold(func(ctx context.Context, conn net.Conn) {
		close(ran)
		_ = conn.Close()
	}, append(base, opts...)...)

	ctx, cancelFn := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- s.Run(ctx) }()

	deadline := time.After(2 * time.Second)
	for s.Addr() == "" {
		select {
		case <-deadline:
			t.Fatal("scaffold never bound")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	c, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("connection never reached the session factory")
	}
	var once bool
	return s, errc, cancelFn, ran, func() {
		if !once {
			once = true
			close(releaseCh)
		}
	}
}

// The drain must WAIT for a connection still inside the session factory.
// Before the fix this returned in ~40µs with a nil error.
func TestScaffold_DrainWaitsForConnStillInSessionFactory(t *testing.T) {
	t.Parallel()

	_, errc, cancel, handlerRan, release := scaffoldInFactory(t, DrainTimeout(10*time.Second))
	cancel() // drain begins while the connection is mid-establishment

	select {
	case err := <-errc:
		t.Fatalf("Run returned while the connection was still in the session factory (err=%v) — "+
			"the drain cannot see accepted-but-unestablished connections", err)
	case <-time.After(150 * time.Millisecond):
		// Correct: still draining.
	}

	release()
	select {
	case err := <-errc:
		if err != nil {
			t.Errorf("Run: %v, want nil once establishment completed inside the drain budget", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run never returned after the factory was released")
	}
	select {
	case <-handlerRan:
	default:
		t.Error("handler never ran, so the drain did not actually wait for establishment")
	}
}

// The other half of the same defect: when establishment outruns the drain
// budget, the failure must be REPORTED. The old code returned nil because the
// connection was in neither live nor reserved; Drain's deadline error counts
// len(live)+reserved, so a reservation makes it visible to the report too.
func TestScaffold_DrainDeadlineReportsUnfinishedEstablishment(t *testing.T) {
	t.Parallel()

	_, errc, cancel, _, release := scaffoldInFactory(t, DrainTimeout(100*time.Millisecond))
	defer release()
	cancel()

	select {
	case err := <-errc:
		if err == nil {
			t.Fatal("Run returned nil after abandoning a connection still in establishment — " +
				"a drain that gives up must not report a clean shutdown")
		}
		if !strings.Contains(err.Error(), "drain deadline") {
			t.Errorf("err = %v, want a drain-deadline report naming the force-closed count", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run never returned; the drain budget should have expired")
	}
}

// A connection accepted after the drain began is REFUSED, not served: Reserve
// returns ok=false once draining, and the accept loop must close the connection
// rather than hand it to a handler. Staged with an injected listener, because a
// real dial cannot win that race — the listener is already closed by then.
func TestScaffold_ConnAcceptedAfterDrainBeganIsRefused(t *testing.T) {
	t.Parallel()

	served := make(chan struct{})
	client, peer := net.Pipe()
	defer client.Close()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := NewScaffold(func(ctx context.Context, conn net.Conn) {
		close(served)
	}, WithListener(&onceConnListener{Listener: ln, conn: peer}), DrainTimeout(time.Second))

	// Drain the registry BEFORE the accept loop hands over the connection, so
	// Reserve is guaranteed to refuse.
	if err := s.reg.Drain(context.Background()); err != nil {
		t.Fatalf("pre-drain: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- s.Run(ctx) }()

	// The refusal closes the connection, so the peer read must fail rather than
	// block. This is the observable proof it was refused and not served.
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Error("connection was not closed: it was served rather than refused")
	}
	select {
	case <-served:
		t.Error("handler ran for a connection accepted after the drain began")
	default:
	}
	cancel()
	<-errc
}

// onceConnListener yields one pre-made conn, then blocks until closed. It lets a
// test drive the accept loop without a real dial.
type onceConnListener struct {
	net.Listener
	conn net.Conn
	done atomic.Bool
}

func (l *onceConnListener) Accept() (net.Conn, error) {
	if !l.done.Swap(true) {
		return l.conn, nil
	}
	return l.Listener.Accept() // blocks until Close, then returns ErrClosed
}

// The Cancel path. A panic during establishment must RELEASE the reserved slot;
// otherwise the drain waits out its whole budget for a session that will never
// arrive, and the fix for an abandoned connection becomes a hang. This is the
// cell that makes "Complete it or Cancel it on every path" a checked claim
// rather than a comment.
func TestScaffold_PanicDuringEstablishmentReleasesTheSlot(t *testing.T) {
	t.Parallel()

	lg := &countLogger{}
	s := NewScaffold(func(ctx context.Context, conn net.Conn) {},
		ScaffoldAddr("127.0.0.1:0"),
		ScaffoldLogger(lg),
		DrainTimeout(5*time.Second), // generous: a leaked slot shows as a hang, not a pass
		ScaffoldSessionFactory(func(ctx context.Context, conn net.Conn) Session {
			panic("factory kaboom")
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- s.Run(ctx) }()
	deadline := time.After(2 * time.Second)
	for s.Addr() == "" {
		select {
		case <-deadline:
			t.Fatal("scaffold never bound")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	c, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// Wait for the panic to be logged, so the connection is provably past the
	// factory before the drain starts.
	for i := 0; lg.errors.Load() == 0; i++ {
		if i > 2000 {
			t.Fatal("factory panic was never logged")
		}
		time.Sleep(time.Millisecond)
	}

	start := time.Now()
	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Errorf("Run: %v, want nil — the slot was released, so there was nothing to wait for", err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Errorf("drain took %v: the reserved slot leaked and the drain waited out its budget", elapsed)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Run never returned: a panic during establishment leaked its reservation")
	}
}
