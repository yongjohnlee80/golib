package widget

import "github.com/yongjohnlee80/golib/tui/keymap"

// An HTMLView's key commands (commands.go declares cmd and the shared helpers).

// HTMLViewScope is an HTMLView's keys in every keyset: keys that never type. The arrows (but Alt's,
// which travel), PgUp, PgDn, Home, End and Esc take any modifier, as its handler did.
// HTMLViewVimScope is the printing keys a Vim keyset reads the page by as well (j/k, g/G, y, Space,
// h/l, ] [ and } {): a modeless keyset (Standard, Nano) types them instead, so its host opens the
// text with them (the Editor's keysets, each its own scope, as tui.editor.vim and
// tui.editor.standard are).
const (
	HTMLViewScope    keymap.Scope = "tui.htmlview"
	HTMLViewVimScope keymap.Scope = "tui.htmlview.vim"
)

var (
	HTMLViewUp       = cmd("tui.htmlview.up", HTMLViewScope, mods("Up", noAlt)...)
	HTMLViewDown     = cmd("tui.htmlview.down", HTMLViewScope, mods("Down", noAlt)...)
	HTMLViewPageUp   = cmd("tui.htmlview.pageUp", HTMLViewScope, mods("PgUp", anyCombo)...)
	HTMLViewPageDown = cmd("tui.htmlview.pageDown", HTMLViewScope, mods("PgDown", anyCombo)...)
	HTMLViewHalfUp   = cmd("tui.htmlview.halfUp", HTMLViewScope, "Ctrl+U")
	HTMLViewHalfDown = cmd("tui.htmlview.halfDown", HTMLViewScope, "Ctrl+D")
	HTMLViewTop      = cmd("tui.htmlview.top", HTMLViewScope, mods("Home", anyCombo)...)
	HTMLViewBottom   = cmd("tui.htmlview.bottom", HTMLViewScope, mods("End", anyCombo)...)
	HTMLViewCopy     = cmd("tui.htmlview.copy", HTMLViewScope, "Ctrl+C")
	HTMLViewClear    = cmd("tui.htmlview.clearSelection", HTMLViewScope, mods("Esc", anyCombo)...) // with a selection
	// a layout's own (HTMLLayoutPan, HTMLLayoutTravel): a wide block's pan, block and heading travel
	HTMLViewLeft            = cmd("tui.htmlview.left", HTMLViewScope, "Left")
	HTMLViewRight           = cmd("tui.htmlview.right", HTMLViewScope, "Right")
	HTMLViewNextBlock       = cmd("tui.htmlview.nextBlock", HTMLViewScope, "Alt+Down")
	HTMLViewPreviousBlock   = cmd("tui.htmlview.previousBlock", HTMLViewScope, "Alt+Up")
	HTMLViewNextHeading     = cmd("tui.htmlview.nextHeading", HTMLViewScope, "Alt+Shift+Down")
	HTMLViewPreviousHeading = cmd("tui.htmlview.previousHeading", HTMLViewScope, "Alt+Shift+Up")

	htmlViewCommands = []keymap.Command{HTMLViewUp, HTMLViewDown, HTMLViewPageUp, HTMLViewPageDown,
		HTMLViewHalfUp, HTMLViewHalfDown, HTMLViewTop, HTMLViewBottom, HTMLViewCopy, HTMLViewClear,
		HTMLViewLeft, HTMLViewRight, HTMLViewNextBlock, HTMLViewPreviousBlock, HTMLViewNextHeading,
		HTMLViewPreviousHeading}

	// the Vim keyset's printing keys, each the command of HTMLViewScope it does (htmlViewVimDoes)
	HTMLViewVimUp              = cmd("tui.htmlview.vim.up", HTMLViewVimScope, "K")
	HTMLViewVimDown            = cmd("tui.htmlview.vim.down", HTMLViewVimScope, "J")
	HTMLViewVimPageDown        = cmd("tui.htmlview.vim.pageDown", HTMLViewVimScope, "Space")
	HTMLViewVimTop             = cmd("tui.htmlview.vim.top", HTMLViewVimScope, "G")
	HTMLViewVimBottom          = cmd("tui.htmlview.vim.bottom", HTMLViewVimScope, "Shift+G")
	HTMLViewVimCopy            = cmd("tui.htmlview.vim.copy", HTMLViewVimScope, "Y")
	HTMLViewVimLeft            = cmd("tui.htmlview.vim.left", HTMLViewVimScope, "H")
	HTMLViewVimRight           = cmd("tui.htmlview.vim.right", HTMLViewVimScope, "L")
	HTMLViewVimNextBlock       = cmd("tui.htmlview.vim.nextBlock", HTMLViewVimScope, "]")
	HTMLViewVimPreviousBlock   = cmd("tui.htmlview.vim.previousBlock", HTMLViewVimScope, "[")
	HTMLViewVimNextHeading     = cmd("tui.htmlview.vim.nextHeading", HTMLViewVimScope, "}")
	HTMLViewVimPreviousHeading = cmd("tui.htmlview.vim.previousHeading", HTMLViewVimScope, "{")
)

// noAlt keeps a key's forms without Alt: Alt+Left and Alt+Right are the page's own.
var noAlt = func(_, _, alt bool) bool { return !alt }
