package vegalite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pv "github.com/yongjohnlee80/golib/parse/vegalite"
)

// barSpecs are the bar charts the goldens hold, as Vega-Lite specs: plotted by parse/vegalite, as
// the page plots them.
var barSpecs = map[string]string{
	"bars-across": `{"mark":"bar","data":{"values":[
		{"c":"north","v":12.5},{"c":"south","v":7.9},{"c":"east","v":3.2},{"c":"west","v":0.4}]},
		"encoding":{"y":{"field":"c","type":"nominal","sort":null},"x":{"field":"v","type":"quantitative"}}}`,
	"bars-up": `{"title":"Rainfall","mark":"bar","data":{"values":[
		{"m":"Jan","mm":81},{"m":"Feb","mm":62},{"m":"Mar","mm":55},{"m":"Apr","mm":44},{"m":"May","mm":36},{"m":"Jun","mm":23}]},
		"encoding":{"x":{"field":"m","type":"ordinal","sort":null},"y":{"field":"mm","type":"quantitative","title":"mm"}}}`,
	"bars-stacked": `{"mark":"bar","data":{"values":[
		{"q":"Q1","r":"east","n":30},{"q":"Q1","r":"west","n":12},
		{"q":"Q2","r":"east","n":24},{"q":"Q2","r":"west","n":21},
		{"q":"Q3","r":"east","n":9},{"q":"Q3","r":"west","n":33}]},
		"encoding":{"y":{"field":"q","type":"nominal"},"x":{"field":"n","type":"quantitative","aggregate":"sum"},
		"color":{"field":"r","type":"nominal"}}}`,
	"bars-overlaid": `{"mark":"bar","data":{"values":[
		{"k":"a","s":"plan","v":10},{"k":"a","s":"done","v":7},
		{"k":"b","s":"plan","v":6},{"k":"b","s":"done","v":8}]},
		"encoding":{"x":{"field":"k","type":"nominal"},"y":{"field":"v","type":"quantitative","stack":null},
		"color":{"field":"s","type":"nominal"}}}`,
	"bars-normalized": `{"mark":"bar","data":{"values":[
		{"q":"Q1","r":"east","n":30},{"q":"Q1","r":"west","n":10},
		{"q":"Q2","r":"east","n":10},{"q":"Q2","r":"west","n":30}]},
		"encoding":{"y":{"field":"q","type":"nominal"},"x":{"field":"n","type":"quantitative","stack":"normalize"},
		"color":{"field":"r","type":"nominal"}}}`,
}

// barRoom is each golden's room in cells: a page's, and a narrow one the plot is widened past.
var barRoom = map[string]int{"bars-across": 48, "bars-up": 40, "bars-stacked": 48, "bars-overlaid": 32,
	"bars-normalized": 48, "bars-narrow": 12}

func init() { barSpecs["bars-narrow"] = barSpecs["bars-across"] }

// plotted is spec plotted by parse/vegalite.
func plotted(t *testing.T, spec string) *pv.Chart {
	t.Helper()
	ch, err := pv.Plot(context.Background(), spec)
	if err != nil {
		t.Fatalf("plot: %v", err)
	}
	return ch
}

// colourRows are d's mark cells as two pictures: each one's series (a letter: a, b, …), then each
// one's background series ('.' the page); other cells blank. What the theme paints them in is the
// series' token (Theme.Series), so these say which colour every cell wears.
func colourRows(d *Drawing) string {
	fg, bg := make([][]byte, d.H), make([][]byte, d.H)
	for y := range d.H {
		fg[y], bg[y] = []byte(strings.Repeat(" ", d.W)), []byte(strings.Repeat(" ", d.W))
	}
	letter := func(s int) byte {
		if s < 0 {
			return '.'
		}
		return byte('a' + s)
	}
	for at, c := range d.cells {
		if c.role == RoleMark {
			fg[at[1]][at[0]], bg[at[1]][at[0]] = letter(c.series), letter(c.bg)
		}
	}
	var b strings.Builder
	for _, part := range []struct {
		name string
		rows [][]byte
	}{{"colour", fg}, {"background", bg}} {
		b.WriteString("-- " + part.name + "\n")
		for _, r := range part.rows {
			b.WriteString(strings.TrimRight(string(r), " ") + "\n")
		}
	}
	return b.String()
}

// barGolden compares d's rows and its colours with testdata/<name>.golden, written with -update.
func barGolden(t *testing.T, name string, d *Drawing) {
	t.Helper()
	got := strings.Join(d.Rows(), "\n") + "\n" + colourRows(d)
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (write it with -update)", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from its golden:\n--- got\n%s--- want\n%s", name, got, want)
	}
}

// The bars tier's pictures, read by eye once and then held, with every mark cell's colour: bars
// across with their values, bars up under a title, a stack and an overlay of two series with their
// legend, a normalized stack (no values), and the bars across in a room too narrow, widened.
func TestBarGoldens(t *testing.T) {
	for name, spec := range barSpecs {
		t.Run(name, func(t *testing.T) {
			d, err := layBars(context.Background(), plotted(t, spec), widths{}, barRoom[name])
			if err != nil {
				t.Fatal(err)
			}
			barGolden(t, name, d)
		})
	}
}

// A horizontal bar sits on its category's row, its label left of the axis on that row and its value
// right of the plot on it too.
func TestABarSharesItsRowWithItsLabelAndValue(t *testing.T) {
	d, err := layBars(context.Background(), plotted(t, barSpecs["bars-across"]), widths{}, 48)
	if err != nil {
		t.Fatal(err)
	}
	rows := d.Rows()
	for _, want := range [][2]string{{"north", "12.5"}, {"south", "7.9"}, {"east", "3.2"}, {"west", "0.4"}} {
		found := false
		for _, r := range rows {
			if strings.HasPrefix(strings.TrimSpace(r), want[0]) {
				found = true
				if !strings.HasSuffix(r, want[1]) || !strings.ContainsAny(r, "▏▎▍▌▋▊▉█") {
					t.Errorf("the row of %s: %q; want its bar and %s at its end", want[0], r, want[1])
				}
			}
		}
		if !found {
			t.Errorf("no row for %s:\n%s", want[0], strings.Join(rows, "\n"))
		}
	}
}

// A room too narrow for the plot widens the drawing past it (the page pans), never squeezes the
// plot under the frame's least.
func TestBarsInANarrowRoomAreWidened(t *testing.T) {
	d, err := layBars(context.Background(), plotted(t, barSpecs["bars-across"]), widths{}, 12)
	if err != nil || d.W <= 12 {
		t.Fatalf("laid (%v), %d wide; want widened past 12", err, d.W)
	}
}

// The tier draws a lone bar layer over categories and refuses anything else with pv.ErrUnsupported,
// so Lay tries the next tier: a line, a layered chart, or bars along a continuous axis.
func TestTheBarsTierRefusesOtherCharts(t *testing.T) {
	for name, spec := range map[string]string{
		"line": `{"mark":"line","data":{"values":[{"x":1,"y":2},{"x":2,"y":3}]},
			"encoding":{"x":{"field":"x","type":"quantitative"},"y":{"field":"y","type":"quantitative"}}}`,
		"layered": `{"data":{"values":[{"k":"a","v":1},{"k":"b","v":3}]},"layer":[
			{"mark":"bar","encoding":{"x":{"field":"k","type":"nominal"},"y":{"field":"v","type":"quantitative"}}},
			{"mark":"rule","encoding":{"y":{"datum":2}}}]}`,
	} {
		if _, err := layBars(context.Background(), plotted(t, spec), widths{}, 40); !errors.Is(err, pv.ErrUnsupported) {
			t.Errorf("%s: %v; want pv.ErrUnsupported", name, err)
		}
	}
	// bars along a continuous axis (a binned or temporal x, were the parser to plot one)
	continuous := &pv.Chart{X: pv.Axis{Lo: 0, Hi: 10}, Y: pv.Axis{Lo: 0, Hi: 5},
		Layers: []pv.Layer{{Mark: pv.MarkBar, Data: []pv.Datum{{X: pv.Pos{Num: 2}, V1: 3}}}}}
	if _, err := layBars(context.Background(), continuous, widths{}, 40); !errors.Is(err, pv.ErrUnsupported) {
		t.Errorf("bars along a continuous axis: %v; want pv.ErrUnsupported", err)
	}
}
