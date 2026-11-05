package mermaid

import (
	"context"
	"fmt"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// classCases are class diagrams drawn in cells, each a golden: every relation end, cardinalities and
// a label, members and an annotation, running down and across, a namespace, and notes.
var classCases = map[string]string{
	"class-basic": `classDiagram
  class Animal {
    <<abstract>>
    +String name
    +int age
    +eat() void
  }
  class Duck {
    +swim()
  }
  Animal <|-- Duck
  Animal <|-- Fish
  Animal "1" *-- "4" Leg : has
  Zoo "1" o-- "*" Animal
  Duck ..> Pond : visits
`,
	"class-lr": `classDiagram
  direction LR
  Customer "1" --> "0..*" Order : places
  Order *-- "1..*" LineItem
  LineItem --> Product
`,
	"class-namespace-note": `classDiagram
  namespace Shapes {
    class Shape {
      +area() float
    }
    class Circle
  }
  Shape <|-- Circle
  Circle --> Point
  note for Circle "r > 0"
  note "Drawn in cells\nby the router"
`,
}

// Class diagrams are drawn through the router, each its golden, and hold its invariants
// (checkGraph): every relation routed, no label lost.
func TestAClassDiagramDrawsInCells(t *testing.T) {
	for name, src := range classCases {
		t.Run(name, func(t *testing.T) {
			cd := parseClass(t, src)
			checkGraph(t, fmt.Sprintf("%s:\n%s", name, src), classGraph(cd))
			g, err := Lay(context.Background(), cd, newCells(1, 1))
			if err != nil {
				t.Fatal(err)
			}
			s := newCells(g.Size.W, g.Size.H)
			g.Paint(s, 0, 0, -1, Theme{})
			golden(t, name, s.String())
		})
	}
}

// parseClass is src's class diagram.
func parseClass(t *testing.T, src string) *pm.ClassDiagram {
	t.Helper()
	d, err := pm.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	cd, ok := d.(*pm.ClassDiagram)
	if !ok {
		t.Fatalf("not a class diagram: %T", d)
	}
	return cd
}

// Each relation end draws its mark, oriented into its box: inheritance a hollow triangle,
// composition a filled diamond, aggregation a hollow one, an association an arrowhead; a plain end
// none. A class's box holds its annotation, name and members, in sections.
func TestAClassRelationsEndsAreTheirMarks(t *testing.T) {
	cd := parseClass(t, classCases["class-basic"])
	g, err := LayClass(context.Background(), cd, newCells(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	for _, mk := range g.marks {
		count[mk]++
	}
	// two inheritances, one composition, one aggregation, one association (dotted)
	for mk, want := range map[string]int{"△": 2, "◆": 1, "◇": 1} {
		if count[mk] != want {
			t.Errorf("%d %s marks, want %d: %v", count[mk], mk, want, count)
		}
	}
	if count["▼"]+count["▲"] != 1 {
		t.Errorf("%d arrowheads, want 1 (Duck ..> Pond): %v", count["▼"]+count["▲"], count)
	}
	for _, b := range g.Boxes {
		if b.ID == "Animal" {
			want := [][]string{{"«abstract»", "Animal"}, {"+String name", "+int age"}, {"+eat() void"}}
			if fmt.Sprint(b.Sections) != fmt.Sprint(want) {
				t.Errorf("Animal's sections %q, want %q", b.Sections, want)
			}
		}
		if b.ID == "Leg" && b.Sections != nil {
			t.Errorf("a class with no members has sections: %q", b.Sections)
		}
	}
}

// A relation's cardinalities are written beside its ends, at the boxes they belong to: each within
// a few cells of its own class's box, never lost.
func TestACardinalityIsBesideItsEnd(t *testing.T) {
	for _, dir := range []string{"", "  direction LR\n"} {
		cd := parseClass(t, "classDiagram\n"+dir+"  Customer \"one\" --> \"many\" Order\n")
		g, err := LayClass(context.Background(), cd, newCells(1, 1))
		if err != nil {
			t.Fatal(err)
		}
		box := map[string]int{}
		for i, b := range g.Boxes {
			box[b.ID] = i
		}
		for label, class := range map[string]string{"one": "Customer", "many": "Order"} {
			r := g.Boxes[box[class]].Rect
			found := false
			for _, tx := range g.texts {
				if tx.s != label {
					continue
				}
				found = true
				// beside the box's side it meets the line on, in the gap next to it
				dx := max(r.X-(tx.x+tx.w), tx.x-(r.X+r.W-1), 0)
				dy := max(r.Y-tx.y, tx.y-(r.Y+r.H-1), 0)
				if dx > 2 || dy > 1 {
					t.Errorf("%q: %q at (%d,%d) is %d,%d cells from %s %+v", dir, label, tx.x, tx.y, dx, dy, class, r)
				}
			}
			if !found {
				t.Errorf("%q: the cardinality %q isn't drawn: %v", dir, label, g.texts)
			}
		}
	}
}

// A note is a dashed box, tied to the class it's for by a dashed line with no ends; a note on the
// diagram is a box alone.
func TestAClassNoteIsADashedBoxTiedByADashedLine(t *testing.T) {
	cd := parseClass(t, classCases["class-namespace-note"])
	g, err := LayClass(context.Background(), cd, newCells(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	var notes int
	for _, b := range g.Boxes {
		if b.Form == FormNote {
			notes++
		}
	}
	if notes != 2 {
		t.Fatalf("%d note boxes, want 2", notes)
	}
	gr := classGraph(cd)
	tie := gr.edges[len(gr.edges)-1]
	if gr.nodes[tie.from].form != FormNote || gr.nodes[tie.to].id != "Circle" || tie.stroke != StrokeDashed ||
		tie.head != EndNone || tie.tail != EndNone {
		t.Errorf("the note's tie: %+v, want a dashed line with no ends from the note to Circle", tie)
	}
	s := newCells(g.Size.W, g.Size.H)
	g.Paint(s, 0, 0, -1, Theme{})
	if drawn := s.String(); !strings.Contains(drawn, "┄") || !strings.Contains(drawn, "┆") {
		t.Errorf("no dashed outline or tie:\n%s", drawn)
	}
}

// A relation whose end is no class is refused, ErrUnsupported, never dropped. (The parser makes a
// class of every name a relation mentions; a model built otherwise can still name none.)
func TestAClassRelationToNoClassIsRefused(t *testing.T) {
	cd := &pm.ClassDiagram{Dir: pm.TB, Classes: []pm.ClassBox{{ID: "A", Label: "A"}},
		Relations: []pm.Relation{{From: "A", To: "Nowhere"}}}
	if _, err := LayClass(context.Background(), cd, newCells(1, 1)); err != ErrUnsupported {
		t.Errorf("a relation to no class: %v, want ErrUnsupported", err)
	}
}

// A class diagram that runs across and is too wide for its room is laid running down (LayWithin),
// as a flowchart is; one that fits keeps its direction.
func TestAWideClassDiagramIsLaidDown(t *testing.T) {
	cd := parseClass(t, "classDiagram\n  direction LR\n  Alpha --> Beta\n  Beta --> Gamma\n  Gamma --> Delta\n  Delta --> Epsilon\n")
	ctx := context.Background()
	wide, err := Lay(ctx, cd, newCells(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	g, err := LayWithin(ctx, cd, newCells(1, 1), wide.Size.W-1)
	if err != nil {
		t.Fatal(err)
	}
	if g.Size.W >= wide.Size.W {
		t.Errorf("laid within %d: %d wide, not narrower than across (%d)", wide.Size.W-1, g.Size.W, wide.Size.W)
	}
	if g, _ := LayWithin(ctx, cd, newCells(1, 1), wide.Size.W); g.Size != wide.Size {
		t.Errorf("a room it fits: %+v, want it across as it is (%+v)", g.Size, wide.Size)
	}
	if classDown(parseClass(t, "classDiagram\n  A --> B\n")) != nil {
		t.Error("a class diagram already running down has a narrower form")
	}
}
