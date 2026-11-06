package vegalite

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pv "github.com/yongjohnlee80/golib/parse/vegalite"
)

var update = flag.Bool("update", false, "write the goldens")

// golden compares d's rows with testdata/<name>.golden, written once with -update.
func golden(t *testing.T, name string, d *Drawing) {
	t.Helper()
	got := strings.Join(d.Rows(), "\n") + "\n"
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

// The Braille tier's pictures, read by eye once and then held: a titled two-series line with
// points and a legend, a stacked area, and a line under a threshold rule.
func TestBrailleGoldens(t *testing.T) {
	months := pv.Axis{Discrete: true, Domain: []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun"}, Title: "Month"}
	two := &pv.Chart{Title: "Sales by region", X: months, Y: pv.Axis{Lo: 0, Hi: 40, Title: "Units"},
		Series: []string{"North", "South"},
		Layers: []pv.Layer{{Mark: pv.MarkLine, Point: true, Filled: true}}}
	for i, m := range months.Domain {
		two.Layers[0].Data = append(two.Layers[0].Data,
			pv.Datum{X: pv.Pos{Str: m}, Series: "North", V1: float64(10 + 5*i)},
			pv.Datum{X: pv.Pos{Str: m}, Series: "South", V1: float64(30 - 4*i)})
	}
	stacked := &pv.Chart{X: pv.Axis{Lo: 0, Hi: 10}, Y: pv.Axis{Lo: 0, Hi: 20}, Series: []string{"a", "b"},
		Layers: []pv.Layer{{Mark: pv.MarkArea, Stacked: true}}}
	for x := 0; x <= 10; x += 2 {
		a, b := float64(4+x/2), float64(3+x%4)
		stacked.Layers[0].Data = append(stacked.Layers[0].Data,
			pv.Datum{X: pv.Pos{Num: float64(x)}, Series: "a", V0: 0, V1: a},
			pv.Datum{X: pv.Pos{Num: float64(x)}, Series: "b", V0: a, V1: a + b})
	}
	threshold := lineChart(pv.MarkLine, 10, at(0, 2), at(3, 8), at(6, 4), at(10, 9))
	threshold.Layers = append(threshold.Layers, pv.Layer{Mark: pv.MarkRule, Rule: &pv.Rule{Values: []pv.Pos{{Num: 6}}}})
	for name, ch := range map[string]*pv.Chart{"braille-two-lines": two, "braille-stacked-area": stacked, "braille-threshold": threshold} {
		t.Run(name, func(t *testing.T) {
			d, ok := lay(ch, 48)
			if !ok {
				t.Fatal("not laid")
			}
			golden(t, name, d)
		})
	}
}
