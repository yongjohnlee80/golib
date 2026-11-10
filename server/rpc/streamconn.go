package rpc

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// StreamConn is a [net.Conn] over a reader and a writer, such as a program's stdin and stdout, whose
// deadlines work whatever the files can do. The [Client] bounds every write with a deadline and the
// session wakes a blocked read with one, so a connection that can't take a deadline can't carry RPC.
//
// A side whose file has deadlines uses them. A side without them (Windows' anonymous pipes, whose
// SetDeadline is [os.ErrNoDeadline], or any plain reader or writer) runs through a goroutine that
// does the blocking call, while the caller waits on the deadline instead:
//
//   - A read that passes its deadline loses nothing. What has arrived stays held, and the next read
//     returns it.
//   - A write that passes its deadline can't be cancelled, since the call is already in the OS. It
//     returns [os.ErrDeadlineExceeded], how much of it got through is unknown, and the connection's
//     write side is broken from then on: every later write fails the same way. Only Close ends the
//     stuck call, when the OS lets it.
//
// A deadline changed during a read or write applies to it, as on a net.Conn.
//
// Concurrency: a read and a write may run together; each side's own calls take turns. Deadlines and
// Close may be called from any goroutine.
func StreamConn(r io.ReadCloser, w io.WriteCloser) net.Conn {
	c := &streamConn{r: r, w: w, closed: make(chan struct{})}
	if d, ok := r.(readDeadliner); ok && d.SetReadDeadline(time.Time{}) == nil {
		c.read, c.readDeadline = r.Read, d.SetReadDeadline
	} else {
		p := &pumpedReader{r: r, closed: c.closed, chunks: make(chan chunk), dl: newWatchedDeadline()}
		c.read, c.readDeadline = p.Read, p.dl.set
	}
	if d, ok := w.(writeDeadliner); ok && d.SetWriteDeadline(time.Time{}) == nil {
		c.write, c.writeDeadline = w.Write, d.SetWriteDeadline
	} else {
		p := &pumpedWriter{w: w, closed: c.closed, jobs: make(chan []byte), results: make(chan writeResult, 1), dl: newWatchedDeadline()}
		c.write, c.writeDeadline = p.Write, p.dl.set
	}
	return c
}

type (
	readDeadliner  interface{ SetReadDeadline(time.Time) error }
	writeDeadliner interface{ SetWriteDeadline(time.Time) error }
)

type streamConn struct {
	r    io.ReadCloser
	w    io.WriteCloser
	read func([]byte) (int, error)

	write         func([]byte) (int, error)
	readDeadline  func(time.Time) error
	writeDeadline func(time.Time) error

	closed    chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func (c *streamConn) Read(b []byte) (int, error)  { return c.read(b) }
func (c *streamConn) Write(b []byte) (int, error) { return c.write(b) }

// Close closes both sides and ends any read or write waiting on one. It is safe to call twice.
func (c *streamConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.closeErr = errors.Join(c.w.Close(), c.r.Close())
	})
	return c.closeErr
}

func (c *streamConn) LocalAddr() net.Addr  { return streamAddr{} }
func (c *streamConn) RemoteAddr() net.Addr { return streamAddr{} }

func (c *streamConn) SetDeadline(t time.Time) error {
	return errors.Join(c.readDeadline(t), c.writeDeadline(t))
}
func (c *streamConn) SetReadDeadline(t time.Time) error  { return c.readDeadline(t) }
func (c *streamConn) SetWriteDeadline(t time.Time) error { return c.writeDeadline(t) }

type streamAddr struct{}

func (streamAddr) Network() string { return "stream" }
func (streamAddr) String() string  { return "stream" }

// watchedDeadline is a deadline a waiter can be told has moved.
type watchedDeadline struct {
	mu      sync.Mutex
	at      time.Time
	changed chan struct{}
}

func newWatchedDeadline() *watchedDeadline {
	return &watchedDeadline{changed: make(chan struct{})}
}

func (d *watchedDeadline) set(t time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.at = t
	close(d.changed)
	d.changed = make(chan struct{})
	return nil
}

// arm is the deadline as of now: already past, or a timer for it (nil when there is none) and the
// channel that closes if it moves. stop releases the timer.
func (d *watchedDeadline) arm() (past bool, fired <-chan time.Time, moved <-chan struct{}, stop func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	stop = func() {}
	if !d.at.IsZero() {
		wait := time.Until(d.at)
		if wait <= 0 {
			return true, nil, d.changed, stop
		}
		t := time.NewTimer(wait)
		fired, stop = t.C, func() { t.Stop() }
	}
	return false, fired, d.changed, stop
}

// chunk is one read's bytes, or the error that ended the reading.
type chunk struct {
	data []byte
	err  error
}

// pumpedReader does the blocking reads on a goroutine. A chunk the pump has read waits in its send
// until a Read takes it, so a Read that gives up on its deadline leaves it there for the next one.
type pumpedReader struct {
	r      io.Reader
	closed <-chan struct{}
	chunks chan chunk
	dl     *watchedDeadline
	start  sync.Once

	mu   sync.Mutex // one Read at a time; guards held and err
	held []byte     // the rest of a chunk larger than the last Read's buffer
	err  error      // what ended the pump
}

func (p *pumpedReader) pump() {
	for {
		buf := make([]byte, 32<<10)
		n, err := p.r.Read(buf)
		if n > 0 && !p.send(chunk{data: buf[:n]}) {
			return
		}
		if err != nil {
			p.send(chunk{err: err})
			return
		}
	}
}

func (p *pumpedReader) send(c chunk) bool {
	select {
	case p.chunks <- c:
		return true
	case <-p.closed:
		return false
	}
}

func (p *pumpedReader) Read(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(b) == 0 {
		return 0, nil
	}
	p.start.Do(func() { go p.pump() })
	for {
		past, fired, moved, stop := p.dl.arm()
		if past {
			return 0, os.ErrDeadlineExceeded
		}
		if len(p.held) > 0 {
			stop()
			n := copy(b, p.held)
			p.held = p.held[n:]
			return n, nil
		}
		if p.err != nil {
			stop()
			return 0, p.err
		}
		select {
		case c := <-p.chunks:
			stop()
			if c.err != nil {
				p.err = c.err
				return 0, c.err
			}
			n := copy(b, c.data)
			p.held = c.data[n:]
			return n, nil
		case <-fired:
			return 0, os.ErrDeadlineExceeded
		case <-moved:
			stop()
		case <-p.closed:
			stop()
			return 0, net.ErrClosed
		}
	}
}

type writeResult struct {
	n   int
	err error
}

// pumpedWriter does the blocking writes on a goroutine, one at a time. A write that outlives its
// deadline can't be taken back, so it breaks the side for good.
type pumpedWriter struct {
	w       io.Writer
	closed  <-chan struct{}
	jobs    chan []byte
	results chan writeResult
	dl      *watchedDeadline
	start   sync.Once
	broken  atomic.Bool

	mu sync.Mutex // one Write at a time
}

func (p *pumpedWriter) pump() {
	for {
		select {
		case b := <-p.jobs:
			n, err := p.w.Write(b)
			p.results <- writeResult{n, err} // buffered: a Write that gave up doesn't hold this
		case <-p.closed:
			return
		}
	}
}

func (p *pumpedWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.broken.Load() {
		return 0, fmt.Errorf("an earlier write passed its deadline: %w", os.ErrDeadlineExceeded)
	}
	select {
	case <-p.closed:
		return 0, net.ErrClosed
	default:
	}
	p.start.Do(func() { go p.pump() })
	own := append([]byte(nil), b...) // the caller may reuse b once this returns, even on a timeout
	for handed := false; !handed; {
		// until the pump takes it, nothing was started, so a deadline here breaks nothing
		past, fired, moved, stop := p.dl.arm()
		if past {
			stop()
			return 0, os.ErrDeadlineExceeded
		}
		select {
		case p.jobs <- own:
			handed = true
		case <-fired:
			return 0, os.ErrDeadlineExceeded
		case <-moved:
		case <-p.closed:
			stop()
			return 0, net.ErrClosed
		}
		stop()
	}
	for {
		past, fired, moved, stop := p.dl.arm()
		if past {
			p.broken.Store(true)
			return 0, os.ErrDeadlineExceeded
		}
		select {
		case r := <-p.results:
			stop()
			return r.n, r.err
		case <-fired:
			p.broken.Store(true)
			return 0, os.ErrDeadlineExceeded
		case <-moved:
			stop()
		case <-p.closed:
			stop()
			return 0, net.ErrClosed
		}
	}
}
