package widget

import (
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// THE EDITOR'S PAINT: the gutter, then the text with its selection, tabs, indentation marks, ruler
// and scroll indicator.

// Render paints the viewport with the visual-selection fill.
func (e *Editor) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 || e.doc.shown { // the document view paints itself
		return
	}
	if g := e.cells.gutter; g > 0 {
		e.renderGutter(s)
		s = s.Sub(tui.Rect{X: g, W: sz.W - g, H: sz.H})
	}
	e.renderText(s)
}

// renderGutter paints the line numbers, each line's on its first screen row, right-aligned: dimmed,
// so they stay out of the text's way, in their own colour (SetLineNumberColor) or muted and faint;
// the cursor's line in the text's colour, as Vim's CursorLineNr, so it shows where you are.
func (e *Editor) renderGutter(s tui.Surface) {
	l, b := &e.cells, &e.core.buf
	h := s.Size().H
	dim := l.styles.Text.Foreground(style.TokenTextMuted).Faint(true)
	if l.numberColored {
		dim = l.styles.Text.Foreground(l.numberColor)
	}
	s.Fill(tui.Rect{W: l.gutter, H: h}, " ", l.styles.Text)
	y := 0
	v := e.view().settled(b.textLines)
	for ln := l.top; ln < len(b.textLines) && y < h; ln++ {
		num := fmt.Sprint(ln + 1)
		st := dim
		if ln == b.ln {
			st = l.styles.Text
		}
		drawText(s, l.gutter-gutterGap-len(num), y, num, st)
		y += max(wrapRowsOfLine(b.textLines, ln, v), 1)
	}
}

// renderText paints the text's area.
func (e *Editor) renderText(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	l, c := &e.cells, e.core
	// The whole area wears the text's look first, as a TextInput's does (Qt's
	// base behind a TextEdit): the cells past each line's text match the
	// cells under it, whatever the editor sits on.
	s.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", l.styles.Text)
	w := e.wrapWidth()
	p := &textPaint{e: e, s: s, hlf: e.beginHighlightFrame(), styledLn: -1, markedLn: -1}
	defer finishHighlightFrame(p.hlf)
	cellsOf := tabAware(s.StringWidth)
	lineFill := func(y, ln int) {
		// A line-wise highlight covers the WHOLE screen row (S2), text or
		// not; clusters then paint over the fill.
		if c.keys.mode == ModeVisualLine && (e.focused() || c.vPinned) && c.Selected(ln, 0) {
			s.Fill(tui.Rect{X: 0, Y: y, W: w, H: 1}, " ", l.styles.Selection.Inherit(l.styles.Text))
		}
	}
	// ends is where each screen row's text ends, for the ruler
	ends := make([]int, sz.H)
	y := 0
	for ln := l.top; ln < len(c.buf.textLines) && y < sz.H; ln++ {
		cs := c.buf.lineClusters(ln)
		if l.wrap == WrapNone {
			lineFill(y, ln)
			x := -l.left
			for col, cl := range cs {
				cw := cellsOf(cl)
				if x+cw > w {
					break
				}
				if x >= 0 {
					p.cluster(x, y, cl, ln, col)
				}
				x += cw
			}
			ends[y] = x
			y++
			continue
		}
		for _, r := range wrapRanges(cs, w, cellsOf) {
			if y >= sz.H {
				break
			}
			lineFill(y, ln)
			x := 0
			for col := r[0]; col < r[1]; col++ {
				p.cluster(x, y, cs[col], ln, col)
				x += cellsOf(cs[col])
			}
			ends[y] = x
			y++
		}
	}
	e.renderRuler(s, ends, w)
	if e.scrollable() {
		paintScrollIndicator(s, sz.W-1, sz.H, l.top, wrapMaxTop(c.buf.textLines, e.view()))
	}
}

// renderRuler draws the ruler's column on each row whose text ends before it; ends is where each
// screen row's text ends, and w the text's width.
func (e *Editor) renderRuler(s tui.Surface, ends []int, w int) {
	l := &e.cells
	if l.ruler <= 0 {
		return
	}
	x := l.ruler - 1
	if l.wrap == WrapNone {
		x -= l.left
	}
	if x < 0 || x >= w {
		return
	}
	st := l.styles.Placeholder.Inherit(l.styles.Text)
	for row := range ends {
		if ends[row] <= x {
			s.SetCell(x, row, "│", st)
		}
	}
}

// textPaint is one paint of the editor's text: the frame's highlighting, and the line whose syntax
// styles and indentation marks were looked up last, which that line's clusters share.
type textPaint struct {
	e          *Editor
	s          tui.Surface
	hlf        *HighlightFrame
	lineStyles []highlight.Style
	styledLn   int
	marked     int // the clusters of markedLn shown as indentation
	markedLn   int
}

// style is the look of the cluster at (ln, col): the text's, its syntax style over that, and the
// selection over both.
func (p *textPaint) style(ln, col int) style.Style {
	e, c := p.e, p.e.core
	st := e.cells.styles.Text
	if ln != p.styledLn {
		p.lineStyles, p.styledLn = p.hlf.Styles(ln), ln
	}
	if c.Highlighting() {
		k := highlight.Normal
		if col < len(p.lineStyles) {
			k = p.lineStyles[col]
		}
		if sst, ok := c.SyntaxStyle(k); ok {
			st = sst.Inherit(st)
		}
	}
	if c.Highlighted(ln, col, e.focused()) {
		st = e.cells.styles.Selection.Inherit(st)
	}
	return st
}

// cluster paints the cluster cl of (ln, col) at (x, y).
func (p *textPaint) cluster(x, y int, cl string, ln, col int) {
	e := p.e
	if ln != p.markedLn {
		p.marked, p.markedLn = e.core.IndentMarked(ln, e.focused()), ln
	}
	st := p.style(ln, col)
	// A tab is tabCells blank cells, the first an arrow when it is marked indentation.
	if cl == "\t" {
		first, fst := " ", st
		if col < p.marked {
			first, fst = "→", e.cells.styles.Placeholder.Inherit(st)
		}
		p.s.SetCell(x, y, first, fst)
		for i := 1; i < tabCells; i++ {
			p.s.SetCell(x+i, y, " ", st)
		}
		return
	}
	// A leading space shown as indentation: dimmed as the ruler is, over any selection,
	// keeping any combining mark it carries.
	if col < p.marked && strings.HasPrefix(cl, " ") {
		cl, st = "·"+cl[1:], e.cells.styles.Placeholder.Inherit(st)
	}
	p.s.SetCell(x, y, cl, st)
}

// tabCells is how many cells a tab takes in the cell editor: the room the native editor gives it
// (four spaces), never the width a terminal gives a control character, which is none.
const tabCells = 4

// tabAware is measure with each tab counted as tabCells.
func tabAware(measure func(string) int) func(string) int {
	return func(s string) int {
		n := strings.Count(s, "\t")
		if n == 0 {
			return measure(s)
		}
		return measure(strings.ReplaceAll(s, "\t", "")) + n*tabCells
	}
}
