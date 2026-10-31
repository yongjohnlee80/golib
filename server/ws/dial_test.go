package ws_test

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/server"
	"github.com/yongjohnlee80/golib/server/ws"
)

// serveUnix serves h on a unix socket of its own, answering the socket's path. The folder is short:
// a socket's path has a length limit a test's temp folder can pass.
func serveUnix(t *testing.T, h http.Handler) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ws")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return path
}

// endpoint is golib's own ws.Handler for fn, at /ws.
func endpoint(fn func(context.Context, *ws.Session), opts ...ws.Option) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /ws", ws.Handler(&server.Registry{}, fn, opts...))
	return mux
}

func bounded(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// DialUnix reaches golib's own endpoint on a unix socket at its route, and a text, a binary and a
// JSON message each come back whole; the subprotocol both offer is the one chosen.
func TestDialUnix_RoundTrip(t *testing.T) {
	t.Parallel()
	path := serveUnix(t, endpoint(echoFn, ws.Subprotocols("v1")))
	ctx := bounded(t)
	c, err := ws.DialUnix(ctx, path, ws.DialPath("/ws"), ws.DialSubprotocols("v1"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if c.Subprotocol() != "v1" {
		t.Fatalf("subprotocol %q, want v1", c.Subprotocol())
	}
	for _, m := range []struct {
		t ws.MessageType
		p []byte
	}{{ws.Text, []byte("hello")}, {ws.Binary, []byte{0, 1, 2, 255}}} {
		if err := c.Write(ctx, m.t, m.p); err != nil {
			t.Fatal(err)
		}
		mt, p, err := c.Read(ctx)
		if err != nil || mt != m.t || !bytes.Equal(p, m.p) {
			t.Fatalf("echo of %v %q: %v %q %v", m.t, m.p, mt, p, err)
		}
	}
	type msg struct {
		Method string `json:"method"`
		ID     int    `json:"id"`
	}
	if err := c.WriteJSON(ctx, msg{"thread/list", 3}); err != nil {
		t.Fatal(err)
	}
	var got msg
	if err := c.ReadJSON(ctx, &got); err != nil || got != (msg{"thread/list", 3}) {
		t.Fatalf("the JSON echo: %+v %v", got, err)
	}
	if err := c.Close(ws.StatusNormalClosure, ""); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// ctx bounds the dial and handshake alone: a socket that takes the connection and never answers
// the handshake is given up at ctx's end, and a connection made outlives the ctx it was dialled
// with. A read's own ctx ends that read.
func TestDialUnix_CtxBoundsTheDialAlone(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("", "ws")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	silent := filepath.Join(dir, "silent.sock")
	ln, err := net.Listen("unix", silent)
	if err != nil {
		t.Fatal(err)
	}
	held := make(chan net.Conn, 4) // taken and held open, never answered
	defer func() {
		ln.Close()
		for len(held) > 0 {
			(<-held).Close()
		}
	}()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			select {
			case held <- conn:
			default:
				conn.Close()
			}
		}
	}()
	short, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	start := time.Now()
	_, err = ws.DialUnix(short, silent)
	cancel()
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("a handshake never answered: %v after %v, want an error at the bound", err, time.Since(start))
	}

	path := serveUnix(t, endpoint(echoFn))
	dialCtx, dialCancel := context.WithCancel(context.Background())
	c, err := ws.DialUnix(dialCtx, path, ws.DialPath("/ws"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	dialCancel() // the connection is not the dial's
	ctx := bounded(t)
	if err := c.Write(ctx, ws.Text, []byte("after")); err != nil {
		t.Fatalf("a write after the dial's ctx ended: %v", err)
	}
	if _, p, err := c.Read(ctx); err != nil || string(p) != "after" {
		t.Fatalf("a read after the dial's ctx ended: %q %v", p, err)
	}
	readCtx, readCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer readCancel()
	if _, _, err := c.Read(readCtx); err == nil {
		t.Fatal("a read with nothing to read outlived its ctx")
	}
}

// A message past the default limit (1 MiB) fails the read, and the connection is closed as too
// big; DialReadLimit raises the limit, and the same message arrives whole.
func TestDialUnix_LargeMessage(t *testing.T) {
	t.Parallel()
	big := bytes.Repeat([]byte("x"), 2<<20)
	sendBig := func(ctx context.Context, s *ws.Session) {
		if err := s.Write(ctx, ws.Text, big); err != nil {
			return
		}
		_, _, _ = s.Read(ctx) // until the peer goes
	}
	path := serveUnix(t, endpoint(sendBig))
	ctx := bounded(t)

	raised, err := ws.DialUnix(ctx, path, ws.DialPath("/ws"), ws.DialReadLimit(4<<20))
	if err != nil {
		t.Fatal(err)
	}
	defer raised.CloseNow()
	if _, p, err := raised.Read(ctx); err != nil || len(p) != len(big) {
		t.Fatalf("a 2 MiB message under a 4 MiB limit: %d bytes, %v", len(p), err)
	}

	plain, err := ws.DialUnix(ctx, path, ws.DialPath("/ws"))
	if err != nil {
		t.Fatal(err)
	}
	defer plain.CloseNow()
	if _, _, err := plain.Read(ctx); err == nil {
		t.Fatal("a 2 MiB message passed the default 1 MiB limit")
	}

	// the default is 1 MiB itself, not the library's smaller one: a message just under it arrives
	under := bytes.Repeat([]byte("y"), (1<<20)-64)
	sendUnder := func(ctx context.Context, s *ws.Session) {
		if err := s.Write(ctx, ws.Text, under); err != nil {
			return
		}
		_, _, _ = s.Read(ctx)
	}
	path2 := serveUnix(t, endpoint(sendUnder))
	c, err := ws.DialUnix(ctx, path2, ws.DialPath("/ws"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if _, p, err := c.Read(ctx); err != nil || len(p) != len(under) {
		t.Fatalf("a message just under 1 MiB: %d bytes, %v", len(p), err)
	}
}

// The peer closing ends the next read with the peer's close code; the client's own close reaches
// the server with the code it gave.
func TestDialUnix_PeerClose(t *testing.T) {
	t.Parallel()
	ctx := bounded(t)
	leaves := func(ctx context.Context, s *ws.Session) {
		_, _, _ = s.Read(ctx)
		_ = s.Close(ws.StatusGoingAway, "bye")
	}
	c, err := ws.DialUnix(ctx, serveUnix(t, endpoint(leaves)), ws.DialPath("/ws"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := c.Write(ctx, ws.Text, []byte("go")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Read(ctx); ws.CloseStatus(err) != ws.StatusGoingAway {
		t.Fatalf("after the peer's close: %v (status %d), want going away", err, ws.CloseStatus(err))
	}

	heard := make(chan ws.StatusCode, 1)
	listens := func(ctx context.Context, s *ws.Session) {
		_, _, err := s.Read(ctx)
		heard <- ws.CloseStatus(err)
	}
	c2, err := ws.DialUnix(ctx, serveUnix(t, endpoint(listens)), ws.DialPath("/ws"))
	if err != nil {
		t.Fatal(err)
	}
	if err := c2.Close(ws.StatusGoingAway, "client leaving"); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case got := <-heard:
		if got != ws.StatusGoingAway {
			t.Fatalf("the server heard close status %d, want going away", got)
		}
	case <-ctx.Done():
		t.Fatal("the server never heard the client's close")
	}
	if ws.CloseStatus(errors.New("not a close")) != -1 {
		t.Fatal("an error that is no close carries a status")
	}
}

// A path that is not absolute names no route, and a socket nobody listens on is no connection:
// both are refused.
func TestDialUnix_Refusals(t *testing.T) {
	t.Parallel()
	ctx := bounded(t)
	path := serveUnix(t, endpoint(echoFn))
	if _, err := ws.DialUnix(ctx, path, ws.DialPath("ws")); err == nil {
		t.Fatal("a relative path was dialled")
	}
	if _, err := ws.DialUnix(ctx, path, ws.DialPath("/elsewhere")); err == nil {
		t.Fatal("a route the server doesn't serve gave a connection")
	}
	dir, err := os.MkdirTemp("", "ws")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	if _, err := ws.DialUnix(ctx, filepath.Join(dir, "none.sock")); err == nil {
		t.Fatal("a socket nobody listens on gave a connection")
	}
}
