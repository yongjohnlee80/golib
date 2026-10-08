package mermaid

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

const stateSrc = `stateDiagram-v2
  [*] --> Idle
  state "Waiting for input" as Idle
  Idle : listens<br>for keys
  Idle --> Busy : key
  state Busy {
    [*] --> Parse
    Parse --> Check
    state Check <<choice>>
    Check --> Run : ok
    Check --> [*] : bad
    state Run {
      direction LR
      [*] --> Split
      state Split <<fork>>
      Split --> A
      Split --> B
      state Join <<join>>
      A --> Join
      B --> Join
      Join --> [*]
    }
  }
  Busy --> Idle
  Busy --> [*]
  note right of Idle : back here<br>when done
  note left of Busy
    does the work
  end note`

func stateInside(r, outer gui.Rect) bool {
	const e = 0.01
	return r.X >= outer.X-e && r.Y >= outer.Y-e && r.X+r.W <= outer.X+outer.W+e && r.Y+r.H <= outer.Y+outer.H+e
}

func stateOverlap(a, b gui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

func stateGroupTitled(t *testing.T, l *Laid, title string) laidGroup {
	t.Helper()
	for _, g := range l.group {
		if len(g.title.lines) == 1 && g.title.lines[0] == title {
			return g
		}
	}
	t.Fatalf("no group titled %q", title)
	return laidGroup{}
}

func stateNodeLabelled(t *testing.T, l *Laid, text string) laidNode {
	t.Helper()
	for _, n := range l.nodes {
		if len(n.label.lines) > 0 && n.label.lines[0] == text {
			return n
		}
	}
	t.Fatalf("no box labelled %q", text)
	return laidNode{}
}

// A state diagram lays out with no box over another, each composite's box holding its body (and a
// nested composite's), and the pseudo-states drawn as their forms.
func TestAStateDiagramIsLaidOut(t *testing.T) {
	l, err := lay(t, stateSrc, 4000)
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range l.nodes {
		for _, b := range l.nodes[i+1:] {
			if stateOverlap(a.box, b.box) {
				t.Errorf("%q's box %+v overlaps %q's %+v", a.label.lines, a.box, b.label.lines, b.box)
			}
		}
	}
	busy, run := stateGroupTitled(t, l, "Busy"), stateGroupTitled(t, l, "Run")
	if !stateInside(run.box, busy.box) {
		t.Errorf("Run %+v is not inside Busy %+v", run.box, busy.box)
	}
	for _, name := range []string{"Parse", "A", "B"} {
		if n := stateNodeLabelled(t, l, name); !stateInside(n.box, busy.box) {
			t.Errorf("%s %+v is not inside Busy %+v", name, n.box, busy.box)
		}
	}
	for _, name := range []string{"A", "B"} {
		if n := stateNodeLabelled(t, l, name); !stateInside(n.box, run.box) {
			t.Errorf("%s %+v is not inside Run %+v", name, n.box, run.box)
		}
	}
	if n := stateNodeLabelled(t, l, "Waiting for input"); stateOverlap(n.box, busy.box) || len(n.parts) != 1 || len(n.parts[0].labels) != 1 {
		t.Errorf("Idle %+v (parts %d) against Busy %+v", n.box, len(n.parts), busy.box)
	}
	forms := map[form]int{}
	for _, n := range l.nodes {
		forms[n.form]++
	}
	// a start in each of the three scopes, an end in each, a fork and a join, a choice, two notes
	want := map[form]int{formStart: 3, formEnd: 3, formBar: 2, formDiamond: 1, formNote: 2, formRound: 4}
	for f, n := range want {
		if forms[f] != n {
			t.Errorf("%d boxes of form %d, want %d (all: %v)", forms[f], f, n, forms)
		}
	}
	// the transitions to and from Busy end at its box
	var toBusy int
	for _, e := range l.edges {
		last := e.pts[len(e.pts)-1]
		if e.head == endArrow && stateInside(gui.Rect{X: last.X, Y: last.Y}, gui.Rect{X: busy.box.X - 1, Y: busy.box.Y - 1, W: busy.box.W + 2, H: busy.box.H + 2}) &&
			!stateInside(gui.Rect{X: last.X, Y: last.Y}, gui.Rect{X: busy.box.X + 1, Y: busy.box.Y + 1, W: busy.box.W - 2, H: busy.box.H - 2}) {
			toBusy++
		}
	}
	if toBusy != 1 {
		t.Errorf("%d transitions end on Busy's border, want 1 (Idle --> Busy)", toBusy)
	}
}

// Every name, description, transition label, composite title and note is drawn.
func TestAStateDiagramPaintsEveryText(t *testing.T) {
	l, err := lay(t, stateSrc, 4000)
	if err != nil {
		t.Fatal(err)
	}
	rc := gui.NewRecordingCanvas(gui.Size{W: 4000, H: 4000}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	texts := 0
	for _, c := range rc.Calls {
		if c.Op == "DrawText" {
			texts++
		}
	}
	// names: Waiting for input, Parse, A, B (4); Idle's description, two lines (2); labels: key,
	// ok, bad (3); titles: Busy, Run (2); notes: two lines and one (3)
	if texts != 14 {
		t.Errorf("%d texts drawn, want 14", texts)
	}
}

// A state diagram wider than the width asked is scaled to it.
func TestAStateDiagramFitsItsWidth(t *testing.T) {
	l, err := lay(t, "stateDiagram-v2\n  direction LR\n  [*] --> A\n  A --> B\n  B --> C\n  C --> D\n  D --> E\n  E --> [*]", 160)
	if err != nil {
		t.Fatal(err)
	}
	if l.Size().W != 160 || l.fit >= 1 {
		t.Errorf("size %+v fit %v: want 160 wide, scaled down", l.Size(), l.fit)
	}
}

// An empty composite and a state with only a note still lay out.
func TestStateEdgeCases(t *testing.T) {
	for _, src := range []string{
		"stateDiagram-v2\n  state Empty {\n  }\n  [*] --> Empty",
		"stateDiagram-v2\n  A\n  note left of A : alone",
		"stateDiagram-v2\n  A --> A : again",
	} {
		l, err := lay(t, src, 2000)
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if s := l.Size(); s.W <= 0 || s.H <= 0 {
			t.Errorf("%q: size %+v", src, s)
		}
	}
}
