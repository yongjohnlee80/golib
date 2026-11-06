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

// Mermaid's documented quadrant charts parse as their goldens (testdata/quadrant, -update writes
// them): a title, two axes each with its ends' labels, the four quadrants' labels in Mermaid's
// numbering, and the points with their two numbers, a styled point's class and styles read and not
// kept.
func TestQuadrantCorpus(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "quadrant", "*.mmd"))
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
		if d.Kind() != mermaid.Quadrant {
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

func quadrantParse(t *testing.T, src string) *mermaid.QuadrantDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	q, ok := d.(*mermaid.QuadrantDiagram)
	if !ok {
		t.Fatalf("%q parsed as %T", src, d)
	}
	return q
}

// The documented example, read field by field: quadrant-1 is the top right, quadrant-2 the top
// left, quadrant-3 the bottom left and quadrant-4 the bottom right, and a point's span is its line.
func TestQuadrantTheDocumentedExample(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("testdata", "quadrant", "documented.mmd"))
	if err != nil {
		t.Fatal(err)
	}
	q := quadrantParse(t, string(src))
	if q.Title != "Reach and engagement of campaigns" {
		t.Errorf("title %q", q.Title)
	}
	if q.XAxis != (mermaid.QuadrantAxis{Low: "Low Reach", High: "High Reach"}) || q.YAxis != (mermaid.QuadrantAxis{Low: "Low Engagement", High: "High Engagement"}) {
		t.Errorf("axes %+v %+v", q.XAxis, q.YAxis)
	}
	if q.Quadrants != [4]string{"We should expand", "Need to promote", "Re-evaluate", "May be improved"} {
		t.Errorf("quadrants %q", q.Quadrants)
	}
	if len(q.Points) != 6 || q.Points[1].Label != "Campaign B" || q.Points[1].X != 0.45 || q.Points[1].Y != 0.23 {
		t.Fatalf("points %+v", q.Points)
	}
	if got := string(src)[q.Points[0].Span[0]:q.Points[0].Span[1]]; got != "Campaign A: [0.3, 0.6]" {
		t.Errorf("point 0's span is %q", got)
	}
}

// What a quadrant chart refuses is a syntax error at its place: a point outside 0 to 1 or not a
// number, a point without its two numbers or its closing bracket, a style no point has, and a line
// that is none of the chart's.
func TestQuadrantRefusals(t *testing.T) {
	for src, want := range map[string]string{
		"quadrantChart\n  A: [1.2, 0.5]\n":           "a point's x 1.2 is outside 0 to 1",
		"quadrantChart\n  A: [0.5, -0.1]\n":          "a point's y -0.1 is outside 0 to 1",
		"quadrantChart\n  A: [half, 0.5]\n":          `a point's x "half" is not a number`,
		"quadrantChart\n  A: [NaN, 0.5]\n":           `a point's x "NaN" is not finite`,
		"quadrantChart\n  A: [0.5]\n":                "a point wants [x, y]: two numbers",
		"quadrantChart\n  A: [0.5, 0.5\n":            "a point's [x, y] is not closed",
		"quadrantChart\n  A: [0.5, 0.5] size: 3\n":   `unexpected "size: 3"`,
		"quadrantChart\n  : [0.5, 0.5]\n":            "a point wants a label before its colon",
		"quadrantChart\n  A: then [0.5, 0.5]\n":      `unexpected "then" between`,
		"quadrantChart\n  axis Low --> High\n":       `unexpected "axis"`,
		"quadrantChart extra\n  A: [0.5, 0.5]\n":     `unexpected "extra" after quadrantChart`,
		"quadrantChart\n  classDef\n":                "a classDef wants a name",
		"quadrantChart\n  classDef c shape: round\n": `unexpected "shape: round"`,
	} {
		_, err := mermaid.Parse(src)
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) || !strings.Contains(se.Msg, want) {
			t.Errorf("%q: %v; want a syntax error saying %q", src, err, want)
		}
	}
}

// The ends of an axis: low --> high, the low alone, and a later axis line replacing an earlier.
func TestQuadrantAxes(t *testing.T) {
	q := quadrantParse(t, "quadrantChart\n  x-axis Slow --> Fast\n  x-axis Cheap\n  y-axis \"Low\" --> \"High\"\n")
	if q.XAxis != (mermaid.QuadrantAxis{Low: "Cheap"}) {
		t.Errorf("x-axis %+v, want the later line's low alone", q.XAxis)
	}
	if q.YAxis != (mermaid.QuadrantAxis{Low: "Low", High: "High"}) {
		t.Errorf("y-axis %+v, want the quotes removed", q.YAxis)
	}
}
