package mermaid

import (
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

const seqExample = `sequenceDiagram
    title Checkout
    autonumber
    actor U as User
    participant W as Web shop
    participant P as Payments
    U->>+W: Place the order
    W->>+P: Charge the card
    loop until settled
        P->>P: Poll the bank
    end
    alt approved
        P-->>-W: Paid
        W-->>U: Order confirmed
    else declined
        P--xW: Refused
        Note right of W: Retry later
    end
    Note over U,W: The order is kept
    W-->>-U: Done`

func overlaps(a, b gui.Rect) bool {
	return a.X < b.X+b.W && b.X < a.X+a.W && a.Y < b.Y+b.H && b.Y < a.Y+a.H
}

// The documented shapes of a sequence diagram lay out: participants top and bottom, apart, each
// message between its participants' lifelines or activations, frames round their steps, numbers
// on the messages, every text painted.
func TestASequenceIsLaidOutAndPainted(t *testing.T) {
	l, err := lay(t, seqExample, 2000)
	if err != nil {
		t.Fatal(err)
	}
	var parts []laidNode
	for _, n := range l.nodes {
		if n.form == formRect && n.label.lines != nil || n.form == formActor {
			parts = append(parts, n)
		}
	}
	if len(parts) != 6 { // three participants, top and bottom
		t.Fatalf("%d participant boxes, want 6", len(parts))
	}
	for i, a := range parts {
		for _, b := range parts[i+1:] {
			if overlaps(a.box, b.box) {
				t.Errorf("%q's box %+v overlaps %q's %+v", a.label.lines, a.box, b.label.lines, b.box)
			}
		}
	}
	msgs, lifelines := 0, 0
	for _, e := range l.edges {
		if e.over {
			msgs++
		} else {
			lifelines++
		}
		if e.label != nil {
			for _, p := range parts {
				if overlaps(e.label.box, p.box) {
					t.Errorf("message text %q sits on participant %q", e.label.lines, p.label.lines)
				}
			}
		}
	}
	if msgs != 7 || lifelines != 3 {
		t.Errorf("%d messages and %d lifelines, want 7 and 3", msgs, lifelines)
	}
	numbers := 0
	for _, n := range l.nodes {
		if n.form == formCircle {
			numbers++
		}
	}
	if numbers != 7 {
		t.Errorf("%d numbers, want one per message", numbers)
	}
	if len(l.group) != 2 {
		t.Fatalf("%d frames, want 2", len(l.group))
	}
	// the loop holds its self message
	loop := l.group[0].box
	for _, e := range l.edges {
		if e.over && len(e.pts) == 4 && !overlaps(loop, gui.Rect{X: e.pts[1].X, Y: e.pts[1].Y, W: 1, H: 1}) {
			t.Errorf("the self message at %v is outside its loop %+v", e.pts, loop)
		}
	}
	if alt := l.group[1]; len(alt.parts) != 2 { // its condition, and else's divider
		t.Errorf("alt's parts %+v", alt.parts)
	}
	if l.Size().W <= 0 || l.Size().H <= 0 || l.fit != 1 {
		t.Errorf("size %+v fit %v", l.Size(), l.fit)
	}

	rc := gui.NewRecordingCanvas(gui.Size{W: 2000, H: 2000}, gui.Size{W: 8, H: 16})
	l.Paint(rc)
	texts := 0
	for _, c := range rc.Calls {
		if c.Op == "DrawText" {
			texts++
		}
	}
	// title; 6 participant labels; 7 message texts; 7 numbers; loop, alt; their conditions; else's;
	// two notes
	if want := 1 + 6 + 7 + 7 + 2 + 2 + 1 + 2; texts != want {
		t.Errorf("%d texts drawn, want %d", texts, want)
	}
}

// A message's text widens the gap between its participants; nothing is drawn left of the margin.
func TestASequenceMakesRoomForItsText(t *testing.T) {
	short, err := lay(t, "sequenceDiagram\n  A->>B: hi", 4000)
	if err != nil {
		t.Fatal(err)
	}
	long, err := lay(t, "sequenceDiagram\n  A->>B: a much longer message than the boxes are wide, on one line", 4000)
	if err != nil {
		t.Fatal(err)
	}
	if long.Size().W <= short.Size().W {
		t.Errorf("the long message's diagram is %v wide, the short one's %v", long.Size().W, short.Size().W)
	}
	left, err := lay(t, "sequenceDiagram\n  Note left of A: a note to the left of the first participant\n  A->>B: hi", 4000)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range left.nodes {
		if n.box.X < 0 {
			t.Errorf("%q drawn at x %v, left of the page", n.label.lines, n.box.X)
		}
	}
}

// Activations stack: a message into a nested activation meets its side, not the lifeline.
func TestActivationsNest(t *testing.T) {
	l, err := lay(t, "sequenceDiagram\n  A->>+B: one\n  A->>+B: two\n  B-->>-A: back\n  B-->>-A: back", 4000)
	if err != nil {
		t.Fatal(err)
	}
	var acts []gui.Rect
	for _, n := range l.nodes {
		if n.form == formRect && n.label.lines == nil {
			acts = append(acts, n.box)
		}
	}
	if len(acts) != 2 || acts[0].X == acts[1].X {
		t.Fatalf("activations %+v: want two, side by side", acts)
	}
	var second []gui.Point
	for _, e := range l.edges {
		if e.over && e.label != nil && e.label.lines[0] == "two" {
			second = e.pts
		}
	}
	inner := acts[0]
	if acts[1].X > acts[0].X {
		inner = acts[1]
	}
	if second == nil || second[1].X != inner.X {
		t.Errorf("the second call ends at %v, want the inner activation's side %v", second, inner.X)
	}
}

// A sequence diagram wider than the width asked is scaled down to it.
func TestASequenceFitsItsWidth(t *testing.T) {
	l, err := lay(t, seqExample, 300)
	if err != nil {
		t.Fatal(err)
	}
	if l.Size().W != 300 || l.fit >= 1 {
		t.Errorf("size %+v fit %v", l.Size(), l.fit)
	}
}
