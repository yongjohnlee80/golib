package mermaid

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

const quadrantDocumented = `quadrantChart
    title Reach and engagement of campaigns
    x-axis Low Reach --> High Reach
    y-axis Low Engagement --> High Engagement
    quadrant-1 We should expand
    quadrant-2 Need to promote
    quadrant-3 Re-evaluate
    quadrant-4 May be improved
    Campaign A: [0.3, 0.6]
    Campaign B: [0.45, 0.23]
    Campaign C: [0.57, 0.69]
    Campaign D: [0.78, 0.34]
    Campaign E: [0.40, 0.34]
    Campaign F: [0.35, 0.78]`

// A quadrant chart lays out as four filled quadrants, neighbours in two tints, each quadrant's
// label on it; a dot a point, in the quadrant its numbers put it in, its label beside it; the axes'
// labels and the title.
func TestAQuadrantChartIsLaidOut(t *testing.T) {
	l, err := lay(t, quadrantDocumented, 2000)
	if err != nil {
		t.Fatal(err)
	}
	var quads []laidNode
	var dots []laidNode
	for _, n := range l.nodes {
		switch n.form {
		case formRect:
			quads = append(quads, n)
		case formStart:
			dots = append(dots, n)
		}
	}
	if len(quads) != 4 || len(dots) != 6 {
		t.Fatalf("%d quadrants and %d dots; want 4 and 6", len(quads), len(dots))
	}
	// Mermaid's numbering: 1 top right, 2 top left, 3 bottom left, 4 bottom right
	q1, q2, q3, q4 := quads[0].box, quads[1].box, quads[2].box, quads[3].box
	if !(q1.X > q2.X && q1.Y == q2.Y && q3.X == q2.X && q3.Y > q2.Y && q4.X == q1.X && q4.Y == q3.Y) {
		t.Fatalf("quadrants %+v %+v %+v %+v; want 1 top right, 2 top left, 3 bottom left, 4 bottom right", q1, q2, q3, q4)
	}
	if quads[0].fill == quads[1].fill || quads[1].fill == quads[2].fill {
		t.Errorf("neighbouring quadrants share a fill: %v %v %v", quads[0].fill, quads[1].fill, quads[2].fill)
	}
	// each dot's centre lies in its point's quadrant: A (0.3, 0.6) top left, B (0.45, 0.23) bottom
	// left, C (0.57, 0.69) top right, D (0.78, 0.34) bottom right
	in := func(n laidNode, b gui.Rect) bool {
		cx, cy := n.box.X+n.box.W/2, n.box.Y+n.box.H/2
		return cx > b.X && cx < b.X+b.W && cy > b.Y && cy < b.Y+b.H
	}
	for i, want := range []gui.Rect{q2, q3, q1, q4} {
		if !in(dots[i], want) {
			t.Errorf("point %d's dot at %+v isn't in its quadrant %+v", i, dots[i].box, want)
		}
	}
	texts := map[string]bool{}
	for _, tx := range l.texts {
		if len(tx.lines) == 1 {
			texts[tx.lines[0]] = true
		}
	}
	for _, s := range []string{"Reach and engagement of campaigns", "We should expand", "Need to promote", "Re-evaluate", "May be improved",
		"Campaign A", "Campaign F", "Low Reach", "High Reach", "Low Engagement", "High Engagement"} {
		if !texts[s] {
			t.Errorf("no label %q", s)
		}
	}
}
