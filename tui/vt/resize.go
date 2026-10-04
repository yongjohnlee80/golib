package vt

import "github.com/yongjohnlee80/golib/tui/style"

// styleNone is the default colour: what reflow pads rows with.
var styleNone style.Color

// Resize changes the screen to rows x cols. The primary screen and its
// scrollback reflow: rows autowrap broke are joined and re-broken at the new
// width, and the cursor (and a saved cursor) keep their place in the text.
// The alternate screen is cut or padded, as xterm's is; its program redraws
// on SIGWINCH. The scroll region resets to the whole screen.
func (s *Screen) Resize(rows, cols int) {
	rows, cols = max(rows, 1), max(cols, 1)
	if rows == s.rows && cols == s.cols {
		return
	}
	onAlt := s.buf == &s.alt

	// The primary's marks: the live cursor when it is shown, and its DECSC
	// slot (which 1049 restores on the way back from the alternate screen).
	var marks []*cursor
	if !onAlt {
		marks = append(marks, &s.cur)
	}
	if s.pri.hasSaved {
		marks = append(marks, &s.pri.saved)
	}
	s.reflowPrimary(rows, cols, marks, !onAlt)

	var altMarks []*cursor
	if onAlt {
		altMarks = append(altMarks, &s.cur)
	}
	if s.alt.hasSaved {
		altMarks = append(altMarks, &s.alt.saved)
	}
	s.alt.lines = cutLines(s.alt.lines, rows, cols, altMarks)

	oldCols := s.cols
	s.rows, s.cols = rows, cols
	s.top, s.bot = 0, rows-1
	tabs := make([]bool, cols)
	copy(tabs, s.tabs)
	for i := (oldCols + 7) / 8 * 8; i < cols; i += 8 {
		tabs[i] = true
	}
	s.tabs = tabs
	s.last.ok = false
}

// mark is a tracked position during reflow: the logical line it is in and
// its cell offset there.
type mark struct {
	c       *cursor
	logical int
	offset  int
	pending bool // the cursor's wrap was pending: offset is one past its cell
}

// reflowPrimary rewraps the scrollback and the primary screen to cols, then
// keeps the last rows physical rows as the screen. anchor says marks[0] is
// the live cursor, which must stay on the screen.
func (s *Screen) reflowPrimary(rows, cols int, marks []*cursor, anchor bool) {
	phys := append(s.hist.all(), s.pri.lines...)
	histN := s.hist.len()

	// Join physical rows into logical lines, placing each mark.
	type logical struct{ cells []Cell }
	var ls []logical
	var ms []mark
	cur := -1
	for i, ln := range phys {
		if cur < 0 || !phys[i-1].wrapped {
			ls = append(ls, logical{})
			cur = len(ls) - 1
		}
		for _, c := range marks {
			if histN+c.row == i {
				off := len(ls[cur].cells) + c.col
				if c.wrapNext {
					off++ // the cursor is past the last column's cell
				}
				ms = append(ms, mark{c: c, logical: cur, offset: off, pending: c.wrapNext})
			}
		}
		cells := ln.cells
		if ln.wrapped && ln.pad {
			cells = cells[:len(cells)-1] // a wide cluster's wrap padding
		}
		ls[cur].cells = append(ls[cur].cells, cells...)
	}

	// Re-break each logical line at cols.
	var out []line
	cursorRow := -1
	for li, lg := range ls {
		cells := trimBlank(lg.cells)
		first := len(out)
		row := make([]Cell, 0, cols)
		flush := func(wrapped bool) {
			for len(row) < cols {
				row = append(row, blank(styleNone))
			}
			out = append(out, line{cells: row, wrapped: wrapped})
			row = make([]Cell, 0, cols)
		}
		offsetPos := make(map[int][2]int) // offset -> (row in this logical, col)
		need := map[int]bool{}
		for _, m := range ms {
			if m.logical == li {
				need[m.offset] = true
			}
		}
		for j := 0; j < len(cells); j++ {
			c := cells[j]
			if c.Width == 0 {
				continue // placed with its lead
			}
			w := int(c.Width)
			if len(row)+w > cols {
				padded := len(row) < cols
				flush(true)
				out[len(out)-1].pad = padded
			}
			if need[j] {
				offsetPos[j] = [2]int{len(out) - first, len(row)}
			}
			if w == 2 && j+1 < len(cells) && need[j+1] {
				offsetPos[j+1] = [2]int{len(out) - first, len(row) + 1}
			}
			row = append(row, c)
			if w == 2 {
				cont := Cell{Attr: c.Attr, FG: c.FG, BG: c.BG}
				if j+1 < len(cells) && cells[j+1].Width == 0 {
					cont = cells[j+1]
				}
				row = append(row, cont)
			}
		}
		// Marks at or past the content's end sit on blanks after it.
		end := len(cells)
		for off := range need {
			if off < end {
				continue
			}
			pos := (len(out)-first)*cols + len(row) + (off - end)
			offsetPos[off] = [2]int{pos / cols, pos % cols}
		}
		maxRow := len(out) - first
		for _, p := range offsetPos {
			maxRow = max(maxRow, p[0])
		}
		flush(false)
		for len(out)-first <= maxRow {
			out[len(out)-1].wrapped = true
			flush(false)
		}
		for _, m := range ms {
			if m.logical != li {
				continue
			}
			p := offsetPos[m.offset]
			m.c.row, m.c.col, m.c.wrapNext = first+p[0], p[1], false
			if (m.pending || m.offset == end) && p[1] == 0 && p[0] > 0 {
				// The cursor is just past the text, which fills its row:
				// keep the wrap pending at the row's end rather than start
				// a row for it.
				m.c.row, m.c.col, m.c.wrapNext = m.c.row-1, cols-1, true
			}
			if anchor && m.c == marks[0] {
				cursorRow = m.c.row
			}
		}
	}

	// Rows below both the cursor and the last text are dropped, so a
	// narrower screen does not push text into the scrollback for blanks.
	last := cursorRow
	for i := len(out) - 1; i > last; i-- {
		if !lineBlank(out[i]) {
			last = i
			break
		}
	}
	out = out[:max(last+1, 0)]

	start := max(len(out)-rows, 0)
	if cursorRow >= 0 {
		start = min(start, cursorRow)
	}
	screen := out[start:min(start+rows, len(out))]
	pri := make([]line, 0, rows)
	pri = append(pri, screen...)
	for len(pri) < rows {
		pri = append(pri, line{cells: blankCells(cols)})
	}
	s.pri.lines = pri
	s.hist.set(out[:start])
	for _, m := range ms {
		m.c.row = min(max(m.c.row-start, 0), rows-1)
	}
}

// trimBlank drops trailing blank cells.
func trimBlank(cells []Cell) []Cell {
	n := len(cells)
	for n > 0 && cells[n-1].isBlank() {
		n--
	}
	return cells[:n]
}

func lineBlank(l line) bool {
	for _, c := range l.cells {
		if !c.isBlank() {
			return false
		}
	}
	return true
}

func blankCells(cols int) []Cell {
	cells := make([]Cell, cols)
	for i := range cells {
		cells[i] = blank(styleNone)
	}
	return cells
}

// cutLines fits the alternate screen to rows x cols: rows are cut or padded
// at the right, and rows leave from the top only as far as keeps the cursor
// on the screen.
func cutLines(ls []line, rows, cols int, marks []*cursor) []line {
	drop := 0
	for _, c := range marks {
		drop = max(drop, c.row-(rows-1))
	}
	ls = ls[drop:]
	out := make([]line, rows)
	for i := range out {
		if i >= len(ls) {
			out[i] = line{cells: blankCells(cols)}
			continue
		}
		cells := make([]Cell, cols)
		n := copy(cells, ls[i].cells)
		for j := n; j < cols; j++ {
			cells[j] = blank(styleNone)
		}
		if cells[cols-1].Width == 2 {
			cells[cols-1] = blank(cells[cols-1].BG) // its second half was cut
		}
		out[i] = line{cells: cells}
	}
	for _, c := range marks {
		c.row = min(max(c.row-drop, 0), rows-1)
		c.col = min(c.col, cols-1)
		c.wrapNext = false
	}
	return out
}
