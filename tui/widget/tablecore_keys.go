package widget

import (
	"github.com/yongjohnlee80/golib/tui"
)

// THE KEYS — vim's grammar over a grid: a count, the g prefix, the y and d doubles, then the
// command's motion, selection key or edit.

// tableKeyState is the keys typed so far of a command not yet complete: a count, and a g waiting
// for its second g. yanked is a y that copied the cursor's cell, which a second y turns into yy.
type tableKeyState struct {
	count    int
	pendingG bool
	yanked   bool
	deleting bool // a d waiting for its second d (dd)
}

// reset drops what was typed.
func (k *tableKeyState) reset() { *k = tableKeyState{} }

// HandleKey moves, selects, sorts, copies and activates:
//
//	[count] k j h l ↑ ↓ ← →   move count cells     [count] w b   next / previous field, across rows
//	PgUp PgDn                 a page               gg G          first / last row ([count]G, [count]gg: row count)
//	0 $ Home End              first / last field   v V Ctrl+V    start, switch or end a selection
//	y                         copy the selection (or the cursor's cell); yy the cursor's row
//	p P                       paste the register after / before (an editable source)
//	d x                       on a selection, cut it: d deletes a V selection's rows, x empties
//	Esc                       end a selection
//	s                         sort by this field (if sortable): up, down, off; one key, replacing a sort of several
//	Enter                     activate the cursor's row
//
// Each key is a command of scope tui.table (widget.TableUp …), so a key map can move it; the digits
// are the count. A g waits for its second g; any other key drops it and runs. Ctrl+V is the one Ctrl
// chord it takes by default, vim's block key: a read-only table has nothing to paste. Other Ctrl, Alt
// and Super chords are the application's.
func (c *TableCore) HandleKey(k tui.KeyEvent) bool {
	if c.editing != nil {
		return c.fieldKey(k)
	}
	if k.Kind == tui.KeyRelease {
		return false
	}
	if c.takeCount(k) {
		return true
	}
	if c.pendingG && isKey(k, c.ctx.Keymap().KeysOf(tableGoLine)...) {
		// g's grammar: the goLine command after it is [count]gg, wherever a map moved either key
		row := max(c.count, 1) - 1
		c.dropPending()
		c.SetCursor(TablePos{row, c.cur.Col})
		return true
	}
	cmd, ok := c.ctx.KeyCommand(k, tableCommands...)
	if !ok {
		return c.unboundKey(k)
	}
	if cmd.ID == TableGoPrefix.ID {
		c.pendingG, c.yanked = true, false // the count waits with it, for [count]gg
		return true
	}
	n, counted := max(c.count, 1), c.count > 0
	wasG, yanked, deleting := c.pendingG, c.yanked, c.deleting
	c.dropPending()
	if wasG {
		n, counted = 1, false // the count went with the g it was typed for
	}
	if p, ok := c.motionTarget(cmd.ID, n, counted); ok {
		c.SetCursor(p) // last: the owner told of the move may replace the rows
		return true
	}
	if c.selectionKey(cmd.ID) {
		return true
	}
	if cmd.ID == TableEscape.ID {
		if c.anchor == nil {
			return wasG || counted // a pending command dropped is the key's work; otherwise it is the application's
		}
		c.SetAnchor(nil)
		return true
	}
	return c.editKey(cmd.ID, yanked, deleting)
}

// takeCount takes a count's digit. It reports whether it took the key.
func (c *TableCore) takeCount(k tui.KeyEvent) bool {
	d, ok := countDigit(k)
	if !ok || !addCount(&c.count, d, tableCountMax) {
		return false
	}
	c.pendingG, c.yanked = false, false
	return true
}

// unboundKey is a key no table command has: it drops a count or a g, and is the application's
// (keysFirst first). On an editable table a printable key opens the cell's field with it typed;
// Space and : stay the application's, as vim's leader and command line are.
func (c *TableCore) unboundKey(k tui.KeyEvent) bool {
	c.dropPending()
	if c.keysFirst != nil && c.keysFirst(k) {
		return true
	}
	if k.Text != "" && k.Text != " " && k.Text != ":" && k.Mods&^tui.ModShift == 0 && k.Code != tui.KeyTab && c.Editable() {
		return c.EditCell(k.Text)
	}
	return false
}

// motionTarget is where cursor motion id moves the cursor, n times where it counts; counted says a
// count was typed ([count]G). ok is false when id is no motion. It moves nothing: the caller moves
// the cursor there as its last step, since the owner told of the move may replace the rows.
func (c *TableCore) motionTarget(id string, n int, counted bool) (p TablePos, ok bool) {
	cur := c.cur
	switch id {
	case TableUp.ID:
		return TablePos{cur.Row - n, cur.Col}, true
	case TableDown.ID:
		return TablePos{cur.Row + n, cur.Col}, true
	case TableLeft.ID:
		return TablePos{cur.Row, cur.Col - n}, true
	case TableRight.ID:
		return TablePos{cur.Row, cur.Col + n}, true
	case TableFieldNext.ID:
		return c.fieldsTarget(n), true
	case TableFieldPrev.ID:
		return c.fieldsTarget(-n), true
	case TablePageUp.ID:
		return TablePos{cur.Row - c.page(), cur.Col}, true
	case TablePageDown.ID:
		return TablePos{cur.Row + c.page(), cur.Col}, true
	case TableLast.ID:
		row := c.Len() - 1
		if counted {
			row = n - 1
		}
		return TablePos{row, cur.Col}, true
	case TableFieldFirst.ID:
		return TablePos{cur.Row, 0}, true
	case TableFieldLast.ID:
		return TablePos{cur.Row, len(c.Fields()) - 1}, true
	}
	return TablePos{}, false
}

// selectionKey runs id when it starts, switches or ends a selection (v V Ctrl+V), or toggles the
// row cursor. It reports whether id was one.
func (c *TableCore) selectionKey(id string) bool {
	switch id {
	case TableVisual.ID:
		c.toggleSelect(TableSelectCells)
	case TableVisualRows.ID:
		c.toggleSelect(TableSelectRows)
	case TableVisualBlock.ID:
		c.toggleSelect(TableSelectBlock)
	case TableRowCursor.ID:
		c.SetRowCursor(!c.rowCursor)
	default:
		return false
	}
	return true
}

// editKey runs the copy, sort, activate and edit commands: yanked and deleting say a y or a d
// before this key waits for its double (yy, dd). It reports whether the key was taken.
func (c *TableCore) editKey(id string, yanked, deleting bool) bool {
	switch id {
	case TableYank.ID:
		switch {
		case c.anchor != nil:
			c.Copy()
		case yanked: // yy: the row (a row cursor's y is the row already: yank)
			c.setRegister(c.rowText(c.cur.Row), true)
		default:
			c.yanked = true // first: Copy tells the owner (TableOnCopy), who may replace the rows
			c.Copy()
		}
	case TableSort.ID:
		if !c.sortable {
			return false
		}
		c.sortKey(c.cur.Col)
	case TableActivate.ID:
		return c.activateOrEdit()
	case TableEditCell.ID:
		return c.EditCell("")
	case TableOpenRow.ID:
		return c.OpenRow()
	case TableClearCell.ID:
		if c.anchor != nil { // x on a selection cuts it, its cells emptied
			return c.Cut(false)
		}
		return c.clearCell()
	case TableDeleteRow.ID:
		if !c.Editable() {
			return false
		}
		if c.anchor != nil { // d on a selection cuts it at once: a V selection's rows go
			return c.Cut(c.kind == TableSelectRows)
		}
		if deleting {
			return c.deleteRow()
		}
		c.deleting = true
	case TablePasteAfter.ID:
		return c.PasteRegister(false)
	case TablePasteBefore.ID:
		return c.PasteRegister(true)
	case TableOpenRowAbove.ID:
		return c.OpenRowAbove()
	}
	return true
}

// activateOrEdit is Enter and a double-click on a cell: an editable table edits the cell, any other
// activates the cursor's row. It reports whether either happened.
func (c *TableCore) activateOrEdit() bool {
	if c.Editable() {
		return c.EditCell("")
	}
	return c.activateCursor()
}
