package vegalite

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	pv "github.com/yongjohnlee80/golib/parse/vegalite"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// widths is the measurer the cells use: tui's own grapheme widths.
type widths struct{}

func (widths) StringWidth(s string) int { return tui.StringWidth(s) }

// lineChart is a chart of one line layer over continuous axes 0..hi on both.
func lineChart(mark pv.MarkKind, hi float64, data ...pv.Datum) *pv.Chart {
	return &pv.Chart{
		X:      pv.Axis{Lo: 0, Hi: hi},
		Y:      pv.Axis{Lo: 0, Hi: hi},
		Layers: []pv.Layer{{Mark: mark, Data: data}},
	}
}

func at(x, v float64) pv.Datum { return pv.Datum{X: pv.Pos{Num: x}, V1: v} }

// lay is the Braille tier on ch in a room of room cells: its drawing, and whether it drew.
func lay(ch *pv.Chart, room int) (*Drawing, bool) {
	d, err := layBraille(context.Background(), ch, widths{}, room)
	return d, err == nil
}

// marks are the drawing's mark cells, by row and column within the drawing.
func marks(d *Drawing) map[[2]int]drawn {
	out := map[[2]int]drawn{}
	for at, c := range d.cells {
		if c.role == RoleMark {
			out[at] = c
		}
	}
	return out
}

// A rising line climbs from the plot's bottom left to its top right: its leftmost mark is on the
// bottom row, its rightmost on the top, and every column between has a mark; the frame labels the
// y axis's ends.
func TestARisingLineClimbsAcrossThePlot(t *testing.T) {
	d, ok := lay(lineChart(pv.MarkLine, 3, at(0, 0), at(1, 1), at(2, 2), at(3, 3)), 30)
	if !ok {
		t.Fatal("a 30-column room has no plot")
	}
	f, _ := layFrame(lineChart(pv.MarkLine, 3), 30, 0, 0, widths{})
	p := f.plot
	ms := marks(d)
	cols := map[int]int{} // column → its lowest row (the line's place there)
	for at := range ms {
		if at[0] < p.X || at[0] >= p.X+p.W || at[1] < p.Y || at[1] >= p.Y+p.H {
			t.Fatalf("a mark at %v is outside the plot %+v", at, p)
		}
		if r, ok := cols[at[0]]; !ok || at[1] > r {
			cols[at[0]] = at[1]
		}
	}
	if len(cols) != p.W {
		t.Fatalf("the line marks %d of the plot's %d columns:\n%s", len(cols), p.W, strings.Join(d.Rows(), "\n"))
	}
	if cols[p.X] != p.Y+p.H-1 {
		t.Errorf("the line starts on row %d, want the bottom row %d", cols[p.X], p.Y+p.H-1)
	}
	if top := minRow(ms, p.X+p.W-1); top != p.Y {
		t.Errorf("the line ends on row %d, want the top row %d", top, p.Y)
	}
	rows := strings.Join(d.Rows(), "\n")
	if !strings.Contains(rows, "0 ┤") && !strings.Contains(rows, "0┤") {
		t.Errorf("the y axis has no 0 at its foot:\n%s", rows)
	}
}

func minRow(ms map[[2]int]drawn, x int) int {
	r := -1
	for at := range ms {
		if at[0] == x && (r < 0 || at[1] < r) {
			r = at[1]
		}
	}
	return r
}

// An area at the axis's top fills the whole plot (every cell ⣿); one at half fills its lower half
// and leaves the upper blank; a value past the domain is drawn at its end, never outside the plot.
func TestAnAreaFillsDownToItsBaseline(t *testing.T) {
	full, _ := lay(lineChart(pv.MarkArea, 4, at(0, 4), at(4, 4)), 20)
	f, _ := layFrame(lineChart(pv.MarkArea, 4), 20, 0, 0, widths{})
	p := f.plot
	for y := p.Y; y < p.Y+p.H; y++ {
		for x := p.X; x < p.X+p.W; x++ {
			if c, ok := full.cells[[2]int{x, y}]; !ok || c.r != '⣿' {
				t.Fatalf("a full area left (%d,%d) as %q:\n%s", x, y, c.r, strings.Join(full.Rows(), "\n"))
			}
		}
	}
	half, _ := lay(lineChart(pv.MarkArea, 4, at(0, 2), at(4, 2)), 20)
	if c, ok := half.cells[[2]int{p.X, p.Y}]; ok && c.role == RoleMark {
		t.Errorf("a half area marks the plot's top row:\n%s", strings.Join(half.Rows(), "\n"))
	}
	if c := half.cells[[2]int{p.X, p.Y + p.H - 1}]; c.r != '⣿' {
		t.Errorf("a half area leaves its bottom row %q, want ⣿", c.r)
	}
	past, _ := lay(lineChart(pv.MarkArea, 4, at(0, 9), at(4, 9)), 20)
	for at := range marks(past) {
		if at[1] < p.Y {
			t.Fatalf("a value past the domain drew above the plot, at %v", at)
		}
	}
}

// A stacked area fills from its lower edge (V0), not the axis: the upper series' cells are above
// the lower series', and each cell wears its series.
func TestAStackedAreaFillsFromItsLowerEdge(t *testing.T) {
	ch := &pv.Chart{
		X: pv.Axis{Lo: 0, Hi: 1}, Y: pv.Axis{Lo: 0, Hi: 4},
		Series: []string{"low", "high"},
		Layers: []pv.Layer{{Mark: pv.MarkArea, Stacked: true, Data: []pv.Datum{
			{X: pv.Pos{Num: 0}, Series: "low", V0: 0, V1: 2}, {X: pv.Pos{Num: 1}, Series: "low", V0: 0, V1: 2},
			{X: pv.Pos{Num: 0}, Series: "high", V0: 2, V1: 4}, {X: pv.Pos{Num: 1}, Series: "high", V0: 2, V1: 4},
		}}},
	}
	d, ok := lay(ch, 20)
	if !ok {
		t.Fatal("no plot")
	}
	f, _ := layFrame(ch, 20, 0, 0, widths{})
	p := f.plot
	if c := d.cells[[2]int{p.X, p.Y}]; c.series != 1 {
		t.Errorf("the plot's top row wears series %d, want 1 (high)", c.series)
	}
	if c := d.cells[[2]int{p.X, p.Y + p.H - 1}]; c.series != 0 {
		t.Errorf("the plot's bottom row wears series %d, want 0 (low)", c.series)
	}
	if len(d.Rows()) == 0 || !strings.Contains(strings.Join(d.Rows(), "\n"), "high") {
		t.Errorf("the legend doesn't name the series:\n%s", strings.Join(d.Rows(), "\n"))
	}
}

// A rule at a y value is a ─ row across the plot over the dots, and one at an x value a │ column;
// where they cross, ┼.
func TestARuleIsAWholeCellLineOverTheDots(t *testing.T) {
	ch := lineChart(pv.MarkLine, 4, at(0, 0), at(4, 4))
	ch.Layers = append(ch.Layers,
		pv.Layer{Mark: pv.MarkRule, Rule: &pv.Rule{Values: []pv.Pos{{Num: 2}}}},
		pv.Layer{Mark: pv.MarkRule, Rule: &pv.Rule{OnX: true, Values: []pv.Pos{{Num: 2}}}})
	d, _ := lay(ch, 24)
	f, _ := layFrame(ch, 24, 0, 0, widths{})
	p := f.plot
	row, col := -1, -1
	for at, c := range d.cells {
		if c.r == '┼' {
			row, col = at[1], at[0]
		}
	}
	if row < 0 {
		t.Fatalf("no ┼ where the rules cross:\n%s", strings.Join(d.Rows(), "\n"))
	}
	for x := p.X; x < p.X+p.W; x++ {
		if r := d.cells[[2]int{x, row}].r; r != '─' && r != '┼' {
			t.Fatalf("the y rule's row has %q at column %d", r, x)
		}
	}
	for y := p.Y; y < p.Y+p.H; y++ {
		if r := d.cells[[2]int{col, y}].r; r != '│' && r != '┼' {
			t.Fatalf("the x rule's column has %q at row %d", r, y)
		}
	}
}

// A filled point is four dots, a ring one; a line with points has both.
func TestAPointIsItsDotAFilledOneFour(t *testing.T) {
	ring := lineChart(pv.MarkPoint, 4, at(2, 2))
	filled := lineChart(pv.MarkPoint, 4, at(2, 2))
	filled.Layers[0].Filled = true
	count := func(ch *pv.Chart) int {
		d, _ := lay(ch, 20)
		n := 0
		for _, c := range marks(d) {
			for b := c.r - brailleBase; b > 0; b &= b - 1 {
				n++
			}
		}
		return n
	}
	if n := count(ring); n != 1 {
		t.Errorf("a ring point sets %d dots, want 1", n)
	}
	if n := count(filled); n != 4 {
		t.Errorf("a filled point sets %d dots, want 4", n)
	}
}

// A room too narrow for a plot of 8 columns, or a layer this tier doesn't draw (a bar), is not
// laid: the host shows the table or the notice instead.
func TestBrailleRefusesANarrowRoomAndABar(t *testing.T) {
	for what, ch := range map[string]*pv.Chart{"a 9-column room": lineChart(pv.MarkLine, 3, at(0, 0)), "a bar layer": lineChart(pv.MarkBar, 3, at(0, 0))} {
		room := 40
		if what == "a 9-column room" {
			room = 9
		}
		if _, err := layBraille(context.Background(), ch, widths{}, room); !errors.Is(err, pv.ErrUnsupported) {
			t.Errorf("%s: %v, want ErrUnsupported (Lay tries the next tier)", what, err)
		}
	}
}

// A chart's series take the page's code colours in SeriesRoles' order, each colour once, a role
// left unset passed over; fewer than two distinct colours keep the tokens' series.
func TestTheSeriesTakeThePagesCodeColours(t *testing.T) {
	red, blue := style.RGB(200, 0, 0), style.RGB(0, 0, 200)
	given := map[highlight.Style]style.Color{highlight.Keyword: red, highlight.String: red, highlight.DecVal: blue}
	th := ThemeFromSyntax(func(r highlight.Style) style.Style {
		if c, ok := given[r]; ok {
			return style.New().Foreground(c)
		}
		return style.New()
	})
	if len(th.Series) != 2 || th.Series[0] != style.ColorSpec(red) || th.Series[1] != style.ColorSpec(blue) {
		t.Fatalf("series %v, want red (keyword; the string's red once) then blue (the number)", th.Series)
	}
	one := ThemeFromSyntax(func(r highlight.Style) style.Style {
		if r == highlight.Keyword {
			return style.New().Foreground(red)
		}
		return style.New()
	})
	if len(one.Series) != len(TokenTheme().Series) {
		t.Fatalf("one code colour gave %d series, want the tokens' %d", len(one.Series), len(TokenTheme().Series))
	}
}
