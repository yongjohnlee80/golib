package mermaid

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

func layModel(t *testing.T, d pm.Diagram, width float32) (*Laid, error) {
	t.Helper()
	m, release := gui.AcquireMeasurer(1)
	defer release()
	return LayDiagram(context.Background(), d, width, testTheme, m)
}

// centre is the middle of node i's box, in Paint's coordinates (the fit applied).
func centre(l *Laid, i int) gui.Point {
	b := l.nodes[i].box
	return gui.Pt((b.X+b.W/2)*l.fit, (b.Y+b.H/2)*l.fit)
}

// A modelled mind map keeps each node's own ID whatever its shape, the default text shape
// included, and shows its label verbatim: titles that Mermaid's syntax would merge, refuse or need
// escaping, and two the same. A point in each node's box answers that node's ID.
func TestLayDiagramKeepsEveryNodesIDAndLabel(t *testing.T) {
	nodes := []pm.MindNode{
		{ID: "n3", Label: "Root doc", Shape: pm.MindDefault, Parent: -1},
		{ID: "n0", Label: "Same title", Shape: pm.MindDefault, Parent: 0},
		{ID: "n5", Label: "Same title", Shape: pm.MindRounded, Parent: 0},
		{ID: "n1", Label: `An <b>ADR</b> "quoted" [x] (y) {{z}}`, Shape: pm.MindHexagon, Parent: 0},
		{ID: "n2", Label: "::icon(fa fa-book)", Shape: pm.MindCloud, Parent: 2},
	}
	l, err := layModel(t, &pm.MindmapDiagram{Nodes: nodes}, 4000)
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range nodes {
		id, ok := l.NodeAt(centre(l, i))
		if !ok || id != n.ID {
			t.Errorf("node %d (%q): NodeAt its centre = %q, %v; want %q", i, n.Label, id, ok, n.ID)
		}
		if got := strings.Join(l.nodes[i].label.lines, " "); got != n.Label {
			t.Errorf("node %d shows %q; want its label verbatim %q", i, got, n.Label)
		}
	}
	l.Paint(gui.NewRecordingCanvas(gui.Size{W: 4000, H: 1000}, gui.Size{W: 8, H: 16}))
}

// NodeAt works in Paint's coordinates: a diagram scaled down to fit answers at its scaled centres,
// and a point in no node answers false.
func TestNodeAtUndoesTheFit(t *testing.T) {
	d, err := pm.Parse(mindmapExample)
	if err != nil {
		t.Fatal(err)
	}
	l, err := layModel(t, d, 300)
	if err != nil {
		t.Fatal(err)
	}
	if l.fit >= 1 {
		t.Fatalf("fixture: fit %v; want the diagram scaled down", l.fit)
	}
	md := d.(*pm.MindmapDiagram)
	for i, n := range md.Nodes {
		if id, ok := l.NodeAt(centre(l, i)); !ok || id != n.ID {
			t.Errorf("node %d: NodeAt its scaled centre = %q, %v; want %q", i, id, ok, n.ID)
		}
	}
	if id, ok := l.NodeAt(gui.Pt(-5, -5)); ok {
		t.Errorf("a point off the diagram answered %q", id)
	}
}

// Where boxes overlap, the node drawn last, on top, answers; a node without an id never does.
func TestNodeAtAnswersTheTopNode(t *testing.T) {
	l := &Laid{fit: 1, nodes: []laidNode{
		{id: "under", box: gui.Rect{X: 0, Y: 0, W: 100, H: 100}},
		{id: "over", box: gui.Rect{X: 50, Y: 50, W: 100, H: 100}},
		{box: gui.Rect{X: 200, Y: 0, W: 50, H: 50}}, // no id: a layer that keeps none
	}}
	if id, _ := l.NodeAt(gui.Pt(75, 75)); id != "over" {
		t.Errorf("the overlap answered %q; want the node drawn last, over", id)
	}
	if id, _ := l.NodeAt(gui.Pt(25, 25)); id != "under" {
		t.Errorf("under alone answered %q", id)
	}
	if id, ok := l.NodeAt(gui.Pt(210, 10)); ok {
		t.Errorf("a node without an id answered %q", id)
	}
}

// LayDiagram refuses, without laying anything out, every model but a mind map, and a mind map
// with a shape mind maps do not define; past the node or label limit is ErrTooLarge.
func TestLayDiagramRefusesWhatItCannotLay(t *testing.T) {
	invalid := map[string]pm.Diagram{
		"nil":                     nil,
		"a nil mind map":          (*pm.MindmapDiagram)(nil),
		"a flowchart":             &pm.FlowchartDiagram{Nodes: []pm.Node{{ID: "a", Label: "A"}}},
		"a flowchart's bad shape": &pm.FlowchartDiagram{Nodes: []pm.Node{{ID: "a", Label: "A", Shape: pm.Shape(255)}}},
		"an undefined shape":      &pm.MindmapDiagram{Nodes: []pm.MindNode{{ID: "a", Label: "A", Parent: -1, Shape: pm.MindShape(255)}}},
		"a shape just past":       &pm.MindmapDiagram{Nodes: []pm.MindNode{{ID: "a", Parent: -1}, {ID: "b", Parent: 0, Shape: pm.MindHexagon + 1}}},
	}
	for name, d := range invalid {
		if _, err := layModel(t, d, 400); !errors.Is(err, ErrInvalidModel) {
			t.Errorf("%s: %v; want ErrInvalidModel", name, err)
		}
	}
	many := &pm.MindmapDiagram{Nodes: []pm.MindNode{{ID: "r", Parent: -1}}}
	for range maxModelNodes {
		many.Nodes = append(many.Nodes, pm.MindNode{ID: "c", Parent: 0})
	}
	long := &pm.MindmapDiagram{Nodes: []pm.MindNode{{ID: "r", Label: strings.Repeat("x", pm.MaxLabel+1), Parent: -1}}}
	for name, d := range map[string]pm.Diagram{"too many nodes": many, "a label too long": long} {
		m, release := gui.AcquireMeasurer(1)
		counted := &countingMeasurer{Measurer: m}
		_, err := LayDiagram(context.Background(), d, 400, testTheme, counted)
		release()
		if !errors.Is(err, pm.ErrTooLarge) || counted.n != 0 {
			t.Errorf("%s: %v after %d labels measured; want mermaid.ErrTooLarge before any", name, err, counted.n)
		}
	}
	every := &pm.MindmapDiagram{Nodes: []pm.MindNode{{ID: "r", Parent: -1}}}
	for s := pm.MindDefault; s <= pm.MindHexagon; s++ {
		every.Nodes = append(every.Nodes, pm.MindNode{ID: "s", Label: "s", Parent: 0, Shape: s})
	}
	if _, err := layModel(t, every, 400); err != nil {
		t.Errorf("every defined shape: %v", err)
	}
}

// LayDiagram refuses a mind map model that breaks the parser's invariants, keeps an empty one
// valid (as Lay("mindmap") is), and lays a parsed model as Lay lays its source.
func TestLayDiagramKeepsTheParsersInvariants(t *testing.T) {
	bad := map[string][]pm.MindNode{
		"two roots":             {{ID: "a", Parent: -1}, {ID: "b", Parent: -1}},
		"the root not first":    {{ID: "a", Parent: 1}, {ID: "b", Parent: -1}},
		"a parent after":        {{ID: "a", Parent: -1}, {ID: "b", Parent: 2}, {ID: "c", Parent: 0}},
		"a parent of itself":    {{ID: "a", Parent: -1}, {ID: "b", Parent: 1}},
		"a parent out of range": {{ID: "a", Parent: -1}, {ID: "b", Parent: 7}},
	}
	for name, nodes := range bad {
		if _, err := layModel(t, &pm.MindmapDiagram{Nodes: nodes}, 400); !errors.Is(err, ErrInvalidModel) {
			t.Errorf("%s: %v; want ErrInvalidModel", name, err)
		}
	}
	empty, err := layModel(t, &pm.MindmapDiagram{}, 400)
	if err != nil {
		t.Fatalf("an empty model: %v; want it laid out empty", err)
	}
	empty.Paint(gui.NewRecordingCanvas(gui.Size{W: 400, H: 400}, gui.Size{W: 8, H: 16}))

	d, err := pm.Parse(mindmapExample)
	if err != nil {
		t.Fatal(err)
	}
	viaModel, err := layModel(t, d, 900)
	if err != nil {
		t.Fatal(err)
	}
	viaSource, err := lay(t, mindmapExample, 900)
	if err != nil {
		t.Fatal(err)
	}
	if viaModel.Size() != viaSource.Size() || len(viaModel.nodes) != len(viaSource.nodes) {
		t.Fatalf("the model laid %v with %d nodes, the source %v with %d", viaModel.Size(), len(viaModel.nodes), viaSource.Size(), len(viaSource.nodes))
	}
}

// countingMeasurer counts the labels measured.
type countingMeasurer struct {
	gui.Measurer
	n int
}

func (c *countingMeasurer) Measure(s string, f gui.Font) gui.Measured {
	c.n++
	return c.Measurer.Measure(s, f)
}

func (c *countingMeasurer) Wrap(s string, f gui.Font, width float32) ([]string, gui.Size) {
	c.n++
	return c.Measurer.Wrap(s, f, width)
}
