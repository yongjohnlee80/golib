package rpc

import (
	"bufio"
	"errors"
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
	c := newConn(nopConn{})
	huge := strings.Repeat("x", 8<<20) // 8 MiB value...
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
	c := newConn(nopConn{})
	at := strings.Repeat("a", 512)
	if err := c.write(stringCodec{}, &Message{Kind: KindResponse, Result: at}, 512); err != nil {
		t.Fatalf("at-bound: %v", err)
	}
	over := strings.Repeat("a", 513)
	if err := c.write(stringCodec{}, &Message{Kind: KindResponse, Result: over}, 512); !errors.Is(err, errEncode) {
		t.Fatalf("over-bound: err = %v, want errEncode", err)
	}
}
