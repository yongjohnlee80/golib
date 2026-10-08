package mermaid_test

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

func parseSeq(t *testing.T, src string) *mermaid.SequenceDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	s, ok := d.(*mermaid.SequenceDiagram)
	if !ok || d.Kind() != mermaid.Sequence {
		t.Fatalf("%q parsed as %T", src, d)
	}
	return s
}

// Participants come declared or first mentioned, in that order; an alias is the label; an actor
// is an actor.
func TestSequenceParticipants(t *testing.T) {
	s := parseSeq(t, "sequenceDiagram\n  title: Checkout\n  participant W as Web <br> shop\n  actor U as User\n  U->>W: buy\n  W->>DB: save")
	if s.Title != "Checkout" {
		t.Errorf("title %q", s.Title)
	}
	want := []mermaid.Participant{{ID: "W", Label: "Web \n shop"}, {ID: "U", Label: "User", Actor: true}, {ID: "DB", Label: "DB"}}
	if len(s.Participants) != len(want) {
		t.Fatalf("participants %+v", s.Participants)
	}
	for i, w := range want {
		g := s.Participants[i]
		if g.ID != w.ID || g.Label != w.Label || g.Actor != w.Actor {
			t.Errorf("participant %d: %+v, want %+v", i, g, w)
		}
	}
}

// Every arrow reads as its line and ends, + and - as activation, the text after the colon.
func TestSequenceArrows(t *testing.T) {
	for src, want := range map[string]mermaid.Step{
		"A->B: x":      {Line: mermaid.Solid, Head: mermaid.SeqNone},
		"A-->B: x":     {Line: mermaid.Dotted, Head: mermaid.SeqNone},
		"A->>B: x":     {Line: mermaid.Solid, Head: mermaid.SeqArrow},
		"A-->>B: x":    {Line: mermaid.Dotted, Head: mermaid.SeqArrow},
		"A-xB: x":      {Line: mermaid.Solid, Head: mermaid.SeqCross},
		"A--xB: x":     {Line: mermaid.Dotted, Head: mermaid.SeqCross},
		"A-)B: x":      {Line: mermaid.Solid, Head: mermaid.SeqAsync},
		"A--)B: x":     {Line: mermaid.Dotted, Head: mermaid.SeqAsync},
		"A<<->>B: x":   {Line: mermaid.Solid, Head: mermaid.SeqArrow, Tail: mermaid.SeqArrow},
		"A<<-->>B: x":  {Line: mermaid.Dotted, Head: mermaid.SeqArrow, Tail: mermaid.SeqArrow},
		"A ->>+ B : x": {Line: mermaid.Solid, Head: mermaid.SeqArrow, Activate: true},
	} {
		s := parseSeq(t, "sequenceDiagram\n"+src)
		if len(s.Steps) != 1 {
			t.Fatalf("%q: steps %+v", src, s.Steps)
		}
		g := s.Steps[0]
		if g.Kind != mermaid.StepMessage || g.From != "A" || g.To != "B" || g.Text != "x" || g.Line != want.Line ||
			g.Head != want.Head || g.Tail != want.Tail || g.Activate != want.Activate {
			t.Errorf("%q: %+v", src, g)
		}
	}
	s := parseSeq(t, "sequenceDiagram\nA->>+B: call\nB-->>-A: reply\nA->>A")
	if !s.Steps[1].Deactivate || s.Steps[1].From != "B" || s.Steps[2].To != "A" || s.Steps[2].Text != "" {
		t.Errorf("reply and self message: %+v", s.Steps[1:])
	}
}

// Notes, frames and their sections, activations and numbering are steps, in order.
func TestSequenceSteps(t *testing.T) {
	s := parseSeq(t, `sequenceDiagram
    autonumber 10 5
    Note right of A: thinks
    Note over A,B: both
    loop Every minute
        A->>B: ping
    end
    alt is sick
        B->>A: not so good
    else is well
        B->>A: fine
    end
    rect rgb(200, 150, 255)
        par to B
            A->>B: hi
        and to C
            A->>C: hi
        end
    end
    critical connect
        A->>DB: open
    option timeout
        A->>A: retry
    end
    activate A
    deactivate A
    autonumber off`)
	kinds := []mermaid.StepKind{mermaid.StepNumber, mermaid.StepNote, mermaid.StepNote,
		mermaid.StepBlock, mermaid.StepMessage, mermaid.StepEnd,
		mermaid.StepBlock, mermaid.StepMessage, mermaid.StepBranch, mermaid.StepMessage, mermaid.StepEnd,
		mermaid.StepBlock, mermaid.StepBlock, mermaid.StepMessage, mermaid.StepBranch, mermaid.StepMessage, mermaid.StepEnd, mermaid.StepEnd,
		mermaid.StepBlock, mermaid.StepMessage, mermaid.StepBranch, mermaid.StepMessage, mermaid.StepEnd,
		mermaid.StepActivate, mermaid.StepDeactivate, mermaid.StepNumber}
	if len(s.Steps) != len(kinds) {
		t.Fatalf("%d steps, want %d: %+v", len(s.Steps), len(kinds), s.Steps)
	}
	for i, k := range kinds {
		if s.Steps[i].Kind != k {
			t.Errorf("step %d: %v, want %v", i, s.Steps[i].Kind, k)
		}
	}
	if n := s.Steps[0]; n.Start != 10 || n.Increment != 5 || n.Off {
		t.Errorf("autonumber %+v", n)
	}
	if !s.Steps[len(s.Steps)-1].Off {
		t.Error("autonumber off is not off")
	}
	if n := s.Steps[2]; n.Place != mermaid.Over || n.From != "A" || n.To != "B" || n.Text != "both" {
		t.Errorf("note over two: %+v", n)
	}
	if n := s.Steps[1]; n.Place != mermaid.RightOf || n.From != "A" || n.To != "A" {
		t.Errorf("note right of: %+v", n)
	}
	if b := s.Steps[6]; b.Block != mermaid.Alt || b.Text != "is sick" || s.Steps[8].Text != "is well" {
		t.Errorf("alt: %+v / %+v", b, s.Steps[8])
	}
	if r := s.Steps[11]; r.Block != mermaid.RectBlock || r.Text != "rgb(200, 150, 255)" {
		t.Errorf("rect: %+v", r)
	}
	if got := s.Steps[0].Span; s.Steps[0].Kind == mermaid.StepNumber && (got[0] <= 0 || got[1] <= got[0]) {
		t.Errorf("span %v", got)
	}
}

// What is wrong in the grammar is a SyntaxError at its line; what golib does not draw is
// ErrUnsupported.
func TestSequenceErrors(t *testing.T) {
	for src, line := range map[string]int{
		"sequenceDiagram\n  loop x\n  A->>B: y":     2,
		"sequenceDiagram\n  end":                    2,
		"sequenceDiagram\n  opt x\n  else y\n  end": 3,
		"sequenceDiagram\n  B-->>-A: reply":         2,
		"sequenceDiagram\n  deactivate A":           2,
		"sequenceDiagram\n  Note above A: x":        2,
		"sequenceDiagram\n  Note left of A,B: x":    2,
		"sequenceDiagram\n  A->>: x":                2,
		"sequenceDiagram\n  A-=B: x":                2,
		"sequenceDiagram\n  autonumber one":         2,
	} {
		_, err := mermaid.Parse(src)
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) || se.Line != line {
			t.Errorf("%q: %v, want a SyntaxError on line %d", src, err, line)
		}
	}
	for _, src := range []string{
		"sequenceDiagram\n  create participant C\n  A->>C: hi",
		"sequenceDiagram\n  A->>B: hi\n  destroy B",
		"sequenceDiagram\n  participant A@{ \"type\": \"boundary\" }",
		"sequenceDiagram\n  link A: Dashboard @ https://example.com",
		"sequenceDiagram\n  A->>B: <b>bold</b>",
		"sequenceDiagram\n  just words",
	} {
		if _, err := mermaid.Parse(src); !errors.Is(err, mermaid.ErrUnsupported) {
			t.Errorf("%q: %v, want ErrUnsupported", src, err)
		}
	}
}
