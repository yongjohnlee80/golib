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
	// topIn is how many of the top line's rows are above the view: the Editor's own Rendered view
	// can lay one line out taller than the view, so it scrolls by rows. 0 in the Raw view.
	topIn int
	left  int
	w, h  int // the text's area: the gutter's columns are not in w

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
}

// Reveal scrolls (line, col) into the viewport.
func (l *cellLayout) Reveal(line, col int) { l.ed.reveal(line, col) }

// PageLines is the viewport's height: a page.
func (l *cellLayout) PageLines() int { return max(l.h, 1) }

// VisibleLines is the lines from the top one to the last whose first row is in the view: the top
// line always starts at the view's first row, so it is never partly off.
func (l *cellLayout) VisibleLines() (first, last int) {
	if l.h <= 0 {
		return l.ed.core.buf.ln, l.ed.core.buf.ln
	}
	return l.ed.rowMap().visible(l.top, l.h)
}

// ScrollLines moves the top line by delta, down no further than the top that shows the last
// line at the view's bottom.
func (l *cellLayout) ScrollLines(delta int) {
	lines := l.ed.core.buf.textLines
	if l.h <= 0 {
		return
	}
	if m := l.ed.rowMap(); m.r != nil { // the Rendered view scrolls by rows, so a tall line scrolls too
		start := m.r.topRow(l.top) + l.topIn + delta
		l.top, l.topIn = m.r.atRow(max(0, min(start, len(m.r.rows)-l.h)))
		l.ed.MarkDirty()
		return
	}
	bottom := l.ed.rowMap().lowestTop(0, len(lines)-1, l.h)
	l.top, l.topIn = max(0, min(l.top+delta, max(bottom, l.top))), 0
	l.ed.MarkDirty()
}

// Measure is s's width in cells, under the App's width policy.
func (l *cellLayout) Measure(s string) int { return l.ed.width(s) }

// RowCount, RowOf and ColAt are the display rows the Editor wraps lines into (EditorRowLayout):
// gj and gk move by them, through the row map. RowOf is the wrap's own mapping, so a break space the wrap
// consumed is at the end of the row before it, as drawn.
func (l *cellLayout) RowCount(line int) int { return l.ed.rowsOfLine(line) }

func (l *cellLayout) RowOf(line, col int) (row, x int) { return l.ed.rowMap().rowOf(line, col) }

func (l *cellLayout) ColAt(line, row, x int) int { return l.ed.rowMap().colAt(line, row, x) }

// Changed repaints, and lays out again when the line count's digits move the gutter's width.
func (l *cellLayout) Changed(int) {
	if l.numbers && l.ed.gutterWidth() != l.gutter {
		l.ed.RequestLayout() // the lines' count has another number of digits: the gutter's width moves
	}
	l.ed.MarkDirty()
}

func (e *Editor) wrapWidth() int { return e.rowMap().width() }

// PointerShapes is the I-beam over the text, where a press puts the caret: the gutter and the
// scroll indicator keep the arrow. A rendered document, which paints in the text's place, gives
// its own.
func (e *Editor) PointerShapes(dst []tui.PointerRegion) []tui.PointerRegion {
	if e.doc.Shown() {
		return dst
	}
	return append(dst, tui.PointerRegion{Rect: tui.Rect{X: e.cells.gutter, W: e.wrapWidth(), H: e.cells.h}, Shape: tui.PointerText})
}

func (e *Editor) scrollable() bool { return e.rowMap().scrollable() }

func (e *Editor) rowsOfLine(i int) int { return e.rowMap().rowCount(i) }

// ensureVisible scrolls the cursor into the viewport.
func (e *Editor) ensureVisible() { e.reveal(e.core.buf.ln, e.core.buf.col) }

// reveal scrolls (ln, col) into the viewport.
func (e *Editor) reveal(ln, col int) {
	l := &e.cells
	if m := e.rowMap(); m.r != nil { // by rows: the caret's own row, however tall its line
		if l.h <= 0 {
			return
		}
		row, _ := m.r.caret(ln, col)
		start := m.r.topRow(l.top) + l.topIn
		switch {
		case row < start:
			start = row
			if row == m.r.firstRow(ln) { // its block's opening rows (a table's top rule) with it
				start = m.r.topRow(ln)
			}
		case row >= start+l.h:
			start = row - l.h + 1
		}
		l.top, l.topIn = m.r.atRow(start)
		l.left = 0
		return
	}
	l.top, l.left = e.rowMap().reveal(l.top, l.left, ln, e.core.buf.cellsAt(ln, col, e.width))
	l.topIn = 0
}

// keepTopLine keeps the view's top line when the Rendered rows changed under it (a picture laid):
// the rows of it above the view are clamped to the rows its block has now, so the view never starts
// past them.
func (e *Editor) keepTopLine() {
	l := &e.cells
	m := e.rowMap()
	if m.r == nil || len(m.r.rows) == 0 {
		l.topIn = 0
		return
	}
	start := min(m.r.topRow(l.top), len(m.r.rows)-1)
	end := len(m.r.rows) // past the top's block's last row
	for ri := start; ri < len(m.r.rows); ri++ {
		if m.r.rowBlock[ri] != m.r.rowBlock[start] {
			end = ri
			break
		}
	}
	l.topIn = max(min(l.topIn, end-1-start), 0)
}

// presentation is what the text is drawn as: its own Rendered layout or not, and the line laid as
// its source while text is typed (-1: none). Entering Insert on a tall rendered block, or Ctrl+T,
// lays the cursor's rows anew with the cursor where it was, so the view is revealed for them again;
// a wheel's scroll changes neither, so it stays.
func (e *Editor) presentation() [2]int {
	p := [2]int{0, -1}
	if e.rendered() {
		p[0] = 1
		if e.cells.typing() {
			p[1] = e.core.buf.ln
		}
	}
	return p
}

// represent asks for a layout when the presentation changed (Insert entered or left on a rendered
// block, the view switched): a mode's change only repaints, and Layout is where the view is revealed
// for the rows laid anew. The layout settles before the next input is routed, so a click queued
// behind the key lands on the revealed rows, not on the new rows under the old offset.
func (e *Editor) represent() {
	if ctx := e.Context(); ctx != nil && e.presentation() != e.presented {
		ctx.SettleBeforeInput()
	}
}

// Layout is greedy on both axes. The gutter, when the numbers show, takes its columns from the
// text's area, never all of it.
func (e *Editor) Layout(c tui.Constraints) tui.Size {
	if e.doc.Shown() {
		return e.layoutDocument(c)
	}
	total := fillW(c)
	e.rowW = total
	e.cells.gutter = min(e.gutterWidth(), total-1)
	e.cells.w = total - e.cells.gutter
	e.cells.h = max(fillH(c)-e.headerRows(), 0)
	moved := e.shown.moved([2]int{e.core.buf.ln, e.core.buf.col}, [2]int{e.cells.h, e.cells.w})
	pics := e.picLaid != e.picGen // a picture laid: the rows changed under the view
	if pics {
		e.picLaid = e.picGen
		e.keepTopLine()
	}
	if p := e.presentation(); moved || p != e.presented || pics && e.picReveal {
		e.presented = p
		e.ensureVisible()
	}
	return c.Constrain(tui.Size{W: total, H: e.cells.h + e.headerRows()})
}

// Cursor implements tui.CursorReporter.
func (e *Editor) Cursor() (int, int, bool) {
	l, b := &e.cells, &e.core.buf
	x, y, ok := e.rowMap().caret(l.top, l.left, b.ln, b.col, b.cellsAt(b.ln, b.col, e.width))
	if !ok {
		return 0, 0, false
	}
	return l.gutter + x, y + e.headerRows(), true
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
	return e.rowMap().hit(l.top, l.left, x, y, e.colAtCells, e.core.normalMax)
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

// width is s's width in the editor's cells: the terminal's measure, a tab tabCells.
func (e *Editor) width(s string) int { return tabAware(e.measure)(s) }

// view is the layout state the shared soft-wrap geometry needs. It is the
// only place this widget's viewport is handed to textBuffer.
func (e *Editor) view() wrapView {
	return wrapView{w: e.cells.w, h: e.cells.h, wrap: e.cells.wrap, measure: e.width}
}
