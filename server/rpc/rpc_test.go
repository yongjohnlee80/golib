package rpc_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/msgpack"
	"github.com/yongjohnlee80/golib/server/rpc"
	"github.com/yongjohnlee80/golib/server/rpc/msgpackrpc"
)

// testClient speaks raw msgpack-RPC over a TCP conn, independent of the
// server's read/write machinery.
type testClient struct {
	t    *testing.T
	conn net.Conn
	br   *bufio.Reader
}

func dial(t *testing.T, addr string) *testClient {
	t.Helper()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return &testClient{t: t, conn: conn, br: bufio.NewReader(conn)}
}

func (c *testClient) sendRaw(b []byte) {
	c.t.Helper()
	if _, err := c.conn.Write(b); err != nil {
		c.t.Fatalf("client write: %v", err)
	}
}

func (c *testClient) send(v any) {
	c.t.Helper()
	b, err := msgpack.Marshal(v)
	if err != nil {
		c.t.Fatalf("client marshal: %v", err)
	}
	c.sendRaw(b)
}

func (c *testClient) request(id int64, method string, params ...any) {
	if params == nil {
		params = []any{}
	}
	c.send([]any{int64(0), id, method, params})
}

func (c *testClient) notify(method string, params ...any) {
	if params == nil {
		params = []any{}
	}
	c.send([]any{int64(2), method, params})
}

// recvResponse reads one message and asserts the response shape, returning
// (msgid, error-value, result).
func (c *testClient) recvResponse() (int64, any, any) {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	v, err := msgpack.Decode(c.br, nil)
	if err != nil {
		c.t.Fatalf("client decode: %v", err)
	}
	arr, ok := v.([]any)
	if !ok || len(arr) != 4 {
		c.t.Fatalf("response shape: %#v", v)
	}
	if tag, _ := arr[0].(int64); tag != 1 {
		c.t.Fatalf("response tag = %v", arr[0])
	}
	id, _ := arr[1].(int64)
	return id, arr[2], arr[3]
}

// expectClosed asserts the server hung up on us.
func (c *testClient) expectClosed() {
	c.t.Helper()
	_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.br.ReadByte(); err == nil {
		c.t.Fatal("expected connection close, got bytes")
	}
}

func errCode(t *testing.T, errVal any) int64 {
	t.Helper()
	m, ok := errVal.(map[string]any)
	if !ok {
		t.Fatalf("wire error shape: %#v", errVal)
	}
	code, ok := m["code"].(int64)
	if !ok {
		t.Fatalf("wire error code: %#v", m["code"])
	}
	return code
}

// startServer runs s and returns a stop func reporting Run's error.
func startServer(t *testing.T, s *rpc.Server) (stop func() error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- s.Run(ctx) }()
	deadline := time.After(2 * time.Second)
	for s.Addr() == "" {
		select {
		case <-deadline:
			t.Fatal("server never bound")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	stopped := false
	stop = func() error {
		if stopped {
			return nil
		}
		stopped = true
		cancel()
		return <-errc
	}
	t.Cleanup(func() { _ = stop() })
	return stop
}

func newEchoServer(t *testing.T, opts ...rpc.Option) *rpc.Server {
	t.Helper()
	opts = append([]rpc.Option{rpc.Addr("127.0.0.1:0")}, opts...)
	s := rpc.New(msgpackrpc.New(nil), opts...)
	s.Handle("echo", func(_ context.Context, req *rpc.Request) (any, error) {
		return req.Params, nil
	})
	return s
}

func TestRPC_RequestResponseOverRealTCP(t *testing.T) {
	t.Parallel()
	s := newEchoServer(t)
	startServer(t, s)
	c := dial(t, s.Addr())

	c.request(7, "echo", "hello", int64(42))
	id, errVal, result := c.recvResponse()
	if id != 7 || errVal != nil {
		t.Fatalf("id = %d, err = %#v", id, errVal)
	}
	arr, ok := result.([]any)
	if !ok || len(arr) != 2 || arr[0] != "hello" || arr[1] != int64(42) {
		t.Fatalf("result = %#v", result)
	}
}

func TestRPC_ConcurrentRequestsMsgidFidelity(t *testing.T) {
	t.Parallel()
	s := rpc.New(msgpackrpc.New(nil), rpc.Addr("127.0.0.1:0"), rpc.MaxConcurrent(4))
	// Answer with the request's own first param after a stagger, so replies
	// interleave out of request order.
	s.Handle("id", func(_ context.Context, req *rpc.Request) (any, error) {
		n, _ := req.Params[0].(int64)
		time.Sleep(time.Duration(50-n) * time.Millisecond)
		return n, nil
	})
	startServer(t, s)
	c := dial(t, s.Addr())

	const n = 20
	for i := int64(0); i < n; i++ {
		c.request(1000+i, "id", i)
	}
	seen := make(map[int64]int64, n)
	for i := 0; i < n; i++ {
		id, errVal, result := c.recvResponse()
		if errVal != nil {
			t.Fatalf("msgid %d: err %#v", id, errVal)
		}
		seen[id] = result.(int64)
	}
	for i := int64(0); i < n; i++ {
		got, ok := seen[1000+i]
		if !ok || got != i {
			t.Fatalf("msgid %d: result %v (present %v), want %d", 1000+i, got, ok, i)
		}
	}
}

func TestRPC_UnknownMethodKeepsConnection(t *testing.T) {
	t.Parallel()
	s := newEchoServer(t)
	startServer(t, s)
	c := dial(t, s.Addr())

	c.request(1, "no.such.method")
	id, errVal, _ := c.recvResponse()
	if id != 1 || errCode(t, errVal) != rpc.CodeMethodNotFound {
		t.Fatalf("id = %d, err = %#v", id, errVal)
	}
	// The connection must survive.
	c.request(2, "echo", "still-alive")
	id, errVal, _ = c.recvResponse()
	if id != 2 || errVal != nil {
		t.Fatalf("follow-up: id = %d, err = %#v", id, errVal)
	}
}

func TestRPC_HandlerErrorTaxonomy(t *testing.T) {
	t.Parallel()
	s := newEchoServer(t)
	s.Handle("typed", func(context.Context, *rpc.Request) (any, error) {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "bad params"}
	})
	s.Handle("untyped", func(context.Context, *rpc.Request) (any, error) {
		return nil, errors.New("plain failure")
	})
	startServer(t, s)
	c := dial(t, s.Addr())

	c.request(1, "typed")
	_, errVal, _ := c.recvResponse()
	if errCode(t, errVal) != rpc.CodeInvalidParams {
		t.Fatalf("typed err = %#v", errVal)
	}
	c.request(2, "untyped")
	_, errVal, _ = c.recvResponse()
	if errCode(t, errVal) != rpc.CodeInternal {
		t.Fatalf("untyped err = %#v", errVal)
	}
	// Untyped error text must NOT reach the wire (deny-before-disclose):
	// only *rpc.Error messages are public.
	if m := errVal.(map[string]any); m["message"] != "internal error" {
		t.Fatalf("untyped error leaked to wire: %#v", m["message"])
	}
}

func TestRPC_HandlerPanicIsolatedAndGeneric(t *testing.T) {
	t.Parallel()
	s := newEchoServer(t)
	s.Handle("boom", func(context.Context, *rpc.Request) (any, error) {
		panic("secret internal detail")
	})
	startServer(t, s)
	c := dial(t, s.Addr())

	c.request(1, "boom")
	id, errVal, _ := c.recvResponse()
	if id != 1 || errCode(t, errVal) != rpc.CodeInternal {
		t.Fatalf("id = %d, err = %#v", id, errVal)
	}
	// Panic detail must not leak to the wire.
	if m := errVal.(map[string]any); m["message"] != "internal error" {
		t.Fatalf("panic leaked to wire: %#v", m["message"])
	}
	// Connection survives a handler panic.
	c.request(2, "echo")
	if id, errVal, _ := c.recvResponse(); id != 2 || errVal != nil {
		t.Fatalf("follow-up after panic: id = %d, err = %#v", id, errVal)
	}
}

func TestRPC_GateHandshakeBeforeMethods(t *testing.T) {
	t.Parallel()
	gate := func(sess *rpc.Session, method string) error {
		if method == "sys.hello" {
			return nil
		}
		if ok, _ := sess.Value("authed").(bool); !ok {
			return &rpc.Error{Code: rpc.CodeAccessDenied, Message: "handshake required"}
		}
		return nil
	}
	s := newEchoServer(t, rpc.WithGate(gate))
	s.Handle("sys.hello", func(_ context.Context, req *rpc.Request) (any, error) {
		req.Session.SetValue("authed", true)
		return "ok", nil
	})
	startServer(t, s)
	c := dial(t, s.Addr())

	// Pre-handshake request: rejected, handler not invoked, conn survives.
	c.request(1, "echo", "sneak")
	id, errVal, _ := c.recvResponse()
	if id != 1 || errCode(t, errVal) != rpc.CodeAccessDenied {
		t.Fatalf("pre-handshake: id = %d, err = %#v", id, errVal)
	}
	// Handshake, then the same method passes.
	c.request(2, "sys.hello")
	if id, errVal, _ := c.recvResponse(); id != 2 || errVal != nil {
		t.Fatalf("hello: id = %d, err = %#v", id, errVal)
	}
	c.request(3, "echo", "in")
	if id, errVal, _ := c.recvResponse(); id != 3 || errVal != nil {
		t.Fatalf("post-handshake: id = %d, err = %#v", id, errVal)
	}

	// A second connection is NOT admitted by the first one's handshake.
	c2 := dial(t, s.Addr())
	c2.request(1, "echo")
	if _, errVal, _ := c2.recvResponse(); errCode(t, errVal) != rpc.CodeAccessDenied {
		t.Fatalf("second conn inherited handshake: %#v", errVal)
	}
}

func TestRPC_NotificationDispatches(t *testing.T) {
	t.Parallel()
	got := make(chan []any, 1)
	s := newEchoServer(t)
	s.Handle("note", func(_ context.Context, req *rpc.Request) (any, error) {
		got <- req.Params
		return nil, nil
	})
	startServer(t, s)
	c := dial(t, s.Addr())

	c.notify("note", "fire-and-forget")
	select {
	case params := <-got:
		if len(params) != 1 || params[0] != "fire-and-forget" {
			t.Fatalf("params = %#v", params)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("notification never dispatched")
	}
	// No reply for a notification; the conn still answers requests.
	c.request(1, "echo")
	if id, errVal, _ := c.recvResponse(); id != 1 || errVal != nil {
		t.Fatalf("post-notification: id = %d, err = %#v", id, errVal)
	}
}

func TestRPC_MalformedFrameClosesConnection(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  []byte
	}{
		{"reserved byte", []byte{0xc1}},
		{"top-level not array", []byte{0xa3, 'a', 'b', 'c'}},
		{"bad type tag", func() []byte {
			b, _ := msgpack.Marshal([]any{int64(9), int64(1), "m", []any{}})
			return b
		}()},
		{"non-string method", func() []byte {
			b, _ := msgpack.Marshal([]any{int64(0), int64(1), int64(5), []any{}})
			return b
		}()},
		{"params not array", func() []byte {
			b, _ := msgpack.Marshal([]any{int64(0), int64(1), "m", "nope"})
			return b
		}()},
		{"msgid out of range", func() []byte {
			b, _ := msgpack.Marshal([]any{int64(0), int64(1) << 40, "m", []any{}})
			return b
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := newEchoServer(t)
			startServer(t, s)
			c := dial(t, s.Addr())
			c.sendRaw(tc.raw)
			c.expectClosed()
		})
	}
}

func TestRPC_MessageTooLargeClosesConnection(t *testing.T) {
	t.Parallel()
	s := newEchoServer(t, rpc.MaxMessageBytes(1024))
	startServer(t, s)
	c := dial(t, s.Addr())

	// Under the bound: fine.
	c.request(1, "echo", string(make([]byte, 200)))
	if id, errVal, _ := c.recvResponse(); id != 1 || errVal != nil {
		t.Fatalf("under-bound: id = %d, err = %#v", id, errVal)
	}
	// Over the bound: connection closes mid-read.
	c.request(2, "echo", string(make([]byte, 4096)))
	c.expectClosed()
}

func TestRPC_ShutdownCancelsInflightAndFlushesReply(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	s := newEchoServer(t)
	s.Handle("wait", func(ctx context.Context, _ *rpc.Request) (any, error) {
		close(entered)
		<-ctx.Done() // Shutdown must cancel us
		return "wound-down", nil
	})
	stop := startServer(t, s)
	c := dial(t, s.Addr())

	c.request(1, "wait")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never entered")
	}
	done := make(chan error, 1)
	go func() { done <- stop() }()

	// Polite drain: the in-flight reply arrives despite shutdown.
	id, errVal, result := c.recvResponse()
	if id != 1 || errVal != nil || result != "wound-down" {
		t.Fatalf("drained reply: id = %d, err = %#v, result = %#v", id, errVal, result)
	}
	c.expectClosed()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown never completed")
	}
}

func TestRPC_BackpressureBoundsConcurrency(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	running, peak := 0, 0
	release := make(chan struct{})
	s := rpc.New(msgpackrpc.New(nil), rpc.Addr("127.0.0.1:0"), rpc.MaxConcurrent(2))
	s.Handle("hold", func(context.Context, *rpc.Request) (any, error) {
		mu.Lock()
		running++
		if running > peak {
			peak = running
		}
		mu.Unlock()
		<-release
		mu.Lock()
		running--
		mu.Unlock()
		return nil, nil
	})
	startServer(t, s)
	c := dial(t, s.Addr())

	for i := int64(1); i <= 6; i++ {
		c.request(i, "hold")
	}
	time.Sleep(200 * time.Millisecond) // let dispatch reach the bound
	close(release)
	for i := 0; i < 6; i++ {
		if _, errVal, _ := c.recvResponse(); errVal != nil {
			t.Fatalf("err = %#v", errVal)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if peak > 2 {
		t.Fatalf("peak concurrency %d exceeds MaxConcurrent 2", peak)
	}
	if peak < 2 {
		t.Fatalf("peak concurrency %d never reached the bound", peak)
	}
}

func TestRPC_NeovimExtHandlesPassThrough(t *testing.T) {
	t.Parallel()
	s := newEchoServer(t)
	startServer(t, s)
	c := dial(t, s.Addr())

	buffer := msgpack.Ext{Type: 0, Data: []byte{0x01}} // nvim Buffer handle
	c.request(1, "echo", buffer)
	_, errVal, result := c.recvResponse()
	if errVal != nil {
		t.Fatalf("err = %#v", errVal)
	}
	arr := result.([]any)
	got, ok := arr[0].(msgpack.Ext)
	if !ok || got.Type != 0 || len(got.Data) != 1 || got.Data[0] != 0x01 {
		t.Fatalf("ext handle mangled: %#v", arr[0])
	}
}

func TestRPC_ManySequentialMessagesOneConnection(t *testing.T) {
	t.Parallel()
	s := newEchoServer(t)
	startServer(t, s)
	c := dial(t, s.Addr())

	for i := int64(1); i <= 100; i++ {
		c.request(i, "echo", fmt.Sprintf("msg-%d", i))
		id, errVal, result := c.recvResponse()
		if id != i || errVal != nil {
			t.Fatalf("iter %d: id = %d, err = %#v", i, id, errVal)
		}
		if arr := result.([]any); arr[0] != fmt.Sprintf("msg-%d", i) {
			t.Fatalf("iter %d: result = %#v", i, result)
		}
	}
}

// --- review-fold regression tests (2026-08-16 lector round 1) ---

// Finding 3: peer disconnect must cancel in-flight handler contexts.
func TestRPC_DisconnectCancelsHandlers(t *testing.T) {
	t.Parallel()
	entered := make(chan struct{})
	finished := make(chan error, 1)
	s := newEchoServer(t)
	s.Handle("wait", func(ctx context.Context, _ *rpc.Request) (any, error) {
		close(entered)
		<-ctx.Done()
		finished <- ctx.Err()
		return nil, ctx.Err()
	})
	startServer(t, s)

	conn, err := net.Dial("tcp", s.Addr())
	if err != nil {
		t.Fatal(err)
	}
	c := &testClient{t: t, conn: conn, br: bufio.NewReader(conn)}
	c.request(1, "wait")
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never entered")
	}
	conn.Close() // peer disappears without shutdown

	select {
	case cerr := <-finished:
		if !errors.Is(cerr, context.Canceled) {
			t.Fatalf("handler ctx err = %v, want Canceled", cerr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler context never cancelled after peer disconnect")
	}
}

// Finding 4: an unencodable handler result must never poison the stream —
// the peer gets a generic internal error and the connection survives.
func TestRPC_UnencodableResultReplacedNotStreamPoison(t *testing.T) {
	t.Parallel()
	s := newEchoServer(t)
	s.Handle("bad", func(context.Context, *rpc.Request) (any, error) {
		// Nested unsupported type: encoding fails only after the response
		// prefix would have been produced.
		return []any{"prefix", struct{}{}}, nil
	})
	startServer(t, s)
	c := dial(t, s.Addr())

	c.request(1, "bad")
	id, errVal, _ := c.recvResponse()
	if id != 1 || errCode(t, errVal) != rpc.CodeInternal {
		t.Fatalf("id = %d, err = %#v", id, errVal)
	}
	if m := errVal.(map[string]any); m["message"] != "internal error" {
		t.Fatalf("message = %#v", m["message"])
	}
	// The stream must be intact: the next request round-trips cleanly.
	c.request(2, "echo", "still-clean")
	id, errVal, result := c.recvResponse()
	if id != 2 || errVal != nil {
		t.Fatalf("follow-up: id = %d, err = %#v", id, errVal)
	}
	if arr := result.([]any); arr[0] != "still-clean" {
		t.Fatalf("follow-up result = %#v", result)
	}
}

// Finding 4 (outbound bound): a reply exceeding MaxMessageBytes is replaced
// by a generic error instead of shipping an unbounded frame.
func TestRPC_OversizedReplyReplaced(t *testing.T) {
	t.Parallel()
	s := newEchoServer(t, rpc.MaxMessageBytes(1024))
	s.Handle("big", func(context.Context, *rpc.Request) (any, error) {
		return string(make([]byte, 8192)), nil
	})
	startServer(t, s)
	c := dial(t, s.Addr())

	c.request(1, "big")
	id, errVal, _ := c.recvResponse()
	if id != 1 || errCode(t, errVal) != rpc.CodeInternal {
		t.Fatalf("id = %d, err = %#v", id, errVal)
	}
	c.request(2, "echo", "ok")
	if id, errVal, _ := c.recvResponse(); id != 2 || errVal != nil {
		t.Fatalf("follow-up: id = %d, err = %#v", id, errVal)
	}
}

// Finding 5: untyped gate errors must cross the wire as a stable generic
// denial, never as their raw text.
func TestRPC_GateUntypedErrorWithheld(t *testing.T) {
	t.Parallel()
	gate := func(_ *rpc.Session, method string) error {
		if method == "echo" {
			return errors.New("secret detail: /etc/autodb/master.key unreadable")
		}
		return nil
	}
	s := newEchoServer(t, rpc.WithGate(gate))
	startServer(t, s)
	c := dial(t, s.Addr())

	c.request(1, "echo")
	_, errVal, _ := c.recvResponse()
	if errCode(t, errVal) != rpc.CodeAccessDenied {
		t.Fatalf("err = %#v", errVal)
	}
	if m := errVal.(map[string]any); m["message"] != "access denied" {
		t.Fatalf("gate detail leaked to wire: %#v", m["message"])
	}
}

// Should-fix 2: construction-time option validation.
func TestRPC_InvalidOptionsPanicAtNew(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		opt  rpc.Option
	}{
		{"MaxConcurrent zero", rpc.MaxConcurrent(0)},
		{"MaxConcurrent negative", rpc.MaxConcurrent(-1)},
		{"MaxMessageBytes zero", rpc.MaxMessageBytes(0)},
		{"DrainTimeout negative", rpc.DrainTimeout(-time.Second)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("New did not panic")
				}
			}()
			_ = rpc.New(msgpackrpc.New(nil), tc.opt)
		})
	}
}

// Finding 1 (transport side): a decoded-value bomb under the raw byte
// window must be refused by the codec's aggregate budgets, not executed.
func TestRPC_DecodedValueBombRefused(t *testing.T) {
	t.Parallel()
	lim := &msgpack.Limits{MaxTotalElements: 4096}
	s := rpc.New(msgpackrpc.New(lim), rpc.Addr("127.0.0.1:0"))
	s.Handle("echo", func(_ context.Context, req *rpc.Request) (any, error) {
		return req.Params, nil
	})
	startServer(t, s)
	c := dial(t, s.Addr())

	// A single request whose params hold 8192 nils: raw bytes are tiny but
	// the decoded value count exceeds the aggregate budget → connection
	// closes (malformed-class refusal), nothing dispatches.
	params := make([]any, 8192)
	c.send([]any{int64(0), int64(1), "echo", params})
	c.expectClosed()
}
