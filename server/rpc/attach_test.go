package rpc_test

// WithSessionAttach and WithConnDialer: a transport that is not a plain
// socket can carry facts it proved (which listener, which identity) into the
// gate and handlers, and a client can run over a connection it did not dial.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/server/rpc"
	"github.com/yongjohnlee80/golib/server/rpc/msgpackrpc"
)

// taggedConn is a net.Conn that carries a fact its transport established,
// the way an SSH channel adapter would carry the authenticated key.
type taggedConn struct {
	net.Conn
	tag string
}

// pipeListener hands the server the far end of in-memory pipes: a byte
// stream that is not a socket, so nothing reaches the server except through
// the listener, and the client must be given its connection.
type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
	n      atomic.Int64
}

func newPipeListener() *pipeListener {
	return &pipeListener{conns: make(chan net.Conn), closed: make(chan struct{})}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (l *pipeListener) Addr() net.Addr { return pipeAddr{} }

// dial returns the client end of a new pipe whose server end is accepted
// as a taggedConn named "conn-<n>".
func (l *pipeListener) dial(ctx context.Context) (net.Conn, error) {
	client, server := net.Pipe()
	tagged := &taggedConn{Conn: server, tag: fmt.Sprintf("conn-%d", l.n.Add(1))}
	select {
	case l.conns <- tagged:
		return client, nil
	case <-ctx.Done():
		client.Close()
		server.Close()
		return nil, ctx.Err()
	case <-l.closed:
		client.Close()
		server.Close()
		return nil, net.ErrClosed
	}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

func tagOf(nc net.Conn) any {
	if tc, ok := nc.(*taggedConn); ok {
		return tc.tag
	}
	return nil
}

func pipeClient(t *testing.T, l *pipeListener) *rpc.Client {
	t.Helper()
	return dialClient(t, "ignored", rpc.WithConnDialer(
		func(ctx context.Context, _, _ string) (net.Conn, error) { return l.dial(ctx) }))
}

// The attachment is visible to the gate on the connection's very first
// request, and to handlers, and each connection sees its own.
func TestSessionAttachReachesGateAndHandler(t *testing.T) {
	t.Parallel()
	l := newPipeListener()
	var mu sync.Mutex
	gateSaw := map[string]any{} // method -> attachment, first request only
	gate := func(sess *rpc.Session, method string) error {
		mu.Lock()
		defer mu.Unlock()
		if _, seen := gateSaw[method]; !seen {
			gateSaw[method] = sess.Attachment()
		}
		if sess.Attachment() == nil {
			return &rpc.Error{Code: rpc.CodeAccessDenied, Message: "untagged"}
		}
		return nil
	}
	s := rpc.New(msgpackrpc.New(nil), rpc.WithListener(l), rpc.WithGate(gate),
		rpc.WithSessionAttach(tagOf))
	s.Handle("whoami", func(_ context.Context, req *rpc.Request) (any, error) {
		return req.Session.Attachment(), nil
	})
	s.Handle("first", func(_ context.Context, req *rpc.Request) (any, error) {
		return req.Session.Attachment(), nil
	})
	startServer(t, s)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c1 := pipeClient(t, l)
	got, err := c1.Call(ctx, "first")
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	mu.Lock()
	sawFirst := gateSaw["first"]
	mu.Unlock()
	if sawFirst != "conn-1" || got != "conn-1" {
		t.Fatalf("first request: gate saw %#v, handler returned %#v; want conn-1 for both", sawFirst, got)
	}

	c2 := pipeClient(t, l)
	if got, err := c2.Call(ctx, "whoami"); err != nil || got != "conn-2" {
		t.Fatalf("second connection: %#v, %v; want conn-2", got, err)
	}
	// The first connection still sees its own, after the second arrived.
	if got, err := c1.Call(ctx, "whoami"); err != nil || got != "conn-1" {
		t.Fatalf("first connection after the second: %#v, %v; want conn-1", got, err)
	}
}

// Without WithSessionAttach every attachment is nil, and a handler writing
// Session values cannot make one appear.
func TestSessionAttachDefaultNilAndNotWritableByValues(t *testing.T) {
	t.Parallel()
	s := newEchoServer(t)
	s.Handle("forge", func(_ context.Context, req *rpc.Request) (any, error) {
		req.Session.SetValue("attachment", "forged")
		return req.Session.Attachment(), nil
	})
	s.Handle("read", func(_ context.Context, req *rpc.Request) (any, error) {
		return req.Session.Attachment(), nil
	})
	startServer(t, s)
	c := dialClient(t, s.Addr())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, m := range []string{"forge", "read"} {
		if got, err := c.Call(ctx, m); err != nil || got != nil {
			t.Fatalf("%s: %#v, %v; want nil attachment", m, got, err)
		}
	}
}

// Concurrent handlers on one connection read the attachment without a data
// race (run with -race) and all see the same value.
func TestSessionAttachConcurrentReads(t *testing.T) {
	t.Parallel()
	l := newPipeListener()
	s := rpc.New(msgpackrpc.New(nil), rpc.WithListener(l), rpc.WithSessionAttach(tagOf))
	s.Handle("whoami", func(_ context.Context, req *rpc.Request) (any, error) {
		return req.Session.Attachment(), nil
	})
	startServer(t, s)
	c := pipeClient(t, l)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errc := make(chan error, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := c.Call(ctx, "whoami")
			if err == nil && got != "conn-1" {
				err = fmt.Errorf("got %#v, want conn-1", got)
			}
			if err != nil {
				errc <- err
			}
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
}

// WithConnDialer receives Dial's ctx, network and addr, and the client runs
// over what it returns.
func TestClientConnDialerReceivesArgsAndCarriesCalls(t *testing.T) {
	t.Parallel()
	s := newEchoServer(t)
	startServer(t, s)
	type key struct{}
	var gotNet, gotAddr string
	var gotCtxVal any
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), key{}, "v"), 5*time.Second)
	defer cancel()
	c, err := rpc.Dial(ctx, s.Addr(), msgpackrpc.New(nil),
		rpc.ClientNetwork("tcp4"),
		rpc.WithConnDialer(func(ctx context.Context, network, addr string) (net.Conn, error) {
			gotNet, gotAddr, gotCtxVal = network, addr, ctx.Value(key{})
			var d net.Dialer
			return d.DialContext(ctx, "tcp", addr)
		}))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if gotNet != "tcp4" || gotAddr != s.Addr() || gotCtxVal != "v" {
		t.Fatalf("dialer got network %q addr %q ctx value %#v", gotNet, gotAddr, gotCtxVal)
	}
	res, err := c.Call(ctx, "echo", "hi")
	if arr, ok := res.([]any); err != nil || !ok || len(arr) != 1 || arr[0] != "hi" {
		t.Fatalf("echo over dialed conn: %#v, %v", res, err)
	}
}

// WithConnDialer takes precedence over WithDialer, in either option order.
func TestClientConnDialerTakesPrecedence(t *testing.T) {
	t.Parallel()
	l := newPipeListener()
	s := rpc.New(msgpackrpc.New(nil), rpc.WithListener(l))
	s.Handle("ping", func(context.Context, *rpc.Request) (any, error) { return "pong", nil })
	startServer(t, s)
	// A net.Dialer that could only fail: an address nothing listens on, with
	// a deadline already past.
	bad := &net.Dialer{Deadline: time.Unix(1, 0)}
	conn := rpc.WithConnDialer(func(ctx context.Context, _, _ string) (net.Conn, error) { return l.dial(ctx) })
	for _, opts := range [][]rpc.ClientOption{
		{rpc.WithDialer(bad), conn},
		{conn, rpc.WithDialer(bad)},
	} {
		c := dialClient(t, "127.0.0.1:1", opts...)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		got, err := c.Call(ctx, "ping")
		cancel()
		if err != nil || got != "pong" {
			t.Fatalf("call: %#v, %v; want pong over the conn dialer", got, err)
		}
	}
}

// A dialer error is returned as is; a nil connection with a nil error is an
// error, never a nil dereference.
func TestClientConnDialerFailures(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	boom := errors.New("tunnel refused")
	if _, err := rpc.Dial(ctx, "x", msgpackrpc.New(nil), rpc.WithConnDialer(
		func(context.Context, string, string) (net.Conn, error) { return nil, boom })); !errors.Is(err, boom) {
		t.Fatalf("dialer error: %v, want %v", err, boom)
	}
	c, err := rpc.Dial(ctx, "x", msgpackrpc.New(nil), rpc.WithConnDialer(
		func(context.Context, string, string) (net.Conn, error) { return nil, nil }))
	if err == nil || c != nil {
		t.Fatalf("nil conn, nil error: client %v, err %v; want an error", c, err)
	}
}
