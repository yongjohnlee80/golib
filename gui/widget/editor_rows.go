package widget

import (
	"github.com/yongjohnlee80/golib/gui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

var _ tuiwidget.EditorRowLayout = (*pixelLayout)(nil)

// THE EDITOR'S DISPLAY ROWS — the GUI Editor's rows for Vim's gj and gk (tui/widget's
// EditorRowLayout), in pixels. A source line's rows are the paragraph lines its spans are drawn
// on: a raw line's wraps, or a rendered block's lines holding it. RowOf is where the caret is drawn
// (posIn: hidden markup on the next drawn cluster); ColAt hit-tests the row (Para.At: past hidden
// clusters to the first drawn one), held to the row's own clusters, so a point past a wrapped
// row's end is its last cluster, not the next row's first.

// rowsOf is line's display rows in its laid block: a block line and one of its paragraph's lines
// each, in reading order. Before the first paint there is no shaper to lay out with: none, so a
// line is one row.
func (l *pixelLayout) rowsOf(line int) (*laidBlock, [][2]int) {
	if l.sh == nil {
		return nil, nil
	}
	i := l.blockOf(line)
	if i >= len(l.blocks) {
		return nil, nil
	}
	lb := l.lay(i, nil)
	var rows [][2]int
	for li, ll := range lb.bl.Lines {
		for pl, pline := range ll.Para.Lines {
			for _, f := range pline.Frags {
				if ll.Spans[f.Span].Line == line {
					rows = append(rows, [2]int{li, pl})
					break
				}
			}
		}
	}
	return lb, rows
}

// RowCount is how many display rows line takes: 1 when it has none of its own (an empty line).
func (l *pixelLayout) RowCount(line int) int {
	_, rows := l.rowsOf(line)
	return max(len(rows), 1)
}

// RowOf is (line, col)'s display row and its x on it, in pixels from the row's start.
func (l *pixelLayout) RowOf(line, col int) (row, x int) {
	lb, rows := l.rowsOf(line)
	if lb == nil || len(rows) == 0 {
		return 0, 0
	}
	li, pos, ok := posIn(lb, line, col)
	if !ok {
		return 0, 0
	}
	para := lb.bl.Lines[li].Para
	r := para.Caret(pos)
	for k, rw := range rows {
		if rw[0] != li {
			continue
		}
		if pline := para.Lines[rw[1]]; r.Y >= pline.Y && r.Y < pline.Y+pline.H {
			return k, int(r.X + 0.5)
		}
	}
	return 0, int(r.X + 0.5)
}

// ColAt is the column of line under x on display row `row`: the cluster x is over, held to the
// row's own clusters.
func (l *pixelLayout) ColAt(line, row, x int) int {
	lb, rows := l.rowsOf(line)
	if lb == nil || len(rows) == 0 {
		return 0
	}
	rw := rows[max(0, min(row, len(rows)-1))]
	ll := lb.bl.Lines[rw[0]]
	pline := ll.Para.Lines[rw[1]]
	lo, hi := -1, -1 // the row's clusters of line: [lo, hi]
	for _, f := range pline.Frags {
		sp := ll.Spans[f.Span]
		if sp.Line != line || f.To <= f.From {
			continue
		}
		a, b := sp.Col+clusters(sp.Text[:f.From]), sp.Col+clusters(sp.Text[:f.To])-1
		if lo < 0 || a < lo {
			lo = a
		}
		hi = max(hi, b)
	}
	pos := ll.Para.At(gui.Pt(float32(x), pline.Y+pline.H/2))
	col := lo
	if sp := ll.Spans[pos.Span]; sp.Line == line {
		col = sp.Col + clusters(sp.Text[:min(pos.Offset, len(sp.Text))])
	}
	if lo >= 0 {
		col = max(lo, min(col, hi))
	}
	return max(col, 0)
}
