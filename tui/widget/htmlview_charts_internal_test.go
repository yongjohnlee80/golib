package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui/style"
)

// mixedChart is a bar under a line: no tier but the table draws it.
const mixedChart = `{"data":{"values":[{"m":"Jan","v":3,"w":1},{"m":"Feb","v":5,"w":2}]},
	"layer":[{"mark":"bar","encoding":{"x":{"field":"m","type":"ordinal"},"y":{"field":"v","type":"quantitative"}}},
	{"mark":"line","encoding":{"x":{"field":"m","type":"ordinal"},"y":{"field":"w","type":"quantitative"}}}]}`

// A <pre class="vega-lite"> is a chart on the page: tui/vegalite's drawing, a row of the layout a
// row of it, painted and panned as a diagram's grid; never its source.
func TestAVegaLitePreIsDrawnAsAChart(t *testing.T) {
	c := layCells(t, `<p>before</p><pre class="vega-lite">`+mixedChart+`</pre><p>after</p>`, 60)
	var pic CellPicture // the page's only picture: the chart
	n := 0
	for _, r := range c.rows {
		if r.grid != nil {
			pic, n = r.grid, n+1
		}
	}
	if n == 0 {
		t.Fatalf("no row of the layout holds a chart: %v", c.rowTexts())
	}
	if n != pic.Size().H {
		t.Fatalf("%d rows of a chart, its drawing %+v; want a row of the layout a row of it", n, pic.Size())
	}
	text := strings.Join(c.rowTexts(), "\n")
	if strings.Contains(text, `"mark"`) || strings.Contains(text, "not drawn") || !strings.Contains(text, "before") || !strings.Contains(text, "after") {
		t.Errorf("the page around the chart:\n%s\nwant the paragraphs, no source and no notice", text)
	}
	s := newRecordSurface(60, len(c.rows))
	c.PaintCells(s, 0, 60, DocPos{}, DocPos{})
	var lines []string
	for y := range len(c.rows) {
		lines = append(lines, s.row(y))
	}
	if painted := strings.Join(lines, "\n"); !strings.Contains(painted, "Jan") || !strings.Contains(painted, "Feb") {
		t.Errorf("the painted page has no chart:\n%s", painted)
	}
}

// A spec outside the subset, or one that doesn't read, is the notice: "vega-lite chart: not
// drawn", in the host's words when it gives them (WithDiagramNotice, told the kind "vega-lite"),
// and never the source nor its data.
func TestAVegaLiteSpecOutsideTheSubsetIsTheNotice(t *testing.T) {
	for _, src := range []string{
		`{"data":{"values":[{"secret":41}]},"transform":[{"calculate":"datum.secret+1","as":"s"}],"mark":"bar"}`,
		`{"mark":`,
	} {
		c := layCells(t, `<pre class="vega-lite">`+src+`</pre>`, 70)
		text := strings.Join(strings.Fields(strings.Join(c.rowTexts(), " ")), " ")
		if text != "▸ vega-lite chart: not drawn in the terminal (Raw shows its source)" {
			t.Errorf("%s: the page says %q; want the notice alone", src, text)
		}
		c.notice = func(kind string) string { return "<" + kind + ">" }
		c.layOut()
		if got := strings.Join(strings.Fields(strings.Join(c.rowTexts(), " ")), " "); got != "▸ <vega-lite>" {
			t.Errorf("%s: with the host's words, %q; want ▸ <vega-lite>", src, got)
		}
	}
}

// A pie on the page wears the page's chart series (chartTheme, the page's code colours): its first
// slice is painted in the first series' colour. That a pie laid from those syntax styles takes
// exactly that series is the editor's (TestAPieLaidFromSyntaxWearsTheChartSeries).
func TestAPiesSlicesWearThePagesChartSeries(t *testing.T) {
	c := newCellHTML(func(s string) int { return len([]rune(s)) })
	var styles SyntaxStyles
	styles[highlight.Keyword] = style.New().Foreground(style.RGB(200, 40, 40))
	styles[highlight.String] = style.New().Foreground(style.RGB(40, 160, 40))
	styles[highlight.DecVal] = style.New().Foreground(style.RGB(40, 40, 200))
	c.syntaxStyles = styles
	c.SetSource([]byte(`<pre class="mermaid">pie
  "Dogs" : 386
  "Cats" : 85
  "Rats" : 15</pre>`))
	c.SetWidth(60)
	held := false
	for _, r := range c.rows {
		held = held || r.grid != nil
	}
	if !held {
		t.Fatalf("no row holds the pie: %v", c.rowTexts())
	}
	want := c.chartTheme().Series
	if len(want) < 3 {
		t.Fatalf("the page's chart series are %v, want three or more", want)
	}
	s := newRecordSurface(60, len(c.rows))
	c.PaintCells(s, 0, 60, DocPos{}, DocPos{})
	found := false
	for y := range len(c.rows) {
		for x := range 60 {
			if s.text[y][x] == "█" { // the first slice's density
				fg, _ := s.style[y][x].GetForeground()
				if fg != want[0] {
					t.Fatalf("the first slice's cell (%d,%d) wears %v, want the first series' %v", x, y, fg, want[0])
				}
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no cell of the first slice was painted")
	}
}
