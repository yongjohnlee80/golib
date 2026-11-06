package mermaid

import (
	"strings"
	"testing"
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

// A horizontal chart is laid out turned: a bar a category running right from the value axis, the
// widest July's, on the band of July's label; every bar starts at one x (the axis).
func TestAHorizontalXYChartIsLaidOut(t *testing.T) {
	l, err := lay(t, strings.Replace(xyDocumented, "xychart-beta", "xychart-beta horizontal", 1), 2000)
	if err != nil {
		t.Fatal(err)
	}
	keyX := float32(1 << 30)
	var jul label
	for _, tx := range l.texts {
		if len(tx.lines) != 1 {
			continue
		}
		switch tx.lines[0] {
		case "bar 1", "line 2":
			keyX = min(keyX, tx.box.X-l.th.Font.Size*2)
		case "jul":
			jul = tx
		}
	}
	var bars []laidNode
	for _, n := range l.nodes {
		if n.form == formRect && n.box.X < keyX {
			bars = append(bars, n)
		}
	}
	if len(bars) != 12 {
		t.Fatalf("%d bars; want 12", len(bars))
	}
	widest := 0
	for i, b := range bars {
		if b.box.X != bars[0].box.X {
			t.Errorf("bar %d starts at x %v, not the axis's %v", i, b.box.X, bars[0].box.X)
		}
		if b.box.W > bars[widest].box.W {
			widest = i
		}
		if b.box.W <= 0 || b.box.H <= 0 {
			t.Errorf("bar %d is %+v", i, b.box)
		}
	}
	bc, jc := bars[widest].box.Y+bars[widest].box.H/2, jul.box.Y+jul.box.H/2
	if bc < jc-2 || bc > jc+2 {
		t.Errorf("the widest bar is centred at y %v, not on jul's band (%v)", bc, jc)
	}
}
