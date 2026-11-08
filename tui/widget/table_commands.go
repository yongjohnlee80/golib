package widget

import "github.com/yongjohnlee80/golib/tui/keymap"

// A table's key commands (commands.go declares cmd and the shared helpers).

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
	TableYank        = cmd("tui.table.yank", TableScope, "Y")      // y copies; y again copies the row
	TableRowCursor   = cmd("tui.table.rowCursor", TableScope, "R") // the cursor between its cell and its row
	TableSort        = cmd("tui.table.sort", TableScope, "S")
	TableActivate    = cmd("tui.table.activate", TableScope, with("Enter")...) // on an editable source: edit the cell
	// An editable source's: open the cell's field, a new row below, delete the
	// row (d, then d), empty the cell.
	TableEditCell  = cmd("tui.table.edit", TableScope, "I", "F2")
	TableOpenRow   = cmd("tui.table.openRow", TableScope, "O")
	TableDeleteRow = cmd("tui.table.deleteRow", TableScope, "D") // d, then d
	TableClearCell = cmd("tui.table.clearCell", TableScope, "X", "Delete")
	// p and P paste the register after and before; O opens a row above, as o below.
	TablePasteAfter   = cmd("tui.table.pasteAfter", TableScope, "P")
	TablePasteBefore  = cmd("tui.table.pasteBefore", TableScope, "Shift+P")
	TableOpenRowAbove = cmd("tui.table.openRowAbove", TableScope, "Shift+O")

	tableCommands = []keymap.Command{TableUp, TableDown, TableLeft, TableRight, TableFieldNext,
		TableFieldPrev, TablePageUp, TablePageDown, TableGoPrefix, TableLast, TableFieldFirst,
		TableFieldLast, TableVisual, TableVisualRows, TableVisualBlock, TableEscape, TableYank,
		TableRowCursor, TableSort, TableActivate, TableEditCell, TableOpenRow, TableDeleteRow, TableClearCell,
		TablePasteAfter, TablePasteBefore, TableOpenRowAbove}
)

// TableFieldScope is the keys of a table cell's open field (TableEdit), before its TextArea's.
const TableFieldScope keymap.Scope = "tui.tableField"

var (
	TableFieldKeep     = cmd("tui.tableField.keep", TableFieldScope, "Enter") // keep, and move down
	TableFieldNewline  = cmd("tui.tableField.newline", TableFieldScope, "Alt+Enter", "Shift+Enter")
	TableFieldNextCell = cmd("tui.tableField.next", TableFieldScope, "Tab")           // keep, and move right
	TableFieldPrevCell = cmd("tui.tableField.previous", TableFieldScope, "Shift+Tab") // keep, and move left
	TableFieldDrop     = cmd("tui.tableField.drop", TableFieldScope, "Esc")
	TableFieldLiteral  = cmd("tui.tableField.literal", TableFieldScope, "Ctrl+V") // then Tab: a tab in the value

	tableFieldCommands = []keymap.Command{TableFieldKeep, TableFieldNewline, TableFieldNextCell,
		TableFieldPrevCell, TableFieldDrop, TableFieldLiteral}
)

// tableGoLine is the key after the table's g that completes gg: a command of its own, as the
// editor's tui.editor.vim.g.goLine is, so a key map moves the second key as well as the prefix.
var tableGoLine = cmd("tui.table.g.goLine", "tui.table.g", "G")
