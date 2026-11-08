package widget

import "github.com/yongjohnlee80/golib/tui/keymap"

// A Tree's key commands (commands.go declares cmd and the shared helpers).

// TreeScope is a Tree's keys.
const TreeScope keymap.Scope = "tui.tree"

var (
	TreeDown     = cmd("tui.tree.down", TreeScope, append(with("Down"), "J")...)
	TreeUp       = cmd("tui.tree.up", TreeScope, append(with("Up"), "K")...)
	TreePageDown = cmd("tui.tree.pageDown", TreeScope, with("PgDown")...)
	TreePageUp   = cmd("tui.tree.pageUp", TreeScope, with("PgUp")...)
	TreeFirst    = cmd("tui.tree.first", TreeScope, append(with("Home"), "G")...)
	TreeLast     = cmd("tui.tree.last", TreeScope, append(with("End"), "Shift+G")...)
	TreeExpand   = cmd("tui.tree.expand", TreeScope, append(with("Right"), "L")...)  // a leaf activates; an open branch steps in
	TreeCollapse = cmd("tui.tree.collapse", TreeScope, append(with("Left"), "H")...) // a closed node steps to its parent
	TreeActivate = cmd("tui.tree.activate", TreeScope, with("Enter")...)

	treeCommands = []keymap.Command{TreeDown, TreeUp, TreePageDown, TreePageUp, TreeFirst, TreeLast,
		TreeExpand, TreeCollapse, TreeActivate}
)
