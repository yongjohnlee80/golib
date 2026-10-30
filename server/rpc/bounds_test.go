package rpc_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/server/rpc"
)

// closedWithin is nil when the server hangs up on c within d, and says why not otherwise: bytes,
// or a connection still open at d (which expectClosed cannot tell from a close).
func closedWithin(c *testClient, d time.Duration) error {
	_ = c.conn.SetReadDeadline(time.Now().Add(d))
	_, err := c.br.ReadByte()
	switch {
	case err == nil:
		return errors.New("the server sent bytes")
	case errors.Is(err, os.ErrDeadlineExceeded):
		return errors.New("still open after " + d.String())
	}
	return nil
}

// served is nil when a request on c is answered.
func served(c *testClient, id int64) error {
	c.request(id, "echo", "x")
	_ = c.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if got, errVal, _ := c.recvResponse(); got != id || errVal != nil {
		return errors.New("not served")
	}
	return nil
}

// helloGate admits sys.hello, and every method once a hello has succeeded on the session.
func helloGate(sess *rpc.Session, method string) error {
	if method == "sys.hello" {
		return nil
	}
	if ok, _ := sess.Value("authed").(bool); !ok {
		return &rpc.Error{Code: rpc.CodeAccessDenied, Message: "handshake required"}
	}
	return nil
}

func withHello(s *rpc.Server) *rpc.Server {
	s.Handle("sys.hello", func(_ context.Context, req *rpc.Request) (any, error) {
		if len(req.Params) > 0 && req.Params[0] == "wrong" {
			return nil, &rpc.Error{Code: rpc.CodeAccessDenied, Message: "bad token"}
		}
		req.Session.SetValue("authed", true)
		return "ok", nil
	})
	return s
}

// TestRPC_HandshakeTimeoutBoundsAnUnhandshakenConnection: with a gate, a connection that says
// nothing, or only fails its handshake, is closed at HandshakeTimeout; one whose handshake succeeds
// may then idle past it and is still served. Without a gate there is no handshake, and a silent
// connection stays.
func TestRPC_HandshakeTimeoutBoundsAnUnhandshakenConnection(t *testing.T) {
	t.Parallel()
	const hs = 300 * time.Millisecond
	s := withHello(newEchoServer(t, rpc.WithGate(helloGate), rpc.HandshakeTimeout(hs)))
	startServer(t, s)
	silent, failed, done := dial(t, s.Addr()), dial(t, s.Addr()), dial(t, s.Addr())
	failed.request(1, "sys.hello", "wrong")
	if _, errVal, _ := failed.recvResponse(); errCode(t, errVal) != rpc.CodeAccessDenied {
		t.Fatalf("a wrong hello: %#v", errVal)
	}
	done.request(1, "sys.hello")
	if _, errVal, _ := done.recvResponse(); errVal != nil {
		t.Fatalf("hello: %#v", errVal)
	}
	if err := closedWithin(silent, 3*time.Second); err != nil {
		t.Errorf("a silent connection past HandshakeTimeout: %v", err)
	}
	if err := closedWithin(failed, 3*time.Second); err != nil {
		t.Errorf("a connection whose only hello failed, past HandshakeTimeout: %v", err)
	}
	time.Sleep(2 * hs)
	if err := served(done, 2); err != nil {
		t.Errorf("a handshaken connection idle past HandshakeTimeout: %v", err)
	}

	open := newEchoServer(t, rpc.HandshakeTimeout(hs)) // no gate
	startServer(t, open)
	quiet := dial(t, open.Addr())
	time.Sleep(2 * hs)
	if err := served(quiet, 1); err != nil {
		t.Errorf("no gate, a connection silent past HandshakeTimeout: %v", err)
	}
}

// TestRPC_MaxConnsClosesOnePastTheCap: past MaxConns a connection is closed before anything of it is
// read; the ones within are served, and a freed place serves a new one.
func TestRPC_MaxConnsClosesOnePastTheCap(t *testing.T) {
	t.Parallel()
	s := newEchoServer(t, rpc.MaxConns(2))
	startServer(t, s)
	a, b := dial(t, s.Addr()), dial(t, s.Addr())
	for i, c := range []*testClient{a, b} {
		if err := served(c, int64(i+1)); err != nil {
			t.Fatalf("connection %d within MaxConns: %v", i+1, err)
		}
	}
	if err := closedWithin(dial(t, s.Addr()), 3*time.Second); err != nil {
		t.Errorf("a third connection past MaxConns(2): %v", err)
	}
	a.conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c := dial(t, s.Addr())
		if err := closedWithin(c, 100*time.Millisecond); err != nil { // still open: it has a place
			if err := served(c, 9); err != nil {
				t.Errorf("a connection in the freed place: %v", err)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no place freed after a connection closed")
		}
	}
}

// TestRPC_IdleTimeoutSparesABusyConnection: IdleTimeout closes a connection that reads nothing for
// it while nothing of its is in flight, and spares one whose request is still running, or that
// keeps sending.
func TestRPC_IdleTimeoutSparesABusyConnection(t *testing.T) {
	t.Parallel()
	const idle = 300 * time.Millisecond
	s := newEchoServer(t, rpc.IdleTimeout(idle))
	s.Handle("slow", func(ctx context.Context, _ *rpc.Request) (any, error) {
		select {
		case <-time.After(3 * idle):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return "done", nil
	})
	startServer(t, s)
	quiet, busy, chatty := dial(t, s.Addr()), dial(t, s.Addr()), dial(t, s.Addr())
	busy.request(1, "slow")
	if id, errVal, result := busy.recvResponse(); id != 1 || errVal != nil || result != "done" {
		t.Errorf("a request running past IdleTimeout: %v %#v %v, want it answered", id, errVal, result)
	}
	if err := served(busy, 2); err != nil { // idle counts from the request's end, not its frame
		t.Errorf("the busy connection, just after its request: %v", err)
	}
	for i := range 6 {
		if err := served(chatty, int64(i+1)); err != nil {
			t.Fatalf("a connection sending every %v: %v", idle/3, err)
		}
		time.Sleep(idle / 3)
	}
	if err := closedWithin(quiet, 3*time.Second); err != nil {
		t.Errorf("an idle connection past IdleTimeout: %v", err)
	}
}

// TestRPC_NewRefusesBadConnectionBounds: a non-positive MaxConns and a negative HandshakeTimeout or
// IdleTimeout are construction bugs, refused at New.
func TestRPC_NewRefusesBadConnectionBounds(t *testing.T) {
	t.Parallel()
	for name, opt := range map[string]rpc.Option{
		"MaxConns(0)": rpc.MaxConns(0), "HandshakeTimeout(-1)": rpc.HandshakeTimeout(-1), "IdleTimeout(-1)": rpc.IdleTimeout(-1),
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s was accepted", name)
				}
			}()
			newEchoServer(t, opt)
		}()
	}
}
