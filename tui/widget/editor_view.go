package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// THE EDITOR'S VIEW: its viewport and where text lands in it — the layout the core asks about
// (cellLayout), revealing the cursor, the caret cell, and the text position under a cell.

// cellLayout is the Editor's view of its core in terminal cells: the viewport, the gutter, the
// ruler, the looks, and the drag's edge auto-scroll. It is the EditorLayout the core reveals,
// measures and pages through.
type cellLayout struct {
	ed *Editor // the widget it lays out: its context measures and repaints

	// viewport (same discipline as TextArea)
	wrap WrapMode
	top  int
	left int
	w, h int // the text's area: the gutter's columns are not in w

	// numbers shows each line's number in a gutter at the left; gutter is the columns it took at
	// the last layout (0 without numbers)
	numbers bool
	gutter  int
	// ruler is the column a vertical guide marks, 1-based; 0 for none (WithRuler).
	ruler int

	// cursorColor is the hardware cursor's colour while cursorColored (tui.CursorColorer)
	cursorColor   style.Color
	cursorColored bool
	// numberColor is the line numbers' colour while numberColored; muted and faint otherwise
	numberColor   style.Color
	numberColored bool

	styles TextInputStyles

	// dragEdge is -1 or +1 while the dragging pointer is above or below the view, which keeps
	// scrolling on dragTick (a GUI editor's auto-scroll); dragX is the pointer's column.
	dragEdge   int
	dragX      int
	dragCancel func()
}

// Reveal scrolls (line, col) into the viewport.
func (l *cellLayout) Reveal(line, col int) { l.ed.reveal(line, col) }

// PageLines is the viewport's height: a page.
func (l *cellLayout) PageLines() int { return max(l.h, 1) }

// VisibleLines is the lines from the top one to the last whose first row is in the view: the top
// line always starts at the view's first row, so it is never partly off.
func (l *cellLayout) VisibleLines() (first, last int) {
	lines := l.ed.core.buf.textLines
	if l.h <= 0 {
		return l.ed.core.buf.ln, l.ed.core.buf.ln
	}
	v := l.ed.view().settled(lines)
	first = max(0, min(l.top, len(lines)-1))
	last, rows := first, 0
	for i := first; i < len(lines) && rows < l.h; i++ {
		last = i
		rows += wrapRowsOfLine(lines, i, v)
	}
	return first, last
}

// ScrollLines moves the top line by delta, down no further than the top that shows the last
// line at the view's bottom.
func (l *cellLayout) ScrollLines(delta int) {
	lines := l.ed.core.buf.textLines
	if l.h <= 0 {
		return
	}
	bottom := lowestTop(lines, 0, len(lines)-1, l.h, l.ed.view())
	l.top = max(0, min(l.top+delta, max(bottom, l.top)))
	l.ed.MarkDirty()
}

// Measure is s's width in cells, under the App's width policy.
func (l *cellLayout) Measure(s string) int { return l.ed.width(s) }

// RowCount, RowOf and ColAt are the display rows the Editor wraps lines into (EditorRowLayout):
// gj and gk move by them. RowOf is the wrap's own mapping (wrapPosOf), so a break space the wrap
// consumed is at the end of the row before it, as drawn.
func (l *cellLayout) RowCount(line int) int { return l.ed.rowsOfLine(line) }

func (l *cellLayout) RowOf(line, col int) (row, x int) {
	lines, v := l.ed.core.buf.textLines, l.ed.view()
	if v.wrap == WrapNone {
		return 0, cellsBefore(clusters(lines[line]), col, v.measure)
	}
	return wrapPosOf(lines, line, col, v)
}

func (l *cellLayout) ColAt(line, row, x int) int {
	lines, v := l.ed.core.buf.textLines, l.ed.view()
	cs := clusters(lines[line])
	rows := [][2]int{{0, len(cs)}}
	if v.wrap != WrapNone {
		rows = wrapRanges(cs, v.usableWidth(lines), v.measure)
	}
	return colInRow(cs, rows, max(0, min(row, len(rows)-1)), x, v.measure)
}

// Changed repaints, and lays out again when the line count's digits move the gutter's width.
func (l *cellLayout) Changed(int) {
	if l.numbers && l.ed.gutterWidth() != l.gutter {
		l.ed.RequestLayout() // the lines' count has another number of digits: the gutter's width moves
	}
	l.ed.MarkDirty()
}

func (e *Editor) wrapWidth() int { return wrapUsableWidth(e.core.buf.textLines, e.view()) }

func (e *Editor) scrollable() bool { return wrapScrollable(e.core.buf.textLines, e.view()) }

func (e *Editor) rowsOfLine(i int) int { return wrapRowsOfLine(e.core.buf.textLines, i, e.view()) }

// ensureVisible scrolls the cursor into the viewport.
func (e *Editor) ensureVisible() { e.reveal(e.core.buf.ln, e.core.buf.col) }

// reveal scrolls (ln, col) into the viewport.
func (e *Editor) reveal(ln, col int) {
	l := &e.cells
	l.top, l.left = revealCaret(e.core.buf.textLines, e.view(), l.top, l.left, ln, e.core.buf.cellsAt(ln, col, e.width))
}

// Layout is greedy on both axes. The gutter, when the numbers show, takes its columns from the
// text's area, never all of it.
func (e *Editor) Layout(c tui.Constraints) tui.Size {
	if e.doc.shown {
		return e.layoutDocument(c)
	}
	total := fillW(c)
	e.cells.gutter = min(e.gutterWidth(), total-1)
	e.cells.w = total - e.cells.gutter
	e.cells.h = fillH(c)
	if e.shown.moved([2]int{e.core.buf.ln, e.core.buf.col}, [2]int{e.cells.h, e.cells.w}) {
		e.ensureVisible()
	}
	return c.Constrain(tui.Size{W: total, H: e.cells.h})
}

// Cursor implements tui.CursorReporter.
func (e *Editor) Cursor() (int, int, bool) {
	l, b := &e.cells, &e.core.buf
	x, y, ok := caretAt(b.textLines, e.view(), l.top, l.left, b.ln, b.col, b.cellsAt(b.ln, b.col, e.width))
	if !ok {
		return 0, 0, false
	}
	return l.gutter + x, y, true
}

// posAt inverts the viewport mapping in Cursor(): a viewport cell becomes a
// buffer position. It is the exact inverse of the forward path for each wrap
// mode, so a click resolves to the position the caret would be drawn at.
//
// Clamping: past end-of-line lands on the last column, below the last painted
// line lands on the last buffer line. A wide grapheme resolves to the cell it
// STARTS at, which is why the column walk accumulates measured widths
// instead of counting cells.
func (e *Editor) posAt(x, y int) (ln, col int) {
	l, b := &e.cells, &e.core.buf
	if len(b.textLines) == 0 {
		return 0, 0
	}
	lastLn := len(b.textLines) - 1

	if l.wrap == WrapNone {
		ln = min(l.top+max(y, 0), lastLn)
		cs := b.lineClusters(ln)
		return ln, e.colAtCells(cs, 0, len(cs), l.left+max(x, 0))
	}

	// WrapSoft: walk the same wrap computation the renderer used, rather than
	// dividing by width — one logical line spans several visual rows.
	remaining := max(y, 0)
	v := e.view().settled(b.textLines)
	for i := l.top; i <= lastLn; i++ {
		rows := wrapRowsOfLine(b.textLines, i, v)
		if remaining < rows || i == lastLn {
			cs := b.lineClusters(i)
			ranges := wrapRanges(cs, v.usable, e.width)
			r := ranges[min(remaining, len(ranges)-1)]
			// Bounded to THIS row: wrapRanges is [start,end), and a word-wrapped
			// row can be shorter than the viewport, so an unbounded scan would run
			// through its blank tail into clusters painted on the NEXT visual row.
			return i, e.colAtCells(cs, r[0], r[1], max(x, 0))
		}
		remaining -= rows
	}
	return lastLn, e.core.normalMax(lastLn)
}

// colAtCells walks clusters in [from,end), accumulating measured cell widths, and
// returns the column whose cell span contains `cells`. A click in the trailing
// half of a double-width grapheme resolves to that grapheme, not the next one.
// Past the end of the range it clamps to the range's LAST column, which is what
// keeps a wrapped row's blank tail on its own row.
func (e *Editor) colAtCells(cs []string, from, end, cells int) int {
	end = min(end, len(cs))
	acc := 0
	for i := from; i < end; i++ {
		w := e.width(cs[i])
		if cells < acc+w {
			return i
		}
		acc += w
	}
	return max(from, end-1)
}

func (e *Editor) wrapPos(ln, col int) (row, x int) {
	return wrapPosOf(e.core.buf.textLines, ln, col, e.view())
}

// width is s's width in the editor's cells: the terminal's measure, a tab tabCells.
func (e *Editor) width(s string) int { return tabAware(e.measure)(s) }

// view is the layout state the shared soft-wrap geometry needs. It is the
// only place this widget's viewport is handed to textBuffer.
func (e *Editor) view() wrapView {
	return wrapView{w: e.cells.w, h: e.cells.h, wrap: e.cells.wrap, measure: e.width}
}
