package mermaid_test

import (
	"errors"
	"strings"
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
		`"A: quoted \"title\""`:                  `A: quoted "title"`,
		`'it''s planned'`:                        "it's planned",
		"Plain words # a note":                   "Plain words",
		"Plain#hash":                             "Plain#hash", // a # inside a word is no comment
		`"A # B" # a note`:                       "A # B",      // a # inside the quotes is the title's
		`"Plan" # a note`:                        "Plan",
		`'it''s' # a note`:                       "it's",
		`"tab\tesc\e caf\u00e9 \x41 \U0001F600"`: "tab\tesc\x1b café A 😀", // YAML's escapes, not Go's
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

// A title that is no YAML scalar (a quote left open, an escape YAML doesn't have or a \u or \U one
// that is no Unicode scalar value, text after the closing quote) is a SyntaxError at its line; one
// longer than MaxLabel as written, its quotes and escapes counted and a comment after it not, is
// ErrTooLarge at its line, as a label in the diagram's body is, and one MaxLabel long is read.
func TestAFrontMatterTitleIsAScalarOfALabelsLength(t *testing.T) {
	for _, title := range []string{`"open`, `'open`, `"bad \q escape"`, `"Plan" trailing`, `'Plan' trailing`,
		`"\uD800"`, `"\uDFFF"`, `"\U00110000"`, `"\UFFFFFFFF"`} {
		_, err := mermaid.Parse("---\nconfig:\n  theme: base\ntitle: " + title + "\n---\nflowchart LR\n  A --> B")
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) || se.Line != 4 {
			t.Errorf("title: %s: %v; want a SyntaxError at its line, 4", title, err)
		}
	}
	long := strings.Repeat("x", mermaid.MaxLabel+1)
	_, err := mermaid.Parse("---\ntitle: " + long + "\n---\nflowchart LR\n  A --> B")
	var nd *mermaid.NotDrawnError
	if !errors.As(err, &nd) || !errors.Is(err, mermaid.ErrTooLarge) || nd.Line != 2 {
		t.Errorf("a title of %d bytes: %v; want ErrTooLarge at its line, 2", len(long), err)
	}
	d, err := mermaid.Parse("---\ntitle: " + long[1:] + "\n---\nflowchart LR\n  A --> B")
	if err != nil || len(mermaid.TitleOf(d)) != mermaid.MaxLabel {
		t.Errorf("a title of MaxLabel bytes: %v; want it read", err)
	}
	// a \u escape that is a scalar value is read, U+FFFD's own and the last one's included
	for title, want := range map[string]string{`"\uFFFD"`: "\uFFFD", `"\U0010FFFF"`: "\U0010FFFF", `"\u00e9t\u00e9"`: "été"} {
		d, err := mermaid.Parse("---\ntitle: " + title + "\n---\nflowchart LR\n  A --> B")
		if err != nil || mermaid.TitleOf(d) != want {
			t.Errorf("title: %s: %q, %v; want %q", title, mermaid.TitleOf(d), err, want)
		}
	}
	// the bound is on the title as written: a quoted one's quotes and escapes count, a comment after
	// it doesn't
	x := func(n int) string { return strings.Repeat("x", n) }
	esc := func(n int) string { return strings.Repeat(`\u0041`, n) }
	for _, c := range []struct {
		title string
		read  string // "" for ErrTooLarge
	}{
		{`"` + x(mermaid.MaxLabel-2) + `"`, x(mermaid.MaxLabel - 2)},
		{`"` + x(mermaid.MaxLabel-1) + `"`, ""},
		{`'` + x(mermaid.MaxLabel-2) + `'`, x(mermaid.MaxLabel - 2)},
		{`'` + x(mermaid.MaxLabel-1) + `'`, ""},
		{`"` + esc(170) + "xx" + `"`, strings.Repeat("A", 170) + "xx"}, // 1024 written
		{`"` + esc(170) + "xxx" + `"`, ""},                             // 1025 written, 173 read
		{`"` + esc(200) + `"`, ""},                                     // 1202 written, 200 read
		{`"` + x(mermaid.MaxLabel-2) + `"  # a comment after it`, x(mermaid.MaxLabel - 2)},
		{x(mermaid.MaxLabel) + " # a comment after it", x(mermaid.MaxLabel)},
	} {
		d, err := mermaid.Parse("---\ntitle: " + c.title + "\n---\nflowchart LR\n  A --> B")
		var nd *mermaid.NotDrawnError
		switch {
		case c.read == "" && (!errors.As(err, &nd) || !errors.Is(err, mermaid.ErrTooLarge) || nd.Line != 2):
			t.Errorf("a title of %d bytes as written: %v; want ErrTooLarge at its line, 2", len(c.title), err)
		case c.read != "" && (err != nil || mermaid.TitleOf(d) != c.read):
			t.Errorf("a title of %d bytes as written: %v; want it read as %d bytes", len(c.title), err, len(c.read))
		}
	}
}
