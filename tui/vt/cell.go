package vt

import "github.com/yongjohnlee80/golib/tui/style"

// Attr is a cell's packed text attributes: style's AttrMask, the bits a Style, tui's cells and
// the web client share.
type Attr = style.AttrMask

const (
	AttrBold      = style.AttrBold
	AttrFaint     = style.AttrFaint
	AttrItalic    = style.AttrItalic
	AttrUnderline = style.AttrUnderline
	AttrBlink     = style.AttrBlink
	AttrReverse   = style.AttrReverse
	AttrInvisible = style.AttrInvisible
	AttrStrike    = style.AttrStrikethrough
)

// Cell is one grid position.
//
// Content is the grapheme cluster shown there; "" is a blank. Width is the
// columns the cluster takes: 1, 2 for a wide character, whose next cell is
// a continuation with Width 0 and no content. FG and BG are the colours as
// the program set them; the zero Color is the default.
type Cell struct {
	Content string
	Width   uint8
	Attr    Attr
	FG, BG  style.Color
}

// blank is an erased cell wearing the pen's background (xterm's
// background-colour erase).
func blank(bg style.Color) Cell { return Cell{Width: 1, BG: bg} }

// isBlank reports whether c shows nothing and carries no colour, so trailing
// runs of it can be trimmed.
func (c Cell) isBlank() bool {
	return c.Content == "" && c.Width == 1 && c.Attr == 0 && c.FG.IsDefault() && c.BG.IsDefault()
}

// CursorShape is the cursor's glyph as DECSCUSR sets it: style's, shared with tui.
type CursorShape = style.CursorShape

const (
	CursorDefault   = style.CursorShapeDefault   // the host's own (DECSCUSR 0)
	CursorBlock     = style.CursorShapeBlock     // DECSCUSR 1, 2
	CursorUnderline = style.CursorShapeUnderline // DECSCUSR 3, 4
	CursorBar       = style.CursorShapeBar       // DECSCUSR 5, 6
)

// MouseMode is the mouse reporting a program asked for.
type MouseMode uint8

const (
	MouseOff    MouseMode = iota
	MouseX10              // 9: presses only
	MouseClick            // 1000: presses and releases
	MouseDrag             // 1002: and motion with a button held
	MouseMotion           // 1003: and all motion
)

// Modes are the terminal modes a host's input encoding and painting depend
// on.
type Modes struct {
	AltScreen      bool // 1049/1047/47: the alternate screen is shown
	AppCursor      bool // DECCKM (1): cursor keys send SS3
	AppKeypad      bool // DECKPAM / DECNKM (66)
	BracketedPaste bool // 2004
	FocusEvents    bool // 1004
	CursorVisible  bool // DECTCEM (25)
	CursorBlink    bool // 12, or a blinking DECSCUSR shape
	Autowrap       bool // DECAWM (7)
	Origin         bool // DECOM (6)
	Insert         bool // IRM (4)
	NewLine        bool // LNM (20): LF also returns the carriage
	Synchronized   bool // 2026: the program is mid-frame; a host may hold painting
	Mouse          MouseMode
	MouseSGR       bool // 1006: SGR mouse encoding
	// KeyboardFlags are the kitty keyboard protocol's enhancement flags the
	// program asked for and this screen supports (KittyDisambiguate): 0 is
	// the legacy encoding every terminal sends.
	KeyboardFlags int
}

// KittyDisambiguate is the kitty keyboard protocol's first enhancement:
// keys whose legacy bytes are ambiguous (Escape, Enter, Tab and Backspace
// with a modifier, a text key with Ctrl or Alt) are sent as CSI … u, so a
// program can tell Shift+Enter from Enter. The other flags are not
// supported and are masked off what a program pushes.
const KittyDisambiguate = 1

// kittyStackMax is how many flag sets a screen's stack holds; a push past
// it evicts the oldest, as the protocol says.
const kittyStackMax = 32
