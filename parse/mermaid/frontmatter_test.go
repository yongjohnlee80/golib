package mermaid_test

import (
	"errors"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

// front is a front matter block: title t, and a config block with nested keys, which are passed
// over.
func front(t string) string {
	return "---\ntitle: " + t + "\nconfig:\n  theme: forest\n  flowchart:\n    curve: basis\n---\n"
}

// Every kind reads a front matter block before its type line, after blank and %% lines, and takes
// its title; config and its nested keys are passed over.
func TestEveryKindTakesItsFrontMattersTitle(t *testing.T) {
	for name, body := range map[string]string{
		"flowchart":   "flowchart LR\n  A --> B",
		"sequence":    "sequenceDiagram\n  A->>B: hi",
		"class":       "classDiagram\nclass A { +f() int }\nA <|-- B",
		"state":       "stateDiagram-v2\n[*] --> S\nS --> [*]",
		"er":          "erDiagram\nA ||--o{ B : has",
		"gantt":       "gantt\ndateFormat YYYY-MM-DD\nsection S\nTask :a1, 2026-01-01, 3d",
		"pie":         "pie\n  \"Dogs\" : 3\n  \"Cats\" : 2",
		"mindmap":     "mindmap\n  root\n    leaf",
		"timeline":    "timeline\n  2004 : one : two",
		"journey":     "journey\nsection S\nA task: 5: Me",
		"sankey":      "sankey-beta\n\nA,B,10\nB,C,4\n",
		"gitGraph":    "gitGraph\n  commit\n  branch dev\n  commit",
		"block":       "block-beta\n  a b",
		"requirement": "requirementDiagram\n",
	} {
		src := "\n%% a comment\n" + front("Weekly plan") + body
		d, err := mermaid.Parse(src)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got := mermaid.TitleOf(d); got != "Weekly plan" {
			t.Errorf("%s: title %q; want the front matter's", name, got)
		}
	}
}

// A quoted title is unquoted (double quotes with their escapes, single quotes with ” for a quote),
// a plain one ends at a comment; a title the diagram's own body names wins over its front matter's.
func TestAFrontMatterTitlesForms(t *testing.T) {
	for title, want := range map[string]string{
		`"A: quoted \"title\""`: `A: quoted "title"`,
		`'it''s planned'`:       "it's planned",
		"Plain words # a note":  "Plain words",
	} {
		d, err := mermaid.Parse(front(title) + "flowchart LR\n  A --> B")
		if err != nil || mermaid.TitleOf(d) != want {
			t.Errorf("title: %s: %q, %v; want %q", title, mermaid.TitleOf(d), err, want)
		}
	}
	d, err := mermaid.Parse(front("Front") + "pie title Body\n  \"A\" : 1")
	if err != nil || mermaid.TitleOf(d) != "Body" {
		t.Errorf("a pie titled in its body: %q, %v; want its body's title to win", mermaid.TitleOf(d), err)
	}
}

// A front matter block with no closing --- is a SyntaxError at its opening line; an error in the
// diagram after a front matter block keeps its own line.
func TestAFrontMattersErrorsKeepTheirLines(t *testing.T) {
	_, err := mermaid.Parse("\n---\ntitle: x\nflowchart LR\n  A --> B")
	var se *mermaid.SyntaxError
	if !errors.As(err, &se) || se.Line != 2 {
		t.Fatalf("an unclosed front matter block: %v; want a SyntaxError at line 2", err)
	}
	_, err = mermaid.Parse(front("x") + "flowchart LR\n  A[open --> B")
	if !errors.As(err, &se) || se.Line != 9 {
		t.Fatalf("a malformed flowchart after front matter: %v; want its own line, 9", err)
	}
}
