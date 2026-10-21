package term

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
)

// Backend is the concrete ANSI terminal implementation of tui.Backend.
// Construct it with Open; all terminal state changes happen in
// Start, so a constructed-but-unstarted Backend is inert.
type Backend struct {
	cfg config

	// Real terminal fds (nil under the pty-less test harness).
	inFile, outFile *os.File
	input           io.Reader
	output          io.Writer

	caps tui.Capabilities

	events   chan tui.Event
	probeCh  chan probeReply
	resizeCh chan struct{}
	done     chan struct{}

	probing atomic.Bool
	kittyOn atomic.Bool
	started atomic.Bool
	stopped atomic.Bool

	stopOnce sync.Once
	stopErr  error
	wg       sync.WaitGroup
	readerOn bool

	errMu   sync.Mutex
	readErr error

	// Acquired-state flags for teardown, set on the Start goroutine and
	// consumed by teardown.
	restoreIn   func() error
	restoreOut  func() error
	pollCleanup func() error // undo makePollable, after the reader joins
	altEntered  bool
	enabled     []vtMode // the modes Start turned on, in order; teardown turns them off in reverse
	kittyPush   bool

	// The output side (emitter.go), guarded by wmu together with output writes.
	wmu sync.Mutex
	em  emitter

	// The Backend contract's latches (tui.CursorLatch, tui.ImageLatch), guarded by cmu: the cursor
	// calls and the image placements and deletes (graphics.go) the next Flush takes.
	cmu    sync.Mutex
	cursor tui.CursorLatch
	images tui.ImageLatch
}

var _ tui.Backend = (*Backend)(nil)

// Open validates the TTY and builds the backend WITHOUT touching terminal
// state; all mode changes happen in Start so a constructed-but-unstarted
// backend is inert.
func Open(opts ...Option) (*Backend, error) {
	cfg := defaultConfig()
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.in == nil || cfg.out == nil {
		return nil, ErrNotTerminal
	}
	if !isTerminal(cfg.in) || !isTerminal(cfg.out) {
		return nil, ErrNotTerminal
	}
	b := newBackend(cfg)
	b.inFile, b.outFile = cfg.in, cfg.out
	b.input, b.output = cfg.in, cfg.out
	return b, nil
}

// newBackend builds the backend core shared by Open and the test harness.
func newBackend(cfg config) *Backend {
	cfg.normalize()
	return &Backend{
		cfg:      cfg,
		events:   make(chan tui.Event, eventBufferSize),
		probeCh:  make(chan probeReply, 32),
		resizeCh: make(chan struct{}, 1),
		done:     make(chan struct{}),
		em:       newEmitter(inTmux(cfg.env)),
	}
}

// newHarness builds a Backend over an arbitrary reader/writer pair — the
// pty-less test seam. No raw mode, no fd queries.
func newHarness(in io.Reader, out io.Writer, opts ...Option) *Backend {
	cfg := defaultConfig()
	cfg.in, cfg.out = nil, nil
	for _, o := range opts {
		o(&cfg)
	}
	b := newBackend(cfg)
	b.input, b.output = in, out
	return b
}

// Start acquires the device: raw mode (and Windows output VT
// modes), alternate screen, the reader goroutine, the capability probe, and
// negotiated mode enablement — each step's undo is recorded before the step
// runs, so a failure mid-Start restores exactly what was acquired.
func (b *Backend) Start(ctx context.Context) error {
	if b.stopped.Load() {
		return ErrClosed
	}
	if b.started.Swap(true) {
		return fmt.Errorf("term: Start called twice (%w)", errs.ErrPrecondition)
	}
	if err := b.start(ctx); err != nil {
		if stopErr := b.Stop(); stopErr != nil {
			return errors.Join(err, stopErr)
		}
		return err
	}
	return nil
}

func (b *Backend) start(ctx context.Context) error {
	// 1. Raw mode; on Windows additionally stdout VT processing.
	if b.inFile != nil {
		restore, err := makeRaw(b.inFile)
		if err != nil {
			return err
		}
		b.restoreIn = restore
		restore, err = enableOutputVT(b.outFile)
		if err != nil {
			return err
		}
		b.restoreOut = restore
		// The read-deadline unblock needs a poller-managed fd, and an
		// inherited tty arrives blocking. After makeRaw (whose
		// Fd() call forces blocking mode), swap in a pollable handle;
		// nothing may call Fd() on it from here on.
		in, cleanup := makePollable(b.inFile)
		b.inFile = in
		b.pollCleanup = cleanup
	}

	// 2. Alternate screen (unless inline mode).
	if b.cfg.altScreen {
		if err := b.write([]byte("\x1b[?1049h")); err != nil {
			return err
		}
		b.altEntered = true
	}

	// 3. Reader goroutines — running during the probe so replies are
	// consumed while early user input is queued, not lost.
	b.startReader()

	// 4. The capability probe. Cancellation discards partial
	// replies: caps is only assigned on success, so a partially-negotiated
	// profile is never observable.
	caps, err := b.runProbe(ctx)
	if err != nil {
		return err
	}
	b.caps = caps

	// 5. Negotiated mode enablement — one write.
	var enable bytes.Buffer
	if caps.BracketedPaste {
		b.enable(&enable, modePaste)
	}
	if b.cfg.mouse && caps.Mouse != tui.TriNo {
		// The enable may be attempted on TriUnknown; Capabilities keeps
		// reporting TriUnknown in that case — capability honesty.
		b.enable(&enable, modeMouse)
	}
	if caps.KittyKeyboard {
		enable.WriteString("\x1b[>3u") // push flags 1+2
		b.kittyPush = true
		b.kittyOn.Store(true)
	}
	if caps.InBandResize {
		b.enable(&enable, modeResize)
	}
	// Focus reporting: required to deliver tui.FocusEvent (Terminal=true)
	// and for the focus-in size re-check. Harmless where unsupported.
	b.enable(&enable, modeFocus)
	if err := b.write(enable.Bytes()); err != nil {
		return err
	}

	// 6. OS resize notifications — real terminals only; the harness
	// exercises resize via mode-2048 reports and scripted events.
	if b.inFile != nil {
		b.startResizeWatcher()
	}
	return nil
}

// vtMode is a terminal mode Start may turn on: the sequence that turns it on, and the one that
// turns it off. Adding a mode is a value here and one enable call; teardown follows the list.
type vtMode struct{ on, off string }

var (
	modePaste  = vtMode{"\x1b[?2004h", "\x1b[?2004l"}                       // bracketed paste
	modeMouse  = vtMode{"\x1b[?1002h\x1b[?1006h", "\x1b[?1006l\x1b[?1002l"} // button-event tracking, SGR reports
	modeResize = vtMode{"\x1b[?2048h", "\x1b[?2048l"}                       // in-band resize reports
	modeFocus  = vtMode{"\x1b[?1004h", "\x1b[?1004l"}                       // focus reports
)

// enable writes m's on sequence into buf and records m for teardown.
func (b *Backend) enable(buf *bytes.Buffer, m vtMode) {
	buf.WriteString(m.on)
	b.enabled = append(b.enabled, m)
}

// Stop restores the terminal completely and stops the reader goroutine.
// Idempotent (sync.Once); safe from deferred panic-recovery paths. After
// Stop returns, Events() is closed.
func (b *Backend) Stop() error {
	b.stopOnce.Do(func() { b.stopErr = b.teardown() })
	return b.stopErr
}

// teardown restores in reverse order of acquisition, best-effort on every
// step, joining failures.
func (b *Backend) teardown() error {
	b.stopped.Store(true)
	var errs []error

	if b.started.Load() {
		// Steps 1–4, emitted as one final write: kitty pop, mode disables,
		// cursor restore (default shape, show, SGR reset), leave alt screen.
		var buf bytes.Buffer
		if b.kittyPush {
			buf.WriteString("\x1b[<u") // a keyboard-stack push, popped before the modes go off
		}
		for i := len(b.enabled) - 1; i >= 0; i-- {
			buf.WriteString(b.enabled[i].off)
		}
		b.wmu.Lock()
		b.em.restore(&buf) // the cursor's own colour, and this program's images deleted
		b.wmu.Unlock()
		buf.WriteString("\x1b[0 q\x1b[?25h\x1b[m")
		if b.altEntered {
			buf.WriteString("\x1b[?1049l")
		}
		if err := b.write(buf.Bytes()); err != nil {
			errs = append(errs, err)
		}
	}

	// Restore saved console/termios modes.
	if b.restoreOut != nil {
		if err := b.restoreOut(); err != nil {
			errs = append(errs, err)
		}
	}
	if b.restoreIn != nil {
		if err := b.restoreIn(); err != nil {
			errs = append(errs, err)
		}
	}

	// Stop the reader: close done, unblock the fd read, wait, then the
	// decode goroutine closes Events() — only the reader's owner closes the
	// channel, and only after the reader has exited.
	close(b.done)
	b.unblockReader()
	b.wg.Wait()
	if !b.readerOn {
		close(b.events) // reader never started; contract still holds
	}
	// Undo makePollable only after the reader has joined: close the
	// private /dev/tty description, or restore O_NONBLOCK on a shared
	// one.
	if b.pollCleanup != nil {
		if err := b.pollCleanup(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Size reports the current cell-grid size, freshly queried.
func (b *Backend) Size() (tui.Size, error) {
	if b.stopped.Load() {
		return tui.Size{}, ErrClosed
	}
	return b.querySize()
}

func (b *Backend) querySize() (tui.Size, error) {
	if b.cfg.sizeFn != nil {
		return b.cfg.sizeFn()
	}
	if b.outFile == nil {
		return tui.Size{W: 80, H: 24}, nil
	}
	return fdSize(b.outFile)
}

// Capabilities reports the negotiated profile. Constant after Start.
func (b *Backend) Capabilities() tui.Capabilities { return b.caps }

// Events is the single, ordered, un-coalesced event source.
// Closed by Stop, and on abnormal reader exit.
func (b *Backend) Events() <-chan tui.Event { return b.events }

// Err reports the terminal error that stopped the reader. Valid once
// Events() is closed or Stop has returned; nil after a clean Stop.
func (b *Backend) Err() error {
	b.errMu.Lock()
	defer b.errMu.Unlock()
	return b.readErr
}

func (b *Backend) setReadErr(err error) {
	b.errMu.Lock()
	if b.readErr == nil {
		b.readErr = err
	}
	b.errMu.Unlock()
}

// write performs one guarded, complete write to the output.
func (b *Backend) write(p []byte) error {
	if len(p) == 0 {
		return nil
	}
	b.wmu.Lock()
	defer b.wmu.Unlock()
	return b.writeAll(p)
}

// WriteClipboard sets the system clipboard via OSC 52 (tui.ClipboardWriter).
// The terminal itself performs the copy, so this works over SSH and inside
// multiplexers/editor terminals that pass OSC 52 through. Terminals commonly
// cap the sequence length (tmux defaults near 100KB), so oversized payloads
// keep their TAIL — for the log-pane use case the most recent lines are the
// ones worth keeping.
func (b *Backend) WriteClipboard(p []byte) error {
	if b.stopped.Load() {
		return ErrClosed
	}
	const maxRaw = 72_000 // base64 expands 4/3 → ~96KB sequence, under common caps
	if len(p) > maxRaw {
		p = p[len(p)-maxRaw:]
	}
	seq := make([]byte, 0, base64.StdEncoding.EncodedLen(len(p))+16)
	seq = append(seq, "\x1b]52;c;"...)
	seq = base64.StdEncoding.AppendEncode(seq, p)
	seq = append(seq, "\x1b\\"...)
	return b.write(seq)
}

// writeAll writes all of p, tolerating short writes and — on a non-blocking
// tty — EAGAIN. A frame is emitted as one Write; when the
// output description is non-blocking (makePollable flips O_NONBLOCK on a
// stdin/stdout pair that share one open-file description) the kernel accepts
// only what fits its buffer and returns a short count with EAGAIN. Dropping
// the remainder left the frame half-painted with no repaint — the "only the
// top of the screen renders" bug. Callers hold wmu.
func (b *Backend) writeAll(p []byte) error {
	for len(p) > 0 {
		n, err := b.output.Write(p)
		if n > 0 {
			p = p[n:]
		}
		if err == nil {
			continue
		}
		// A non-blocking fd with bytes still pending: wait for writability
		// (not a busy spin) and resume. Any other error is real.
		if len(p) > 0 && (errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK)) && b.outFile != nil {
			if werr := waitWritable(b.outFile); werr != nil {
				return werr
			}
			continue
		}
		return err
	}
	return nil
}
