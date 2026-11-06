package mermaid_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

// Mermaid's documented XY charts parse as their goldens (testdata/xychart, -update writes them): a
// title, the x axis's categories or its range, the y axis's title and range, and each bar and line
// with its name and its values in order.
func TestXYChartCorpus(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "xychart", "*.mmd"))
	if err != nil || len(files) < 3 {
		t.Fatalf("the corpus: %d files, %v", len(files), err)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		d, err := mermaid.Parse(string(src))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if d.Kind() != mermaid.XYChart {
			t.Errorf("%s: kind %v", f, d.Kind())
		}
		got, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		golden := strings.TrimSuffix(f, ".mmd") + ".json"
		if *update {
			if err := os.WriteFile(golden, append(got, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%v (run with -update to write it)", err)
		}
		if string(want) != string(got)+"\n" {
			t.Errorf("%s differs from its golden:\n%s", f, got)
		}
	}
}

func xyParse(t *testing.T, src string) *mermaid.XYChartDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	x, ok := d.(*mermaid.XYChartDiagram)
	if !ok {
		t.Fatalf("%q parsed as %T", src, d)
	}
	return x
}

// The documented chart, read field by field: twelve months, a y range, a bar and a line of twelve
// values each, a series' span its line.
func TestXYChartTheDocumentedExample(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "xychart", "documented.mmd"))
	if err != nil {
		t.Fatal(err)
	}
	x := xyParse(t, string(src))
	if x.Title != "Sales Revenue" || x.Horizontal {
		t.Errorf("title %q, horizontal %v", x.Title, x.Horizontal)
	}
	if len(x.XAxis.Categories) != 12 || x.XAxis.Categories[0] != "jan" || x.XAxis.Categories[11] != "dec" || x.XAxis.Ranged {
		t.Errorf("x axis %+v", x.XAxis)
	}
	if x.YAxis.Title != "Revenue (in $)" || !x.YAxis.Ranged || x.YAxis.Min != 4000 || x.YAxis.Max != 11000 {
		t.Errorf("y axis %+v", x.YAxis)
	}
	if len(x.Series) != 2 || x.Series[0].Kind != mermaid.XYBar || x.Series[1].Kind != mermaid.XYLine || len(x.Series[1].Values) != 12 || x.Series[1].Values[6] != 11000 {
		t.Fatalf("series %+v", x.Series)
	}
	if got := string(src)[x.Series[0].Span[0]:x.Series[0].Span[1]]; !strings.HasPrefix(got, "bar [5000") {
		t.Errorf("the bar's span is %q", got)
	}
	if x.Points() != 12 {
		t.Errorf("points %d", x.Points())
	}
}

// With no categories the x axis has as many points as its longest series; quoted categories keep
// their commas; a title of one word needs no quotes; a horizontal header is read.
func TestXYChartAxesAndHeader(t *testing.T) {
	x := xyParse(t, "xychart-beta horizontal\n  x-axis Months [\"Jan, early\", feb]\n  y-axis 0 --> 1\n  bar [0.5, 1]\n")
	if !x.Horizontal || x.XAxis.Title != "Months" || len(x.XAxis.Categories) != 2 || x.XAxis.Categories[0] != "Jan, early" {
		t.Errorf("header and x axis: %v %+v", x.Horizontal, x.XAxis)
	}
	if x.YAxis.Title != "" || x.YAxis.Min != 0 || x.YAxis.Max != 1 {
		t.Errorf("y axis %+v", x.YAxis)
	}
	y := xyParse(t, "xychart\n  line [1, 2, 3]\n  bar [4, 5]\n")
	if y.Points() != 3 || y.XAxis.Categories != nil {
		t.Errorf("points %d, categories %v; want 3 from the longest series", y.Points(), y.XAxis.Categories)
	}
}

// What an XY chart refuses is a syntax error at its place: a series longer than its axis's
// categories, a value that is no number, a range whose min isn't below its max, categories on the y
// axis, an unclosed list, a series with no values, and a line that is none of the chart's.
func TestXYChartRefusals(t *testing.T) {
	for src, want := range map[string]string{
		"xychart-beta\n  x-axis [a, b]\n  bar [1, 2, 3]\n":        "a bar of 3 values is longer than the x axis's 2 categories",
		"xychart-beta\n  x-axis [a, b, c]\n  line [1, 2, 3, 4]\n": "a line of 4 values is longer than the x axis's 3 categories",
		"xychart-beta\n  bar [1, two]\n":                          `a bar's value 2 "two" is not a number`,
		"xychart-beta\n  bar [1, NaN]\n":                          `a bar's value 2 "NaN" is not finite`,
		"xychart-beta\n  y-axis 10 --> 5\n":                       "an axis's min 10 is not below its max 5",
		"xychart-beta\n  y-axis [a, b]\n":                         "the y axis takes a range, min --> max, not categories",
		"xychart-beta\n  bar [1, 2\n":                             "a [ ] list is not closed",
		"xychart-beta\n  bar []\n":                                "a bar has no values",
		"xychart-beta\n  bar 1, 2\n":                              "a bar wants its values in [ ]",
		"xychart-beta\n  area [1, 2]\n":                           `unexpected "area"`,
		"xychart-beta sideways\n":                                 `unexpected "sideways" after xychart`,
		"xychart-beta\n  x-axis \"t\" 5\n":                        `unexpected "5": an axis wants`,
	} {
		_, err := mermaid.Parse(src)
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) || !strings.Contains(se.Msg, want) {
			t.Errorf("%q: %v; want a syntax error saying %q", src, err, want)
		}
	}
}
