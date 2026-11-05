package mermaid

import (
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
)

// Role is what a label is, for its cap.
type Role uint8

const (
	RoleLabel   Role = iota // a box's, a text's, a legend row's, a title's
	RoleMessage             // a sequence message
)

// caps is the one table of caps, each its role's p95 in a survey of 1,430 labels from the KB's and
// AutoDoc's diagrams (2026-11-05): labels' p95 is 46–48 cells, sequence messages' 57. A label
// longer than its cap is drawn cut with …, its full text revealed on hover (HitAt).
var caps = [...]int{
	RoleLabel:   48,
	RoleMessage: 56,
}

// ellipsis ends a cut label. It is East Asian Ambiguous, so its width is the Measurer's: one cell,
// or two under an ambiguous-wide policy.
const ellipsis = "…"

// capLabel is s cut to its role's cap at a grapheme cluster's edge, with … last when it was cut: a
// wide cluster that would cross the cap drops whole, and the … takes its own width from the cap.
// w is the shown label's width. A kind measures a label only through it, before layout, so the
// room a layout reserves is the room drawn.
func capLabel(s string, r Role, m Measurer) (shown string, w int, capped bool) {
	w = m.StringWidth(s)
	limit := caps[r]
	if w <= limit {
		return s, w, false
	}
	ew := m.StringWidth(ellipsis)
	var b strings.Builder
	used := 0
	for cl := range tui.Graphemes(s) {
		cw := m.StringWidth(cl)
		if used+cw > limit-ew {
			break
		}
		b.WriteString(cl)
		used += cw
	}
	b.WriteString(ellipsis)
	return b.String(), used + ew, true
}

// hit is a capped label's cells and its whole text.
type hit struct {
	r    tui.Rect
	full string
}

// HitAt is the whole text of the capped label at (x, y) in the grid, and its cells; ok is false
// over anything else, an uncapped label included: only a cut label has more to reveal.
func (g *Grid) HitAt(x, y int) (full string, r tui.Rect, ok bool) {
	for _, h := range g.hits {
		if x >= h.r.X && x < h.r.X+h.r.W && y >= h.r.Y && y < h.r.Y+h.r.H {
			return h.full, h.r, true
		}
	}
	return "", tui.Rect{}, false
}

// label puts s at (x, y), capped as a label.
func (g *Grid) label(x, y int, s string, m Measurer) { g.labelAs(x, y, s, RoleLabel, m) }

// labelAs puts s at (x, y), capped as role r; a capped one keeps its whole text for hover.
func (g *Grid) labelAs(x, y int, s string, r Role, m Measurer) {
	if s == "" {
		return
	}
	shown, w, capped := capLabel(s, r, m)
	g.texts = append(g.texts, text{x: x, y: y, w: w, s: shown})
	if capped {
		g.hits = append(g.hits, hit{r: tui.Rect{X: x, Y: y, W: w, H: 1}, full: s})
	}
}

// box adds b at (x, y), its label capped, its size the form's around the shown label, and returns
// it.
func (g *Grid) box(b Box, x, y int, m Measurer) Box {
	w, h := boxSize(b.Form, b.Label, m)
	return g.boxIn(b, tui.Rect{X: x, Y: y, W: w, H: h}, m)
}

// boxIn adds b in r, a rect its layout gave it (a note widened over its participants), its label
// capped; a capped one's box reveals it whole (HitAt).
func (g *Grid) boxIn(b Box, r tui.Rect, m Measurer) Box {
	var capped bool
	b.shown, _, capped = capLabel(b.Label, RoleLabel, m)
	b.Rect = r
	if capped {
		g.hits = append(g.hits, hit{r: r, full: b.Label})
	}
	if len(b.Sections) > 0 { // each row capped, a capped one revealing itself whole over its own row
		b.sectionsShown = make([][]string, len(b.Sections))
		y := r.Y + 1
		for si, sec := range b.Sections {
			if si > 0 {
				y++ // the rule
			}
			for _, row := range sec {
				shown, _, rowCapped := capLabel(row, RoleLabel, m)
				b.sectionsShown[si] = append(b.sectionsShown[si], shown)
				if rowCapped {
					g.hits = append(g.hits, hit{r: tui.Rect{X: r.X, Y: y, W: r.W, H: 1}, full: row})
				}
				y++
			}
		}
	}
	g.Boxes = append(g.Boxes, b)
	return b
}

// boxSize is the size a box of form f takes around label, capped: what a layout reserves for it.
func boxSize(f Form, label string, m Measurer) (w, h int) {
	_, lw, _ := capLabel(label, RoleLabel, m)
	return formSize(f, lw)
}

// title puts the diagram's title at the top left, a blank row after it, and returns the first row
// free under it: 0 when there is no title.
func (g *Grid) title(s string, m Measurer) (y int) {
	if s == "" {
		return 0
	}
	g.label(0, 0, s, m)
	return 2
}

// widest is the widest of ss in cells, each capped as a label: a column's width.
func widest(m Measurer, ss ...string) int {
	w := 0
	for _, s := range ss {
		_, sw, _ := capLabel(s, RoleLabel, m)
		w = max(w, sw)
	}
	return w
}

// Tick is a mark on an axis: its column and the label written over it.
type Tick struct {
	X     int
	Label string
}

// axis draws an axis w cells wide from column x0: its line on row y+1, a ┬ in it at each tick,
// and each tick's label on row y over its ┬. It returns the ticks' columns, for gridColumns.
func (g *Grid) axis(x0, w, y int, ticks []Tick, m Measurer) (cols []int) {
	g.run(cell{x0, y + 1}, cell{x0 + w - 1, y + 1})
	for _, t := range ticks {
		cols = append(cols, t.X)
		g.label(t.X, y, t.Label, m)
		g.lines[cell{t.X, y + 1}] |= south
	}
	return cols
}

// gridMark is a grid column's cell: dotted, so a fill drawn over it (a bar) reads above it.
const gridMark = "┊"

// gridColumns marks the cells under each of cols, rows y0 to y1, with gridMark: a chart's grid,
// under its axis's ticks. Fills paint over marks, so a bar covers its cells of the grid.
func (g *Grid) gridColumns(cols []int, y0, y1 int) {
	for y := y0; y <= y1; y++ {
		for _, x := range cols {
			g.marks[cell{x, y}] = gridMark
		}
	}
}

// legendRow is one row of a key under the drawing: a swatch the kind gives (a slice's character, a
// footnote's mark), its label, and right-aligned columns (a share, then a value when the kind
// carries one).
type legendRow struct {
	swatch string
	label  string
	cols   []string
}

// legend adds rows to the key finish places under the drawing. Rows draw in the order given, and a
// kind gives its model's order: where swatches repeat, that order is the reader's key.
func (g *Grid) legend(rows ...legendRow) { g.notes = append(g.notes, rows...) }

// footnote is a mark for a label with no room on its line (an edge's, a frame's title): ¹…⁹, then
// *10…, one sequence per Grid, and the label as a legend row under that mark.
func (g *Grid) footnote(label string) (mark string) {
	g.foot++
	mark = footMark(g.foot)
	g.legend(legendRow{swatch: mark, label: label})
	return mark
}

// footMark is footnote n's mark: ¹…⁹, then *10, *11, ….
func footMark(n int) string {
	if n >= 1 && n <= 9 {
		return []string{"¹", "²", "³", "⁴", "⁵", "⁶", "⁷", "⁸", "⁹"}[n-1]
	}
	return "*" + strconv.Itoa(n)
}

// placeLegend is finish's: each legend row as a text under the drawing, a blank row first, the
// swatches in one column, the labels (capped) in the next, the columns right-aligned after the
// widest label. A capped label's cells reveal it whole (HitAt).
func (g *Grid) placeLegend(ms Measurer) {
	if len(g.notes) == 0 {
		return
	}
	m := ms.StringWidth
	labels := make([]string, len(g.notes))
	full := make([]bool, len(g.notes))
	sw, lw := 0, 0
	colW := []int{}
	for i, r := range g.notes {
		var capped bool
		labels[i], _, capped = capLabel(r.label, RoleLabel, ms)
		full[i] = capped
		sw = max(sw, m(r.swatch))
		lw = max(lw, m(labels[i]))
		for i, c := range r.cols {
			if i == len(colW) {
				colW = append(colW, 0)
			}
			colW[i] = max(colW[i], m(c))
		}
	}
	y := g.Size.H + 1
	if g.Size.H == 0 {
		y = 0
	}
	labelX := 0
	if sw > 0 {
		labelX = sw + 1
	}
	for ri, r := range g.notes {
		var b strings.Builder
		b.WriteString(r.swatch + strings.Repeat(" ", sw-m(r.swatch)))
		if sw > 0 {
			b.WriteString(" ")
		}
		b.WriteString(labels[ri] + strings.Repeat(" ", lw-m(labels[ri])))
		if full[ri] {
			g.hits = append(g.hits, hit{r: tui.Rect{X: labelX, Y: y, W: m(labels[ri]), H: 1}, full: r.label})
		}
		for i, c := range r.cols {
			b.WriteString("  " + strings.Repeat(" ", colW[i]-m(c)) + c)
		}
		line := strings.TrimRight(b.String(), " ")
		g.texts = append(g.texts, text{x: 0, y: y, w: m(line), s: line})
		y++
	}
	g.notes = nil
	g.measure()
}
