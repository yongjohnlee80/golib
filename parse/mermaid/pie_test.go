package mermaid_test

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

func pieParse(t *testing.T, src string) *mermaid.PieDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	p, ok := d.(*mermaid.PieDiagram)
	if !ok || d.Kind() != mermaid.Pie {
		t.Fatalf("%q parsed as %T", src, d)
	}
	return p
}

// Mermaid's documented example: the title on the header line, each slice its quoted label and its
// value, in order, each with the span of its line.
func TestPieTheDocumentedExample(t *testing.T) {
	p := pieParse(t, `pie title NETFLIX
    "Time spent looking for movie" : 70
    "Time spent watching it" : 30`)
	if p.Title != "NETFLIX" {
		t.Errorf("title %q", p.Title)
	}
	if p.ShowData {
		t.Error("showData without showData in the header")
	}
	want := []mermaid.PieSlice{{Label: "Time spent looking for movie", Value: 70}, {Label: "Time spent watching it", Value: 30}}
	if len(p.Slices) != len(want) {
		t.Fatalf("slices %+v", p.Slices)
	}
	for i, w := range want {
		g := p.Slices[i]
		if g.Label != w.Label || g.Value != w.Value {
			t.Errorf("slice %d: %+v, want %+v", i, g, w)
		}
		if g.Span[0] >= g.Span[1] {
			t.Errorf("slice %d: span %+v", i, g.Span)
		}
	}
}

// showData on the header sets the flag; a title statement in the body sets the title after it, as
// Mermaid's own directive does.
func TestPieShowDataAndItsTitle(t *testing.T) {
	p := pieParse(t, `pie showData
    title The split
    "a" : 0.25
    "b" : 1e2`)
	if !p.ShowData {
		t.Error("showData")
	}
	if p.Title != "The split" {
		t.Errorf("title %q", p.Title)
	}
	if len(p.Slices) != 2 || p.Slices[0].Value != 0.25 || p.Slices[1].Value != 100 {
		t.Fatalf("slices %+v", p.Slices)
	}
}

// A pie with nothing in it is an empty pie: a title, perhaps, and no slices.
func TestPieEmptyAcceptsTheTrivial(t *testing.T) {
	p := pieParse(t, "pie")
	if p.Title != "" || len(p.Slices) != 0 {
		t.Errorf("an empty pie: %+v", p)
	}
	p = pieParse(t, "pie\n  %% a comment\n  accTitle: t\n  accDescr: d\n")
	if len(p.Slices) != 0 {
		t.Errorf("slices %+v", p.Slices)
	}
	p = pieParse(t, `pie
    "" : 0
    "zero" : 0
    "same label" : 1
    "same label" : 2`)
	if len(p.Slices) != 4 || p.Slices[0].Label != "" || p.Slices[1].Value != 0 || p.Slices[2].Label != "same label" {
		t.Fatalf("slices %+v", p.Slices)
	}
}

// What is wrong in the pie's grammar is a SyntaxError at its line; what is outside the subset is
// ErrUnsupported.
func TestPieErrors(t *testing.T) {
	for src, line := range map[string]int{
		"pie sideways\n  \"a\": 1":    1,
		"pie showData junk":           1,
		"pie\n  a: 1":                 2, // an unquoted label
		"pie\n  \"a 1":                2, // not closed
		"pie\n  \"a\" 1":              2, // no colon
		"pie\n  \"a\" :":              2, // no value
		"pie\n  \"a\" : x":            2, // not a number
		"pie\n  \"a\" : NaN":          2,
		"pie\n  \"a\" : -Inf":         2,
		"pie\n  \"a\" : -1":           2, // negative
		"pie\n  \"a\": 1\n  \"b\": x": 3,
		"pie\n  accTitle\n  \"a\": 1": 0,
	} {
		_, err := mermaid.Parse(src)
		var se *mermaid.SyntaxError
		switch {
		case line > 0 && !errors.As(err, &se):
			t.Errorf("%q: %v, want a SyntaxError", src, err)
		case line > 0 && se.Line != line:
			t.Errorf("%q: %v, want line %d", src, err, line)
		case line == 0 && !errors.Is(err, mermaid.ErrUnsupported):
			t.Errorf("%q: %v, want ErrUnsupported", src, err)
		}
	}
}

// A slice's label follows the label rules of every other kind: HTML is outside the subset, a
// label past the limit is too large, and both name their line.
func TestPieLabelsFollowTheLabelRules(t *testing.T) {
	if _, err := mermaid.Parse("pie\n  \"a <b>x</b>\": 1"); !errors.Is(err, mermaid.ErrUnsupported) {
		t.Errorf("HTML in a slice label: %v, want ErrUnsupported", err)
	}
	big := "pie\n  \"" + strings.Repeat("a", mermaid.MaxLabel+1) + "\": 1"
	if _, err := mermaid.Parse(big); !errors.Is(err, mermaid.ErrTooLarge) {
		t.Errorf("a slice label over the limit: %v, want ErrTooLarge", err)
	}
	p := pieParse(t, "pie\n  \"#quot;q#9829;#quot; and a<br/>break\": 1")
	if p.Slices[0].Label != "\"q\u2665\" and a\nbreak" {
		t.Errorf("label %q", p.Slices[0].Label)
	}
	if math.IsNaN(p.Slices[0].Value) || math.IsInf(p.Slices[0].Value, 0) {
		t.Errorf("value %v", p.Slices[0].Value)
	}
}

// Every slice of the documented example parses to the same model twice over: the parse is stable.
func TestPieParsesTwiceAlike(t *testing.T) {
	src := "pie title T\n  \"a\": 2\n  \"b\": 6"
	a, b := pieParse(t, src), pieParse(t, src)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("twice: %+v and %+v", a, b)
	}
}
