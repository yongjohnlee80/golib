package rpc_test

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/server/rpc"
	"github.com/yongjohnlee80/golib/server/rpc/msgpackrpc"
)

// replyThenEnd is a connection to a server that answers the first request and
// closes (as sys.shutdown does), in the one order that cost the reply: the
// reply and EOF reach the client while its write is still returning, the
// reader poisons the client (closing this connection), and only then does
// the write finish — so resetting its deadline fails on a closed connection.
type replyThenEnd struct {
	r      *io.PipeReader
	w      *io.PipeWriter
	once   sync.Once
	closed chan struct{}
}

func newReplyThenEnd() *replyThenEnd {
	r, w := io.Pipe()
	return &replyThenEnd{r: r, w: w, closed: make(chan struct{})}
}

func (c *replyThenEnd) Read(p []byte) (int, error) { return c.r.Read(p) }

func (c *replyThenEnd) Write(p []byte) (int, error) {
	var b bytes.Buffer
	bw := bufio.NewWriter(&b)
	_ = msgpackrpc.New(nil).Write(bw, &rpc.Message{Kind: rpc.KindResponse, ID: 1, Result: "stopped"})
	_ = bw.Flush()
	go func() {
		_, _ = c.w.Write(b.Bytes())
		_ = c.w.Close() // EOF after the reply
	}()
	select { // the write returns only once the reader has ended the client
	case <-c.closed:
	case <-time.After(5 * time.Second):
	}
	return len(p), nil
}

func (c *replyThenEnd) Close() error {
	c.once.Do(func() { close(c.closed); _ = c.r.Close() })
	return nil
}

func (c *replyThenEnd) SetWriteDeadline(time.Time) error {
	select {
	case <-c.closed:
		return net.ErrClosed
	default:
		return nil
	}
}

func (c *replyThenEnd) LocalAddr() net.Addr             { return &net.UnixAddr{Name: "fake", Net: "unix"} }
func (c *replyThenEnd) RemoteAddr() net.Addr            { return &net.UnixAddr{Name: "fake", Net: "unix"} }
func (c *replyThenEnd) SetDeadline(time.Time) error     { return nil }
func (c *replyThenEnd) SetReadDeadline(time.Time) error { return nil }

// TestAReplyBeforeTheEndIsTheCallsAnswer: a server that replies and then
// closes answers the call — the reply, not the EOF that followed it — however
// the end interleaves with the call's own write.
func TestAReplyBeforeTheEndIsTheCallsAnswer(t *testing.T) {
	for i := range 50 {
		conn := newReplyThenEnd()
		c, err := rpc.Dial(context.Background(), "fake", msgpackrpc.New(nil),
			rpc.WithConnDialer(func(context.Context, string, string) (net.Conn, error) { return conn, nil }))
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		got, err := c.Call(ctx, "sys.shutdown")
		cancel()
		_ = c.Close()
		if err != nil || got != "stopped" {
			t.Fatalf("round %d: %v, %v; want the reply that came before the end", i, got, err)
		}
	}
}
