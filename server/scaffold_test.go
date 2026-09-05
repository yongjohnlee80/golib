package server

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/logger"
)

// echoHandler serves a trivial line-echo protocol.
func echoHandler(_ context.Context, conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	line, err := r.ReadString('\n')
	if err != nil {
		return
	}
	_, _ = conn.Write([]byte("echo:" + line))
}

// runScaffold starts s.Run on a goroutine and returns a stop func that
// cancels and reports Run's error.
func runScaffold(t *testing.T, s *Scaffold) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- s.Run(ctx) }()
	// Wait for bind.
	deadline := time.After(2 * time.Second)
	for s.Addr() == "" {
		select {
		case <-deadline:
			t.Fatal("scaffold never bound")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	return func() error {
		cancel()
		return <-errc
	}
}

func TestScaffold_EchoOverRealTCP(t *testing.T) {
	t.Parallel()
	s := NewScaffold(echoHandler, ScaffoldAddr("127.0.0.1:0"))
	stop := runScaffold(t, s)

	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hi\n")); err != nil {
		t.Fatal(err)
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || reply != "echo:hi\n" {
		t.Fatalf("reply = %q, err = %v", reply, err)
	}
	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestScaffold_InjectedListenerServesWithoutBinding(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := NewScaffold(echoHandler, WithListener(ln), ScaffoldAddr("should-be-ignored"))
	stop := runScaffold(t, s)
	if s.Addr() != ln.Addr().String() {
		t.Fatalf("Addr = %q, want the injected listener's %q", s.Addr(), ln.Addr())
	}
	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestScaffold_ShutdownCancelsConnCtxAndDrains(t *testing.T) {
	t.Parallel()
	var sawCancel atomic.Bool
	release := make(chan struct{})
	s := NewScaffold(func(ctx context.Context, conn net.Conn) {
		defer conn.Close()
		select {
		case <-ctx.Done():
			sawCancel.Store(true)
		case <-release:
		}
	}, ScaffoldAddr("127.0.0.1:0"), DrainTimeout(2*time.Second))
	stop := runScaffold(t, s)

	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// Wait until the connection is registered.
	deadline := time.After(2 * time.Second)
	for s.Sessions().Len() == 0 {
		select {
		case <-deadline:
			t.Fatal("connection never registered")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !sawCancel.Load() {
		t.Error("per-connection context was not cancelled on shutdown")
	}
	if n := s.Sessions().Len(); n != 0 {
		t.Errorf("registry has %d sessions after clean shutdown", n)
	}
}

func TestScaffold_HandlerPanicIsIsolated(t *testing.T) {
	t.Parallel()
	lg := &countLogger{}
	s := NewScaffold(func(_ context.Context, conn net.Conn) {
		defer conn.Close()
		buf := make([]byte, 1)
		_, _ = conn.Read(buf)
		if buf[0] == 'p' {
			panic("kaboom")
		}
		_, _ = conn.Write([]byte("ok"))
	}, ScaffoldAddr("127.0.0.1:0"), ScaffoldLogger(lg))
	stop := runScaffold(t, s)

	// First connection panics the handler...
	c1, _ := net.Dial("tcp", s.Addr())
	_, _ = c1.Write([]byte("p"))
	c1.Close()

	// ...the server keeps serving.
	c2, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	_, _ = c2.Write([]byte("x"))
	buf := make([]byte, 2)
	if _, err := c2.Read(buf); err != nil || string(buf) != "ok" {
		t.Fatalf("post-panic request failed: %q %v", buf, err)
	}
	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// This read is deliberately a single check after stop(), NOT a bounded poll.
	//
	// It used to rely on accidental synchronization — c2's round-trip is served
	// by a different goroutine and orders nothing against c1's panic — and it
	// failed on loaded CI accordingly. It is sound now because the accept loop
	// RESERVES each connection before its goroutine exists, Drain waits for
	// reservations as well as live sessions, and serveConn logs before it
	// unregisters. So when stop() returns, the log has necessarily happened.
	//
	// Converting this to a poll would make it WEAKER, not safer: it would go
	// green again if that drain-visibility guarantee regressed, which is the
	// defect this ordering now depends on.
	// TestScaffold_DrainWaitsForConnStillInSessionFactory is the cell that
	// proves the load-bearing link, and a mutation reverting the reservation
	// reddens it.
	if lg.errors.Load() == 0 {
		t.Error("handler panic was not logged")
	}
}

// failingListener returns a terminal error after its first accepted conn.
type failingListener struct {
	net.Listener
	failed atomic.Bool
}

func (f *failingListener) Accept() (net.Conn, error) {
	if f.failed.Swap(true) {
		return nil, errors.New("terminal accept failure")
	}
	return f.Listener.Accept()
}

func TestScaffold_TerminalAcceptErrorReturned(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := NewScaffold(echoHandler, WithListener(&failingListener{Listener: ln}),
		DrainTimeout(time.Second))

	errc := make(chan error, 1)
	go func() { errc <- s.Run(context.Background()) }()

	// One conn passes through, then Accept fails terminally.
	conn, derr := net.Dial("tcp", ln.Addr().String())
	if derr == nil {
		conn.Close()
	}
	select {
	case err := <-errc:
		if err == nil || !strings.Contains(err.Error(), "terminal accept failure") {
			t.Fatalf("Run = %v, want the terminal accept error", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return on terminal accept error")
	}
}

// countLogger counts Error-severity records.
type countLogger struct{ errors atomic.Int32 }

func (c *countLogger) Log(s logger.Severity, _ any) {
	if s == logger.SeverityError {
		c.errors.Add(1)
	}
}

// drainRecordingSession records whether the registry ended it politely
// (Drain) or forcefully (bare Close), for the ScaffoldSessionFactory tests.
type drainRecordingSession struct {
	conn    net.Conn
	drained atomic.Bool
	closed  atomic.Bool
}

func (d *drainRecordingSession) Close() error {
	d.closed.Store(true)
	return d.conn.Close()
}

func (d *drainRecordingSession) Drain(context.Context) error {
	d.drained.Store(true)
	return d.conn.Close()
}

func TestScaffold_SessionFactoryReplacesDefaultSession(t *testing.T) {
	t.Parallel()
	var made atomic.Pointer[drainRecordingSession]
	var handlerSaw atomic.Pointer[drainRecordingSession]
	connected := make(chan struct{})

	s := NewScaffold(
		func(ctx context.Context, conn net.Conn) {
			if sess, ok := SessionFromContext(ctx).(*drainRecordingSession); ok {
				handlerSaw.Store(sess)
			}
			close(connected)
			// Hold the connection open until drain closes it.
			_, _ = bufio.NewReader(conn).ReadString('\n')
		},
		ScaffoldAddr("127.0.0.1:0"),
		ScaffoldSessionFactory(func(_ context.Context, conn net.Conn) Session {
			d := &drainRecordingSession{conn: conn}
			made.Store(d)
			return d
		}),
	)
	stop := runScaffold(t, s)

	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	<-connected

	if s.Sessions().Len() != 1 {
		t.Fatalf("registry Len = %d, want 1", s.Sessions().Len())
	}
	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	d := made.Load()
	if d == nil {
		t.Fatal("session factory never invoked")
	}
	if handlerSaw.Load() != d {
		t.Fatal("SessionFromContext did not return the factory's session")
	}
	if !d.drained.Load() {
		t.Fatal("drain used bare Close, not the session's Drain")
	}
}

func TestScaffold_SessionFactoryNilResultFallsBack(t *testing.T) {
	t.Parallel()
	sawDefault := make(chan bool, 1)
	s := NewScaffold(
		func(ctx context.Context, conn net.Conn) {
			_, ok := SessionFromContext(ctx).(connSession)
			sawDefault <- ok
			echoHandler(ctx, conn)
		},
		ScaffoldAddr("127.0.0.1:0"),
		ScaffoldSessionFactory(func(context.Context, net.Conn) Session { return nil }),
	)
	stop := runScaffold(t, s)

	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hi\n")); err != nil {
		t.Fatal(err)
	}
	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || reply != "echo:hi\n" {
		t.Fatalf("reply = %q, err = %v", reply, err)
	}
	if !<-sawDefault {
		t.Fatal("nil factory result did not fall back to the default session")
	}
	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// Review finding 6 (2026-08-16): a session-factory panic must be isolated
// like a handler panic — recovered, logged, connection closed — never
// allowed to escape the per-connection goroutine and kill the process.
func TestScaffold_SessionFactoryPanicIsIsolated(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	s := NewScaffold(echoHandler,
		ScaffoldAddr("127.0.0.1:0"),
		ScaffoldSessionFactory(func(context.Context, net.Conn) Session {
			if calls.Add(1) == 1 {
				panic("factory blew up")
			}
			return nil // second conn: default session
		}),
	)
	stop := runScaffold(t, s)

	// First connection triggers the factory panic; the process must survive
	// and the conn must be closed by the scaffold.
	c1, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	_ = c1.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := bufio.NewReader(c1).ReadByte(); err == nil {
		t.Fatal("expected first connection to be closed after factory panic")
	}

	// The accept loop must still serve subsequent connections.
	c2, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	if _, err := c2.Write([]byte("hi\n")); err != nil {
		t.Fatal(err)
	}
	reply, err := bufio.NewReader(c2).ReadString('\n')
	if err != nil || reply != "echo:hi\n" {
		t.Fatalf("post-panic conn: reply = %q, err = %v", reply, err)
	}
	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// slowLogger models any real sink: a file, a socket, a syslog daemon. Writing a
// record takes time, and that time is what exposes an ordering bug the fast
// in-memory sink of the test above can only expose intermittently.
type slowLogger struct {
	delay time.Duration
	n     atomic.Int32
}

func (l *slowLogger) Log(s logger.Severity, _ any) {
	if s != logger.SeverityError {
		return
	}
	time.Sleep(l.delay)
	l.n.Add(1)
}

// A graceful shutdown must not be able to COMPLETE before a panicking
// connection's record is written.
//
// Run returning is normally followed by the process exiting, so a drain that
// finishes first loses the only account of why the connection died — and loses it
// precisely when an operator is looking for it. serveConn's recovery defer
// therefore logs before it unregisters, since the drain is what waits on the
// registry.
//
// The slow sink is what makes this deterministic rather than a race: with the
// unregister first, stop() returns while the record is still being written.
func TestScaffold_PanicIsLoggedBeforeShutdownCompletes(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	lg := &slowLogger{delay: 150 * time.Millisecond}
	s := NewScaffold(func(_ context.Context, conn net.Conn) {
		defer conn.Close()
		close(entered)
		panic("kaboom")
	}, ScaffoldAddr("127.0.0.1:0"), ScaffoldLogger(lg))
	stop := runScaffold(t, s)

	c, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	<-entered // the handler is inside, so the session is registered

	if err := stop(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if lg.n.Load() == 0 {
		t.Error("the drain completed before the panic record was written — after Run " +
			"returns the process usually exits, so the record is simply lost")
	}
}
