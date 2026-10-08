package mermaid

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

const animals = `classDiagram
    note "From Duck till Zebra"
    Animal <|-- Duck
    note for Duck "can fly\ncan swim"
    Animal <|-- Fish
    Animal "1" *-- "many" Zebra : herds
    Animal : +int age
    Animal: +isMammal()
    class Duck{
        <<bird>>
        +String beakColor
        +swim()
    }
    Fish ..> Water
    Fish o-- Fin
    Fish ..|> Swimmer
    Zebra -- Stripe
    Zebra --> Grass
    namespace Pond {
        class Water
        class Fin
    }`

func boxesOverlap(a, b gui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// A class diagram lays out with no box over another, every name, annotation, member, note,
// label and cardinality painted, each relation's ends as its kind is drawn.
func TestAClassDiagramIsLaidOutAndPainted(t *testing.T) {
	l, err := lay(t, animals, 4000)
	if err != nil {
		t.Fatal(err)
	}
	// 9 classes and 2 notes
	if len(l.nodes) != 11 || len(l.group) != 1 {
		t.Fatalf("%d boxes, %d namespaces; want 11, 1", len(l.nodes), len(l.group))
	}
	for i, a := range l.nodes {
		for _, b := range l.nodes[i+1:] {
			if boxesOverlap(a.box, b.box) {
				t.Errorf("%q's box %+v overlaps %q's %+v", a.label.lines, a.box, b.label.lines, b.box)
			}
		}
	}
	// relations in source order, then the note's line
	want := []struct{ tail, head end }{
		{endTriangle, endNone},    // Animal <|-- Duck
		{endTriangle, endNone},    // Animal <|-- Fish
		{endDiamond, endNone},     // Animal *-- Zebra
		{endNone, endOpen},        // Fish ..> Water: a dependency
		{endDiamondOpen, endNone}, // Fish o-- Fin: the diamond at Fish
		{endNone, endTriangle},    // Fish ..|> Swimmer
		{endNone, endNone},        // Zebra -- Stripe
		{endNone, endArrow},       // Zebra --> Grass
		{endNone, endNone},        // the note on Duck
	}
	if len(l.edges) != len(want) {
		t.Fatalf("%d edges, want %d", len(l.edges), len(want))
	}
	for i, w := range want {
		if e := l.edges[i]; e.tail != w.tail || e.head != w.head {
			t.Errorf("edge %d: ends %v/%v, want %v/%v", i, e.tail, e.head, w.tail, w.head)
		}
	}
	z := l.edges[2]
	if z.tailLabel == nil || z.headLabel == nil || z.label == nil {
		t.Fatalf("Animal *-- Zebra's labels %v %v %v", z.tailLabel, z.label, z.headLabel)
	}
	if !near(z.tailLabel.box, z.pts[0], 60) || !near(z.headLabel.box, z.pts[len(z.pts)-1], 60) {
		t.Errorf("cardinalities %+v %+v not by their ends %v %v", z.tailLabel.box, z.headLabel.box, z.pts[0], z.pts[len(z.pts)-1])
	}

	rc := gui.NewRecordingCanvas(gui.Size{W: 4000, H: 4000}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	texts := 0
	for _, c := range rc.Calls {
		if c.Op == "DrawText" {
			texts++
		}
	}
	// names: 9; Duck's annotation; members: Animal 2, Duck 2; notes: 1 + 2 lines;
	// the namespace's title; the relation label and two cardinalities
	if want := 9 + 1 + 4 + 3 + 1 + 3; texts != want {
		t.Errorf("%d texts drawn, want %d", texts, want)
	}
}

// near reports whether r's centre is within d of p.
func near(r gui.Rect, p gui.Point, d float32) bool {
	return dist(gui.Pt(r.X+r.W/2, r.Y+r.H/2), p) <= d
}

// A box holds its compartments: the name above the first divider, each member inside the box.
func TestAClassBoxHoldsItsCompartments(t *testing.T) {
	l, err := lay(t, "classDiagram\n  class Duck{\n    <<interface>>\n    +String beakColor\n    +swim() bool\n  }", 2000)
	if err != nil {
		t.Fatal(err)
	}
	n := l.nodes[0]
	if len(n.parts) != 3 {
		t.Fatalf("%d parts, want the annotation and two compartments", len(n.parts))
	}
	head, attrs, methods := n.parts[0], n.parts[1], n.parts[2]
	if head.y != 0 || head.labels[0].box.Y+head.labels[0].box.H > n.label.box.Y+0.01 {
		t.Errorf("the annotation %+v is not above the name %+v", head.labels[0].box, n.label.box)
	}
	if !(n.label.box.Y+n.label.box.H <= attrs.y && attrs.y < methods.y && methods.y < n.box.Y+n.box.H) {
		t.Errorf("name %+v, dividers %v %v, box %+v: out of order", n.label.box, attrs.y, methods.y, n.box)
	}
	for _, p := range []part{attrs, methods} {
		for _, lb := range p.labels {
			if !lb.left || lb.box.Y < p.y || lb.box.X < n.box.X || lb.box.X+lb.box.W > n.box.X+n.box.W+0.01 || lb.box.Y+lb.box.H > n.box.Y+n.box.H+0.01 {
				t.Errorf("member %q %+v is not inside its compartment under %v in %+v", lb.lines, lb.box, p.y, n.box)
			}
		}
	}
}

// A class diagram wider than the width asked is scaled down to it.
func TestAWideClassDiagramFits(t *testing.T) {
	l, err := lay(t, "classDiagram\n  direction LR\n  A --> B\n  B --> C\n  C --> D\n  D --> E\n  E --> F", 300)
	if err != nil {
		t.Fatal(err)
	}
	if l.Size().W != 300 || l.fit >= 1 {
		t.Errorf("size %+v fit %v: want 300 wide, scaled down", l.Size(), l.fit)
	}
}
