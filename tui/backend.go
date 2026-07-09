package tui

import "context"

// Backend is the driver seam between the runtime and a concrete terminal
// (ADR-0001 §2.4 #5, ADR-0002 §2.1). The App owns exactly one Backend for
// its lifetime. It is Ratatui-Backend-shaped: a cell-diff sink, an event
// source, and a capability report behind one lifecycle.
type Backend interface {
	// Start acquires the device: raw mode, VT modes, alternate screen,
	// the capability probe, and the input reader goroutine. It blocks
	// until the probe fence resolves or the probe timeout expires
	// (bounded, 250ms by default; ADR-0002 §2.6), then returns.
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
	// changes as a SINGLE buffered write (the one-write rule, ADR-0003
	// §2.5/§2.9). An empty diff with unchanged cursor state writes zero
	// bytes. The diff is ordered row-major by the caller (ADR-0003 §2.2).
	Flush(diff []CellUpdate) error

	// Cursor state is LATCHED, not immediate: these record desired state
	// which the next Flush emits, so a frame is always one write.
	ShowCursor()
	HideCursor()
	SetCursor(x, y int)
	SetCursorShape(s CursorShape)

	// Capabilities reports the negotiated profile. Constant after Start.
	Capabilities() Capabilities

	// Events is the single, ordered, UN-COALESCED event source: key,
	// mouse, paste, resize, focus — the backend only decodes and emits.
	// Fed by the backend's one reader goroutine (ADR-0002 §2.9); closed
	// by Stop. The App's intake stage consumes it exclusively and owns
	// ALL coalescing and overflow policy (ADR-0005 §2.4).
	Events() <-chan Event

	// Err reports the terminal error that stopped the reader or failed
	// the probe. Valid once Events() is closed or Stop has returned; nil
	// after a clean Stop. The App loop calls Err when Events() closes to
	// distinguish clean shutdown from reader failure (ADR-0005).
	Err() error
}

// CellUpdate is one dirty cell in a frame diff (ADR-0002 §2.1). Cell is
// ADR-0003's grapheme-cluster cell.
type CellUpdate struct {
	X, Y int
	Cell Cell
}

// CursorShape selects the hardware cursor glyph (DECSCUSR).
type CursorShape uint8

const (
	CursorShapeDefault   CursorShape = iota // terminal's configured default
	CursorShapeBlock                        // DECSCUSR 1/2
	CursorShapeUnderline                    // DECSCUSR 3/4
	CursorShapeBar                          // DECSCUSR 5/6
)

// ColorProfile is the canonical color-capability tier (ADR-0002 §2.2).
// ADR-0006 resolves style.Color against exactly this enum — it is the field
// style resolution consumes; the raw probed colors in Capabilities are
// supporting data.
type ColorProfile uint8

const (
	ProfileMono      ColorProfile = iota // no color
	ProfileANSI16                        // the 16 base SGR colors
	ProfileANSI256                       // indexed 256 (SGR 38;5)
	ProfileTrueColor                     // 24-bit RGB (SGR 38;2)
)

// Tri is a three-valued capability answer for features whose support can be
// genuinely unknowable (ADR-0002 §2.2). An optimistic request is NEVER
// reported as support — capability honesty.
type Tri uint8

const (
	TriUnknown Tri = iota // requested/attempted; no verifiable answer
	TriNo                 // verifiably unsupported
	TriYes                // verifiably supported
)

// Capabilities is the negotiated feature profile of the attached terminal,
// resolved once during Start by live probing (ADR-0002 §2.6) — never from
// terminfo (ADR-0001 §2.4 #4). Degradation is per-capability, not per-$TERM.
type Capabilities struct {
	ColorProfile   ColorProfile // env pre-seed + XTGETTCAP "RGB"; consumed by ADR-0006
	KittyKeyboard  bool         // kitty progressive enhancement; flags 1+2 pushed when true
	SyncOutput     bool         // DEC private mode 2026 (synchronized output)
	InBandResize   bool         // mode 2048 in-band resize reports
	UnicodeCore    bool         // mode 2027 grapheme-cluster semantics (consumed by ADR-0003 §2.8)
	BracketedPaste bool         // mode 2004
	Mouse          Tri          // SGR mouse — TriYes only on a verifiable DECRQM ?1006 answer
	Undercurl      bool         // XTGETTCAP "Smulx" (styled underlines; ADR-0006)

	// DarkBackground derives from the OSC 11 reply (relative luminance of
	// DefaultBG < 0.5). The unknown-fallback is DOCUMENTED AND FIXED: when
	// the query goes unanswered, ASSUME DARK — the statistically safer
	// default for terminals. ADR-0006 adaptive colors key off this bit.
	DarkBackground bool

	// Raw probed default colors, retained for style resolution and
	// diagnostics; Known=false when the OSC query went unanswered.
	DefaultFG ProbedColor
	DefaultBG ProbedColor
}

// ProbedColor is a raw OSC 10/11 default-color reply (ADR-0002 §2.2).
type ProbedColor struct {
	R, G, B uint8
	Known   bool
}
