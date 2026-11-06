package mermaid

import (
	"math"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// A pie lays out as its wedges and its legend: each slice a wedge, clockwise from 12 o'clock, in
// the model's order, each wedge stroked apart from its neighbour; the legend a chip, a label and
// a share a slice, every share a percentage; the title over all. A wedge wide enough holds its
// share's percentage; a narrow one does not.
func TestAPieIsLaidOut(t *testing.T) {
	l, err := lay(t, "pie title NETFLIX\n  \"Time spent looking for movie\" : 70\n  \"Time spent watching it\" : 30", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.sectors) != 2 {
		t.Fatalf("laid %d wedges; want 2", len(l.sectors))
	}
	s0, s1 := l.sectors[0], l.sectors[1]
	if s0.a0 != 0 || math.Abs(float64(s0.a1-0.7*2*math.Pi)) > 1e-3 || math.Abs(float64(s1.a1-2*math.Pi)) > 1e-3 || s1.a0 != s0.a1 {
		t.Fatalf("wedge angles [%v, %v], [%v, %v]; want [0, 70%%] then [70%%, 100%%] of the circle",
			s0.a0, s0.a1, s1.a0, s1.a1)
	}
	if s0.fill == s1.fill {
		t.Errorf("the two wedges share one colour: %v", s0.fill)
	}
	if s0.cx != s1.cx || s0.cy != s1.cy || s0.r != s1.r {
		t.Errorf("wedges of one pie: [%v] and [%v]", s0, s1)
	}
	chips := 0
	var has70, has30, hasTitle, hasLabel bool
	for _, n := range l.nodes {
		if n.form == formRect && n.box.W == n.box.H && n.label.lines == nil {
			chips++
		}
	}
	for _, t2 := range l.texts {
		if len(t2.lines) != 1 {
			continue
		}
		switch t2.lines[0] {
		case "70%":
			has70 = true
		case "30%":
			has30 = true
		case "NETFLIX":
			hasTitle = true
		case "Time spent looking for movie":
			hasLabel = true
		}
	}
	if chips != 2 {
		t.Errorf("legend chips %d; want 2", chips)
	}
	if !hasTitle || !hasLabel || !has30 {
		t.Errorf("the legend's texts: title %v, label %v, 30%% share %v", hasTitle, hasLabel, has30)
	}
	if !has70 {
		t.Error("the 70% wedge holds no percentage")
	}
}

// showData puts each slice's value beside its share in the legend, as Mermaid's does.
func TestAPieShowsDataWhenAsked(t *testing.T) {
	l, err := lay(t, "pie showData\n  \"a\" : 2.5\n  \"b\" : 7.5", 2000)
	if err != nil {
		t.Fatal(err)
	}
	var has25, hasValue bool
	for _, t2 := range l.texts {
		if len(t2.lines) != 1 {
			continue
		}
		switch {
		case t2.lines[0] == "2.5 (25%)":
			hasValue = true
		case t2.lines[0] == "25%":
			has25 = true
		}
	}
	if !has25 {
		t.Error("no 25% share in the legend")
	}
	if !hasValue {
		t.Error("showData left the legend without the slice's value")
	}
}

// A narrow wedge holds no percentage: the label would not fit inside it. The legend tells every
// share, so a count of the texts tells the wedge labels from the legend's own: the legend holds one
// share a slice, and only the 98% wedge adds one of its own.
func TestANarrowWedgeHoldsNoPercentage(t *testing.T) {
	l, err := lay(t, "pie\n  \"big\" : 98\n  \"sliver\" : 1\n  \"sliver too\" : 1", 2000)
	if err != nil {
		t.Fatal(err)
	}
	ones, nines := 0, 0
	for _, t2 := range l.texts {
		if len(t2.lines) != 1 {
			continue
		}
		switch t2.lines[0] {
		case "1%", "1.0%":
			ones++
		case "98%":
			nines++
		}
	}
	if ones != 2 {
		t.Errorf("1%% texts %d; want 2, the legend's two shares alone (no wedge label)", ones)
	}
	if nines != 2 {
		t.Errorf("98%% texts %d; want 2, the legend's share and the wide wedge's own", nines)
	}
}

// A pie whose values sum to nothing has no angles: its legend alone, every share zero.
func TestAValuelessPieLaysOutItsLegendAlone(t *testing.T) {
	l, err := lay(t, "pie title Nothing\n  \"a\" : 0\n  \"b\" : 0", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.sectors) != 0 {
		t.Fatalf("a valueless pie laid %d wedges; want none", len(l.sectors))
	}
	var zeros int
	for _, t2 := range l.texts {
		if len(t2.lines) == 1 && t2.lines[0] == "0%" {
			zeros++
		}
	}
	if zeros != 2 {
		t.Errorf("legend shares %d; want two of 0%%", zeros)
	}
}

// An empty pie lays out empty, as Lay of an empty mind map does; a pie of one slice fills its whole
// circle with the theme's node fill.
func TestAnEmptyPieAndAPieOfOne(t *testing.T) {
	l, err := lay(t, "pie", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.sectors) != 0 || len(l.nodes) != 0 {
		t.Fatalf("an empty pie laid %+v", l)
	}
	l, err = lay(t, "pie\n  \"all\" : 1", 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.sectors) != 1 || l.sectors[0].a1-l.sectors[0].a0 != 2*math.Pi || l.sectors[0].fill != testTheme.NodeFill {
		t.Fatalf("a pie of one: %+v", l.sectors)
	}
}

// A pie paints on the recording canvas: its wedges filled and stroked, its title, legend and the
// percentages of the wedges wide enough drawn as text.
func TestAPiePaints(t *testing.T) {
	l, err := lay(t, "pie showData title T\n  \"a\" : 60\n  \"b\" : 40", 2000)
	if err != nil {
		t.Fatal(err)
	}
	rc := gui.NewRecordingCanvas(gui.Size{W: 2000, H: 1000}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	texts, fills, strokes := 0, 0, 0
	for _, c := range rc.Calls {
		switch c.Op {
		case "DrawText":
			texts++
		case "FillPath":
			fills++
		case "StrokePath":
			strokes++
		}
	}
	// the title, two legend labels, two shares with their values, two wedge percentages
	if texts != 7 {
		t.Errorf("%d texts drawn, want 7", texts)
	}
	if fills != 2 || strokes != 2 {
		t.Errorf("%d fills and %d strokes drawn, want 2 of each: the two wedges", fills, strokes)
	}
}

// A pie wider than the width asked scales to fit, as every other diagram does.
func TestAWidePieScalesToFit(t *testing.T) {
	wide := "pie title A very long title that outgrows the width asked for the diagram\n"
	for i := range 30 {
		wide += "  \"slice number " + string(rune('a'+i)) + "\" : 1\n"
	}
	l, err := lay(t, wide, 400)
	if err != nil {
		t.Fatal(err)
	}
	if l.size.W > 400.5 {
		t.Errorf("size %v, want no wider than 400", l.size)
	}
	if len(l.sectors) != 30 {
		t.Errorf("wedges %d; want 30", len(l.sectors))
	}
}
