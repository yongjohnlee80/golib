package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// KEY COMMANDS — every key a golib widget acts on, declared as data (ADR 1792255248 §4.6). A widget
// asks its Context which of its own commands a key is (tui.Context.KeyCommand); a key-mapping scheme
// changes their keys by id. Each widget's defaults reproduce the keys its handler took before
// commands existed, written out key by key: where the old handler let Shift through to a functional
// key, the Shift form is a default too.
//
// A label's mnemonic (&File, a button's underlined letter) is not a command and cannot be rebound.

// cmd declares one command.
func cmd(id string, scope keymap.Scope, keys ...string) keymap.Command {
	return keymap.Command{ID: id, Scope: scope, Keys: tuikey.MustParse(keys...)}
}

// with is each text and its Shift form, for a functional key the old handlers took either way.
func with(texts ...string) []string {
	out := make([]string, 0, 2*len(texts))
	for _, t := range texts {
		out = append(out, t, "Shift+"+t)
	}
	return out
}

// ListScope is a List's keys.
const ListScope keymap.Scope = "tui.list"

var (
	ListUp       = cmd("tui.list.up", ListScope, append(with("Up"), "K")...)
	ListDown     = cmd("tui.list.down", ListScope, append(with("Down"), "J")...)
	ListTop      = cmd("tui.list.top", ListScope, "G")
	ListBottom   = cmd("tui.list.bottom", ListScope, "Shift+G")
	ListPageUp   = cmd("tui.list.pageUp", ListScope, with("PgUp")...)
	ListPageDown = cmd("tui.list.pageDown", ListScope, with("PgDown")...)
	ListHome     = cmd("tui.list.home", ListScope, with("Home")...)
	ListEnd      = cmd("tui.list.end", ListScope, with("End")...)
	ListActivate = cmd("tui.list.activate", ListScope, with("Enter")...)
	ListToggle   = cmd("tui.list.toggle", ListScope, "Space") // multi-select only
	ListMenu     = cmd("tui.list.contextMenu", ListScope, "Shift+F10", "Menu")

	listCommands = []keymap.Command{ListMenu, ListUp, ListDown, ListTop, ListBottom, ListPageUp,
		ListPageDown, ListHome, ListEnd, ListActivate, ListToggle}
)

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

// TableScope is a table's keys (TableCore: DataTable, and the GUI's table).
const TableScope keymap.Scope = "tui.table"

// The table's keys are a small grammar, as an editor's: a count ([count]j), and g waiting for its
// second g. The digits are that grammar, not commands; the first key of every command, the g prefix's
// included, is a command.
var (
	TableUp          = cmd("tui.table.up", TableScope, append(with("Up"), "K")...)
	TableDown        = cmd("tui.table.down", TableScope, append(with("Down"), "J")...)
	TableLeft        = cmd("tui.table.left", TableScope, append(with("Left"), "H")...)
	TableRight       = cmd("tui.table.right", TableScope, append(with("Right"), "L")...)
	TableFieldNext   = cmd("tui.table.fieldNext", TableScope, "W")
	TableFieldPrev   = cmd("tui.table.fieldPrev", TableScope, "B")
	TablePageUp      = cmd("tui.table.pageUp", TableScope, with("PgUp")...)
	TablePageDown    = cmd("tui.table.pageDown", TableScope, with("PgDown")...)
	TableGoPrefix    = cmd("tui.table.goPrefix", TableScope, "G")   // g, then g: the first row ([count]gg: row count)
	TableLast        = cmd("tui.table.last", TableScope, "Shift+G") // [count]G: row count
	TableFieldFirst  = cmd("tui.table.fieldFirst", TableScope, append(with("Home"), "0")...)
	TableFieldLast   = cmd("tui.table.fieldLast", TableScope, append(with("End"), "$")...)
	TableVisual      = cmd("tui.table.selectCells", TableScope, "V")
	TableVisualRows  = cmd("tui.table.selectRows", TableScope, "Shift+V")
	TableVisualBlock = cmd("tui.table.selectBlock", TableScope, "Ctrl+V")
	TableEscape      = cmd("tui.table.escape", TableScope, with("Esc")...)
	TableYank        = cmd("tui.table.yank", TableScope, "Y") // y copies; y again copies the row
	TableSort        = cmd("tui.table.sort", TableScope, "S")
	TableActivate    = cmd("tui.table.activate", TableScope, with("Enter")...)

	tableCommands = []keymap.Command{TableUp, TableDown, TableLeft, TableRight, TableFieldNext,
		TableFieldPrev, TablePageUp, TablePageDown, TableGoPrefix, TableLast, TableFieldFirst,
		TableFieldLast, TableVisual, TableVisualRows, TableVisualBlock, TableEscape, TableYank,
		TableSort, TableActivate}
)

// KeyCommands is every key command golib declares: the runtime's and every widget's, for keymap.Build.
// (Commands is the Editor command line's registry; these are keys.)
func KeyCommands() []keymap.Command {
	out := append([]keymap.Command(nil), tui.Commands()...)
	out = append(out, listCommands...)
	out = append(out, treeCommands...)
	out = append(out, tableCommands...)
	return out
}
