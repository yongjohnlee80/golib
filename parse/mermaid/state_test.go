package mermaid_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

func stateDiagram(t *testing.T, src string) *mermaid.StateDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	sd, ok := d.(*mermaid.StateDiagram)
	if !ok || d.Kind() != mermaid.State {
		t.Fatalf("%q parsed as %T", src, d)
	}
	return sd
}

func stateByID(t *testing.T, d *mermaid.StateDiagram, id string) mermaid.StateNode {
	t.Helper()
	for _, s := range d.States {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no state %q in %+v", id, d.States)
	return mermaid.StateNode{}
}

// The documented examples parse, both headers.
func TestStateDocumentedExamples(t *testing.T) {
	for _, src := range []string{
		"stateDiagram-v2\n    [*] --> Still\n    Still --> [*]\n\n    Still --> Moving\n    Moving --> Still\n    Moving --> Crash\n    Crash --> [*]",
		"stateDiagram\n    [*] --> Still\n    Still --> [*]\n    Still --> Moving\n    Moving --> Crash\n    Crash --> [*]",
		"stateDiagram-v2\n    stateId",
		"stateDiagram-v2\n    state \"This is a state description\" as s2",
		"stateDiagram-v2\n    s2 : This is a state description",
		"stateDiagram-v2\n    s1 --> s2: A transition",
		"stateDiagram-v2\n    [*] --> First\n    state First {\n        [*] --> second\n        second --> [*]\n    }",
		"stateDiagram-v2\n    [*] --> First\n\n    state First {\n        [*] --> Second\n\n        state Second {\n            [*] --> second\n            second --> Third\n\n            state Third {\n                [*] --> third\n                third --> [*]\n            }\n        }\n    }",
		"stateDiagram-v2\n    state if_state <<choice>>\n    [*] --> IsPositive\n    IsPositive --> if_state\n    if_state --> False: if n < 0\n    if_state --> True : if n >= 0",
		"stateDiagram-v2\n    state fork_state <<fork>>\n      [*] --> fork_state\n      fork_state --> State2\n      fork_state --> State3\n\n      state join_state <<join>>\n      State2 --> join_state\n      State3 --> join_state\n      join_state --> State4\n      State4 --> [*]",
		"stateDiagram-v2\n        State1: The state with a note\n        note right of State1\n            Important information! You can write\n            notes.\n        end note\n        State1 --> State2\n        note left of State2 : This is the note to the left.",
		"stateDiagram\n    direction LR\n    [*] --> A\n    A --> B\n    B --> C\n    state B {\n      direction LR\n      a --> b\n    }\n    B --> D",
		"stateDiagram-v2\n    [*] --> Still\n    Still --> [*]\n%% this is a comment\n    Still --> Moving\n    Moving --> Still %% another comment\n",
		"stateDiagram\n   direction TB\n\n   accTitle: This is the accessible title\n   accDescr: This is an accessible description\n\n   classDef notMoving fill:white\n   classDef movement font-style:italic\n   classDef badBadEvent fill:#f00,color:white,font-weight:bold,stroke-width:2,stroke:yellow\n\n   [*]--> Still\n   Still --> [*]\n   Still --> Moving\n   Moving --> Still\n   Moving --> Crash\n   Crash --> [*]\n\n   class Still notMoving\n   class Moving, Crash movement\n   class Crash badBadEvent\n   class end badBadEvent",
		"stateDiagram\n   [*] --> Still:::notMoving\n   Still --> [*]\n   Still --> Moving:::movement\n   Moving --> Still\n   Moving --> Crash:::movement\n   Crash:::badBadEvent --> [*]\n   classDef notMoving fill:white\n   classDef movement font-style:italic;\n   classDef badBadEvent fill:#f00,color:white,font-weight:bold,stroke-width:2,stroke:yellow",
	} {
		if _, err := mermaid.Parse(src); err != nil {
			// one documented example writes a comment after a statement, which Mermaid reads as
			// part of its state's id; only that one may fail, as a syntax error
			var se *mermaid.SyntaxError
			if strings.Contains(src, "Still %% another") && errors.As(err, &se) {
				continue
			}
			t.Errorf("%q: %v", src, err)
		}
	}
}

// Each scope has its own start and end; a transition from [*] leaves the start, one to it
// reaches the end; states belong to the scope they are first written in.
func TestStateScopesStartsAndEnds(t *testing.T) {
	d := stateDiagram(t, "stateDiagram-v2\n  [*] --> First\n  state First {\n    direction LR\n    [*] --> second\n    second --> [*]\n  }\n  First --> [*]")
	want := map[string]struct {
		kind      mermaid.StateKind
		parent    string
		composite bool
	}{
		"[*]start":       {mermaid.StateStart, "", false},
		"First":          {mermaid.StateNormal, "", true},
		"[*]start@First": {mermaid.StateStart, "First", false},
		"second":         {mermaid.StateNormal, "First", false},
		"[*]end@First":   {mermaid.StateEnd, "First", false},
		"[*]end":         {mermaid.StateEnd, "", false},
	}
	if len(d.States) != len(want) {
		t.Fatalf("%d states, want %d: %+v", len(d.States), len(want), d.States)
	}
	for id, w := range want {
		s := stateByID(t, d, id)
		if s.Kind != w.kind || s.Parent != w.parent || s.Composite != w.composite {
			t.Errorf("%s: %v in %q composite %v; want %v in %q composite %v", id, s.Kind, s.Parent, s.Composite, w.kind, w.parent, w.composite)
		}
	}
	if f := stateByID(t, d, "First"); f.Dir != mermaid.LR || f.Span[1] <= f.Span[0] {
		t.Errorf("First's direction %v, span %v", f.Dir, f.Span)
	}
	if len(d.Transitions) != 4 || d.Transitions[1].From != "[*]start@First" || d.Transitions[2].To != "[*]end@First" {
		t.Errorf("transitions %+v", d.Transitions)
	}
}

// Names, descriptions, labels, pseudo-states and notes.
func TestStateLabelsKindsAndNotes(t *testing.T) {
	d := stateDiagram(t, `stateDiagram-v2
  state "Waiting for input" as wait
  wait : idle<br>until a key
  wait : a second line
  state pick <<choice>>
  state split <<fork>>
  state merge <<join>>
  wait --> pick : key #amp; more
  note right of wait
    first line
    second line
  end note
  note left of pick : choose`)
	w := stateByID(t, d, "wait")
	if w.Label != "Waiting for input" || len(w.Descriptions) != 2 || w.Descriptions[0] != "idle\nuntil a key" {
		t.Errorf("wait: %q %q", w.Label, w.Descriptions)
	}
	for id, k := range map[string]mermaid.StateKind{"pick": mermaid.StateChoice, "split": mermaid.StateFork, "merge": mermaid.StateJoin} {
		if s := stateByID(t, d, id); s.Kind != k {
			t.Errorf("%s is %v, want %v", id, s.Kind, k)
		}
	}
	if len(d.Transitions) != 1 || d.Transitions[0].Label != "key & more" {
		t.Errorf("transitions %+v", d.Transitions)
	}
	if len(d.Notes) != 2 || d.Notes[0].State != "wait" || d.Notes[0].Side != mermaid.RightOf || d.Notes[0].Text != "first line\nsecond line" ||
		d.Notes[1].Side != mermaid.LeftOf || d.Notes[1].Text != "choose" {
		t.Errorf("notes %+v", d.Notes)
	}
}

// classDef default, class, ::: and style resolve onto states, later winning.
func TestStateStyles(t *testing.T) {
	d := stateDiagram(t, "stateDiagram-v2\n  classDef default stroke:#111\n  classDef hot fill:#f00\n  A:::hot --> B\n  class B hot\n  style B fill:#0f0")
	a, b := stateByID(t, d, "A"), stateByID(t, d, "B")
	if len(a.Classes) != 1 || len(a.Style) == 0 {
		t.Errorf("A: %v %v", a.Classes, a.Style)
	}
	get := func(st mermaid.Style, name string) string {
		v := ""
		for _, p := range st {
			if p.Name == name {
				v = p.Value
			}
		}
		return v
	}
	if get(a.Style, "fill") != "#f00" || get(a.Style, "stroke") != "#111" {
		t.Errorf("A's style %v", a.Style)
	}
	if get(b.Style, "fill") != "#0f0" {
		t.Errorf("B's style %v: style wins over its class", b.Style)
	}
}

// What golib does not draw is declined, whatever else is in the diagram.
func TestStateDeclined(t *testing.T) {
	for _, src := range []string{
		"stateDiagram-v2\n  state Active {\n    a --> b\n    --\n    c --> d\n  }",
		"stateDiagram-v2\n  hide empty description\n  A --> B",
		"stateDiagram-v2\n  A --> B\n  click A callback",
		"stateDiagram-v2\n  state First {\n    a\n  }\n  state Second {\n    b\n  }\n  a --> b",
		"stateDiagram-v2\n  state First {\n    a --> First\n  }",
		"stateDiagram-v2\n  A --> B\n  note \"floating\" as N",
		"stateDiagram-v2\n  A --> B : <b>bold</b>",
	} {
		if _, err := mermaid.Parse(src); !errors.Is(err, mermaid.ErrUnsupported) {
			t.Errorf("%q: %v, want ErrUnsupported", src, err)
		}
	}
}

// Malformed state diagrams are syntax errors, where they go wrong.
func TestStateSyntaxErrors(t *testing.T) {
	for _, c := range []struct {
		src       string
		line, col int
		msg       string
	}{
		{"stateDiagram-v2\n  A -->", 2, 8, "wants a state"},
		{"stateDiagram-v2\n  state A {\n  a", 3, 4, "no closing }"},
		{"stateDiagram-v2\n  }", 2, 3, "without a composite"},
		{"stateDiagram-v2\n  state X <<loop>>", 2, 11, "unknown state type"},
		{"stateDiagram-v2\n  note right of A\n  text", 2, 3, "no end note"},
		{"stateDiagram-v2\n  note above A : x", 2, 8, "left of"},
		{"stateDiagram-v2\n  direction up", 2, 13, "unknown direction"},
		{"stateDiagram-v2\n  state \"open as A", 2, 9, "not closed"},
		{"stateDiagram-v2\n  [*]", 2, 3, "start or an end"},
		{"stateDiagram-v2\n  A B", 2, 5, "unexpected"},
		{"stateDiagram-v2\n  state X <<fork>> {", 2, 9, "has no body"},
	} {
		_, err := mermaid.Parse(c.src)
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("%q: %v, want a SyntaxError", c.src, err)
			continue
		}
		if se.Line != c.line || se.Col != c.col || !strings.Contains(se.Msg, c.msg) {
			t.Errorf("%q: %v, want line %d column %d with %q", c.src, se, c.line, c.col, c.msg)
		}
	}
}
