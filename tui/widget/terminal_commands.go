package widget

import (
	tuikey "github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// A Terminal's key commands (declared through internal/kit's Cmd and its helpers).

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
	TerminalPrefix   = kit.Cmd("tui.terminal.prefix", TerminalScope, "Ctrl+\\", "Ctrl+Shift+\\")
	TerminalToNormal = kit.Cmd("tui.terminal.normalMode", TerminalScope, "Esc")
	TerminalRestart  = kit.Cmd("tui.terminal.restart", TerminalScope, append(kit.Mods("Enter", kit.AnyCombo), "I")...)

	terminalCommands = []keymap.Command{TerminalPrefix, TerminalToNormal, TerminalRestart}
)

// TerminalNormalScope is a Terminal's normal mode: moving through its scrollback, selecting and
// searching, as Vim reads the keys. The key after g is its grammar (g again: the first line).
const TerminalNormalScope keymap.Scope = "tui.terminal.normal"

var (
	TermHalfUp     = kit.Cmd("tui.terminal.normal.halfUp", TerminalNormalScope, "Ctrl+U")
	TermHalfDown   = kit.Cmd("tui.terminal.normal.halfDown", TerminalNormalScope, "Ctrl+D")
	TermPageUp     = kit.Cmd("tui.terminal.normal.pageUp", TerminalNormalScope, "Ctrl+B", "PgUp", "Shift+PgUp")
	TermPageDown   = kit.Cmd("tui.terminal.normal.pageDown", TerminalNormalScope, "Ctrl+F", "PgDown", "Shift+PgDown")
	TermCancel     = kit.Cmd("tui.terminal.normal.cancel", TerminalNormalScope, "Esc", "Shift+Esc") // a selection or g; else the host's
	TermInput      = kit.Cmd("tui.terminal.normal.input", TerminalNormalScope, "I", "A", "Enter", "Shift+Enter")
	TermLeft       = kit.Cmd("tui.terminal.normal.left", TerminalNormalScope, "H", "Left", "Shift+Left")
	TermRight      = kit.Cmd("tui.terminal.normal.right", TerminalNormalScope, "L", "Right", "Shift+Right")
	TermDown       = kit.Cmd("tui.terminal.normal.down", TerminalNormalScope, "J", "Down", "Shift+Down")
	TermUp         = kit.Cmd("tui.terminal.normal.up", TerminalNormalScope, "K", "Up", "Shift+Up")
	TermLineStart  = kit.Cmd("tui.terminal.normal.lineStart", TerminalNormalScope, "0", "Home", "Shift+Home")
	TermLineEnd    = kit.Cmd("tui.terminal.normal.lineEnd", TerminalNormalScope, "$", "End", "Shift+End")
	TermWord       = kit.Cmd("tui.terminal.normal.wordForward", TerminalNormalScope, "W")
	TermWordBack   = kit.Cmd("tui.terminal.normal.wordBackward", TerminalNormalScope, "B")
	TermWordEnd    = kit.Cmd("tui.terminal.normal.wordEnd", TerminalNormalScope, "E")
	TermWORD       = kit.Cmd("tui.terminal.normal.WORDForward", TerminalNormalScope, "Shift+W")
	TermWORDBack   = kit.Cmd("tui.terminal.normal.WORDBackward", TerminalNormalScope, "Shift+B")
	TermWORDEnd    = kit.Cmd("tui.terminal.normal.WORDEnd", TerminalNormalScope, "Shift+E")
	TermGoPrefix   = kit.Cmd("tui.terminal.normal.goPrefix", TerminalNormalScope, "G") // then G: the first line; E, Shift+E: ge, gE
	TermBottom     = kit.Cmd("tui.terminal.normal.bottom", TerminalNormalScope, "Shift+G")
	TermVisual     = kit.Cmd("tui.terminal.normal.visual", TerminalNormalScope, "V")
	TermVisualLine = kit.Cmd("tui.terminal.normal.visualLine", TerminalNormalScope, "Shift+V")
	TermYank       = kit.Cmd("tui.terminal.normal.yank", TerminalNormalScope, "Y") // the selection
	TermSearch     = kit.Cmd("tui.terminal.normal.search", TerminalNormalScope, "/")
	TermSearchBack = kit.Cmd("tui.terminal.normal.searchBackward", TerminalNormalScope, "?")
	TermNext       = kit.Cmd("tui.terminal.normal.next", TerminalNormalScope, "N")
	TermPrev       = kit.Cmd("tui.terminal.normal.prev", TerminalNormalScope, "Shift+N")

	terminalNormalCommands = []keymap.Command{TermHalfUp, TermHalfDown, TermPageUp, TermPageDown,
		TermCancel, TermInput, TermLeft, TermRight, TermDown, TermUp, TermLineStart, TermLineEnd, TermWord,
		TermWordBack, TermWordEnd, TermWORD, TermWORDBack, TermWORDEnd, TermGoPrefix, TermBottom, TermVisual, TermVisualLine, TermYank,
		TermSearch, TermSearchBack, TermNext, TermPrev}
)

// TerminalSearchScope is a Terminal's search line in normal mode: it takes every key while open; typed
// text is not a command.
const TerminalSearchScope keymap.Scope = "tui.terminal.search"

var (
	TermSearchClose     = kit.Cmd("tui.terminal.search.close", TerminalSearchScope, kit.Mods("Esc", kit.AnyCombo)...)
	TermSearchRun       = kit.Cmd("tui.terminal.search.run", TerminalSearchScope, kit.Mods("Enter", kit.AnyCombo)...)
	TermSearchBackspace = kit.Cmd("tui.terminal.search.backspace", TerminalSearchScope, kit.Mods("Backspace", kit.AnyCombo)...)

	terminalSearchCommands = []keymap.Command{TermSearchClose, TermSearchRun, TermSearchBackspace}
)
