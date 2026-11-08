package widget

import (
	tuikey "github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// A Terminal's key commands (commands.go declares cmd and the shared helpers).

// The literal continuations: g after g, and Ctrl+\ after the Terminal's prefix, which sends it.
var (
	literalG         = tuikey.MustParse("G")
	literalE         = tuikey.MustParse("E")
	literalShiftE    = tuikey.MustParse("Shift+E")
	literalCtrlSlash = tuikey.MustParse("Ctrl+\\", "Ctrl+Shift+\\")
)

// TerminalScope is a Terminal's own keys in input mode, where every other key is the program's: the
// escape prefix (Ctrl+\, then Ctrl+n for normal mode or Ctrl+\ to send it; the key after it is its
// grammar, not a command), Esc into normal mode with WithVimKeys outside a full-screen program, and,
// once the program has exited, Enter or i to start one again.
const TerminalScope keymap.Scope = "tui.terminal"

var (
	TerminalPrefix   = cmd("tui.terminal.prefix", TerminalScope, "Ctrl+\\", "Ctrl+Shift+\\")
	TerminalToNormal = cmd("tui.terminal.normalMode", TerminalScope, "Esc")
	TerminalRestart  = cmd("tui.terminal.restart", TerminalScope, append(mods("Enter", anyCombo), "I")...)

	terminalCommands = []keymap.Command{TerminalPrefix, TerminalToNormal, TerminalRestart}
)

// TerminalNormalScope is a Terminal's normal mode: moving through its scrollback, selecting and
// searching, as Vim reads the keys. The key after g is its grammar (g again: the first line).
const TerminalNormalScope keymap.Scope = "tui.terminal.normal"

var (
	TermHalfUp     = cmd("tui.terminal.normal.halfUp", TerminalNormalScope, "Ctrl+U")
	TermHalfDown   = cmd("tui.terminal.normal.halfDown", TerminalNormalScope, "Ctrl+D")
	TermPageUp     = cmd("tui.terminal.normal.pageUp", TerminalNormalScope, "Ctrl+B", "PgUp", "Shift+PgUp")
	TermPageDown   = cmd("tui.terminal.normal.pageDown", TerminalNormalScope, "Ctrl+F", "PgDown", "Shift+PgDown")
	TermCancel     = cmd("tui.terminal.normal.cancel", TerminalNormalScope, "Esc", "Shift+Esc") // a selection or g; else the host's
	TermInput      = cmd("tui.terminal.normal.input", TerminalNormalScope, "I", "A", "Enter", "Shift+Enter")
	TermLeft       = cmd("tui.terminal.normal.left", TerminalNormalScope, "H", "Left", "Shift+Left")
	TermRight      = cmd("tui.terminal.normal.right", TerminalNormalScope, "L", "Right", "Shift+Right")
	TermDown       = cmd("tui.terminal.normal.down", TerminalNormalScope, "J", "Down", "Shift+Down")
	TermUp         = cmd("tui.terminal.normal.up", TerminalNormalScope, "K", "Up", "Shift+Up")
	TermLineStart  = cmd("tui.terminal.normal.lineStart", TerminalNormalScope, "0", "Home", "Shift+Home")
	TermLineEnd    = cmd("tui.terminal.normal.lineEnd", TerminalNormalScope, "$", "End", "Shift+End")
	TermWord       = cmd("tui.terminal.normal.wordForward", TerminalNormalScope, "W")
	TermWordBack   = cmd("tui.terminal.normal.wordBackward", TerminalNormalScope, "B")
	TermWordEnd    = cmd("tui.terminal.normal.wordEnd", TerminalNormalScope, "E")
	TermWORD       = cmd("tui.terminal.normal.WORDForward", TerminalNormalScope, "Shift+W")
	TermWORDBack   = cmd("tui.terminal.normal.WORDBackward", TerminalNormalScope, "Shift+B")
	TermWORDEnd    = cmd("tui.terminal.normal.WORDEnd", TerminalNormalScope, "Shift+E")
	TermGoPrefix   = cmd("tui.terminal.normal.goPrefix", TerminalNormalScope, "G") // then G: the first line; E, Shift+E: ge, gE
	TermBottom     = cmd("tui.terminal.normal.bottom", TerminalNormalScope, "Shift+G")
	TermVisual     = cmd("tui.terminal.normal.visual", TerminalNormalScope, "V")
	TermVisualLine = cmd("tui.terminal.normal.visualLine", TerminalNormalScope, "Shift+V")
	TermYank       = cmd("tui.terminal.normal.yank", TerminalNormalScope, "Y") // the selection
	TermSearch     = cmd("tui.terminal.normal.search", TerminalNormalScope, "/")
	TermSearchBack = cmd("tui.terminal.normal.searchBackward", TerminalNormalScope, "?")
	TermNext       = cmd("tui.terminal.normal.next", TerminalNormalScope, "N")
	TermPrev       = cmd("tui.terminal.normal.prev", TerminalNormalScope, "Shift+N")

	terminalNormalCommands = []keymap.Command{TermHalfUp, TermHalfDown, TermPageUp, TermPageDown,
		TermCancel, TermInput, TermLeft, TermRight, TermDown, TermUp, TermLineStart, TermLineEnd, TermWord,
		TermWordBack, TermWordEnd, TermWORD, TermWORDBack, TermWORDEnd, TermGoPrefix, TermBottom, TermVisual, TermVisualLine, TermYank,
		TermSearch, TermSearchBack, TermNext, TermPrev}
)

// TerminalSearchScope is a Terminal's search line in normal mode: it takes every key while open; typed
// text is not a command.
const TerminalSearchScope keymap.Scope = "tui.terminal.search"

var (
	TermSearchClose     = cmd("tui.terminal.search.close", TerminalSearchScope, mods("Esc", anyCombo)...)
	TermSearchRun       = cmd("tui.terminal.search.run", TerminalSearchScope, mods("Enter", anyCombo)...)
	TermSearchBackspace = cmd("tui.terminal.search.backspace", TerminalSearchScope, mods("Backspace", anyCombo)...)

	terminalSearchCommands = []keymap.Command{TermSearchClose, TermSearchRun, TermSearchBackspace}
)
