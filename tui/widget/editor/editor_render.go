package editor

import (
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/tui/widget/core"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/internal/cellimages"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// THE EDITOR'S PAINT: the gutter, then the text with its selection, tabs, indentation marks, ruler
// and scroll indicator.

// Render paints the viewport with the visual-selection fill.
func (e *Editor) Render(s tui.Surface) {
	e.shownImages = e.shownImages[:0] // the Rendered view's paint reports the images it shows; any other none
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	if top := e.headerRows(); top > 0 {
		// the switch's row: blank in the text's look, the switch at its right
		s.Fill(tui.Rect{W: sz.W, H: top}, " ", e.cells.styles.Text)
		x := e.switchX()
		e.sw.SetStyle(e.cells.styles.Text)
		e.sw.Render(s.Sub(tui.Rect{X: x, W: sz.W - x, H: 1}))
		s = s.Sub(tui.Rect{Y: top, W: sz.W, H: sz.H - top})
		sz = s.Size()
	}
	if sz.H <= 0 || e.doc.Shown() { // the document view paints itself
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
	if r := e.rowMap().r; r != nil {
		e.renderRenderedGutter(s, r, dim)
		return
	}
	y := 0
	m := e.rowMap().settled()
	for ln := l.top; ln < len(b.Lines) && y < h; ln++ {
		num := fmt.Sprint(ln + 1)
		st := dim
		if ln == b.Ln {
			st = l.styles.Text
		}
		kit.DrawText(s, l.gutter-gutterGap-len(num), y, num, st)
		y += max(m.rowCount(ln), 1)
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
	if m := e.rowMap(); m.r != nil {
		e.renderRendered(s, m)
		return
	}
	w := e.wrapWidth()
	p := &textPaint{e: e, s: s, hlf: e.beginHighlightFrame(), styledLn: -1, markedLn: -1}
	defer finishHighlightFrame(p.hlf)
	cellsOf := tabAware(s.StringWidth)
	m := e.rowMap().measuring(cellsOf) // painted as the surface measures; wrapped to the Editor's width (w)
	lineFill := func(y, ln int) {
		// A line-wise highlight covers the WHOLE screen row (S2), text or
		// not; clusters then paint over the fill.
		if c.keys.mode == ModeVisualLine && (e.Focused() || c.vPinned) && c.Selected(ln, 0) {
			s.Fill(tui.Rect{X: 0, Y: y, W: w, H: 1}, " ", l.styles.Selection.Inherit(l.styles.Text))
		}
	}
	// ends is where each screen row's text ends, for the ruler
	ends := make([]int, sz.H)
	y := 0
	for ln := l.top; ln < len(c.buf.Lines) && y < sz.H; ln++ {
		cs := c.buf.LineClusters(ln)
		if l.wrap == core.WrapNone {
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
		for _, r := range m.wrapAt(cs, w) {
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
		kit.PaintScrollIndicator(s, sz.W-1, sz.H, l.top, e.rowMap().maxTop())
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
	if l.wrap == core.WrapNone {
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
	if c.Highlighted(ln, col, e.Focused()) {
		st = e.cells.styles.Selection.Inherit(st)
	}
	return st
}

// cluster paints the cluster cl of (ln, col) at (x, y).
func (p *textPaint) cluster(x, y int, cl string, ln, col int) {
	e := p.e
	if ln != p.markedLn {
		p.marked, p.markedLn = e.core.IndentMarked(ln, e.Focused()), ln
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

// renderRendered paints the Editor's own Rendered view: its laid rows from the top line's first, each
// cell in its renderer's look, a source cell in the selection's over it when selected.
func (e *Editor) renderRendered(s tui.Surface, m rowMap) {
	sz := s.Size()
	l, c, r := &e.cells, e.core, m.r
	first := r.topRow(l.top) + l.topIn
	shown := map[int][2]int{}            // each wide block's first and last row on the view, for its marks
	marks := map[int][]highlight.Style{} // each source line's overlay (a find's marks, the host's), as painted
	marked := func(ln, col int, st style.Style) style.Style {
		ms, ok := marks[ln]
		if !ok {
			ms = c.OverlayStyles(ln)
			marks[ln] = ms
		}
		if col < len(ms) && ms[col] != highlight.Normal {
			if ms, ok := c.SyntaxStyle(ms[col]); ok {
				return ms.Inherit(st)
			}
		}
		return st
	}
	for y := 0; y < sz.H && first+y < len(r.rows); y++ {
		ri := first + y
		bi := r.rowBlock[ri]
		x0, w := m.content(bi)
		off, cx := m.panOf(bi), 0
		for _, cl := range r.rows[ri].Cells {
			cw := max(cl.W, 1)
			x := x0 + cx - off
			if cx += cw; x < x0 {
				continue // panned off to the left: a wide cluster cut there too
			}
			if x >= sz.W || (m.maxPan(bi) > 0 && x+cw > x0+w) {
				break // past the room, or a wide block's right edge column
			}
			st := onPage(cl.Style, l.styles.Text)
			if cl.Line >= 0 {
				st = marked(cl.Line, cl.Col, st)
				if c.Highlighted(cl.Line, cl.Col, e.Focused()) {
					st = l.styles.Selection.Inherit(st)
				}
			}
			s.SetCell(x, y, cl.Text, st)
		}
		if pic, ok := r.blockPic[bi].(*cellimages.Picture); ok && (y == 0 || r.rowBlock[ri-1] != bi) {
			// an image, from its first row on the view: its top left cell in the Editor, rows of it above
			// the view counted (the App crops)
			x0, _ := m.content(bi)
			e.shownImages = append(e.shownImages, shownPicture{pic, e.cells.gutter + x0 - m.panOf(bi), e.headerRows() + y - (ri - r.blockRow[bi])})
		}
		if m.maxPan(bi) > 0 {
			ys, ok := shown[bi]
			if !ok {
				ys[0] = y
			}
			ys[1] = y
			shown[bi] = ys
		}
	}
	th := e.cellTheme()
	mark := th.Syntax[MarkdownQuietStyle].Inherit(th.Text)
	for bi, ys := range shown {
		if x0, w := m.content(bi); x0 > 0 { // a room too narrow for its edge columns has no marks
			core.PaintPanMarks(s, x0-1, x0+w, (ys[0]+ys[1])/2, m.panOf(bi), m.maxPan(bi), mark)
		}
	}
	if m.scrollable() && m.width() < sz.W { // only in the column the layout left it: a one-cell view is all text
		kit.PaintScrollIndicator(s, sz.W-1, sz.H, first, max(len(r.rows)-sz.H, 0))
	}
}

// renderRenderedGutter numbers each Rendered row that starts its line (its line's first row), never
// an unnumbered one (a box's rule); the cursor's line in the text's colour.
func (e *Editor) renderRenderedGutter(s tui.Surface, r *renderedLayout, dim style.Style) {
	l, b := &e.cells, &e.core.buf
	first := r.topRow(l.top) + l.topIn
	for y := 0; y < s.Size().H && first+y < len(r.rows); y++ {
		ri := first + y
		ln := r.lineOfRow(ri)
		if rs := r.lineRows[ln]; r.rows[ri].Unnumbered || len(rs) == 0 || rs[0] != ri {
			continue
		}
		num := fmt.Sprint(ln + 1)
		st := dim
		if ln == b.Ln {
			st = l.styles.Text
		}
		kit.DrawText(s, l.gutter-gutterGap-len(num), y, num, st)
	}
}

// onPage is a Rendered cell's style st drawn on the page: a colour st leaves unset (a picture's
// blank cell sets none) is the page's own, as the fill under the cell, never the terminal's
// default. Only the two colours: st's attributes stay its own, so a page style's bold or italic
// never reaches a picture.
func onPage(st, page style.Style) style.Style {
	if _, ok := st.GetBackground(); !ok {
		if bg, ok := page.GetBackground(); ok {
			st = st.Background(bg)
		}
	}
	if _, ok := st.GetForeground(); !ok {
		if fg, ok := page.GetForeground(); ok {
			st = st.Foreground(fg)
		}
	}
	return st
}
