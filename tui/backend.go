package tui

import (
	"context"

	"github.com/yongjohnlee80/golib/tui/style"
)

// Backend is the primary driver seam between the TUI runtime and the underlying
// terminal display device.
//
// The App owns exactly one Backend for its lifetime. Backend provides a clean,
// portable abstraction consisting of four core responsibilities:
//
//  1. Device Lifecycle: Synchronous acquisition of raw mode, alternate screen buffer,
//     and terminal capability probing ([Backend.Start]), plus guaranteed teardown
//     and terminal restoration ([Backend.Stop]).
//  2. Event Stream: An ordered, un-coalesced stream of decoded terminal input events
//     (keystrokes, mouse actions, window resize, bracketed paste) via [Backend.Events].
//  3. Capability Negotiation: Live feature detection results ([Backend.Capabilities])
//     determining supported color depths, Kitty keyboard protocol, and synchronized output.
//  4. Frame Output: Latched hardware cursor positioning and atomic frame emission
//     via [Backend.Flush], which receives the row-major slice of dirty cell updates.
//
// In production, the ANSI driver in tui/term implements Backend. In tests, [TestBackend]
// provides a fully deterministic in-memory simulator that runs without a PTY.
type Backend interface {
	// Start acquires the device: raw mode, VT modes, alternate screen,
	// the capability probe, and the input reader goroutine. It blocks
	// until the probe fence resolves or the probe timeout expires
	// (bounded, 250ms by default), then returns. The bound matters: a
	// terminal that never answers a capability query must not hang startup,
	// so an unanswered probe times out safely (leaving capabilities like mouse
	// at TriUnknown for optimistic fallback) rather than hanging.
	// Cancelling ctx during the probe aborts it, discards partial
	// capability replies, restores the terminal, and returns ctx.Err().
	Start(ctx context.Context) error

	// Stop restores the terminal completely and stops the reader
	// goroutine. Idempotent (sync.Once). Safe from deferred
	// panic-recovery paths. After Stop returns, Events() is closed.
	Stop() error

	// Size reports the current cell-grid size.
	Size() (Size, error)

	// Flush applies one frame's cell diff plus any latched cursor-state
	// changes as a SINGLE buffered write (the one-write rule). An empty diff
	// with unchanged cursor state writes zero bytes. The diff is ordered
	// row-major by the caller.
	Flush(diff []CellUpdate) error

	// Cursor state is LATCHED, not immediate: these record desired state
	// which the next Flush emits, so a frame is always one write. CursorLatch
	// is the latch a backend holds for them.
	ShowCursor()
	HideCursor()
	SetCursor(x, y int)
	SetCursorShape(s CursorShape)

	// Capabilities reports the negotiated profile. Constant after Start.
	Capabilities() Capabilities

	// Events is the single, ordered, UN-COALESCED event source: key, mouse,
	// paste, resize, focus — the backend only decodes and emits. Fed by the
	// backend's one reader goroutine; closed by Stop. The App's intake stage
	// consumes it exclusively and owns ALL coalescing and overflow policy.
	Events() <-chan Event

	// Err reports the terminal error that stopped the reader or failed the
	// probe. Valid once Events() is closed or Stop has returned; nil after a
	// clean Stop. The App loop calls Err when Events() closes to distinguish
	// clean shutdown from reader failure.
	Err() error
}

// CellUpdate is one dirty cell in a frame diff, carrying the grapheme-cluster
// cell that belongs at that position.
type CellUpdate struct {
	X, Y int
	Cell Cell
}

// CursorColorSetter is a Backend that can colour the hardware cursor (a terminal's OSC 12). set
// false gives the cursor back its own colour (OSC 112). A backend without it leaves the cursor as
// the terminal draws it.
type CursorColorSetter interface {
	SetCursorColor(c CellColor, set bool)
}

// A FRAME'S SIZE. Two capabilities give a backend the size a frame was laid out at, the App's cell
// buffer for it; the App asserts both once (backendCaps) and uses one per flush:
//   - SizedFlusher, for a backend whose apply needs the size and the diff as one call (a renderer
//     whose screen is not a terminal's, as gui's is): FlushSized replaces Flush.
//   - FrameSizer, for one that keeps Flush and only needs to be told: FrameSize, then Flush.
//
// SizedFlusher takes precedence: a backend with both gets FlushSized alone, never FrameSize then
// Flush.

// SizedFlusher is a Backend that is handed each frame's size with its diff: one whose screen is
// not a terminal's (a plugin sending its frames to a host, say), and so must know the geometry a
// diff was drawn at. A resize can reach such a backend before the App has drawn at the new size; the
// size given here, not the backend's own Size, is the one the diff indexes. The App probes for it
// at every flush (renderFrame and the too-small screen alike); a backend without it gets Flush, once,
// with the same diff and the same latched cursor.
//
// size is the App's cell buffer for this frame. After a size change the buffer keeps no previous
// frame, so the first diff at a new size covers every cell (a wide cell's continuation covered by
// its head, as in Flush). diff is valid only during the call: it is the App's reused scratch, never
// to be kept.
type SizedFlusher interface {
	FlushSized(size Size, diff []CellUpdate) error
}

// FrameSizer is a Backend that is told each frame's size just before that frame's Flush: the App's
// cell buffer for it, the size the diff was laid out at. It is told beside Flush, never instead of
// it, so a wrapper that embeds such a backend and overrides Flush still sees every frame.
// TestBackend uses it to drop a frame drawn for a grid InjectResize has replaced, as a terminal
// clips a frame drawn for its old size. A backend that is also a SizedFlusher is never told: it gets
// FlushSized alone.
type FrameSizer interface {
	FrameSize(size Size)
}

// ViolationRecorder is a Backend that is told of every constraint violation: a component's Layout
// returning a size outside its constraints, which the App clamps (and logs) and goes on. A test
// backend records them so a suite can fail on a silent clamp (TestBackend, FailOnViolations); any
// diagnostic backend may. Called on the loop goroutine, during layout; it must only record.
type ViolationRecorder interface {
	RecordConstraintViolation(v ConstraintViolation)
}

// PointerResolver is a Backend whose pointer events are finished when the App dispatches them:
// a native window that lays its cells out in regions of its own (golib/gui's panes) can only map
// a position to cells against the layout the App routes with, and the App may lay out again
// between the backend reading an event and dispatching it (a resize is delivered ahead of queued
// input). The App calls ResolvePointer on the loop goroutine for every MouseEvent whose
// Source.Ref is set, before anything else looks at it: the too-small screen, click counting,
// capture and routing. It returns the event with X, Y, SubX and SubY set; the App then clears
// Source. A backend without it leaves such events as they were sent.
type PointerResolver interface {
	ResolvePointer(ev MouseEvent) MouseEvent
}

// CursorState is the hardware cursor as a Backend's cursor calls ask for it: where it is (PosSet
// once a position was given), whether it shows, its shape, and its colour (Colored false: the
// terminal's own, and Color is then zero).
type CursorState struct {
	X, Y    int
	PosSet  bool
	Visible bool
	Shape   CursorShape
	Color   CellColor
	Colored bool
}

// CursorLatch is the Backend contract's latched cursor, for a backend to hold as a field: the
// cursor calls (ShowCursor, HideCursor, SetCursor, SetCursorShape, CursorColorSetter's
// SetCursorColor) record the state wanted, and the next Flush takes it. It keeps what it is given:
// a backend that cannot show a value (a palette colour on a terminal that takes only RGB) decides
// that before it latches. It does not lock; the backend's own lock guards it. The zero value is a
// hidden cursor at no position, as a backend starts.
type CursorLatch struct {
	want, taken CursorState
}

// Show latches the cursor visible.
func (l *CursorLatch) Show() { l.want.Visible = true }

// Hide latches the cursor hidden.
func (l *CursorLatch) Hide() { l.want.Visible = false }

// SetPos latches the cursor's position.
func (l *CursorLatch) SetPos(x, y int) { l.want.X, l.want.Y, l.want.PosSet = x, y, true }

// SetShape latches the cursor's shape.
func (l *CursorLatch) SetShape(s CursorShape) { l.want.Shape = s }

// SetColor latches the cursor's colour, or with set false the terminal's own.
func (l *CursorLatch) SetColor(c CellColor, set bool) {
	if !set {
		c = CellColor{}
	}
	l.want.Color, l.want.Colored = c, set
}

// Take is the state a Flush applies, and whether it differs from the state the last Take gave: a
// change undone within the frame (shown, then hidden again) is no change, and a backend writes
// nothing for it.
func (l *CursorLatch) Take() (state CursorState, changed bool) {
	changed = l.want != l.taken
	l.taken = l.want
	return l.want, changed
}

// ImageOp is one latched image call: a placement (Place set) or the delete of image Delete.
type ImageOp struct {
	Place  *ImagePlacement
	Delete uint32
}

// ImageLatch is GraphicsBackend's latched images, for a backend to hold as a field: PlaceImage and
// DeleteImage record their calls, and the next Flush takes them, in the order made. It does not
// lock; the backend's own lock guards it.
type ImageLatch struct {
	ops []ImageOp
}

// Place latches a placement.
func (l *ImageLatch) Place(p ImagePlacement) { l.ops = append(l.ops, ImageOp{Place: &p}) }

// Delete latches the delete of image id.
func (l *ImageLatch) Delete(id uint32) { l.ops = append(l.ops, ImageOp{Delete: id}) }

// Take is the calls latched since the last Take, in the order made; nil when there were none.
func (l *ImageLatch) Take() []ImageOp {
	ops := l.ops
	l.ops = nil
	return ops
}

// CursorShape selects the hardware cursor glyph (DECSCUSR): style's, shared with vt.
type CursorShape = style.CursorShape

const (
	CursorShapeDefault   = style.CursorShapeDefault   // terminal's configured default
	CursorShapeBlock     = style.CursorShapeBlock     // DECSCUSR 1/2
	CursorShapeUnderline = style.CursorShapeUnderline // DECSCUSR 3/4
	CursorShapeBar       = style.CursorShapeBar       // DECSCUSR 5/6
)

// ColorProfile is the canonical color-capability tier, and style resolution
// resolves a style.Color against exactly this enum. It is the field that
// decides what a color becomes; the raw probed colors in Capabilities are
// supporting data that fed this answer, not inputs to it.
type ColorProfile uint8

const (
	ProfileMono      ColorProfile = iota // no color
	ProfileANSI16                        // the 16 base SGR colors
	ProfileANSI256                       // indexed 256 (SGR 38;5)
	ProfileTrueColor                     // 24-bit RGB (SGR 38;2)
)

// Tri is a three-valued capability answer for features whose support can be
// genuinely unknowable. An optimistic request is NEVER reported as support
// — capability honesty.
type Tri uint8

const (
	TriUnknown Tri = iota // requested/attempted; no verifiable answer
	TriNo                 // verifiably unsupported
	TriYes                // verifiably supported
)

// Capabilities is the negotiated feature profile of the attached terminal,
// resolved once during Start by LIVE PROBING — never from terminfo. A
// terminfo entry describes what a terminal type is supposed to do; the probe
// describes what the terminal actually in front of us answers, which is not
// always the same. Degradation is therefore per-capability, not per-$TERM: a
// terminal that lacks one feature keeps every other.
type Capabilities struct {
	ColorProfile   ColorProfile // env pre-seed + XTGETTCAP "RGB"
	KittyKeyboard  bool         // kitty progressive enhancement; flags 1+2 pushed when true
	SyncOutput     bool         // DEC private mode 2026 (synchronized output)
	InBandResize   bool         // mode 2048 in-band resize reports
	UnicodeCore    bool         // mode 2027 grapheme-cluster semantics
	BracketedPaste bool         // mode 2004
	Mouse          Tri          // SGR mouse — TriYes only on a verifiable DECRQM ?1006 answer
	Undercurl      bool         // XTGETTCAP "Smulx" (styled underlines)
	// KittyGraphics is kitty's graphics protocol, probed by its own query (a=q): TriYes only on
	// an OK reply, TriNo on an error reply or a DA1 fence that came first from the terminal
	// itself, TriUnknown otherwise (a multiplexer answers DA1 itself, so behind one the fence
	// proves nothing). Kitty KEYBOARD support is no evidence of it.
	KittyGraphics Tri

	// NativeViews: the backend draws native views over their cells (it is a NativeHost,
	// native.go), so a component that reports one may paint only its background beneath it.
	NativeViews bool

	// DarkBackground derives from the OSC 11 reply (relative luminance of
	// DefaultBG < 0.5). The unknown-fallback is DOCUMENTED AND FIXED: when
	// the query goes unanswered, ASSUME DARK — the statistically safer
	// default for terminals. Adaptive colors key off this bit, so guessing
	// wrong makes text low-contrast rather than invisible.
	DarkBackground bool

	// Raw probed default colors, retained for style resolution and
	// diagnostics; Known=false when the OSC query went unanswered.
	DefaultFG ProbedColor
	DefaultBG ProbedColor
}

// ProbedColor is a raw OSC 10/11 default-color reply.
type ProbedColor struct {
	R, G, B uint8
	Known   bool
}

// Capabilities is the attached terminal's negotiated profile (Backend.Capabilities), so a host can
// choose what to show: an image where the terminal draws one, a fallback where it does not.
func (a *App) Capabilities() Capabilities { return a.backend.Capabilities() }

// backendCaps is a backend's optional capabilities, asserted once, at NewApp: the backend is fixed
// for the App's life, so no frame or event asks again. Discovery stays by interface, in this one
// place. A nil field is a capability the backend lacks.
type backendCaps struct {
	sized       SizedFlusher
	framed      FrameSizer
	cursorColor CursorColorSetter
	graphics    GraphicsBackend
	violations  ViolationRecorder
	metrics     CellMetrics
	clipW       ClipboardWriter
	clipR       ClipboardReader
	natives     NativeHost
	styler      NativeStyler
	shapes      PointerShapeHost
	pointer     PointerResolver
	faces       FaceHost
}

func capsOf(b Backend) backendCaps {
	var c backendCaps
	c.sized, _ = b.(SizedFlusher)
	c.framed, _ = b.(FrameSizer)
	c.cursorColor, _ = b.(CursorColorSetter)
	c.graphics, _ = b.(GraphicsBackend)
	c.violations, _ = b.(ViolationRecorder)
	c.metrics, _ = b.(CellMetrics)
	c.clipW, _ = b.(ClipboardWriter)
	c.clipR, _ = b.(ClipboardReader)
	c.natives, _ = b.(NativeHost)
	c.styler, _ = b.(NativeStyler)
	c.shapes, _ = b.(PointerShapeHost)
	c.pointer, _ = b.(PointerResolver)
	c.faces, _ = b.(FaceHost)
	return c
}
