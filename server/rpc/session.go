package rpc

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
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

// errEncode wraps a codec encoding failure that occurred entirely in the
// staging buffer — no bytes reached the socket, so the stream is intact and
// the caller may substitute a different (encodable) reply.
var errEncode = fmt.Errorf("rpc: response encoding failed")

// conn is one live connection's machinery: buffered codec endpoints, the
// staged-and-serialized write path, drain-safe request admission, and the
// polite-drain contract. It is the Session the transport's
// ScaffoldSessionFactory registers.
type conn struct {
	raw  net.Conn
	lim  *windowReader
	br   *bufio.Reader
	sess *Session

	// Write path: each reply is encoded COMPLETELY into scratch first, then
	// written to the socket as one finished frame. A mid-encode failure
	// therefore never leaves partial bytes on the wire (the stream would be
	// unrecoverable). capw enforces the outbound size bound DURING encoding,
	// so an oversized handler result is refused as it streams — scratch
	// never accumulates more than the cap plus one bufio flush chunk. All
	// fields are guarded by writeMu.
	writeMu  sync.Mutex
	scratch  bytes.Buffer
	capw     cappedWriter
	scratchW *bufio.Writer

	// Admission gate: draining and inflight.Add are only touched under
	// admitMu, making "no new work after drain begins" atomic with the
	// WaitGroup lifecycle (a positive Add can never race Drain's wait).
	admitMu  sync.Mutex
	draining bool
	inflight sync.WaitGroup

	// workDone is closed by serveConn — the signal's single owner — once the
	// read loop has exited AND every admitted handler has finished. Drain
	// waits on it instead of spawning a watcher goroutine.
	workDone chan struct{}
}

var (
	_ server.Session = (*conn)(nil)
	_ server.Drainer = (*conn)(nil)
)

func newConn(nc net.Conn) *conn {
	lim := &windowReader{src: nc}
	c := &conn{
		raw:      nc,
		lim:      lim,
		br:       bufio.NewReader(lim),
		sess:     &Session{peer: nc.RemoteAddr()},
		workDone: make(chan struct{}),
	}
	c.capw.buf = &c.scratch
	c.scratchW = bufio.NewWriter(&c.capw)
	return c
}

// admit reserves an execution slot for one request, atomically with the
// drain state. It returns false once draining has begun.
func (c *conn) admit() bool {
	c.admitMu.Lock()
	defer c.admitMu.Unlock()
	if c.draining {
		return false
	}
	c.inflight.Add(1)
	return true
}

// isDraining reports whether Drain has begun.
func (c *conn) isDraining() bool {
	c.admitMu.Lock()
	defer c.admitMu.Unlock()
	return c.draining
}

// write sends one message: staged into scratch under writeMu — with the
// maxBytes bound enforced DURING encoding by the capping writer, so an
// oversized result is refused as it streams instead of after it has been
// fully accumulated — then written to the socket as a complete frame. An
// error wrapping errEncode means nothing reached the socket; any other
// error is a socket failure and the connection is no longer usable.
func (c *conn) write(codec Codec, m *Message, maxBytes int64) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.scratch.Reset()
	c.capw.remaining = maxBytes
	c.scratchW.Reset(&c.capw)
	if err := codec.Write(c.scratchW, m); err != nil {
		return fmt.Errorf("%w: %w", errEncode, err)
	}
	if err := c.scratchW.Flush(); err != nil {
		return fmt.Errorf("%w: %w", errEncode, err)
	}
	_, err := c.raw.Write(c.scratch.Bytes())
	if c.scratch.Cap() > scratchRetainMax {
		// Don't let one large reply pin its buffer for the connection's life.
		c.scratch = bytes.Buffer{}
		c.capw.buf = &c.scratch
	}
	return err
}

// scratchRetainMax caps the reply staging buffer retained between writes.
const scratchRetainMax = 1 << 20

// cappedWriter refuses bytes beyond remaining, failing an over-bound encode
// while it streams — the staging buffer never grows past the cap plus one
// bufio flush chunk, no matter how large the handler's result value is.
type cappedWriter struct {
	buf       *bytes.Buffer
	remaining int64
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, fmt.Errorf("%w: outbound frame over bound", ErrMessageTooLarge)
	}
	w.remaining -= int64(len(p))
	return w.buf.Write(p)
}

// Close force-terminates the connection (registry deadline path).
func (c *conn) Close() error { return c.raw.Close() }

// Drain ends the connection politely: close admission (no new requests can
// Add), unblock the read loop with an immediate read deadline, wait —
// bounded by ctx — for serveConn to report all admitted work finished and
// its replies flushed, then close.
func (c *conn) Drain(ctx context.Context) error {
	c.admitMu.Lock()
	c.draining = true
	c.admitMu.Unlock()
	_ = c.raw.SetReadDeadline(time.Now())
	select {
	case <-c.workDone:
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
