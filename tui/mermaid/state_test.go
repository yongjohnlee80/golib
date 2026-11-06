package mermaid

import (
	"context"
	"errors"
	"strings"
	"testing"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
)

const stateBasicSrc = `stateDiagram-v2
  [*] --> Still
  Still --> Moving : push
  Moving --> Still : stop
  Moving --> Crash
  Crash --> [*]
`

const stateCompositeSrc = `stateDiagram-v2
  [*] --> Idle
  Idle --> Done
  Done --> [*]
  state Busy {
    [*] --> Slow
    Slow --> Fast : speed up
    Fast --> [*]
  }
`

const stateFormsSrc = `stateDiagram-v2
  state fork_state <<fork>>
  state join_state <<join>>
  state check <<choice>>
  [*] --> fork_state
  fork_state --> A
  fork_state --> B
  A --> join_state
  B --> join_state
  join_state --> check
  check --> Big : n > 9
  check --> Small : n ≤ 9
  Big : a large one
  Big : kept for later
  note right of Small : a small one
`

// A state diagram draws in cells as its goldens: states rounded, ● a start, ◉ an end, each
// transition a line into an arrowhead with its label; a composite a frame round its states, its
// name on its border; a fork or join a bar, a choice the diamond's outline, a state's descriptions
// in a section under its name, a note a box tied to its state by a dashed line.
func TestAStateDiagramDrawsInCells(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"state-basic", stateBasicSrc},
		{"state-composite", stateCompositeSrc},
		{"state-forms", stateFormsSrc},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, got := paint(t, c.src)
			golden(t, c.name, got)
		})
	}
}

// Every transition is drawn, none left out: each has its route (the links layGraph gives it), or is
// a self-loop (its ↺); each state but a composite is a box with its ID (a host maps a selection
// back), the composite a frame with its name; a start and an end each paint one glyph, on one row.
// Counting arrowheads would not do: a fan-in shares its target's port, and one arrowhead.
func TestEveryStateTransitionIsDrawn(t *testing.T) {
	for _, src := range []string{stateBasicSrc, stateCompositeSrc, stateFormsSrc} {
		sd := mustParse(t, src).(*pm.StateDiagram)
		gr := stateGraph(sd)
		_, routes, err := layGraph(context.Background(), gr, newCells(1, 1))
		if err != nil {
			t.Fatalf("layGraph: %v", err)
		}
		for i, tr := range sd.Transitions { // the graph's first edges, in order
			if e := gr.edges[i]; len(routes[i]) == 0 && e.from != e.to {
				t.Errorf("%s --> %s: no route", tr.From, tr.To)
			}
		}
		g, out := paint(t, src)
		rows := strings.Split(out, "\n")
		ids := map[string]bool{}
		for _, b := range g.Boxes {
			ids[b.ID] = true
		}
		for _, s := range sd.States {
			if s.Composite == ids[s.ID] {
				t.Errorf("state %s (composite %v): a box %v; want a box for every state but a composite", s.ID, s.Composite, ids[s.ID])
			}
		}
		for _, b := range g.Boxes {
			if b.Form != FormDot && b.Form != FormEndDot {
				continue
			}
			glyph := map[Form]string{FormDot: "●", FormEndDot: "◉"}[b.Form]
			row := []rune(rows[b.Rect.Y] + strings.Repeat(" ", b.Rect.X+b.Rect.W))
			if n := strings.Count(string(row[b.Rect.X:b.Rect.X+b.Rect.W]), glyph); b.Rect.H != 1 || n != 1 {
				t.Errorf("a start or end %+v paints %d %s; want one, on one row", b.Rect, n, glyph)
			}
		}
	}
	_, out := paint(t, stateCompositeSrc)
	if !strings.Contains(out, "╭─ Busy ") {
		t.Errorf("the composite's name isn't on its frame's border:\n%s", out)
	}
}

// A self-transition is the router's ↺ beside its box, and its label isn't lost: it is the layer's
// footnote.
func TestAStateSelfTransitionKeepsItsLabel(t *testing.T) {
	g, out := paint(t, "stateDiagram-v2\n  [*] --> Wait\n  Wait --> Wait : retry\n  Wait --> [*]\n")
	if !strings.Contains(out, "↺") {
		t.Errorf("no ↺ for Wait --> Wait:\n%s", out)
	}
	var kept bool
	for _, n := range g.notes {
		kept = kept || n.label == "retry"
	}
	if !kept && !strings.Contains(out, "retry") {
		t.Errorf("the self-transition's label is lost:\n%s", out)
	}
}

// What a transition to or from a composite can't be drawn as, a line ending on the border, is
// ErrUnsupported (Drawn and Lay), for the host's own rendering, never a picture with it left out:
// one from a composite to itself, one to a composite holding no state. (One between a composite and
// a state inside it, which never crosses the border, the parser refuses already: a state used
// across composite states.) A transition into a composite or out of it is drawn.
func TestOnlyATransitionThatCantEndOnACompositesBorderIsRefused(t *testing.T) {
	for _, src := range []string{
		"stateDiagram-v2\n  state Busy {\n    [*] --> Slow\n  }\n  Busy --> Busy\n",
		"stateDiagram-v2\n  [*] --> Empty\n  state Empty {\n  }\n",
	} {
		d, err := pm.Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := Drawn(d); !errors.Is(err, ErrUnsupported) {
			t.Errorf("Drawn(%q): %v; want ErrUnsupported", src, err)
		}
		if _, err := Lay(context.Background(), d, newCells(1, 1)); !errors.Is(err, ErrUnsupported) {
			t.Errorf("Lay(%q): %v; want ErrUnsupported", src, err)
		}
	}
	for _, src := range []string{
		"stateDiagram-v2\n  [*] --> Busy\n  state Busy {\n    [*] --> Slow\n  }\n",
		"stateDiagram-v2\n  state Busy {\n    [*] --> Slow\n  }\n  Busy --> Done\n",
		stateCompositeSrc,
	} {
		d := mustParse(t, src)
		if err := Drawn(d); err != nil {
			t.Errorf("Drawn(%q): %v; want it drawn", src, err)
		}
		if _, err := Lay(context.Background(), d, newCells(1, 1)); err != nil {
			t.Errorf("Lay(%q): %v; want it laid", src, err)
		}
	}
}

// A note is a box with an ID of its own (never a state's), tied to its state by a dashed line with
// no ends.
func TestAStateNoteIsTiedByADashedLine(t *testing.T) {
	gr := stateGraph(mustParse(t, stateFormsSrc).(*pm.StateDiagram))
	var tie *gedge
	for i, e := range gr.edges {
		if e.from >= 0 && strings.HasPrefix(gr.nodes[e.from].id, "note:") {
			tie = &gr.edges[i]
		}
	}
	if tie == nil {
		t.Fatalf("no note node tied to a state: %+v", gr.edges)
	}
	if tie.stroke != StrokeDashed || tie.head != EndNone || tie.tail != EndNone || gr.nodes[tie.to].id != "Small" {
		t.Errorf("the note's tie %+v; want a dashed line with no ends to Small", *tie)
	}
}

// A diagram that runs across is laid down when it doesn't fit (its narrower form, as a flowchart's).
func TestAStateDiagramAcrossHasANarrowerForm(t *testing.T) {
	d := mustParse(t, "stateDiagram-v2\n  direction LR\n  [*] --> A\n  A --> B\n  B --> [*]\n")
	down, ok := stateDown(d).(*pm.StateDiagram)
	if !ok || down.Dir != pm.TB || d.(*pm.StateDiagram).Dir != pm.LR {
		t.Fatalf("stateDown: %+v; want a TB copy, the original kept LR", down)
	}
	if stateDown(down) != nil {
		t.Error("a TB diagram has a narrower form")
	}
}

func mustParse(t *testing.T, src string) pm.Diagram {
	t.Helper()
	d, err := pm.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return d
}
