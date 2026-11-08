package widget

import (
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// An HTMLView's key commands (declared through internal/kit's Cmd and its helpers).

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
	HTMLViewUp       = kit.Cmd("tui.htmlview.up", HTMLViewScope, kit.Mods("Up", noAlt)...)
	HTMLViewDown     = kit.Cmd("tui.htmlview.down", HTMLViewScope, kit.Mods("Down", noAlt)...)
	HTMLViewPageUp   = kit.Cmd("tui.htmlview.pageUp", HTMLViewScope, kit.Mods("PgUp", kit.AnyCombo)...)
	HTMLViewPageDown = kit.Cmd("tui.htmlview.pageDown", HTMLViewScope, kit.Mods("PgDown", kit.AnyCombo)...)
	HTMLViewHalfUp   = kit.Cmd("tui.htmlview.halfUp", HTMLViewScope, "Ctrl+U")
	HTMLViewHalfDown = kit.Cmd("tui.htmlview.halfDown", HTMLViewScope, "Ctrl+D")
	HTMLViewTop      = kit.Cmd("tui.htmlview.top", HTMLViewScope, kit.Mods("Home", kit.AnyCombo)...)
	HTMLViewBottom   = kit.Cmd("tui.htmlview.bottom", HTMLViewScope, kit.Mods("End", kit.AnyCombo)...)
	HTMLViewCopy     = kit.Cmd("tui.htmlview.copy", HTMLViewScope, "Ctrl+C")
	HTMLViewClear    = kit.Cmd("tui.htmlview.clearSelection", HTMLViewScope, kit.Mods("Esc", kit.AnyCombo)...) // with a selection
	// a layout's own (HTMLLayoutPan, HTMLLayoutTravel): a wide block's pan, block and heading travel
	HTMLViewLeft            = kit.Cmd("tui.htmlview.left", HTMLViewScope, "Left")
	HTMLViewRight           = kit.Cmd("tui.htmlview.right", HTMLViewScope, "Right")
	HTMLViewNextBlock       = kit.Cmd("tui.htmlview.nextBlock", HTMLViewScope, "Alt+Down")
	HTMLViewPreviousBlock   = kit.Cmd("tui.htmlview.previousBlock", HTMLViewScope, "Alt+Up")
	HTMLViewNextHeading     = kit.Cmd("tui.htmlview.nextHeading", HTMLViewScope, "Alt+Shift+Down")
	HTMLViewPreviousHeading = kit.Cmd("tui.htmlview.previousHeading", HTMLViewScope, "Alt+Shift+Up")

	htmlViewCommands = []keymap.Command{HTMLViewUp, HTMLViewDown, HTMLViewPageUp, HTMLViewPageDown,
		HTMLViewHalfUp, HTMLViewHalfDown, HTMLViewTop, HTMLViewBottom, HTMLViewCopy, HTMLViewClear,
		HTMLViewLeft, HTMLViewRight, HTMLViewNextBlock, HTMLViewPreviousBlock, HTMLViewNextHeading,
		HTMLViewPreviousHeading}

	// the Vim keyset's printing keys, each the command of HTMLViewScope it does (htmlViewVimDoes)
	HTMLViewVimUp              = kit.Cmd("tui.htmlview.vim.up", HTMLViewVimScope, "K")
	HTMLViewVimDown            = kit.Cmd("tui.htmlview.vim.down", HTMLViewVimScope, "J")
	HTMLViewVimPageDown        = kit.Cmd("tui.htmlview.vim.pageDown", HTMLViewVimScope, "Space")
	HTMLViewVimTop             = kit.Cmd("tui.htmlview.vim.top", HTMLViewVimScope, "G")
	HTMLViewVimBottom          = kit.Cmd("tui.htmlview.vim.bottom", HTMLViewVimScope, "Shift+G")
	HTMLViewVimCopy            = kit.Cmd("tui.htmlview.vim.copy", HTMLViewVimScope, "Y")
	HTMLViewVimLeft            = kit.Cmd("tui.htmlview.vim.left", HTMLViewVimScope, "H")
	HTMLViewVimRight           = kit.Cmd("tui.htmlview.vim.right", HTMLViewVimScope, "L")
	HTMLViewVimNextBlock       = kit.Cmd("tui.htmlview.vim.nextBlock", HTMLViewVimScope, "]")
	HTMLViewVimPreviousBlock   = kit.Cmd("tui.htmlview.vim.previousBlock", HTMLViewVimScope, "[")
	HTMLViewVimNextHeading     = kit.Cmd("tui.htmlview.vim.nextHeading", HTMLViewVimScope, "}")
	HTMLViewVimPreviousHeading = kit.Cmd("tui.htmlview.vim.previousHeading", HTMLViewVimScope, "{")
)

// noAlt keeps a key's forms without Alt: Alt+Left and Alt+Right are the page's own.
var noAlt = func(_, _, alt bool) bool { return !alt }
