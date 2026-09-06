package tui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/errs"
)

// TestBackend is the deterministic, PTY-free in-memory Backend, in the shape
// Ratatui's TestBackend and tcell's SimulationScreen use. Start and Stop only
// manage the event channel; Flush applies the diff to the grid, records the
// latched cursor state, and increments the flush counter.
//
// It exists so a full interaction script — keys in, rendered grid out — can
// run in CI with no PTY and no timing. That is what makes TUI behaviour
// testable at all: with a real terminal there is nothing to assert against
// except bytes, and nothing to make deterministic.
//
// Flush PANICS if a diff would leave an ORPHANED WIDE-CELL HALF: a double-width
// grapheme occupies two columns, and a diff that writes one of them without
// the other leaves a grid that no terminal can render coherently. In a test
// binary a panic is the right answer — it is a test failure carrying the
// offending coordinate, where a silently repaired grid would let the bug
// reach a real terminal.
type TestBackend struct {
	mu sync.Mutex

	w, h int
	grid [][]Cell

	caps    Capabilities
	events  chan Event
	err     error
	started bool
	stopped bool
	stop    sync.Once

	// Latched cursor state (recorded by the cursor ops)…
	latchX, latchY int
	latchVisible   bool
	latchShape     CursorShape
	// …and the applied state the last Flush emitted.
	curX, curY int
	curVisible bool
	curShape   CursorShape

	flushes int

	clipboard []byte // last WriteClipboard payload (tui.ClipboardWriter)

	violations        []ConstraintViolation
	violationsDropped int
}

// WriteClipboard implements ClipboardWriter: records the payload for
// assertion via Clipboard().
func (b *TestBackend) WriteClipboard(p []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return fmt.Errorf("tui: TestBackend.WriteClipboard after Stop (%w)", errs.ErrClosed)
	}
	b.clipboard = append(b.clipboard[:0], p...)
	return nil
}

// Clipboard returns the last WriteClipboard payload (nil if none).
func (b *TestBackend) Clipboard() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.clipboard...)
}

var _ Backend = (*TestBackend)(nil)

// testBackendConfig collects the option-set state for NewTestBackend.
type testBackendConfig struct {
	caps     Capabilities
	eventBuf int
}

// TestBackendOption customizes a TestBackend under construction.
type TestBackendOption func(*testBackendConfig)

// WithTestCapabilities overrides the reported capability profile. Default:
// everything on.
func WithTestCapabilities(c Capabilities) TestBackendOption {
	return func(cfg *testBackendConfig) { cfg.caps = c }
}

// WithTestEventBuffer sets the Events() channel buffer (default 1024).
// Inject returns an error when a script exceeds it (fail loud) rather than
// blocking the test goroutine.
func WithTestEventBuffer(n int) TestBackendOption {
	return func(cfg *testBackendConfig) { cfg.eventBuf = n }
}

// fullCapabilities is the TestBackend default: everything on.
func fullCapabilities() Capabilities {
	return Capabilities{
		ColorProfile:   ProfileTrueColor,
		KittyKeyboard:  true,
		SyncOutput:     true,
		InBandResize:   true,
		UnicodeCore:    true,
		BracketedPaste: true,
		Mouse:          TriYes,
		Undercurl:      true,
		DarkBackground: true,
		DefaultFG:      ProbedColor{R: 229, G: 229, B: 229, Known: true},
		DefaultBG:      ProbedColor{Known: true},
	}
}

// NewTestBackend builds a w×h in-memory terminal with a blank grid.
func NewTestBackend(w, h int, opts ...TestBackendOption) *TestBackend {
	cfg := testBackendConfig{caps: fullCapabilities(), eventBuf: 1024}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.eventBuf < 0 {
		cfg.eventBuf = 0
	}
	b := &TestBackend{
		caps:   cfg.caps,
		events: make(chan Event, cfg.eventBuf),
	}
	b.setGridLocked(w, h)
	return b
}

// setGridLocked (re)allocates the grid as w×h blank cells.
func (b *TestBackend) setGridLocked(w, h int) {
	w, h = max(w, 0), max(h, 0)
	b.w, b.h = w, h
	b.grid = make([][]Cell, h)
	for y := range b.grid {
		row := make([]Cell, w)
		for x := range row {
			row[x] = blankCell
		}
		b.grid[y] = row
	}
}

// Start implements Backend: it only marks the backend started — a
// TestBackend has no device to acquire.
func (b *TestBackend) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped {
		return fmt.Errorf("tui: TestBackend is stopped (%w)", errs.ErrClosed)
	}
	b.started = true
	return nil
}

// Stop implements Backend: idempotent (sync.Once); closes Events().
func (b *TestBackend) Stop() error {
	b.stop.Do(func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.stopped = true
		close(b.events)
	})
	return nil
}

// Size implements Backend.
func (b *TestBackend) Size() (Size, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return Size{W: b.w, H: b.h}, nil
}

// Capabilities implements Backend. Constant after construction.
func (b *TestBackend) Capabilities() Capabilities { return b.caps }

// Events implements Backend.
func (b *TestBackend) Events() <-chan Event { return b.events }

// Err implements Backend: nil after a clean Stop, or the error scripted via
// SetErr. Scripting it is how a test drives the loop's reader-failure paths,
// which are otherwise only reachable by breaking a real terminal.
func (b *TestBackend) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.err
}

// SetErr scripts the terminal error surfaced by Err() after the channel
// closes.
func (b *TestBackend) SetErr(err error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.err = err
}

// Inject enqueues events onto the Events() channel in call order (scripted
// input). It returns an error when the script exceeds the configured buffer
// (WithTestEventBuffer, default 1024) instead of blocking the test
// goroutine, and when the backend is already stopped — fail loud.
func (b *TestBackend) Inject(evs ...Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.injectLocked(evs...)
}

func (b *TestBackend) injectLocked(evs ...Event) error {
	if b.stopped {
		return fmt.Errorf("tui: TestBackend.Inject after Stop (%w)", errs.ErrClosed)
	}
	for i, ev := range evs {
		select {
		case b.events <- ev:
		default:
			return fmt.Errorf("tui: TestBackend event buffer full (cap %d) — injected %d of %d events; raise WithTestEventBuffer or drain Events()",
				cap(b.events), i, len(evs))
		}
	}
	return nil
}

// InjectResize resizes the grid, invalidates it (fresh blank cells), and
// posts a ResizeEvent — exactly the externally observable behavior of a
// real resize. It panics if the event buffer is full (fail loud; a resize
// script that overflows the buffer is a test bug).
func (b *TestBackend) InjectResize(w, h int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.setGridLocked(w, h)
	if err := b.injectLocked(ResizeEvent{W: b.w, H: b.h}); err != nil {
		panic("tui: TestBackend.InjectResize: " + err.Error())
	}
}

// Cursor ops implement Backend's latched cursor contract: they record
// desired state which the next Flush applies.

// ShowCursor latches the cursor visible.
func (b *TestBackend) ShowCursor() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.latchVisible = true
}

// HideCursor latches the cursor hidden.
func (b *TestBackend) HideCursor() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.latchVisible = false
}

// SetCursor latches the cursor position.
func (b *TestBackend) SetCursor(x, y int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.latchX, b.latchY = x, y
}

// SetCursorShape latches the cursor shape.
func (b *TestBackend) SetCursorShape(s CursorShape) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.latchShape = s
}

// Flush implements Backend: applies the diff to the grid structurally (byte
// economy is a term-emitter concern), records the latched cursor state as
// applied, and increments the flush counter. A width-2 head update covers
// its continuation cell. Flush panics — with the coordinate — on an
// out-of-range update or a diff that leaves an orphaned wide-cell half.
func (b *TestBackend) Flush(diff []CellUpdate) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, u := range diff {
		if u.X < 0 || u.Y < 0 || u.X >= b.w || u.Y >= b.h {
			panic(fmt.Sprintf("tui: TestBackend.Flush: update outside the %dx%d grid at (%d, %d)", b.w, b.h, u.X, u.Y))
		}
		if u.Cell.Width == 2 && u.X+1 >= b.w {
			panic(fmt.Sprintf("tui: TestBackend.Flush: wide cell in the last column at (%d, %d) — W3 violation", u.X, u.Y))
		}
		b.grid[u.Y][u.X] = u.Cell
		if u.Cell.Width == 2 {
			b.grid[u.Y][u.X+1] = Cell{Content: "", Width: 0, Attrs: u.Cell.Attrs}
		}
	}
	b.assertNoOrphansLocked()
	b.curX, b.curY = b.latchX, b.latchY
	b.curVisible = b.latchVisible
	b.curShape = b.latchShape
	b.flushes++
	return nil
}

// assertNoOrphansLocked re-asserts the wide-cell invariants over the whole
// grid after every applied diff: every continuation has a width-2 head
// immediately left; every width-2 head has a continuation immediately
// right.
func (b *TestBackend) assertNoOrphansLocked() {
	for y, row := range b.grid {
		for x, c := range row {
			switch {
			case c.Continuation() && (x == 0 || row[x-1].Width != 2):
				panic(fmt.Sprintf("tui: TestBackend.Flush: orphaned wide-cell continuation at (%d, %d) — no width-2 head to its left (W1)", x, y))
			case c.Width == 2 && (x+1 >= b.w || !row[x+1].Continuation()):
				panic(fmt.Sprintf("tui: TestBackend.Flush: wide-cell head at (%d, %d) without its continuation (W1/W3)", x, y))
			}
		}
	}
}

// Snapshot returns a deep copy of the grid.
func (b *TestBackend) Snapshot() [][]Cell {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([][]Cell, len(b.grid))
	for y, row := range b.grid {
		out[y] = append([]Cell(nil), row...)
	}
	return out
}

// String returns the grid as text, one row per line. A wide cell's cluster
// stands for both of its columns; continuation cells emit nothing.
func (b *TestBackend) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var sb strings.Builder
	for y, row := range b.grid {
		if y > 0 {
			sb.WriteByte('\n')
		}
		for _, c := range row {
			if c.Continuation() {
				continue
			}
			if c.Content == "" {
				sb.WriteByte(' ')
				continue
			}
			sb.WriteString(c.Content)
		}
	}
	return sb.String()
}

// CursorPos reports the cursor state the last Flush applied.
func (b *TestBackend) CursorPos() (x, y int, visible bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.curX, b.curY, b.curVisible
}

// CursorShape reports the cursor shape the last Flush applied.
func (b *TestBackend) CursorShape() CursorShape {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.curShape
}

// Flushes returns the Flush count, so a test can assert HOW MANY writes a
// sequence produced rather than only what was drawn. That is the one-write
// rule's evidence: a frame is meant to reach the terminal as a single
// buffered write, and a count that grows faster than the frames did means
// something is flushing per change instead of per frame.
func (b *TestBackend) Flushes() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.flushes
}

// maxViolations bounds the retained ConstraintViolation records per run;
// beyond it violations are counted but not stored. A component that clamps on
// every frame would otherwise grow this slice without limit for the length of
// the run, and the first few records are what a reader needs anyway.
const maxViolations = 1024

// RecordConstraintViolation retains one clamped Layout return for later
// assertion. The framework calls it whenever a component returns a Size
// outside the Constraints it was given and the tree clamps the answer.
//
// Clamping is deliberately silent at runtime — a misbehaving widget must not
// corrupt its siblings' geometry — so without this record a layout bug would
// be invisible.
func (b *TestBackend) RecordConstraintViolation(v ConstraintViolation) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.violations) >= maxViolations {
		b.violationsDropped++
		return
	}
	b.violations = append(b.violations, v)
}

// ConstraintViolations returns the violations recorded this run.
func (b *TestBackend) ConstraintViolations() []ConstraintViolation {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]ConstraintViolation(nil), b.violations...)
}

// FailOnViolations fails t (with one line per violation) when the run
// clamped anything — widget test suites call it in a helper/cleanup so
// silent clamps cannot ship.
func FailOnViolations(t *testing.T, b *TestBackend) {
	t.Helper()
	b.mu.Lock()
	violations := append([]ConstraintViolation(nil), b.violations...)
	dropped := b.violationsDropped
	b.mu.Unlock()
	for _, v := range violations {
		t.Errorf("tui: constraint violation: node %d (%s) returned %+v under %+v (clamped)", v.Node, v.Type, v.Got, v.C)
	}
	if dropped > 0 {
		t.Errorf("tui: %d further constraint violations dropped (bound %d)", dropped, maxViolations)
	}
}
