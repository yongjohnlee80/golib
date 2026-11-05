package mermaid

import (
	"context"
	"fmt"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

// erCases are ER diagrams drawn in cells, each a golden: every crow's foot, attribute tables, a dashed
// relationship, running down and across, and an entity related to itself.
var erCases = map[string]string{
	"er-basic": `erDiagram
  CUSTOMER {
    string name PK
    string email UK "unique, lower case"
    int age
  }
  ORDER {
    int id PK
    int customer FK
  }
  CUSTOMER ||--o{ ORDER : places
  ORDER ||--|{ LINE_ITEM : contains
  CUSTOMER }|..|{ ADDRESS : uses
  ORDER |o--o| INVOICE : bills
`,
	"er-lr": `erDiagram
  direction LR
  CUSTOMER ||--o{ ORDER : places
  ORDER ||--|{ LINE_ITEM : contains
  ORDER |o..o| INVOICE : bills
`,
	"er-self": `erDiagram
  EMPLOYEE ||--o{ EMPLOYEE : manages
  EMPLOYEE ||--|| DESK : sits
`,
}

func parseER(t *testing.T, src string) *pm.ERDiagram {
	t.Helper()
	d, err := pm.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	er, ok := d.(*pm.ERDiagram)
	if !ok {
		t.Fatalf("not an ER diagram: %T", d)
	}
	return er
}

func paintedER(t *testing.T, g *Grid) string {
	t.Helper()
	s := newCells(g.Size.W, g.Size.H)
	g.Paint(s, 0, 0, -1, Theme{})
	return s.String()
}

// ER diagrams are drawn through the router, each its golden, and hold its invariants (checkGraph):
// every relationship routed, every end's crow's foot beside its own box, no label lost.
func TestAnERDiagramDrawsInCells(t *testing.T) {
	for name, src := range erCases {
		t.Run(name, func(t *testing.T) {
			er := parseER(t, src)
			checkGraph(t, fmt.Sprintf("%s:\n%s", name, src), erGraph(er, newCells(1, 1)))
			g, err := Lay(context.Background(), er, newCells(1, 1))
			if err != nil {
				t.Fatal(err)
			}
			golden(t, name, paintedER(t, g))
		})
	}
}

// Each cardinality is its own crow's foot, at its own end, in every direction: the cell beside the box
// oriented to open toward it, the line's next cell out its other half.
func TestEachCardinalityIsItsCrowsFoot(t *testing.T) {
	for _, c := range []struct {
		name, card string
		inner      map[pm.Dir]string // beside the box, by the diagram's direction
		outer      string            // the line's next cell out: "" none
	}{
		{name: "exactly one", card: "||", inner: map[pm.Dir]string{pm.TB: "╪", pm.BT: "╪", pm.LR: "╫", pm.RL: "╫"}},
		{name: "zero or one", card: "o|", inner: map[pm.Dir]string{pm.TB: "┼", pm.BT: "┼", pm.LR: "┼", pm.RL: "┼"}, outer: "○"},
		{name: "one or more", card: "|{", inner: map[pm.Dir]string{pm.TB: "∧", pm.BT: "∨", pm.LR: "<", pm.RL: ">"}, outer: "┼"},
		{name: "zero or more", card: "o{", inner: map[pm.Dir]string{pm.TB: "∧", pm.BT: "∨", pm.LR: "<", pm.RL: ">"}, outer: "○"},
	} {
		for dir, word := range map[pm.Dir]string{pm.TB: "TB", pm.BT: "BT", pm.LR: "LR", pm.RL: "RL"} {
			t.Run(c.name+" "+word, func(t *testing.T) {
				er := parseER(t, "erDiagram\n  direction "+word+"\n  AAA ||--"+c.card+" BBB : x\n")
				g, err := LayER(context.Background(), er, newCells(1, 1))
				if err != nil {
					t.Fatal(err)
				}
				var b Box
				for _, x := range g.Boxes {
					if x.ID == "BBB" {
						b = x
					}
				}
				r := b.Rect
				// the cell before BBB's box along the line, and the next one out, by the ranks' way
				var at, out []cell
				switch dir {
				case pm.TB:
					for x := r.X; x < r.X+r.W; x++ {
						at, out = append(at, cell{x, r.Y - 1}), append(out, cell{x, r.Y - 2})
					}
				case pm.BT:
					for x := r.X; x < r.X+r.W; x++ {
						at, out = append(at, cell{x, r.Y + r.H}), append(out, cell{x, r.Y + r.H + 1})
					}
				case pm.LR:
					for y := r.Y; y < r.Y+r.H; y++ {
						at, out = append(at, cell{r.X - 1, y}), append(out, cell{r.X - 2, y})
					}
				case pm.RL:
					for y := r.Y; y < r.Y+r.H; y++ {
						at, out = append(at, cell{r.X + r.W, y}), append(out, cell{r.X + r.W + 1, y})
					}
				}
				for i := range at {
					if g.marks[at[i]] != "" {
						if got := g.marks[at[i]]; got != c.inner[dir] {
							t.Errorf("beside the box: %q, want %q\n%s", got, c.inner[dir], paintedER(t, g))
						}
						if got := g.marks[out[i]]; got != c.outer {
							t.Errorf("the next cell out: %q, want %q\n%s", got, c.outer, paintedER(t, g))
						}
						return
					}
				}
				t.Errorf("no mark beside BBB %+v:\n%s", r, paintedER(t, g))
			})
		}
	}
}

// An entity's attributes are a table: its type, name, keys and comment each in a column, a column no
// row fills left out, each row trimmed; an entity with none is its name alone.
func TestAnEntitysAttributesAreATable(t *testing.T) {
	er := parseER(t, erCases["er-basic"])
	gr := erGraph(er, newCells(1, 1))
	want := map[string][][]string{
		"CUSTOMER": {{"CUSTOMER"}, {"string  name   PK", "string  email  UK  unique, lower case", "int     age"}},
		"ORDER":    {{"ORDER"}, {"int  id        PK", "int  customer  FK"}},
	}
	for _, n := range gr.nodes {
		if w, ok := want[n.id]; ok {
			if fmt.Sprintf("%q", n.sections) != fmt.Sprintf("%q", w) {
				t.Errorf("%s's sections %q, want %q", n.id, n.sections, w)
			}
		} else if n.sections != nil {
			t.Errorf("%s has no attributes but sections %q", n.id, n.sections)
		}
	}
}

// An identifying relationship is solid, any other dashed.
func TestARelationshipsStrokeSaysIfItIdentifies(t *testing.T) {
	gr := erGraph(parseER(t, "erDiagram\n  A ||--|| B : x\n  A ||..|| C : y\n"), newCells(1, 1))
	if gr.edges[0].stroke != StrokeLight || gr.edges[1].stroke != StrokeDashed {
		t.Errorf("strokes %d and %d, want solid then dashed", gr.edges[0].stroke, gr.edges[1].stroke)
	}
}

// A relationship of an entity with itself is the ↺, its label and cardinalities a footnote: nothing of
// it lost, though a self-loop has no ends to draw them at.
func TestARelationshipWithItselfKeepsItsCardinalities(t *testing.T) {
	g, err := LayER(context.Background(), parseER(t, erCases["er-self"]), newCells(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	drawn := paintedER(t, g)
	if !strings.Contains(drawn, "↺") || !strings.Contains(drawn, "manages 1 to 0..*") {
		t.Errorf("no ↺, or its footnote doesn't say the label and cardinalities:\n%s", drawn)
	}
	// with no label, the cardinalities alone
	g, err = LayER(context.Background(), parseER(t, "erDiagram\n  E ||--o{ E : \"\"\n"), newCells(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	if drawn := paintedER(t, g); !strings.Contains(drawn, "1 to 0..*") {
		t.Errorf("an unlabelled one's footnote doesn't say its cardinalities:\n%s", drawn)
	}
}

// A relationship whose end is no entity is refused, ErrUnsupported, never dropped.
func TestAnERRelationshipToNoEntityIsRefused(t *testing.T) {
	er := &pm.ERDiagram{Dir: pm.TB, Entities: []pm.Entity{{ID: "A", Label: "A"}},
		Relationships: []pm.Relationship{{From: "A", To: "Nowhere", FromCard: pm.ExactlyOne, ToCard: pm.ExactlyOne}}}
	if _, err := LayER(context.Background(), er, newCells(1, 1)); err != ErrUnsupported {
		t.Errorf("a relationship to no entity: %v, want ErrUnsupported", err)
	}
}

// An ER diagram that runs across and is too wide for its room is laid running down (LayWithin); one
// that fits keeps its direction.
func TestAWideERDiagramIsLaidDown(t *testing.T) {
	er := parseER(t, "erDiagram\n  direction LR\n  ALPHA ||--o{ BETA : a\n  BETA ||--o{ GAMMA : b\n  GAMMA ||--o{ DELTA : c\n")
	ctx := context.Background()
	wide, err := Lay(ctx, er, newCells(1, 1))
	if err != nil {
		t.Fatal(err)
	}
	g, err := LayWithin(ctx, er, newCells(1, 1), wide.Size.W-1)
	if err != nil {
		t.Fatal(err)
	}
	if g.Size.W >= wide.Size.W {
		t.Errorf("laid within %d: %d wide, not narrower than across (%d)", wide.Size.W-1, g.Size.W, wide.Size.W)
	}
	if erDown(parseER(t, "erDiagram\n  A ||--|| B : x\n")) != nil {
		t.Error("a diagram already running down has a narrower form")
	}
}
