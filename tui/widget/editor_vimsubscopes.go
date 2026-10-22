package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// THE VIM GRAMMAR'S SECOND KEYS: the key after an armed g, and the i or a
// that names a text object, are commands of sub-scopes of their own, so a key map moves them by id
// ("tui.editor.vim.g.goLine"). A pending key is looked up in its sub-scope first, before the root
// table and its Alt and Super bubbling: a sub-scope binding is reachable however it is bound. A
// key the sub-scope does not bind goes on as any key does.

const (
	vimGScope      keymap.Scope = "tui.editor.vim.g"
	vimObjectScope keymap.Scope = "tui.editor.vim.object"
)

// goKey is what a key after an armed g does.
type goKey int

const (
	goNone        goKey = iota
	goLine              // gg: to a line (the count's, else the first)
	goWordEndBack       // ge
	goWORDEndBack       // gE
	goRowDown           // gj: a screen row down
	goRowUp             // gk
	goStarPartial       // g*: * without the word's bounds
	goHashPartial       // g#
	goJoinRaw           // gJ: join, keeping the space as it is
	goCaseToggle        // g~: the case-toggling operator
	goLower             // gu
	goUpper             // gU
)

// vimGCommands are the g prefix's second keys, today's code points their defaults.
var vimGCommands = []struct {
	cmd keymap.Command
	key goKey
}{
	{cmd("tui.editor.vim.g.goLine", vimGScope, "G"), goLine},
	{cmd("tui.editor.vim.g.wordEndBack", vimGScope, "E"), goWordEndBack},
	{cmd("tui.editor.vim.g.wORDEndBack", vimGScope, "Shift+E"), goWORDEndBack},
	{cmd("tui.editor.vim.g.rowDown", vimGScope, "J"), goRowDown},
	{cmd("tui.editor.vim.g.rowUp", vimGScope, "K"), goRowUp},
	{cmd("tui.editor.vim.g.starPartial", vimGScope, "*"), goStarPartial},
	{cmd("tui.editor.vim.g.hashPartial", vimGScope, "#"), goHashPartial},
	{cmd("tui.editor.vim.g.joinRaw", vimGScope, "Shift+J"), goJoinRaw},
	{cmd("tui.editor.vim.g.caseToggle", vimGScope, "~"), goCaseToggle},
	{cmd("tui.editor.vim.g.lower", vimGScope, "U"), goLower},
	{cmd("tui.editor.vim.g.upper", vimGScope, "Shift+U"), goUpper},
}

// vimObjectCommands are the keys that name a text object's extent: inside it, or around it.
var vimObjectCommands = []struct {
	cmd    keymap.Command
	around bool
}{
	{cmd("tui.editor.vim.object.inner", vimObjectScope, "I"), false},
	{cmd("tui.editor.vim.object.around", vimObjectScope, "A"), true},
}

// goKeyOf is the g command k is, as the App's key map binds it (its defaults without one).
func (c *EditorCore) goKeyOf(k tui.KeyEvent) goKey {
	for _, g := range vimGCommands {
		if isKey(k, c.keys.builtFrom.KeysOf(g.cmd)...) {
			return g.key
		}
	}
	return goNone
}

// objectOf is whether k names a text object's extent, and whether it is around rather than inner.
func (c *EditorCore) objectOf(k tui.KeyEvent) (around, ok bool) {
	for _, o := range vimObjectCommands {
		if isKey(k, c.keys.builtFrom.KeysOf(o.cmd)...) {
			return o.around, true
		}
	}
	return false, false
}

// caseOpOfGo is the case operator a g command arms, ActUnbound for any other.
func caseOpOfGo(g goKey) Action {
	switch g {
	case goCaseToggle:
		return ActCaseToggleOp
	case goLower:
		return ActLowerOp
	case goUpper:
		return ActUpperOp
	}
	return ActUnbound
}
