package mermaid

import (
	"errors"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

const xyDocumented = `xychart-beta
    title "Sales Revenue"
    x-axis [jan, feb, mar, apr, may, jun, jul, aug, sep, oct, nov, dec]
    y-axis "Revenue (in $)" 4000 --> 11000
    bar [5000, 6000, 7500, 8200, 9500, 10500, 11000, 10200, 9200, 8500, 7000, 6000]
    line [5000, 6000, 7500, 8200, 9500, 10500, 11000, 10200, 9200, 8500, 7000, 6000]`

// An XY chart lays out as a bar a point and a line through a dot a point, each over its own
// category's label: July, the range's top, is the tallest bar and the line's highest dot, both
// centred over the "jul" label. The value axis's ticks carry the range's ends.
func TestAnXYChartIsLaidOut(t *testing.T) {
	l, err := lay(t, xyDocumented, 2000)
	if err != nil {
		t.Fatal(err)
	}
	// the key's chips are a box and a dot too: right of the plot, past the last category's label
	keyX := float32(1 << 30)
	for _, tx := range l.texts {
		if len(tx.lines) == 1 && (tx.lines[0] == "bar 1" || tx.lines[0] == "line 2") {
			keyX = min(keyX, tx.box.X-l.th.Font.Size*2)
		}
	}
	var bars, dots []laidNode
	for _, n := range l.nodes {
		if n.box.X >= keyX {
			continue
		}
		switch n.form {
		case formRect:
			bars = append(bars, n)
		case formStart:
			dots = append(dots, n)
		}
	}
	if len(bars) != 12 || len(dots) != 12 {
		t.Fatalf("%d bars and %d dots; want 12 each", len(bars), len(dots))
	}
	var jul label
	texts := map[string]bool{}
	for _, tx := range l.texts {
		if len(tx.lines) == 1 {
			texts[tx.lines[0]] = true
			if tx.lines[0] == "jul" {
				jul = tx
			}
		}
	}
	julX := jul.box.X + jul.box.W/2
	tallest, highest := 0, 0
	for i := range bars {
		if bars[i].box.H > bars[tallest].box.H {
			tallest = i
		}
		if dots[i].box.Y < dots[highest].box.Y {
			highest = i
		}
	}
	for what, n := range map[string]laidNode{"the tallest bar": bars[tallest], "the highest dot": dots[highest]} {
		if cx := n.box.X + n.box.W/2; cx < julX-1 || cx > julX+1 {
			t.Errorf("%s is centred at %v, not over jul (%v)", what, cx, julX)
		}
	}
	for _, s := range []string{"Sales Revenue", "Revenue (in $)", "4000", "11000", "jan", "dec"} {
		if !texts[s] {
			t.Errorf("no label %q", s)
		}
	}
}

// A horizontal chart is not laid out yet: ErrUnsupported, for the caller's fallback.
func TestAHorizontalXYChartIsNotLaidOut(t *testing.T) {
	_, err := lay(t, "xychart-beta horizontal\n  x-axis [a, b]\n  bar [1, 2]", 2000)
	if !errors.Is(err, pm.ErrUnsupported) {
		t.Errorf("a horizontal chart: %v, want ErrUnsupported", err)
	}
}

// A value past the declared range is laid out clamped to it, its bar and its dot where the range's
// end would put them, never past the plot into the title.
func TestAnXYValuePastItsRangeIsClampedInTheWindow(t *testing.T) {
	const head = "xychart-beta\n  title Clamped\n  x-axis [a, b, c]\n  y-axis 0 --> 10\n"
	past, err := lay(t, head+"  bar [-5, 5, 15]\n  line [-5, 5, 15]\n", 2000)
	if err != nil {
		t.Fatal(err)
	}
	ends, err := lay(t, head+"  bar [0, 5, 10]\n  line [0, 5, 10]\n", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(past.nodes) != len(ends.nodes) {
		t.Fatalf("%d nodes; want %d", len(past.nodes), len(ends.nodes))
	}
	for i := range past.nodes {
		if past.nodes[i].box != ends.nodes[i].box {
			t.Errorf("node %d at %+v; want %+v, as at the range's end", i, past.nodes[i].box, ends.nodes[i].box)
		}
	}
}
