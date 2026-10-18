package widget

// THE FILTER — the LOADED rows a predicate keeps, as a view of the source, as the sort is: the
// source keeps every row, the display shows those the filter keeps in the sort's order, and rows
// that arrive later are filtered and merged in by Changed. The display rows are the kept ones:
// Len, Cell, Rows, the row numbers, the selection and the copies all count them. A filter reads
// only what is loaded and fetches nothing.

// SetFilter shows only the loaded rows p keeps (TableTrue), in the sort's order; nil shows every
// row. The cursor stays on its source row while that row is shown, and otherwise goes to the first
// row; a selection ends. When no row is kept the table is empty: Len is 0, there is no cursor row,
// and nothing can be selected or copied. The cursor is revealed, and a pager is asked for nothing.
//
// A predicate from ParseTableWhere names fields by position; one naming a field the source does
// not have is dropped, here and when the source changes, as a sort key on a missing field is.
func (c *TableCore) SetFilter(p TablePredicate) {
	if p == nil && c.filter == nil {
		return
	}
	c.CancelEdit() // the display rows it was opened among change
	keep := -1
	if c.Len() > 0 {
		keep = c.SourceRow(c.cur.Row)
	}
	c.filter = c.validFilter(p)
	c.anchor, c.kind, c.dragging = nil, TableSelectCells, false
	c.arrange()
	c.cur.Row = 0
	if keep >= 0 {
		c.cur.Row = c.displayRow(keep) // 0 when the filter hides it
	}
	c.clamp()
	c.show()
}

// Filtered is the rows shown and the rows loaded: "shown of loaded rows" while a filter is on. They
// are equal without one.
func (c *TableCore) Filtered() (shown, loaded int) { return c.Len(), c.loaded() }

// validFilter is p, or nil when it reads a field the source lacks.
func (c *TableCore) validFilter(p TablePredicate) TablePredicate {
	if w, ok := p.(interface{ fieldsRead() int }); ok && w.fieldsRead() > len(c.Fields()) {
		return nil
	}
	return p
}

// arrange is the display order of the loaded rows: the sort's order, or the source's, keeping
// only the rows the filter keeps. Filtering the sorted rows and sorting the kept ones give the
// same order (the sort is stable, and the filter reads each row alone); this way each shown row's
// place among all the loaded rows is known, which is how near their end the cursor is (wantMore).
func (c *TableCore) arrange() {
	var all []int
	if c.keys != nil {
		all = c.sorted()
	}
	if c.filter == nil {
		c.order, c.place = all, nil
		return
	}
	n := c.loaded()
	order, place := make([]int, 0, n), make([]int, 0, n)
	for i := range n {
		r := i
		if all != nil {
			r = all[i]
		}
		if c.filter.Eval(c.src, r) == TableTrue {
			order, place = append(order, r), append(place, i)
		}
	}
	c.order, c.place = order, place
}

// loadedPlace is the cursor's place among the loaded rows, in the sort's order: its display row
// without a filter, and with one the place of the row it is on, the first when none is shown.
func (c *TableCore) loadedPlace() int {
	if c.filter == nil {
		return c.cur.Row
	}
	if c.cur.Row >= 0 && c.cur.Row < len(c.place) {
		return c.place[c.cur.Row]
	}
	return 0
}
