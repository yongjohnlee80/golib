package vt

import "github.com/yongjohnlee80/golib/tui/style"

// Attr is a cell's packed text attributes.
type Attr uint16

const (
	AttrBold Attr = 1 << iota
	AttrFaint
	AttrItalic
	AttrUnderline
	AttrBlink
	AttrReverse
	AttrInvisible
	AttrStrike
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

// CursorShape is the cursor's glyph as DECSCUSR sets it.
type CursorShape uint8

const (
	CursorDefault   CursorShape = iota // the host's own (DECSCUSR 0)
	CursorBlock                        // DECSCUSR 1, 2
	CursorUnderline                    // DECSCUSR 3, 4
	CursorBar                          // DECSCUSR 5, 6
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
}
