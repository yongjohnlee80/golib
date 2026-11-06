package mermaid_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

// Mermaid's documented sankeys parse as their goldens (testdata/sankey, -update writes them): the
// nodes in order of first mention, and each flow from its source to its target with its value. A
// quoted field keeps its commas, and "" in it is a quote.
func TestSankeyCorpus(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "sankey", "*.mmd"))
	if err != nil || len(files) < 2 {
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
		if d.Kind() != mermaid.Sankey {
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

func sankeyParse(t *testing.T, src string) *mermaid.SankeyDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	s, ok := d.(*mermaid.SankeyDiagram)
	if !ok {
		t.Fatalf("%q parsed as %T", src, d)
	}
	return s
}

// A quoted name keeps its commas and its doubled quotes; a node mentioned again is the same node; a
// flow's span is its line.
func TestSankeyFieldsAndNodes(t *testing.T) {
	src := "sankey-beta\n\nA,\"B, the second\",1.5\n\"A\",\"say \"\"hi\"\"\",2\nB2,A,3\n"
	s := sankeyParse(t, src)
	if want := []string{"A", "B, the second", `say "hi"`, "B2"}; strings.Join(s.Nodes, "|") != strings.Join(want, "|") {
		t.Errorf("nodes %q, want %q", s.Nodes, want)
	}
	if len(s.Links) != 3 || s.Links[1].Source != 0 || s.Links[1].Target != 2 || s.Links[1].Value != 2 {
		t.Fatalf("links %+v", s.Links)
	}
	if got := src[s.Links[0].Span[0]:s.Links[0].Span[1]]; got != `A,"B, the second",1.5` {
		t.Errorf("link 0's span is %q", got)
	}
}

// A node's flows in and out, what passes through it, and its depth: a source 0, any other one more
// than the deepest node flowing into it.
func TestSankeyThroughAndDepths(t *testing.T) {
	s := sankeyParse(t, "sankey-beta\na,b,2\na,c,3\nb,c,1\nc,d,4\n")
	if s.Out(0) != 5 || s.In(2) != 4 || s.Through(2) != 4 || s.Through(1) != 2 {
		t.Errorf("out(a) %v, in(c) %v, through(c) %v, through(b) %v", s.Out(0), s.In(2), s.Through(2), s.Through(1))
	}
	if got := s.Depths(); len(got) != 4 || got[0] != 0 || got[1] != 1 || got[2] != 2 || got[3] != 3 {
		t.Errorf("depths %v, want [0 1 2 3]: c is after b, which is after a", got)
	}
}

// What a sankey refuses is a syntax error at its line: a value that isn't a number, isn't finite or
// isn't above 0, a line of other than three fields, an empty name, an open quote, a node flowing into
// itself, a flow that closes a cycle, and anything after the header.
func TestSankeyRefusals(t *testing.T) {
	for src, want := range map[string]string{
		"sankey-beta\na,b,x\n":               `a sankey line's value "x" is not a number`,
		"sankey-beta\na,b,Inf\n":             `a sankey line's value "Inf" is not finite`,
		"sankey-beta\na,b,0\n":               `a sankey line's value "0" is not above 0`,
		"sankey-beta\na,b,-2\n":              `a sankey line's value "-2" is not above 0`,
		"sankey-beta\na,b\n":                 "a sankey line wants three fields, source,target,value: it has 2",
		"sankey-beta\na,b,1,2\n":             "it has 4",
		"sankey-beta\n,b,1\n":                "one is empty",
		"sankey-beta\n\"a,b,1\n":             "a quoted field is not closed",
		"sankey-beta\na,a,1\n":               `"a" flows into itself`,
		"sankey-beta\na,b,1\nb,c,1\nc,a,1\n": `"c" to "a" closes a cycle`,
		"sankey-beta extra\na,b,1\n":         `unexpected "extra" after sankey`,
	} {
		_, err := mermaid.Parse(src)
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) || !strings.Contains(se.Msg, want) {
			t.Errorf("%q: %v; want a syntax error saying %q", src, err, want)
		}
	}
}
