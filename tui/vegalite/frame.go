package vegalite

import (
	"math"
	"slices"
	"strings"

	"github.com/yongjohnlee80/golib/highlight"
	pv "github.com/yongjohnlee80/golib/parse/vegalite"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// THE FRAME — what every tier draws inside (bars, Braille plots): the title and subtitle, the y
// axis's tick labels down the left with their ┤ on the axis, the x axis's floor with its ┬ and
// labels, the axis titles, and the legend. It leaves the plot: a rectangle of cells and the two
// maps from a channel's value to a place in it, which a tier draws with. The frame reads the
// chart's axes as parse/vegalite plotted them (domains, ticks, formats), so a chart's numbers
// read the same in the window and the terminal.

// Measurer is the one thing a layout needs from a surface: text width in cells (tui.Surface
// satisfies it).
type Measurer interface{ StringWidth(s string) int }

// Role is what a cell or a text draws; each takes one of the Theme's styles.
type Role uint8

const (
	RoleText  Role = iota // a title, a tick label, a legend row
	RoleAxis              // an axis line, its tick marks, a grid line
	RoleMark              // a mark in a series' colour: a bar, a dot pattern, a rule
	RoleMuted             // an axis title, the legend's names
)

// Theme is the painter's styles: the text, the axes, and the series' colours in order (a series
// past the end takes them again from the first). A mark is the page's text style in its series'
// colour; a cell where two stacked series meet wears one's colour over the other's.
type Theme struct {
	Text, Axis, Muted style.Style
	Series            []style.ColorSpec
}

// SeriesRoles are the syntax roles whose colours a chart's series take, in order: the page's code
// colours, which every theme sets and which read on its page (they paint its code blocks), and which
// are hues apart from one another. A role a theme leaves unset, or sets to another's colour, is
// passed over.
var SeriesRoles = []highlight.Style{highlight.Keyword, highlight.String, highlight.DecVal,
	highlight.Function, highlight.DataType, highlight.Constant, highlight.BuiltIn, highlight.Preprocessor}

// TokenTheme is the chart theme a page paints in when it has no syntax colours to give: every style
// on the page's own background (style.PageText), as the diagrams' (tui/mermaid TokenTheme), and the
// series in the tokens every theme sets for the page: the document's accent, the text, the muted
// text. A host with code colours gives ThemeFromSyntax instead.
func TokenTheme() Theme {
	return Theme{Text: style.PageText(), Axis: style.PageText().Foreground(style.TokenTextMuted),
		Muted:  style.PageText().Foreground(style.TokenTextMuted),
		Series: []style.ColorSpec{style.TokenAccentDocument, style.TokenForeground, style.TokenTextMuted}}
}

// ThemeFromSyntax is the chart theme in the page's code colours: syntax gives each highlight role's
// style (a host's SyntaxStyles), and the series take SeriesRoles' foregrounds in order, each colour
// once. Fewer than two distinct ones is TokenTheme's series.
func ThemeFromSyntax(syntax func(highlight.Style) style.Style) Theme {
	th := TokenTheme()
	var series []style.ColorSpec
	var seen []style.Color
	for _, r := range SeriesRoles {
		c, ok := syntax(r).GetForeground()
		if !ok || c.IsDefault() || slices.Contains(seen, c) {
			continue
		}
		seen = append(seen, c)
		series = append(series, c)
	}
	if len(series) >= 2 {
		th.Series = series
	}
	return th
}

// seriesStyle is series fg's style in th, on series bg's colour (bg −1: the page).
func (th Theme) seriesStyle(fg, bg int) style.Style {
	st := th.Text
	if fg >= 0 && len(th.Series) > 0 {
		st = st.Foreground(th.Series[fg%len(th.Series)])
	}
	if bg >= 0 && len(th.Series) > 0 {
		st = st.Background(th.Series[bg%len(th.Series)])
	}
	return st
}

// Rect is a rectangle of cells.
type Rect struct{ X, Y, W, H int }

// Drawing is a chart laid out in cells, painted later into any surface of at least W×H.
type Drawing struct {
	W, H  int
	cells map[[2]int]drawn // the marks and the axes, one rune a cell
	texts []text           // labels, each w cells wide at (x, y)
}

type drawn struct {
	r      rune
	role   Role
	series int // the mark's series (its foreground), −1 for none
	bg     int // the series under it (a stack's meeting cell), −1 for the page
}

type text struct {
	x, y, w int
	s       string
	role    Role
	series  int // a legend swatch's series, else -1
}

func newDrawing(w, h int) *Drawing { return &Drawing{W: w, H: h, cells: map[[2]int]drawn{}} }

// Size is the drawing's size in cells: what the page sets aside, and pans when wider.
func (d *Drawing) Size() tui.Size { return tui.Size{W: d.W, H: d.H} }

func (d *Drawing) put(x, y int, r rune, role Role, series int) {
	if x < 0 || y < 0 || x >= d.W || y >= d.H {
		return
	}
	d.cells[[2]int{x, y}] = drawn{r, role, series, -1}
}

// Cell is a mark a tier draws: a glyph at (X, Y) within the plot, in series FG's colour on series
// BG's (−1: the page). A bar's eighth where two stacked segments meet is one series over another.
type Cell struct {
	X, Y   int
	Glyph  rune
	FG, BG int
}

// mark draws a tier's cell into the plot.
func (f *frame) mark(c Cell) {
	x, y := f.plot.X+c.X, f.plot.Y+c.Y
	if c.X < 0 || c.Y < 0 || c.X >= f.plot.W || c.Y >= f.plot.H {
		return
	}
	f.d.cells[[2]int{x, y}] = drawn{c.Glyph, RoleMark, c.FG, c.BG}
}

func (d *Drawing) say(x, y int, s string, role Role, m Measurer) {
	if y < 0 || y >= d.H || s == "" {
		return
	}
	d.texts = append(d.texts, text{x: x, y: y, w: m.StringWidth(s), s: s, role: role, series: -1})
}

// Paint draws d into s with its top left at (x0, y0).
func (d *Drawing) Paint(s tui.Surface, x0, y0 int, th Theme) {
	for at, c := range d.cells {
		st := th.Axis
		switch c.role {
		case RoleMark:
			st = th.seriesStyle(c.series, c.bg)
		case RoleText:
			st = th.Text
		case RoleMuted:
			st = th.Muted
		}
		s.SetCell(x0+at[0], y0+at[1], string(c.r), st)
	}
	for _, t := range d.texts {
		st := th.Text
		switch {
		case t.series >= 0:
			st = th.seriesStyle(t.series, -1)
		case t.role == RoleMuted:
			st = th.Muted
		case t.role == RoleAxis:
			st = th.Axis
		}
		x := x0 + t.x
		for g := range tui.Graphemes(t.s) {
			s.SetCell(x, y0+t.y, g, st)
			x += max(tui.StringWidth(g), 1)
		}
	}
}

// Rows are d as text, a row a line, each its texts over its cells: what a golden reads. A wide
// grapheme takes its cells, the next of them left out.
func (d *Drawing) Rows() []string {
	grid := make([][]string, d.H)
	for y := range grid {
		grid[y] = make([]string, d.W)
		for x := range grid[y] {
			grid[y][x] = " "
		}
	}
	for at, c := range d.cells {
		grid[at[1]][at[0]] = string(c.r)
	}
	for _, t := range d.texts {
		x := t.x
		for g := range tui.Graphemes(t.s) {
			w := max(tui.StringWidth(g), 1)
			if x < 0 || x+w > d.W {
				break
			}
			grid[t.y][x] = g
			for k := 1; k < w; k++ {
				grid[t.y][x+k] = ""
			}
			x += w
		}
	}
	out := make([]string, d.H)
	for y, row := range grid {
		out[y] = strings.TrimRight(strings.Join(row, ""), " ")
	}
	return out
}

// frame is a chart's frame laid out: the drawing it is in, the plot's rectangle, and the maps a
// tier draws with.
type frame struct {
	d    *Drawing
	plot Rect
	ch   *pv.Chart
}

// plotRows is the plot's height in rows a chart gets: its own height in Vega-Lite pixels at about
// 20 a row, within 6 and 20; 10 for one that says none.
func plotRows(ch *pv.Chart) int {
	if ch.Height <= 0 {
		return 10
	}
	return min(max(int(math.Round(float64(ch.Height)/20)), 6), 20)
}

// niced is ch with each continuous axis that asks for it (Axis.Nice) widened to nice ends for the
// ticks it gets here (NiceDomain), so the ticks land on the axis's ends: parse/vegalite plots the
// data's own range and leaves the rounding to a renderer, whose tick count is its own.
func niced(ch *pv.Chart, rows, width int) *pv.Chart {
	c := *ch
	if c.X.Nice && !c.X.Discrete && !c.X.Temporal {
		c.X.Lo, c.X.Hi = pv.NiceDomain(c.X.Lo, c.X.Hi, max(width/8, 2))
	}
	if c.Y.Nice && !c.Y.Discrete && !c.Y.Temporal {
		c.Y.Lo, c.Y.Hi = pv.NiceDomain(c.Y.Lo, c.Y.Hi, max(rows/2, 2))
	}
	return &c
}

// ticksFor is an axis's ticks and their labels: a discrete axis its categories; a continuous one
// its nice ticks, about one every two rows (or eight columns) of n cells.
func ticksFor(a pv.Axis, n, every int) (vals []float64, labels []string) {
	if a.Discrete {
		for i, c := range a.Domain {
			vals, labels = append(vals, float64(i)), append(labels, c)
		}
		return vals, labels
	}
	count := max(n/every, 2)
	if a.Temporal {
		for _, v := range pv.TimeTicks(a.Lo, a.Hi, count) {
			vals, labels = append(vals, v), append(labels, pv.FormatTime(v))
		}
		return vals, labels
	}
	ticks, step := pv.Ticks(a.Lo, a.Hi, count)
	for _, v := range ticks {
		l := pv.FormatNumber(v, step)
		if a.Percent {
			l = pv.FormatPercent(v, step)
		}
		vals, labels = append(vals, v), append(labels, l)
	}
	return vals, labels
}

// layFrame lays ch's frame in a room width cells wide: the title on top, the legend under the x
// axis, the y labels as wide as the widest, and gutter columns kept right of the plot for a tier's
// own labels (a horizontal bar's value). The plot is rows tall (a horizontal bar chart's: a row a
// category), or plotRows' choice for rows ≤ 0; it takes the width that is left, and ok is false
// when that is under 8 columns (a host shows the table or the notice instead).
func layFrame(ch *pv.Chart, width, gutter, rows int, m Measurer) (f *frame, ok bool) {
	if rows <= 0 {
		rows = plotRows(ch)
	}
	ch = niced(ch, rows, width)
	top := 0
	titled := []string{}
	if ch.Title != "" {
		titled = append(titled, ch.Title)
	}
	if ch.Subtitle != "" {
		titled = append(titled, ch.Subtitle)
	}
	top = len(titled)
	if ch.Y.Title != "" && !ch.Y.Off {
		top++ // the y axis's title, over its labels
	}
	yVals, yLabels := ticksFor(ch.Y, rows, 2)
	yw := 0
	if !ch.Y.Off {
		for _, l := range yLabels {
			yw = max(yw, m.StringWidth(l))
		}
		yw = min(yw, width/3)
	}
	left := 0
	if !ch.Y.Off {
		left = yw + 2 // the labels, a space, the axis │
	}
	plotW := width - left - max(gutter, 0)
	if plotW < 8 {
		return nil, false
	}
	below := 0
	if !ch.X.Off {
		below = 2 // the floor, the labels
		if ch.X.Title != "" {
			below++
		}
	}
	legend := len(ch.Series) > 1 && !ch.ColorLegend.Off
	if legend {
		below++
	}
	f = &frame{ch: ch, plot: Rect{X: left, Y: top, W: plotW, H: rows}}
	d := newDrawing(width, top+rows+below)
	f.d = d
	for i, t := range titled {
		role := RoleText
		if i > 0 {
			role = RoleMuted
		}
		s := cut(t, width, m)
		d.say((width-m.StringWidth(s))/2, i, s, role, m)
	}
	if ch.Y.Title != "" && !ch.Y.Off {
		d.say(0, len(titled), cut(ch.Y.Title, width, m), RoleMuted, m)
	}
	if !ch.Y.Off {
		ax := left - 1
		for y := top; y < top+rows; y++ {
			d.put(ax, y, '│', RoleAxis, -1)
		}
		for i, v := range yVals {
			y := top + int(math.Round(f.yCell(v)))
			if ch.Y.Discrete {
				y = top + int(f.yBand(int(v)))
			}
			if y < top || y >= top+rows {
				continue
			}
			d.put(ax, y, '┤', RoleAxis, -1)
			l := cut(yLabels[i], yw, m)
			d.say(yw-m.StringWidth(l), y, l, RoleText, m)
		}
	}
	if !ch.X.Off {
		floor := top + rows
		if !ch.Y.Off {
			d.put(left-1, floor, '└', RoleAxis, -1)
		}
		for x := left; x < left+plotW; x++ {
			d.put(x, floor, '─', RoleAxis, -1)
		}
		xVals, xLabels := ticksFor(ch.X, plotW, 8)
		band := plotW
		if n := len(ch.X.Domain); ch.X.Discrete && n > 0 {
			band = max(plotW/n, 1)
		}
		lastEnd := -1
		for i, v := range xVals {
			x := left + int(math.Round(f.xCell(pv.Pos{Num: v, Str: labelOf(ch.X, xLabels, i)})))
			if x < left || x >= width {
				continue
			}
			d.put(x, floor, '┬', RoleAxis, -1)
			w := band
			if !ch.X.Discrete {
				w = 12
			}
			l := cut(xLabels[i], w, m)
			lw := m.StringWidth(l)
			at := min(max(x-lw/2, left), width-lw)
			if at <= lastEnd {
				continue // would overlap the label before it: left out, its ┬ kept
			}
			d.say(at, floor+1, l, RoleText, m)
			lastEnd = at + lw
		}
		if ch.X.Title != "" {
			t := cut(ch.X.Title, plotW, m)
			d.say(left+(plotW-m.StringWidth(t))/2, floor+2, t, RoleMuted, m)
		}
	}
	if legend {
		f.layLegend(d.H-1, width, m)
	}
	return f, true
}

// labelOf is the category at tick i of a discrete axis, "" for a continuous one.
func labelOf(a pv.Axis, labels []string, i int) string {
	if a.Discrete && i < len(labels) {
		return labels[i]
	}
	return ""
}

// layLegend writes the legend on row y: each series' swatch in its colour, then its name.
func (f *frame) layLegend(y, width int, m Measurer) {
	x := 0
	swatch := f.swatch()
	for i, name := range f.ch.Series {
		n := cut(name, max(width-x-2, 0), m)
		if n == "" || x+2+m.StringWidth(n) > width {
			break
		}
		f.d.texts = append(f.d.texts, text{x: x, y: y, w: 1, s: swatch, role: RoleMark, series: i})
		f.d.say(x+2, y, n, RoleMuted, m)
		x += 2 + m.StringWidth(n) + 2
	}
}

// swatch is the legend's mark for the chart's first layer: a bar's or an area's ■, a line's ─, a
// point's ●.
func (f *frame) swatch() string {
	if len(f.ch.Layers) > 0 {
		switch f.ch.Layers[0].Mark {
		case pv.MarkLine, pv.MarkRule:
			return "─"
		case pv.MarkPoint:
			return "●"
		}
	}
	return "■"
}

// xCell is a value's column within the plot, 0 to W−1 (a fraction: a tier rounds or scales it).
// A discrete axis places a category at its band's middle.
func (f *frame) xCell(p pv.Pos) float64 {
	a, w := f.ch.X, float64(f.plot.W)
	if a.Discrete {
		return f.band(a, p.Str, w)
	}
	return frac(p.Num, a.Lo, a.Hi) * (w - 1)
}

// yCell is a value's row within the plot, 0 (top, the axis's high end) to H−1.
func (f *frame) yCell(v float64) float64 {
	return (1 - frac(v, f.ch.Y.Lo, f.ch.Y.Hi)) * float64(f.plot.H-1)
}

// yBand is category i's row on a discrete y axis: its band's middle.
func (f *frame) yBand(i int) float64 {
	n := max(len(f.ch.Y.Domain), 1)
	return (float64(i) + 0.5) * float64(f.plot.H) / float64(n)
}

// band is category c's middle on a discrete axis of w cells; an unknown one is the axis's start.
func (f *frame) band(a pv.Axis, c string, w float64) float64 {
	n := max(len(a.Domain), 1)
	for i, d := range a.Domain {
		if d == c {
			return (float64(i) + 0.5) * w / float64(n)
		}
	}
	return 0
}

// BAND AND MEASURE MAPS, in cells, for a tier that draws in whole cells and eighths (bars).

// XBand is category c's band on a discrete x axis: its first column within the plot and its width,
// the plot's columns shared as evenly as whole cells allow. ok is false for a category the axis
// doesn't have.
func (f *frame) XBand(c string) (start, length int, ok bool) {
	return bandOf(f.ch.X.Domain, c, f.plot.W)
}

// YBand is category c's band on a discrete y axis, rows from the plot's top.
func (f *frame) YBand(c string) (start, length int, ok bool) {
	return bandOf(f.ch.Y.Domain, c, f.plot.H)
}

func bandOf(domain []string, c string, cells int) (start, length int, ok bool) {
	n := len(domain)
	for i, d := range domain {
		if d == c {
			start, end := i*cells/n, (i+1)*cells/n
			return start, max(end-start, 1), true
		}
	}
	return 0, 0, false
}

// XOffset is v's distance from the plot's left edge in cells, fractional (a bar's end in eighths),
// within 0 and the plot's width.
func (f *frame) XOffset(v float64) float64 {
	return frac(v, f.ch.X.Lo, f.ch.X.Hi) * float64(f.plot.W)
}

// YOffset is v's distance up from the plot's floor in cells, fractional, within 0 and its height.
func (f *frame) YOffset(v float64) float64 {
	return frac(v, f.ch.Y.Lo, f.ch.Y.Hi) * float64(f.plot.H)
}

// SayRight writes s right-aligned in the gutter beside plot row y (a horizontal bar's value), cut
// to the gutter's width.
func (f *frame) SayRight(y int, s string, m Measurer) {
	gutter := f.d.W - f.plot.X - f.plot.W - 1 // a space after the plot
	if gutter < 1 || y < 0 || y >= f.plot.H {
		return
	}
	s = cut(s, gutter, m)
	f.d.say(f.d.W-m.StringWidth(s), f.plot.Y+y, s, RoleText, m)
}

// Plot is the plot's rectangle within the drawing.
func (f *frame) Plot() Rect { return f.plot }

// frac is v's place between lo and hi, held within 0 and 1 (a value past the domain is drawn at
// its end, never outside the plot).
func frac(v, lo, hi float64) float64 {
	if hi == lo || math.IsNaN(v) {
		return 0
	}
	return min(max((v-lo)/(hi-lo), 0), 1)
}

// cut is s within w cells, cut at a grapheme with … at the cut; w under 1 is "".
func cut(s string, w int, m Measurer) string {
	if w < 1 {
		return ""
	}
	if m.StringWidth(s) <= w {
		return s
	}
	var b strings.Builder
	used := 0
	for g := range tui.Graphemes(s) {
		gw := m.StringWidth(g)
		if used+gw > w-1 {
			break
		}
		b.WriteString(g)
		used += gw
	}
	return b.String() + "…"
}
