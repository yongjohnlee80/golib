package vegalite

import (
	"context"
	"errors"
	"strings"
	"testing"

	pv "github.com/yongjohnlee80/golib/parse/vegalite"
)

// tableOf is the table tier's drawing of src, plotted.
func tableOf(t *testing.T, src string) *Drawing {
	t.Helper()
	ch, err := pv.Plot(context.Background(), src)
	if err != nil {
		t.Fatalf("Plot: %v", err)
	}
	d, err := layTable(context.Background(), ch, widths{}, 80)
	if err != nil {
		t.Fatalf("layTable: %v", err)
	}
	return d
}

// row is the drawing's row holding s, fields split on runs of spaces; nil when none does.
func row(d *Drawing, s string) []string {
	for _, r := range d.Rows() {
		if strings.Contains(r, s) {
			return strings.Fields(r)
		}
	}
	return nil
}

// A stacked bar's table: a row a category in the axis's order, a column a series, each cell the
// series' own part of the pile (not its top), under the head row's titles and their rule.
func TestATableShowsEachSeriesPartOfAStack(t *testing.T) {
	d := tableOf(t, `{"title":"Sales","data":{"values":[
		{"c":"b","s":"y","v":2},{"c":"a","s":"y","v":1},{"c":"b","s":"x","v":3},{"c":"a","s":"x","v":4},{"c":"a","s":"x","v":1}]},
		"mark":"bar","encoding":{"x":{"field":"c","type":"nominal"},"y":{"aggregate":"sum","field":"v","type":"quantitative"},
		"color":{"field":"s","type":"nominal"}}}`)
	rows := d.Rows()
	if rows[0] != "Sales" || strings.Join(strings.Fields(rows[1]), " ") != "c x y" || !strings.HasPrefix(rows[2], "─") {
		t.Fatalf("the title, head and rule rows:\n%s", strings.Join(rows, "\n"))
	}
	if a, b := row(d, "a "), row(d, "b "); strings.Join(a, " ") != "a 5 1" || strings.Join(b, " ") != "b 3 2" {
		t.Errorf("rows a %v, b %v; want a 5 1 and b 3 2 (each series' part)", a, b)
	}
	golden(t, "table-stacked-bar", d)
}

// A normalized stack's parts are percents; a column's values share their decimals.
func TestATablesNormalizedStackIsInPercent(t *testing.T) {
	d := tableOf(t, `{"data":{"values":[{"c":"a","s":"x","v":1},{"c":"a","s":"y","v":3},{"c":"b","s":"x","v":1},{"c":"b","s":"y","v":1}]},
		"mark":"bar","encoding":{"x":{"field":"c","type":"nominal"},"y":{"field":"v","type":"quantitative","stack":"normalize"},
		"color":{"field":"s","type":"nominal"}}}`)
	if a, b := row(d, "a "), row(d, "b "); strings.Join(a, " ") != "a 25% 75%" || strings.Join(b, " ") != "b 50% 50%" {
		t.Errorf("rows a %v, b %v; want a 25%% 75%% and b 50%% 50%%", a, b)
	}
}

// Layers of mixed marks are a column per layer's series, headed by the mark; a rule is a line
// under the table, its values on its axis.
func TestATableOfMixedLayersAndARule(t *testing.T) {
	d := tableOf(t, `{"data":{"values":[{"m":"Jan","v":3,"w":1.5},{"m":"Feb","v":5,"w":2.25}]},
		"layer":[{"mark":"bar","encoding":{"x":{"field":"m","type":"ordinal","sort":null},"y":{"field":"v","type":"quantitative"}}},
		{"mark":"line","encoding":{"x":{"field":"m","type":"ordinal","sort":null},"y":{"field":"w","type":"quantitative"}}},
		{"mark":"rule","encoding":{"y":{"datum":4}}}]}`)
	if jan := row(d, "Jan"); strings.Join(jan, " ") != "Jan 3 1.50" {
		t.Errorf("row Jan %v; want Jan 3 1.50 (the line's column in its own decimals)", jan)
	}
	if head := strings.Join(strings.Fields(d.Rows()[0]), " "); !strings.Contains(head, "bar:") || !strings.Contains(head, "line:") {
		t.Errorf("head %q; want a column per layer, headed by its mark", head)
	}
	if r := row(d, "rule:"); r == nil || r[len(r)-1] != "4" {
		t.Errorf("the rule's line %v; want it at 4", r)
	}
	golden(t, "table-mixed-layers", d)
}

// Lay draws through the tiers in order, and the table takes a chart no other tier draws (a bar
// under a line); a spec outside the subset is its refusal, for the page's notice.
func TestLayFallsToTheTableAndRefusesOutsideTheSubset(t *testing.T) {
	d, err := Lay(context.Background(), `{"data":{"values":[{"m":"Jan","v":3,"w":1}]},
		"layer":[{"mark":"bar","encoding":{"x":{"field":"m","type":"ordinal"},"y":{"field":"v","type":"quantitative"}}},
		{"mark":"line","encoding":{"x":{"field":"m","type":"ordinal"},"y":{"field":"w","type":"quantitative"}}}]}`, widths{}, 80)
	if err != nil || row(d, "Jan") == nil {
		t.Fatalf("Lay: %v; want the table", err)
	}
	if _, err := Lay(context.Background(), `{"data":{"values":[]},"transform":[{"filter":"true"}],"mark":"bar"}`, widths{}, 80); !errors.Is(err, ErrUnsupported) {
		t.Errorf("a transform: %v; want ErrUnsupported", err)
	}
}
