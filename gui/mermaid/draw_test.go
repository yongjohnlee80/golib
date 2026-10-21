package mermaid

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// Every form, end and frame a layer can ask for paints: shapes as paths, every label as text.
func TestEveryPrimitivePaints(t *testing.T) {
	m, release := gui.AcquireMeasurer(1)
	defer release()
	lb := func(s string) label { return newLabel(m, s, testTheme.Font, 0, testTheme.Text) }
	l := &Laid{th: testTheme, size: gui.Size{W: 900, H: 600}, fit: 1}
	x := float32(10)
	for f := formRect; f <= formFace; f++ {
		box := gui.Rect{X: x, Y: 10, W: 40, H: 60}
		n := laidNode{form: f, box: box, fill: testTheme.NodeFill, stroke: testTheme.NodeStroke, width: 1.2, label: lb("n").centredIn(box)}
		if f == formRect {
			n.parts = []part{{y: 40, labels: []label{lb("+a()").at(x+2, 42)}}}
		}
		l.nodes = append(l.nodes, n)
		x += 45
	}
	y := float32(100)
	for e := endNone; e <= endZeroMany; e++ {
		head := lb("1..*").at(300, y)
		l.edges = append(l.edges, laidEdge{pts: []gui.Point{{X: 10, Y: y}, {X: 290, Y: y}}, head: e, tail: e, color: testTheme.Line,
			headLabel: &head, line: pm.Solid, over: e%2 == 0})
		y += 25
	}
	l.group = append(l.group, laidGroup{box: gui.Rect{X: 400, Y: 100, W: 200, H: 150}, title: lb("loop every minute").at(404, 102),
		tab: true, dashed: false, parts: []part{{y: 170, dashed: true, labels: []label{lb("[else]").at(404, 172)}}}})
	l.texts = append(l.texts, lb("title"))

	rc := gui.NewRecordingCanvas(gui.Size{W: 900, H: 600}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	texts, paths := 0, 0
	for _, c := range rc.Calls {
		switch c.Op {
		case "DrawText":
			texts++
		case "FillPath", "StrokePath":
			paths++
		}
	}
	// a label per box (formNone's too) and the compartment, a head label per end, the group's
	// title and its divider's label, the free text
	want := int(formFace-formRect+1) + 1 + int(endZeroMany-endNone+1) + 2 + 1
	if texts != want {
		t.Errorf("%d texts drawn, want %d", texts, want)
	}
	if paths < int(formFace-formRect)+int(endZeroMany-endNone) {
		t.Errorf("only %d paths drawn", paths)
	}
}
