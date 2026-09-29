package rpc

import (
	"bufio"
	"context"
	"errors"
	"math"
	"net"
	"strings"
	"testing"
	"time"
)

// nopConn is a discard-only net.Conn for exercising conn.write in isolation.
type nopConn struct{}

func (nopConn) Read([]byte) (int, error)         { return 0, nil }
func (nopConn) Write(p []byte) (int, error)      { return len(p), nil }
func (nopConn) Close() error                     { return nil }
func (nopConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (nopConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (nopConn) SetDeadline(time.Time) error      { return nil }
func (nopConn) SetReadDeadline(time.Time) error  { return nil }
func (nopConn) SetWriteDeadline(time.Time) error { return nil }

// stringCodec encodes m.Result as raw bytes — enough to drive the staging
// path with values of controlled size.
type stringCodec struct{}

func (stringCodec) Read(*bufio.Reader) (*Message, error) { return nil, errors.New("unused") }
func (stringCodec) Write(w *bufio.Writer, m *Message) error {
	_, err := w.WriteString(m.Result.(string))
	return err
}

// Review r2 residual (finding 4): the outbound bound must be enforced WHILE
// the reply encodes, not after it has fully accumulated — staging memory
// stays near the cap even when the handler's value is vastly larger.
func TestConnWriteRefusesOversizeDuringEncoding(t *testing.T) {
	c := newConn(nopConn{}, nil)
	huge := strings.Repeat("x", 8<<20)                                              // 8 MiB value...
	err := c.write(stringCodec{}, &Message{Kind: KindResponse, Result: huge}, 1024) // ...1 KiB bound
	if !errors.Is(err, errEncode) {
		t.Fatalf("err = %v, want errEncode wrap", err)
	}
	if !errors.Is(err, ErrMessageTooLarge) {
		t.Fatalf("err = %v, want ErrMessageTooLarge in chain", err)
	}
	// The staging buffer must have refused the stream early: allowing the
	// cap plus one bufio flush chunk, nowhere near the 8 MiB value.
	if got := c.scratch.Cap(); got > 64<<10 {
		t.Fatalf("staging buffer grew to %d bytes for an over-bound value", got)
	}
	// The connection's write path stays usable for an in-bound frame.
	if err := c.write(stringCodec{}, &Message{Kind: KindResponse, Result: "ok"}, 1024); err != nil {
		t.Fatalf("in-bound write after refusal: %v", err)
	}
}

// An exactly-at-bound frame passes; one byte over is refused.
func TestConnWriteBoundBoundary(t *testing.T) {
	c := newConn(nopConn{}, nil)
	at := strings.Repeat("a", 512)
	if err := c.write(stringCodec{}, &Message{Kind: KindResponse, Result: at}, 512); err != nil {
		t.Fatalf("at-bound: %v", err)
	}
	over := strings.Repeat("a", 513)
	if err := c.write(stringCodec{}, &Message{Kind: KindResponse, Result: over}, 512); !errors.Is(err, errEncode) {
		t.Fatalf("over-bound: err = %v, want errEncode", err)
	}
}

// Msgids never wrap — exhaustion poisons with ErrMsgIDExhausted.
func TestClientMsgIDExhaustionPoisons(t *testing.T) {
	c := &Client{
		conn:    nopConn{},
		nextID:  uint64(math.MaxUint32) + 1, // space already spent
		pending: map[uint32]chan clientResp{},
		done:    make(chan struct{}),
	}
	if _, _, err := c.register(); !errors.Is(err, ErrMsgIDExhausted) {
		t.Fatalf("err = %v, want ErrMsgIDExhausted", err)
	}
	select {
	case <-c.Done():
	default:
		t.Fatal("exhaustion did not poison")
	}
	if !errors.Is(c.Err(), ErrMsgIDExhausted) {
		t.Fatalf("Err = %v", c.Err())
	}
}

// TestAReplyThatArrivedIsTheAnswer: a response already delivered wins over the
// client's end and over ctx, both ready too when the wait looks — as when a
// server replies and then closes. A select choosing at random among the three
// would fail about half the rounds; 200 rounds leave that no hiding place.
func TestAReplyThatArrivedIsTheAnswer(t *testing.T) {
	for i := range 200 {
		c := &Client{done: make(chan struct{}), termErr: errors.New("rpc: read: EOF")}
		close(c.done)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		ch := make(chan clientResp, 1)
		ch <- clientResp{result: "stopped"}
		got, err := c.await(ctx, ch)
		if err != nil || got != "stopped" {
			t.Fatalf("round %d: %v, %v; want the reply that arrived", i, got, err)
		}
	}
	// no reply: the end is the answer, and ctx's when it is the only one
	c := &Client{done: make(chan struct{}), termErr: errors.New("rpc: read: EOF")}
	close(c.done)
	if _, err := c.await(context.Background(), make(chan clientResp, 1)); err == nil || err.Error() != "rpc: read: EOF" {
		t.Fatalf("no reply, the client ended: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&Client{done: make(chan struct{})}).await(ctx, make(chan clientResp, 1)); !errors.Is(err, context.Canceled) {
		t.Fatalf("no reply, ctx done: %v", err)
	}
}
