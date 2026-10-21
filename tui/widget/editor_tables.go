package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
)

// TABLE EDITING — a Markdown table's cells walked with Tab, in Insert mode, when on
// (CoreTableEditing / SetTableEditing):
//
//   - Tab on a table row (a line that starts and ends with '|') goes to the next cell and selects
//     its text, so what is typed replaces it; past the row's last cell, the next row's first (the
//     delimiter row, |---|, is passed over). Past the table's last cell a new empty row is added.
//   - Shift+Tab goes back a cell the same way.
//   - With a cell selected, typing replaces it; Backspace and Delete empty it; any other key drops
//     the selection and does what it always does.
//
// Off a table row the keys do what they always do (Tab nests a list item, or types a tab). The
// lines it applies on are list editing's (SetListEditingWhere): a Markdown host leaves its code
// blocks out.

// CoreTableEditing turns table editing on or off.
func CoreTableEditing(on bool) CoreOption { return func(c *EditorCore) { c.tableEditing = on } }

// SetTableEditing turns table editing on or off; off drops a cell's selection.
func (c *EditorCore) SetTableEditing(on bool) {
	c.tableEditing = on
	if !on && c.buf.anchor != nil {
		c.buf.anchor = nil
		c.markDirty()
	}
}

// TableEditing reports whether table editing is on.
func (c *EditorCore) TableEditing() bool { return c.tableEditing }

// tableRow reports whether line ln is a table row table editing applies on.
func (c *EditorCore) tableRow(ln int) ([][2]int, bool) {
	if ln < 0 || ln >= len(c.buf.textLines) || (c.listWhere != nil && !c.listWhere(ln)) {
		return nil, false
	}
	return tableCells(c.buf.textLines[ln])
}

// tableKey handles an Insert-mode key for table editing, reporting whether it did.
func (c *EditorCore) tableKey(k tui.KeyEvent) bool {
	b := &c.buf
	if !c.tableEditing || c.readOnly {
		return false
	}
	if b.anchor != nil {
		switch {
		case k.Code == tui.KeyBackspace || k.Code == tui.KeyDelete:
			lo, hi, ok := b.selection()
			b.anchor = nil
			if ok {
				c.beginGroup()
				b.deleteRegion(lo, hi)
				c.edited()
			}
			return true
		case k.Code == tui.KeyTab:
			// moves on below, from the selection's cell
		case k.Text != "" && k.Mods&^tui.ModShift == 0:
			return false // typed: insertText replaces the selection
		default:
			b.anchor = nil // any other key: the selection goes, the key does what it does
			c.markDirty()
			return false
		}
	}
	if k.Code != tui.KeyTab || k.Mods&^tui.ModShift != 0 {
		return false
	}
	cells, ok := c.tableRow(b.ln)
	if !ok {
		return false
	}
	back := k.Mods&tui.ModShift != 0
	at := cellAt(cells, b.col)
	if b.anchor != nil {
		at = cellAt(cells, min(b.anchor.col, b.col))
	}
	b.anchor = nil
	ln, i := b.ln, at
	for {
		if back {
			i--
		} else {
			i++
		}
		if i >= 0 && i < len(cells) {
			break
		}
		next := ln + 1
		if back {
			next = ln - 1
		}
		for next >= 0 && next < len(b.textLines) && isDelimiterRow(b.textLines[next]) {
			if back {
				next--
			} else {
				next++
			}
		}
		row, ok := c.tableRow(next)
		if !ok {
			if back {
				c.markDirty()
				return true // the first cell: nowhere back to go
			}
			c.addTableRow(ln, len(cells))
			return true
		}
		ln, cells = next, row
		if back {
			i = len(cells)
		} else {
			i = -1
		}
	}
	c.selectCell(ln, cells[i])
	return true
}

// cellAt is the index of the cell holding column col (the one before it, between cells).
func cellAt(cells [][2]int, col int) int {
	at := 0
	for i, cl := range cells {
		if col >= cl[0]-1 {
			at = i
		}
	}
	return at
}

// selectCell puts the cursor at the end of the cell's text with its text selected, so typing
// replaces it; an empty cell is only where the cursor goes.
func (c *EditorCore) selectCell(ln int, cell [2]int) {
	b := &c.buf
	c.hist.close()
	b.ln, b.col, b.desired = ln, cell[1], -1
	if cell[1] > cell[0] {
		b.anchor = &taPos{ln: ln, col: cell[0]}
	}
	c.reveal()
	c.markDirty()
}

// addTableRow adds an empty row of n cells under line ln and goes to its first cell.
func (c *EditorCore) addTableRow(ln, n int) {
	b := &c.buf
	c.beginGroup()
	b.ln, b.col, b.anchor = ln, len(b.lineClusters(ln)), nil
	b.insertText("\n|" + strings.Repeat("   |", n))
	c.edited()
	b.col = 2 // after "| "
	c.reveal()
}

// inCellSelection reports whether (ln, col) is inside a table cell's selection.
func (c *EditorCore) inCellSelection(ln, col int) bool {
	lo, hi, ok := c.buf.selection()
	return ok && c.tableEditing && (taPos{ln, col}).in(lo, hi)
}
