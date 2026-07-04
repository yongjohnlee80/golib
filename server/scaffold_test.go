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
