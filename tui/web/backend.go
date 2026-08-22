// Package web renders an existing golib TUI in a browser (ADR-0009).
//
// It implements [tui.Backend] over a server-side cell grid: each Flush diff is
// applied to the grid, and the grid is emitted as HTML. The browser is a dumb
// surface — it displays cells and reports input. No Component, layout or widget
// code changes, which is the point: the Backend seam already existed, and this
// package is the test of whether it was drawn in the right place.
//
// # What this is for, and what it is not
//
// It exists so a user who can already reach a CLI server can drive its TUI from
// a browser without a second UI being written. It is deliberately not a web
// application: an app that wants a web front end should use a real front-end
// framework, and this package would be the wrong foundation for one.
//
// # The interesting part is not rendering
//
// Painting a character grid as HTML is straightforward. The parts that took the
// design work are the frame aggregate that survives a slow client without
// diverging ([framer]), the input contract that turns browser events into
// [tui.Event] values without inventing or duplicating them, and the session and
// authentication rules, since this exposes a terminal to a network.
package web

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/tui"
)

// Errors a caller can act on.
var (
	// ErrNotStarted is returned by Size before Start has completed: there is no
	// grid size until a client has reported one.
	ErrNotStarted = errors.New("web: backend not started")

	// ErrStopped is returned once Stop has run.
	ErrStopped = errors.New("web: backend stopped")

	// ErrNoClient means Start's context ended before any browser attached.
	ErrNoClient = errors.New("web: no client attached")

	// ErrPendingLimit means the unauthenticated waiting room is full. It is a
	// refusal with 1013 Try Again Later, not a queue: queueing would move the
	// unbounded waiting room one level down rather than remove it.
	ErrPendingLimit = errors.New("web: too many unauthenticated connections")

	// ErrEventOverflow means a client produced events faster than the App could
	// consume them. The transport closes the connection rather than growing the
	// queue: an un-coalesced channel is part of the Backend contract, so the
	// only honest options are backpressure or disconnection.
	ErrEventOverflow = errors.New("web: event queue overflow")
)

// Hello is what a client reports when it attaches (ADR-0009 §2.3, §2.6).
//
// Every capability the backend claims derives from this, because the alternative
// is flattering itself: a browser is not a terminal, and the profile must say so.
type Hello struct {
	// Cols and Rows are the client's MEASURED grid size.
	Cols, Rows int

	// Metrics are the measured font cell size in px.
	Metrics Metrics

	// Pointer reports whether the client has a pointing device. Mouse support is
	// TriYes only when this is true — an optimistic assumption is never reported
	// as support (ADR-0002 §2.2).
	Pointer bool

	// PrefersDark comes from the client's prefers-color-scheme.
	PrefersDark bool

	// FontAgreement reports that the client's width probe agreed with the
	// server's width calculation. It informs UnicodeCore and is never presented
	// as proof: a finite probe string cannot establish that every Unicode
	// grapheme agrees (§2.6).
	FontAgreement bool
}

// Grid bounds. A client chooses these numbers, so they cannot be trusted to be
// sane: the grid allocates Cols*Rows cells, and the product is what has to be
// bounded rather than either factor alone.
//
// 1000x500 is far past any real terminal (a 4K display at a 6px cell is roughly
// 640x180) and 200k cells is about 6 MB of Cell values per grid, of which a
// session holds up to three. Beyond these a client is not describing a window.
const (
	MaxCols  = 1000
	MaxRows  = 500
	MaxCells = 200_000
)

// ErrGridTooLarge means a client asked for a grid this server will not allocate.
var ErrGridTooLarge = errors.New("web: requested grid exceeds the configured bounds")

// validGrid checks a client-supplied size BEFORE anything is allocated.
//
// The multiplication is done on int64 and compared, rather than performed in int
// and checked afterwards: Cols*Rows in int overflows for large inputs and the
// product can come out small and positive, which is exactly how a bounds check
// gets passed by the value it was meant to stop.
func validGrid(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("%w: %dx%d is not a grid", ErrGridTooLarge, cols, rows)
	}
	if cols > MaxCols || rows > MaxRows {
		return fmt.Errorf("%w: %dx%d exceeds %dx%d", ErrGridTooLarge, cols, rows, MaxCols, MaxRows)
	}
	if int64(cols)*int64(rows) > MaxCells {
		return fmt.Errorf("%w: %dx%d is %d cells, limit %d",
			ErrGridTooLarge, cols, rows, int64(cols)*int64(rows), MaxCells)
	}
	return nil
}

// valid reports whether a hello is usable.
func (h Hello) valid() bool {
	return validGrid(h.Cols, h.Rows) == nil && h.Metrics.valid()
}

// Backend implements [tui.Backend] for one browser session.
//
// One session is one Backend is one [tui.App]. Sharing a Backend between
// sessions would mean sharing a grid, a cursor and an event stream, which is a
// different product (a shared-terminal multiplexer) and not this one.
type Backend struct {
	log       logger.Logger
	eventCap  int
	framer    *framer
	events    chan tui.Event
	attached  chan Hello // Start waits here for the first client
	attachOne sync.Once
	stopOnce  sync.Once
	done      chan struct{}

	// sendMu serializes event producers against the sole closer.
	//
	// A done-channel check is NOT sufficient: a producer can pass it and then be
	// descheduled, and Stop closes the channel underneath it — which is a data
	// race and then a send on a closed channel. Producers hold the read lock for
	// the duration of the send; Stop takes the write lock, so no send can be in
	// flight while the close happens.
	sendMu sync.RWMutex
	closed bool

	// resizeMu makes a resize an ORDERED TRANSITION rather than two steps.
	//
	// Submitting the event and mutating the grid are separate operations, so an
	// App could dequeue the ResizeEvent and call Size() in the window between
	// them — and get the OLD size, which is the exact disagreement the submit-
	// first ordering was supposed to prevent (lector r2).
	//
	// Size AND Flush both take it. Flush matters at least as much: an App that
	// dequeues an expansion and paints at a coordinate valid in the NEW size
	// would otherwise have its cells applied to the old, smaller grid, which
	// drops them silently — the App's render is simply lost and the screen stays
	// blank there (lector r3). Serializing Flush makes "the event is visible" and
	// "the grid can accept the new coordinates" the same moment.
	resizeMu sync.Mutex

	// resizeGap runs between submitting the resize event and mutating the grid.
	//
	// A TEST SEAM, nil in production, and it exists because the property it
	// checks is otherwise only observable by luck: the window between the two
	// operations is a few instructions wide, so a test that races an observer
	// against it passes whether or not the lock is held. With the hook the
	// interleaving is forced and the assertion means something.
	resizeGap func()

	mu      sync.Mutex
	started bool
	caps    tui.Capabilities
	cursor  cursorState
	metrics Metrics
	err     error
}

// Option configures a Backend.
type Option func(*Backend)

// WithLogger sets the log sink. Defaults to logger.Nop{}.
func WithLogger(l logger.Logger) Option {
	return func(b *Backend) {
		if l != nil {
			b.log = l
		}
	}
}

// EventQueue sets the un-coalesced event queue depth (default 256).
//
// The queue is bounded because an unbounded one is a memory-exhaustion vector
// driven by whoever is typing. It is NOT coalesced, because coalescing is the
// App intake stage's job and doing it here would silently change the semantics
// every other backend provides (ADR-0005 §2.4).
func EventQueue(n int) Option {
	return func(b *Backend) {
		if n > 0 {
			b.eventCap = n
		}
	}
}

// DefaultEventQueue is the default event queue depth.
const DefaultEventQueue = 256

// New builds a Backend for one session.
//
// The grid starts at a placeholder size and is replaced by the client's measured
// size at attach: the server never guesses font metrics (§2.6).
func New(opts ...Option) *Backend {
	b := &Backend{
		log:      logger.Nop{},
		eventCap: DefaultEventQueue,
		framer:   newFramer(80, 24),
		attached: make(chan Hello, 1),
		done:     make(chan struct{}),
	}
	for _, o := range opts {
		if o != nil {
			o(b)
		}
	}
	b.events = make(chan tui.Event, b.eventCap)
	return b
}

// Start waits for a client to attach and resolves the capability profile from
// its hello.
//
// It blocks, which mirrors the terminal backend's probe fence: there is no
// device to acquire, but there is equally no grid size and no capability profile
// until a client has reported one, and returning early would hand the App a
// size the server invented. Cancelling ctx aborts the wait.
func (b *Backend) Start(ctx context.Context) error {
	select {
	case <-b.done:
		return ErrStopped
	case <-ctx.Done():
		return errors.Join(ErrNoClient, ctx.Err())
	case hello := <-b.attached:
		b.mu.Lock()
		b.started = true
		b.caps = capabilitiesFrom(hello)
		b.metrics = hello.Metrics
		b.mu.Unlock()
		b.framer.resize(hello.Cols, hello.Rows)
		logger.Info(b.log, sessionEvent{Kind: "start", Cols: hello.Cols, Rows: hello.Rows})
		return nil
	}
}

// capabilitiesFrom builds an HONEST profile (§2.3).
//
// Every field is either structurally true of this backend or reported by the
// client. Nothing is assumed: KittyKeyboard is false because a browser has no
// analogue, and Mouse is TriYes only on a client that says it has a pointer.
func capabilitiesFrom(h Hello) tui.Capabilities {
	mouse := tui.TriNo
	if h.Pointer {
		mouse = tui.TriYes
	}
	return tui.Capabilities{
		// The browser paints whatever hex we emit.
		ColorProfile: tui.ProfileTrueColor,
		// True by construction: this backend owns frame commit, so a frame is
		// atomic without needing a terminal mode for it.
		SyncOutput: true,
		// Paste arrives as a discrete browser event, so it is always bracketed
		// in the sense that matters — the App can tell it from typing.
		BracketedPaste: true,
		// Resize is reported in band on the same channel as everything else.
		InBandResize: true,
		// No browser analogue. An optimistic request is never reported as
		// support (ADR-0002 §2.2).
		KittyKeyboard: false,
		// Only on the client's confirmation, and even then §2.6 describes it
		// conservatively: the probe informs this bit, it does not prove it.
		UnicodeCore: h.FontAgreement,
		Mouse:       mouse,
		// No probe exists for styled underlines in a browser, and CSS
		// text-decoration-style is not the same feature.
		Undercurl:      false,
		DarkBackground: h.PrefersDark,
	}
}

// Stop tears the session down. Idempotent, and safe from a deferred
// panic-recovery path.
func (b *Backend) Stop() error {
	b.stopOnce.Do(func() {
		close(b.done)
		// Under the write lock, so no producer can be mid-send. Closing events
		// is part of the contract: the App loop distinguishes a clean shutdown
		// from a reader failure by checking Err after the channel closes.
		b.sendMu.Lock()
		b.closed = true
		close(b.events)
		b.sendMu.Unlock()
		logger.Info(b.log, sessionEvent{Kind: "stop"})
	})
	return nil
}

// Size reports the client's last measured grid size.
func (b *Backend) Size() (tui.Size, error) {
	b.mu.Lock()
	started := b.started
	b.mu.Unlock()
	if !started {
		return tui.Size{}, ErrNotStarted
	}
	// Serialized against Resize, so an App that dequeues a ResizeEvent and asks
	// for the size cannot observe the half-applied transition.
	b.resizeMu.Lock()
	defer b.resizeMu.Unlock()
	w, h := b.framer.size()
	return tui.Size{W: w, H: h}, nil
}

// Flush applies one frame's diff and latched cursor state, then publishes.
//
// It never blocks on network I/O and never waits for a client. The App loop
// calls this once per frame and ADR-0003's one-write rule assumes it is fast; a
// slow or vanished browser must not stall the UI (§2.4).
func (b *Backend) Flush(diff []tui.CellUpdate) error {
	select {
	case <-b.done:
		return ErrStopped
	default:
	}
	b.mu.Lock()
	cursor := b.cursor
	b.mu.Unlock()
	// Serialized against Resize: see resizeMu. Without this a render that
	// followed a dequeued expansion landed on the pre-resize grid and its cells
	// were dropped.
	b.resizeMu.Lock()
	defer b.resizeMu.Unlock()
	b.framer.publish(diff, cursor)
	return nil
}

// Cursor state is LATCHED, never immediate: it is recorded here and emitted with
// the next frame, so a frame remains one atomic update (§2.2).

// ShowCursor latches the cursor visible.
func (b *Backend) ShowCursor() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cursor.Visible = true
}

// HideCursor latches the cursor hidden.
func (b *Backend) HideCursor() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cursor.Visible = false
}

// SetCursor latches the cursor position.
func (b *Backend) SetCursor(x, y int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cursor.X, b.cursor.Y = x, y
}

// SetCursorShape latches the cursor shape.
func (b *Backend) SetCursorShape(s tui.CursorShape) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cursor.Shape = s
}

// Capabilities reports the negotiated profile. Constant after Start.
func (b *Backend) Capabilities() tui.Capabilities {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.caps
}

// Events is the single, ordered, UN-COALESCED event source, closed by Stop.
func (b *Backend) Events() <-chan tui.Event { return b.events }

// Err reports the transport error that ended the session; nil after a clean
// Stop.
func (b *Backend) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

// --- the transport's side of the backend ------------------------------------

// Attach binds a client to this backend. The FIRST attach releases Start.
//
// A second attach to a live session is refused rather than silently taking over:
// session takeover is an authorization decision, not a transport convenience,
// and answering it here would mean whoever connects last wins.
func (b *Backend) Attach(h Hello) error {
	if !h.valid() {
		return errors.New("web: client hello has no usable size or font metrics")
	}
	select {
	case <-b.done:
		return ErrStopped
	default:
	}
	var first bool
	b.attachOne.Do(func() {
		first = true
		b.attached <- h
	})
	if first {
		return nil
	}
	// A reconnect of the SAME session: resize to whatever the new client
	// measured and resync from scratch, since it holds nothing.
	b.mu.Lock()
	b.metrics = h.Metrics
	b.mu.Unlock()
	if err := b.Resize(h.Cols, h.Rows); err != nil {
		return err
	}
	b.framer.reset()
	logger.Info(b.log, sessionEvent{Kind: "reattach", Cols: h.Cols, Rows: h.Rows})
	return nil
}

// Detach records that the client went away. The session stays alive so a
// reconnect can resync; eviction is the session manager's decision.
func (b *Backend) Detach() {
	b.framer.reset()
	logger.Info(b.log, sessionEvent{Kind: "detach"})
}

// Resize records a client-reported size change and emits the event.
//
// The grid is NOT mutated unless the event can also be delivered. Changing the
// server's idea of the size while the App still believes the old one leaves the
// two disagreeing with no mechanism to notice — the App lays out for a size that
// no longer exists and every subsequent frame is wrong.
func (b *Backend) Resize(cols, rows int) error {
	if err := validGrid(cols, rows); err != nil {
		return err
	}
	b.resizeMu.Lock()
	defer b.resizeMu.Unlock()
	if err := b.Submit(tui.ResizeEvent{W: cols, H: rows}); err != nil {
		return err
	}
	if b.resizeGap != nil {
		b.resizeGap()
	}
	b.framer.resize(cols, rows)
	return nil
}

// Submit queues one decoded event.
//
// Returns [ErrEventOverflow] when the queue is full, and the caller closes the
// connection. It does NOT drop the event and it does not coalesce: the Backend
// contract promises an ordered un-coalesced stream, so silently discarding one
// would make this backend behave differently from every other, and coalescing
// here would take a decision that belongs to the App's intake stage.
func (b *Backend) Submit(ev tui.Event) error {
	// The read lock spans the closed check AND the send, which is what makes the
	// pair atomic with respect to Stop.
	b.sendMu.RLock()
	defer b.sendMu.RUnlock()
	if b.closed {
		return ErrStopped
	}
	select {
	case b.events <- ev:
		return nil
	default:
		return ErrEventOverflow
	}
}

// AckFrame records the client's acknowledgement of a revision.
func (b *Backend) AckFrame(rev uint64) { b.framer.ack(rev) }

// NextFrame returns the frame to send, if any.
func (b *Backend) NextFrame() (Frame, bool) { return b.framer.next() }

// PendingFrame reports whether a frame is waiting to go out.
func (b *Backend) PendingFrame() bool { return b.framer.pending() }

// Fail records the transport error that ended the session, then stops.
func (b *Backend) Fail(err error) {
	if err == nil {
		_ = b.Stop()
		return
	}
	b.mu.Lock()
	if b.err == nil {
		b.err = err
	}
	b.mu.Unlock()
	logger.Warning(b.log, err, sessionEvent{Kind: "fail"})
	_ = b.Stop()
}

// Metrics reports the client's measured font metrics.
func (b *Backend) Metrics() Metrics {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.metrics
}

// sessionEvent is the logged session lifecycle record. It carries no user
// content — never a keystroke, never cell text.
type sessionEvent struct {
	Kind       string
	Cols, Rows int
}

func (s sessionEvent) String() string {
	out := "web session " + s.Kind
	if s.Cols > 0 {
		out += " size=" + itoa(s.Cols) + "x" + itoa(s.Rows)
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// compile-time proof this satisfies the seam it exists to test
var _ tui.Backend = (*Backend)(nil)
