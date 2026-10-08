package mermaid

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

const erSrc = `erDiagram
    CUSTOMER ||--o{ ORDER : places
    ORDER ||--|{ LINE-ITEM : contains
    CUSTOMER }|..|{ DELIVERY-ADDRESS : uses
    CUSTOMER {
        string name PK "the customer's legal name"
        string custNumber UK
        string sector
    }
    ORDER {
        int orderNumber PK
        string deliveryAddress FK
    }`

// Entities are laid out with no box over another, each a name over its attribute table.
func TestAnERDiagramIsLaidOut(t *testing.T) {
	l, err := lay(t, erSrc, 2000)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.nodes) != 4 || len(l.edges) != 3 {
		t.Fatalf("laid %d entities and %d relationships, want 4 and 3", len(l.nodes), len(l.edges))
	}
	for i, a := range l.nodes {
		for _, b := range l.nodes[i+1:] {
			if a.box.X < b.box.X+b.box.W && b.box.X < a.box.X+a.box.W && a.box.Y < b.box.Y+b.box.H && b.box.Y < a.box.Y+a.box.H {
				t.Errorf("%q's box %+v overlaps %q's %+v", a.label.lines, a.box, b.label.lines, b.box)
			}
		}
	}
	cust := l.nodes[0]
	if len(cust.parts) != 3 || cust.parts[0].y <= cust.box.Y || cust.parts[0].y >= cust.box.Y+cust.box.H {
		t.Fatalf("CUSTOMER's table: %d rows, divider at %v in %+v", len(cust.parts), cust.parts[0].y, cust.box)
	}
	for _, p := range cust.parts {
		for _, lb := range p.labels {
			if !lb.left || lb.box.Y < cust.parts[0].y || lb.box.X < cust.box.X || lb.box.X+lb.box.W > cust.box.X+cust.box.W+0.01 {
				t.Errorf("cell %q at %+v is not inside the table of %+v", lb.lines, lb.box, cust.box)
			}
		}
	}
	if len(l.nodes[2].parts) != 0 {
		t.Errorf("LINE-ITEM has no attributes but %d rows", len(l.nodes[2].parts))
	}
}

// Every row's cells sit in the same columns: a column's x is the same in every row that fills it.
func TestAttributeColumnsAlign(t *testing.T) {
	l, err := lay(t, `erDiagram
    T {
        string a PK "first"
        varchar(255) longerName
        int b FK, UK "third, a longer comment"
    }`, 2000)
	if err != nil {
		t.Fatal(err)
	}
	rows := l.nodes[0].parts
	if len(rows) != 3 {
		t.Fatalf("%d rows, want 3", len(rows))
	}
	// each column is the cell index in each row (-1: that row leaves it empty): the first and last
	// rows fill all four, the second only its type and name
	cols := [][]int{{0, 0, 0}, {1, 1, 1}, {2, -1, 2}, {3, -1, 3}}
	for c, at := range cols {
		x := rows[0].labels[at[0]].box.X
		for r, i := range at {
			if i >= 0 && rows[r].labels[i].box.X != x {
				t.Errorf("column %d: row %d's cell at x %v, row 0's at %v", c, r, rows[r].labels[i].box.X, x)
			}
		}
	}
	if len(rows[1].labels) != 2 {
		t.Errorf("row 1 has %d cells, want 2: its keys and comment are empty", len(rows[1].labels))
	}
	if !(rows[0].labels[0].box.X < rows[0].labels[1].box.X && rows[0].labels[1].box.X < rows[0].labels[2].box.X) {
		t.Error("the columns are not left to right")
	}
}

// Each relationship's crow's feet sit at the right ends, its line solid when identifying.
func TestCrowsFeetAtTheirEnds(t *testing.T) {
	l, err := lay(t, erSrc, 2000)
	if err != nil {
		t.Fatal(err)
	}
	near := func(p gui.Point, r gui.Rect) float32 {
		dx := max(r.X-p.X, 0, p.X-(r.X+r.W))
		dy := max(r.Y-p.Y, 0, p.Y-(r.Y+r.H))
		return dx + dy
	}
	box := map[string]gui.Rect{}
	for _, n := range l.nodes {
		box[n.label.lines[0]] = n.box
	}
	for i, c := range []struct {
		from, to   string
		tail, head end
		line       pm.LineKind
	}{
		{"CUSTOMER", "ORDER", endOne, endZeroMany, pm.Solid},
		{"ORDER", "LINE-ITEM", endOne, endMany, pm.Solid},
		{"CUSTOMER", "DELIVERY-ADDRESS", endMany, endMany, pm.Dotted},
	} {
		e := l.edges[i]
		if e.tail != c.tail || e.head != c.head || e.line != c.line {
			t.Errorf("%s–%s: ends %v/%v line %v, want %v/%v %v", c.from, c.to, e.tail, e.head, e.line, c.tail, c.head, c.line)
		}
		first, last := e.pts[0], e.pts[len(e.pts)-1]
		if near(first, box[c.from]) > 1 || near(last, box[c.to]) > 1 {
			t.Errorf("%s–%s runs from %v to %v: not from %s's box to %s's", c.from, c.to, first, last, c.from, c.to)
		}
		if e.label == nil {
			t.Errorf("%s–%s has no label", c.from, c.to)
		}
	}
}

// Painted: every entity's name, every attribute cell, every relationship's label.
func TestAnERDiagramPaintsItsText(t *testing.T) {
	l, err := lay(t, erSrc, 2000)
	if err != nil {
		t.Fatal(err)
	}
	rc := gui.NewRecordingCanvas(gui.Size{W: 2000, H: 2000}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	texts := 0
	for _, c := range rc.Calls {
		if c.Op == "DrawText" {
			texts++
		}
	}
	// 4 names; CUSTOMER's rows: 4 + 3 + 2 cells; ORDER's: 3 + 3; 3 labels
	if want := 4 + 9 + 6 + 3; texts != want {
		t.Errorf("%d texts drawn, want %d", texts, want)
	}
}

// A wide ER diagram is scaled to the width asked.
func TestAnERDiagramFitsItsWidth(t *testing.T) {
	l, err := lay(t, erSrc, 150)
	if err != nil {
		t.Fatal(err)
	}
	if l.Size().W != 150 || l.fit >= 1 {
		t.Errorf("size %+v fit %v: want 150 wide, scaled down", l.Size(), l.fit)
	}
}
