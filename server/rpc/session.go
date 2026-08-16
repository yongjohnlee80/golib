package rpc

import (
	"bufio"
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yongjohnlee80/golib/server"
)

// Session is the per-connection state shared by the gate and handlers: the
// peer address plus a small key/value store for handshake products (identity,
// negotiated flags). Safe for concurrent use.
type Session struct {
	peer net.Addr

	mu   sync.Mutex
	vals map[string]any
}

// Peer returns the remote address.
func (s *Session) Peer() net.Addr { return s.peer }

// Value returns the stored value for key, or nil.
func (s *Session) Value(key string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.vals[key]
}

// SetValue stores v under key (a handshake handler admitting the connection,
// for example, so the gate can observe it on subsequent requests).
func (s *Session) SetValue(key string, v any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vals == nil {
		s.vals = make(map[string]any)
	}
	s.vals[key] = v
}

// conn is one live connection's machinery: buffered codec endpoints, the
// serialized write path, in-flight accounting, and the polite-drain contract.
// It is the Session the transport's ScaffoldSessionFactory registers.
type conn struct {
	raw  net.Conn
	lim  *windowReader
	br   *bufio.Reader
	bw   *bufio.Writer
	sess *Session

	writeMu  sync.Mutex
	inflight sync.WaitGroup
	draining atomic.Bool
}

var (
	_ server.Session = (*conn)(nil)
	_ server.Drainer = (*conn)(nil)
)

func newConn(nc net.Conn) *conn {
	lim := &windowReader{src: nc}
	return &conn{
		raw:  nc,
		lim:  lim,
		br:   bufio.NewReader(lim),
		bw:   bufio.NewWriter(nc),
		sess: &Session{peer: nc.RemoteAddr()},
	}
}

// write sends one message: serialized against concurrent replies, flushed so
// the peer never waits on a buffered response.
func (c *conn) write(codec Codec, m *Message) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := codec.Write(c.bw, m); err != nil {
		return err
	}
	return c.bw.Flush()
}

// Close force-terminates the connection (registry deadline path).
func (c *conn) Close() error { return c.raw.Close() }

// Drain ends the connection politely: stop reading new requests (an
// immediate read deadline unblocks the read loop), let in-flight handlers
// finish and flush their replies — bounded by ctx — then close.
func (c *conn) Drain(ctx context.Context) error {
	c.draining.Store(true)
	_ = c.raw.SetReadDeadline(time.Now())
	done := make(chan struct{})
	go func() {
		c.inflight.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
	return c.raw.Close()
}

// windowReader bounds how many bytes the underlying connection yields for
// one message window (reset by the read loop before each codec.Read). A
// message that overruns its window fails with ErrMessageTooLarge instead of
// letting a hostile peer stream unbounded bytes into one decode (R4). The
// effective per-message bound is MaxMessageBytes plus bufio read-ahead
// already counted against the previous window.
type windowReader struct {
	src net.Conn
	n   int64
}

func (w *windowReader) reset(n int64) { w.n = n }

func (w *windowReader) Read(p []byte) (int, error) {
	if w.n <= 0 {
		return 0, ErrMessageTooLarge
	}
	if int64(len(p)) > w.n {
		p = p[:w.n]
	}
	n, err := w.src.Read(p)
	w.n -= int64(n)
	return n, err
}
