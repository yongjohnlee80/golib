package mermaid_test

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// TestCorpus parses Mermaid's documented flowchart examples, each against its golden model.
func TestCorpus(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "flowchart", "*.mmd"))
	if err != nil || len(files) < 20 {
		t.Fatalf("the corpus: %d files, %v", len(files), err)
	}
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		d, err := mermaid.Parse(string(src))
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if d.Kind() != mermaid.Flowchart {
			t.Errorf("%s: kind %v", f, d.Kind())
		}
		got, err := json.MarshalIndent(d, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		golden := strings.TrimSuffix(f, ".mmd") + ".json"
		if *update {
			if err := os.WriteFile(golden, append(got, '\n'), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%v (run with -update to write it)", err)
		}
		if string(want) != string(got)+"\n" {
			t.Errorf("%s differs from its golden:\n%s", f, got)
		}
	}
}

func flowchart(t *testing.T, src string) *mermaid.FlowchartDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return d.(*mermaid.FlowchartDiagram)
}

// Spans slice the source to the mention that defined each node.
func TestSpans(t *testing.T) {
	src := "flowchart LR\n  id1[Box] --> id2((Round))\n  id1 --> id3\n"
	fc := flowchart(t, src)
	want := map[string]string{"id1": "id1[Box]", "id2": "id2((Round))", "id3": "id3"}
	for _, n := range fc.Nodes {
		if got := src[n.Span[0]:n.Span[1]]; got != want[n.ID] {
			t.Errorf("%s spans %q, want %q", n.ID, got, want[n.ID])
		}
	}
	if e := fc.Edges[0]; !strings.Contains(src[e.Span[0]:e.Span[1]], "-->") {
		t.Errorf("edge span %q", src[e.Span[0]:e.Span[1]])
	}
}

func TestSyntaxErrors(t *testing.T) {
	for _, c := range []struct {
		src       string
		line, col int
		msg       string
	}{
		{"", 1, 1, "empty"},
		{"flowchart XY\n  A", 1, 11, "unknown direction"},
		{"flowchart LR\n  A -->", 2, 8, "expected a node id"},
		{"flowchart LR\n  A[open", 2, 5, "not closed"},
		{"flowchart LR\n  A[\"open]", 2, 5, "quoted label is not closed"},
		{"flowchart LR\n  A[\"q\"x]", 2, 8, "expected"},
		{"flowchart LR\n  subgraph one\n  A", 3, 4, "has no end"},
		{"flowchart LR\n  end", 2, 3, "end without a subgraph"},
		{"flowchart LR\n  subgraph a\n  end x", 3, 7, "after end"},
		{"flowchart LR\n  A - B", 2, 5, "expected a link"},
		{"flowchart LR\n  A <-- B", 2, 8, "no closing link"},
		{"flowchart LR\n  A <--- B", 2, 5, "needs one at its end"},
		{"flowchart LR\n  A -- text B", 2, 7, "no closing link"},
		{"flowchart LR\n  A ~~ B", 2, 5, "~~~"},
		{"flowchart LR\n  A o~~~ B", 2, 5, "expected a link"},
		{"flowchart LR\n  A -- t -->|u| B", 2, 13, "not both"},
		{"flowchart LR\n  A -->|open B", 2, 8, "not closed"},
		{"flowchart LR\n  A:::", 2, 7, "class name"},
		{"flowchart LR\n  classDef x fill", 2, 14, "name:value"},
		{"flowchart LR\n  classDef x", 2, 13, "no style properties"},
		{"flowchart LR\n  class A", 2, 9, "one class name"},
		{"flowchart LR\n  direction UP", 2, 13, "unknown direction"},
		{"flowchart LR\n  subgraph", 2, 11, "wants an id"},
		{"flowchart LR\n  subgraph a [open\n  end", 2, 12, "not closed"},
		{"flowchart LR\n  subgraph a\n  end\n  subgraph a\n  end", 4, 12, "defined twice"},
		{"flowchart LR\n  -->", 2, 3, "expected a node id"},
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

func TestUnsupported(t *testing.T) {
	for _, src := range []string{
		"sequenceDiagram\n  box Aqua Team\n  participant A\n  end",
		"stateDiagram-v2\n  state A {\n  a\n  --\n  b\n  }",
		"classDiagram\n  A <|-- B\n  click A call x()",
		"pie\n  \"a\": 1",
		"gantt\n  excludes weekends\n  a :2026-01-01, 3d",
		"flowchart-elk TD\n  A-->B",
		"---\ntitle: x\n---\nflowchart LR\n  A-->B",
		"flowchart LR\n  A-->B\n  click A callback",
		"flowchart LR\n  A-->B\n  linkStyle 0 stroke:#ff3",
		"flowchart LR\n  A@{ shape: rect }",
		"flowchart LR\n  A e1@--> B",
		"flowchart LR\n  A[fa:fa-twitter for peace]",
		"flowchart LR\n  A[<b>bold</b>]",
		"flowchart LR\n  subgraph one\n  a\n  end\n  b --> one",
		"flowchart LR\n  accDescr {\n  long\n  }",
		// Unsupported anywhere wins over a syntax error elsewhere.
		"flowchart LR\n  A -->\n  click A call x()",
	} {
		if _, err := mermaid.Parse(src); !errors.Is(err, mermaid.ErrUnsupported) {
			t.Errorf("%q: %v, want ErrUnsupported", src, err)
		}
	}
}

func TestTooLarge(t *testing.T) {
	big := "flowchart LR\n" + strings.Repeat("A-->B\n", mermaid.MaxSource/6+1)
	if _, err := mermaid.Parse(big); !errors.Is(err, mermaid.ErrTooLarge) {
		t.Errorf("a %d-byte source: %v, want ErrTooLarge", len(big), err)
	}
	long := "flowchart LR\n  A[" + strings.Repeat("x", mermaid.MaxLabel+1) + "]"
	if _, err := mermaid.Parse(long); !errors.Is(err, mermaid.ErrTooLarge) {
		t.Errorf("a long label: %v, want ErrTooLarge", err)
	}
}

func TestNames(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{mermaid.Flowchart.String(), "flowchart"},
		{mermaid.ER.String(), "er"},
		{mermaid.Kind(99).String(), "99"},
		{mermaid.Dir(0).String(), "0"},
		{mermaid.LR.String(), "LR"},
		{mermaid.TrapezoidAlt.String(), "trapezoid-alt"},
		{mermaid.Invisible.String(), "invisible"},
		{mermaid.Cross.String(), "cross"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
	se := &mermaid.SyntaxError{Line: 3, Col: 4, Msg: "bad"}
	if se.Error() != "mermaid: line 3, column 4: bad" {
		t.Errorf("SyntaxError text %q", se.Error())
	}
}

// TestNotDrawnSaysItsLine: every ErrUnsupported and ErrTooLarge is a *NotDrawnError that says what
// and on which line (0: the type, or the whole source), still errors.Is its sentinel, and keeps the
// text it always had; <br> in a label is drawn.
func TestNotDrawnSaysItsLine(t *testing.T) {
	long := strings.Repeat("x", mermaid.MaxLabel+1)
	for _, c := range []struct {
		src        string
		sentinel   error
		line       int
		what, text string
	}{
		{"flowchart LR\n  A-->B\n  C[<job id>]", mermaid.ErrUnsupported, 3, "HTML in a label", "mermaid: not drawn natively: HTML in a label (line 3)"},
		{"flowchart LR\n  A[fa:fa-twitter for peace]", mermaid.ErrUnsupported, 2, "an icon", "mermaid: not drawn natively: an icon (line 2)"},
		// unsupported anywhere wins over a syntax error before it, at its own line
		{"flowchart LR\n  A -->\n  click A call x()", mermaid.ErrUnsupported, 3, "click", "mermaid: not drawn natively: click (line 3)"},
		{"gitGraph\n  commit", mermaid.ErrUnsupported, 0, `"gitGraph" diagrams`, `mermaid: not drawn natively: "gitGraph" diagrams`},
		{"---\ntitle: x\n---\nflowchart LR\n  A-->B", mermaid.ErrUnsupported, 1, "front matter", "mermaid: not drawn natively: front matter (line 1)"},
		{"flowchart LR\n  A-->B\n  A[" + long + "]", mermaid.ErrTooLarge, 3, "a label of 1025 bytes, more than 1024",
			"mermaid: over a limit: a label of 1025 bytes on line 3, more than 1024"},
	} {
		_, err := mermaid.Parse(c.src)
		var nd *mermaid.NotDrawnError
		if !errors.As(err, &nd) || !errors.Is(err, c.sentinel) {
			t.Errorf("%.40q: %v (%T), want a *NotDrawnError that is %v", c.src, err, err, c.sentinel)
			continue
		}
		if nd.Line != c.line || nd.What != c.what || err.Error() != c.text {
			t.Errorf("%.40q: line %d, what %q, text %q; want line %d, %q, %q", c.src, nd.Line, nd.What, err.Error(), c.line, c.what, c.text)
		}
	}
	big := "flowchart LR\n" + strings.Repeat("A-->B\n", mermaid.MaxSource/6+1)
	var nd *mermaid.NotDrawnError
	if _, err := mermaid.Parse(big); !errors.As(err, &nd) || nd.Line != 0 || !errors.Is(err, mermaid.ErrTooLarge) {
		t.Errorf("a %d-byte source: %v, want a *NotDrawnError of no line that is ErrTooLarge", len(big), err)
	}
	if _, err := mermaid.Parse("flowchart LR\n  A[one<br/>two] --> B[three<br>four]"); err != nil {
		t.Errorf("<br> in a label: %v, want drawn", err)
	}
}
