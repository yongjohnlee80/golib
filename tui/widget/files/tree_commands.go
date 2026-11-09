package files

import (
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// A Tree's key commands (declared through internal/kit's Cmd and its helpers).

// TreeScope is a Tree's keys.
const TreeScope keymap.Scope = "tui.tree"

var (
	TreeDown     = kit.Cmd("tui.tree.down", TreeScope, append(kit.With("Down"), "J")...)
	TreeUp       = kit.Cmd("tui.tree.up", TreeScope, append(kit.With("Up"), "K")...)
	TreePageDown = kit.Cmd("tui.tree.pageDown", TreeScope, kit.With("PgDown")...)
	TreePageUp   = kit.Cmd("tui.tree.pageUp", TreeScope, kit.With("PgUp")...)
	TreeFirst    = kit.Cmd("tui.tree.first", TreeScope, append(kit.With("Home"), "G")...)
	TreeLast     = kit.Cmd("tui.tree.last", TreeScope, append(kit.With("End"), "Shift+G")...)
	TreeExpand   = kit.Cmd("tui.tree.expand", TreeScope, append(kit.With("Right"), "L")...)  // a leaf activates; an open branch steps in
	TreeCollapse = kit.Cmd("tui.tree.collapse", TreeScope, append(kit.With("Left"), "H")...) // a closed node steps to its parent
	TreeActivate = kit.Cmd("tui.tree.activate", TreeScope, kit.With("Enter")...)

	treeCommands = []keymap.Command{TreeDown, TreeUp, TreePageDown, TreePageUp, TreeFirst, TreeLast,
		TreeExpand, TreeCollapse, TreeActivate}
)
