package widget

import (
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// A table's key commands (declared through internal/kit's Cmd and its helpers).

// TableScope is a table's keys (TableCore: DataTable, and the GUI's table).
const TableScope keymap.Scope = "tui.table"

// The table's keys are a small grammar, as an editor's: a count ([count]j), and g waiting for its
// second g. The digits are that grammar, not commands; the first key of every command, the g prefix's
// included, is a command.
var (
	TableUp          = kit.Cmd("tui.table.up", TableScope, append(kit.With("Up"), "K")...)
	TableDown        = kit.Cmd("tui.table.down", TableScope, append(kit.With("Down"), "J")...)
	TableLeft        = kit.Cmd("tui.table.left", TableScope, append(kit.With("Left"), "H")...)
	TableRight       = kit.Cmd("tui.table.right", TableScope, append(kit.With("Right"), "L")...)
	TableFieldNext   = kit.Cmd("tui.table.fieldNext", TableScope, "W")
	TableFieldPrev   = kit.Cmd("tui.table.fieldPrev", TableScope, "B")
	TablePageUp      = kit.Cmd("tui.table.pageUp", TableScope, kit.With("PgUp")...)
	TablePageDown    = kit.Cmd("tui.table.pageDown", TableScope, kit.With("PgDown")...)
	TableGoPrefix    = kit.Cmd("tui.table.goPrefix", TableScope, "G")   // g, then g: the first row ([count]gg: row count)
	TableLast        = kit.Cmd("tui.table.last", TableScope, "Shift+G") // [count]G: row count
	TableFieldFirst  = kit.Cmd("tui.table.fieldFirst", TableScope, append(kit.With("Home"), "0")...)
	TableFieldLast   = kit.Cmd("tui.table.fieldLast", TableScope, append(kit.With("End"), "$")...)
	TableVisual      = kit.Cmd("tui.table.selectCells", TableScope, "V")
	TableVisualRows  = kit.Cmd("tui.table.selectRows", TableScope, "Shift+V")
	TableVisualBlock = kit.Cmd("tui.table.selectBlock", TableScope, "Ctrl+V")
	TableEscape      = kit.Cmd("tui.table.escape", TableScope, kit.With("Esc")...)
	TableYank        = kit.Cmd("tui.table.yank", TableScope, "Y")      // y copies; y again copies the row
	TableRowCursor   = kit.Cmd("tui.table.rowCursor", TableScope, "R") // the cursor between its cell and its row
	TableSort        = kit.Cmd("tui.table.sort", TableScope, "S")
	TableActivate    = kit.Cmd("tui.table.activate", TableScope, kit.With("Enter")...) // on an editable source: edit the cell
	// An editable source's: open the cell's field, a new row below, delete the
	// row (d, then d), empty the cell.
	TableEditCell  = kit.Cmd("tui.table.edit", TableScope, "I", "F2")
	TableOpenRow   = kit.Cmd("tui.table.openRow", TableScope, "O")
	TableDeleteRow = kit.Cmd("tui.table.deleteRow", TableScope, "D") // d, then d
	TableClearCell = kit.Cmd("tui.table.clearCell", TableScope, "X", "Delete")
	// p and P paste the register after and before; O opens a row above, as o below.
	TablePasteAfter   = kit.Cmd("tui.table.pasteAfter", TableScope, "P")
	TablePasteBefore  = kit.Cmd("tui.table.pasteBefore", TableScope, "Shift+P")
	TableOpenRowAbove = kit.Cmd("tui.table.openRowAbove", TableScope, "Shift+O")

	tableCommands = []keymap.Command{TableUp, TableDown, TableLeft, TableRight, TableFieldNext,
		TableFieldPrev, TablePageUp, TablePageDown, TableGoPrefix, TableLast, TableFieldFirst,
		TableFieldLast, TableVisual, TableVisualRows, TableVisualBlock, TableEscape, TableYank,
		TableRowCursor, TableSort, TableActivate, TableEditCell, TableOpenRow, TableDeleteRow, TableClearCell,
		TablePasteAfter, TablePasteBefore, TableOpenRowAbove}
)

// TableFieldScope is the keys of a table cell's open field (TableEdit), before its TextArea's.
const TableFieldScope keymap.Scope = "tui.tableField"

var (
	TableFieldKeep     = kit.Cmd("tui.tableField.keep", TableFieldScope, "Enter") // keep, and move down
	TableFieldNewline  = kit.Cmd("tui.tableField.newline", TableFieldScope, "Alt+Enter", "Shift+Enter")
	TableFieldNextCell = kit.Cmd("tui.tableField.next", TableFieldScope, "Tab")           // keep, and move right
	TableFieldPrevCell = kit.Cmd("tui.tableField.previous", TableFieldScope, "Shift+Tab") // keep, and move left
	TableFieldDrop     = kit.Cmd("tui.tableField.drop", TableFieldScope, "Esc")
	TableFieldLiteral  = kit.Cmd("tui.tableField.literal", TableFieldScope, "Ctrl+V") // then Tab: a tab in the value

	tableFieldCommands = []keymap.Command{TableFieldKeep, TableFieldNewline, TableFieldNextCell,
		TableFieldPrevCell, TableFieldDrop, TableFieldLiteral}
)

// tableGoLine is the key after the table's g that completes gg: a command of its own, as the
// editor's tui.editor.vim.g.goLine is, so a key map moves the second key as well as the prefix.
var tableGoLine = kit.Cmd("tui.table.g.goLine", "tui.table.g", "G")
