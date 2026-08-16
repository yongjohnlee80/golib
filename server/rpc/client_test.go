package rpc_test

// Client contract (ADR-0009): msgid correlation under concurrency,
// ctx-bounded calls, Done/Err terminal state, bounded reentrancy-safe
// notification dispatch, and the hostile-message taxonomy.

import (
	"bufio"
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/msgpack"
	"github.com/yongjohnlee80/golib/server/rpc"
	"github.com/yongjohnlee80/golib/server/rpc/msgpackrpc"
)

func dialClient(t *testing.T, addr string, opts ...rpc.ClientOption) *rpc.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := rpc.Dial(ctx, addr, msgpackrpc.New(nil), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestClientConcurrentCalls(t *testing.T) {
	t.Parallel()
	s := rpc.New(msgpackrpc.New(nil), rpc.Addr("127.0.0.1:0"), rpc.MaxConcurrent(8))
	s.Handle("id", func(_ context.Context, req *rpc.Request) (any, error) {
		n, _ := req.Params[0].(int64)
		time.Sleep(time.Duration(40-n) * time.Millisecond) // replies out of order
		return n, nil
	})
	startServer(t, s)
	c := dialClient(t, s.Addr())

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := int64(0); i < n; i++ {
		wg.Add(1)
		go func(i int64) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			got, err := c.Call(ctx, "id", i)
			if err != nil {
				errs[i] = err
				return
			}
			if got != i {
				errs[i] = errors.New("wrong correlation")
			}
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
}

func TestClientCancelledCallLeavesConnectionUsable(t *testing.T) {
	t.Parallel()
	block := make(chan struct{})
	s := rpc.New(msgpackrpc.New(nil), rpc.Addr("127.0.0.1:0"))
	s.Handle("slow", func(ctx context.Context, _ *rpc.Request) (any, error) {
		select {
		case <-block:
		case <-ctx.Done():
		}
		return "late", nil
	})
	s.Handle("echo", func(_ context.Context, req *rpc.Request) (any, error) {
		return req.Params[0], nil
	})
	startServer(t, s)
	c := dialClient(t, s.Addr())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.Call(ctx, "slow"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	close(block) // the late response to the abandoned id must be dropped

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	got, err := c.Call(ctx2, "echo", "alive")
	if err != nil || got != "alive" {
		t.Fatalf("follow-up = (%v, %v), want alive", got, err)
	}
}

func TestClientServerErrorRoundTrips(t *testing.T) {
	t.Parallel()
	s := rpc.New(msgpackrpc.New(nil), rpc.Addr("127.0.0.1:0"))
	s.Handle("fail", func(context.Context, *rpc.Request) (any, error) {
		return nil, &rpc.Error{Code: rpc.CodeInvalidParams, Message: "bad input"}
	})
	startServer(t, s)
	c := dialClient(t, s.Addr())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.Call(ctx, "fail")
	var re *rpc.Error
	if !errors.As(err, &re) || re.Code != rpc.CodeInvalidParams || re.Message != "bad input" {
		t.Fatalf("err = %v, want structured *Error", err)
	}
}

func TestClientDoneOnCloseAndServerDrop(t *testing.T) {
	t.Parallel()
	s := newEchoServer(t)
	stop := startServer(t, s)
	c := dialClient(t, s.Addr())

	// Server shutdown → the client observes the terminal state via Done.
	if err := stop(); err != nil {
		t.Fatalf("server stop: %v", err)
	}
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done never closed after server shutdown")
	}
	if c.Err() == nil {
		t.Fatal("Err() nil after Done")
	}

	// Close on an already-poisoned client is a safe no-op.
	_ = c.Close()

	// A fresh client's Close publishes ErrClientClosed.
	s2 := newEchoServer(t)
	startServer(t, s2)
	c2 := dialClient(t, s2.Addr())
	_ = c2.Close()
	select {
	case <-c2.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done never closed after Close")
	}
	if !errors.Is(c2.Err(), rpc.ErrClientClosed) {
		t.Fatalf("Err = %v, want ErrClientClosed", c2.Err())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := c2.Call(ctx, "echo"); !errors.Is(err, rpc.ErrClientClosed) {
		t.Fatalf("Call after Close = %v, want ErrClientClosed", err)
	}
}

// fakeServer accepts one connection and hands it to fn as raw msgpack-RPC.
func fakeServer(t *testing.T, fn func(conn net.Conn, br *bufio.Reader)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fn(conn, bufio.NewReader(conn))
	}()
	return ln.Addr().String()
}

func rawFrame(t *testing.T, v any) []byte {
	t.Helper()
	b, err := msgpack.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestClientNotificationsOrderAndReentrancy(t *testing.T) {
	t.Parallel()
	// The fake server pushes two notifications, then answers requests.
	addr := fakeServer(t, func(conn net.Conn, br *bufio.Reader) {
		conn.Write(rawFrame(t, []any{int64(2), "evt", []any{int64(1)}}))
		conn.Write(rawFrame(t, []any{int64(2), "evt", []any{int64(2)}}))
		for {
			v, err := msgpack.Decode(br, nil)
			if err != nil {
				return
			}
			arr := v.([]any)
			if arr[0] == int64(0) { // request → echo its msgid as the result
				conn.Write(rawFrame(t, []any{int64(1), arr[1], nil, "pong"}))
			}
		}
	})

	var mu sync.Mutex
	var order []int64
	nested := make(chan error, 1)
	cliCh := make(chan *rpc.Client, 1) // hands the client to the callback race-free
	var once sync.Once

	c := dialClient(t, addr, rpc.OnNotification(func(method string, params []any) {
		mu.Lock()
		order = append(order, params[0].(int64))
		mu.Unlock()
		once.Do(func() {
			// Reentrancy: a notification handler may Call.
			cli := <-cliCh
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, err := cli.Call(ctx, "ping")
			nested <- err
		})
	}))
	cliCh <- c

	select {
	case err := <-nested:
		if err != nil {
			t.Fatalf("reentrant call: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reentrant call never completed")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) < 2 || order[0] != 1 || order[1] != 2 {
		t.Fatalf("notification order = %v, want [1 2]", order)
	}
}

func TestClientNotificationPanicPoisons(t *testing.T) {
	t.Parallel()
	addr := fakeServer(t, func(conn net.Conn, _ *bufio.Reader) {
		conn.Write(rawFrame(t, []any{int64(2), "boom", []any{}}))
		time.Sleep(2 * time.Second)
	})
	c := dialClient(t, addr, rpc.OnNotification(func(string, []any) {
		panic("handler bug")
	}))
	select {
	case <-c.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done never closed after handler panic")
	}
	if c.Err() == nil {
		t.Fatal("Err nil after handler panic")
	}
}

func TestClientHostileFramesPoison(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		frame []byte
	}{
		{"malformed bytes", []byte{0xc1, 0x00}},
		{"server-initiated request", nil}, // built below
	}
	cases[1].frame = rawFrame(t, []any{int64(0), int64(9), "srv.push", []any{}})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			addr := fakeServer(t, func(conn net.Conn, _ *bufio.Reader) {
				conn.Write(tc.frame)
				time.Sleep(2 * time.Second)
			})
			c := dialClient(t, addr)
			select {
			case <-c.Done():
			case <-time.After(5 * time.Second):
				t.Fatal("Done never closed on hostile frame")
			}
			if errors.Is(c.Err(), rpc.ErrClientClosed) || c.Err() == nil {
				t.Fatalf("Err = %v, want a transport poison cause", c.Err())
			}
		})
	}
}

func TestClientUnknownIDResponseTolerated(t *testing.T) {
	t.Parallel()
	addr := fakeServer(t, func(conn net.Conn, br *bufio.Reader) {
		// An unsolicited-but-well-formed response: must be dropped.
		conn.Write(rawFrame(t, []any{int64(1), int64(4242), nil, "ghost"}))
		for {
			v, err := msgpack.Decode(br, nil)
			if err != nil {
				return
			}
			arr := v.([]any)
			if arr[0] == int64(0) {
				conn.Write(rawFrame(t, []any{int64(1), arr[1], nil, "ok"}))
			}
		}
	})
	c := dialClient(t, addr)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := c.Call(ctx, "anything")
	if err != nil || got != "ok" {
		t.Fatalf("call after ghost response = (%v, %v)", got, err)
	}
}

func TestClientNotifyReachesServer(t *testing.T) {
	t.Parallel()
	got := make(chan string, 1)
	s := newEchoServer(t)
	s.Handle("note", func(_ context.Context, req *rpc.Request) (any, error) {
		got <- req.Params[0].(string)
		return nil, nil
	})
	startServer(t, s)
	c := dialClient(t, s.Addr())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Notify(ctx, "note", "fire"); err != nil {
		t.Fatal(err)
	}
	select {
	case v := <-got:
		if v != "fire" {
			t.Fatalf("notify param = %q", v)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("notification never dispatched")
	}
}
