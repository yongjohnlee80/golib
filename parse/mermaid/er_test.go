package mermaid_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

func er(t *testing.T, src string) *mermaid.ERDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	if d.Kind() != mermaid.ER {
		t.Fatalf("kind %v, want er", d.Kind())
	}
	return d.(*mermaid.ERDiagram)
}

// The documented example: entities in order of mention, every relationship with its two
// cardinalities, the identifying ones solid.
func TestERRelationships(t *testing.T) {
	d := er(t, `erDiagram
    CUSTOMER ||--o{ ORDER : places
    ORDER ||--|{ LINE-ITEM : contains
    CUSTOMER }|..|{ DELIVERY-ADDRESS : uses
    PRODUCT |o--o| CATALOG : "listed in"
    A }o--|| B : ""`)
	var ids []string
	for _, e := range d.Entities {
		ids = append(ids, e.ID)
	}
	if want := []string{"CUSTOMER", "ORDER", "LINE-ITEM", "DELIVERY-ADDRESS", "PRODUCT", "CATALOG", "A", "B"}; !slices.Equal(ids, want) {
		t.Errorf("entities %v, want %v", ids, want)
	}
	want := []mermaid.Relationship{
		{From: "CUSTOMER", To: "ORDER", FromCard: mermaid.ExactlyOne, ToCard: mermaid.ZeroOrMore, Identifying: true, Label: "places"},
		{From: "ORDER", To: "LINE-ITEM", FromCard: mermaid.ExactlyOne, ToCard: mermaid.OneOrMore, Identifying: true, Label: "contains"},
		{From: "CUSTOMER", To: "DELIVERY-ADDRESS", FromCard: mermaid.OneOrMore, ToCard: mermaid.OneOrMore, Label: "uses"},
		{From: "PRODUCT", To: "CATALOG", FromCard: mermaid.ZeroOrOne, ToCard: mermaid.ZeroOrOne, Identifying: true, Label: "listed in"},
		{From: "A", To: "B", FromCard: mermaid.ZeroOrMore, ToCard: mermaid.ExactlyOne, Identifying: true},
	}
	if len(d.Relationships) != len(want) {
		t.Fatalf("%d relationships, want %d", len(d.Relationships), len(want))
	}
	for i, r := range d.Relationships {
		r.Span = [2]int{}
		if r != want[i] {
			t.Errorf("relationship %d: %+v, want %+v", i, r, want[i])
		}
	}
}

// Every symbol reads as its cardinality on either side, and the words read as their symbols.
func TestERCardinalities(t *testing.T) {
	for _, c := range []struct {
		rel        string
		from, to   mermaid.Cardinality
		identifies bool
	}{
		{"||--||", mermaid.ExactlyOne, mermaid.ExactlyOne, true},
		{"|o--o|", mermaid.ZeroOrOne, mermaid.ZeroOrOne, true},
		{"o|..|o", mermaid.ZeroOrOne, mermaid.ZeroOrOne, false},
		{"}|--|{", mermaid.OneOrMore, mermaid.OneOrMore, true},
		{"}o--o{", mermaid.ZeroOrMore, mermaid.ZeroOrMore, true},
		{"o{--}o", mermaid.ZeroOrMore, mermaid.ZeroOrMore, true},
		{"||.-o{", mermaid.ExactlyOne, mermaid.ZeroOrMore, false},
		{"||-.o{", mermaid.ExactlyOne, mermaid.ZeroOrMore, false},
		{"one or more to zero or one", mermaid.OneOrMore, mermaid.ZeroOrOne, true},
		{"only one optionally to zero or many", mermaid.ExactlyOne, mermaid.ZeroOrMore, false},
		{"1 to many(0)", mermaid.ExactlyOne, mermaid.ZeroOrMore, true},
		{"one or zero to many(1)", mermaid.ZeroOrOne, mermaid.OneOrMore, true},
		{"0+ to 1+", mermaid.ZeroOrMore, mermaid.OneOrMore, true},
		{"one or many to zero or more", mermaid.OneOrMore, mermaid.ZeroOrMore, true},
	} {
		d := er(t, "erDiagram\n  X "+c.rel+" Y : r")
		r := d.Relationships[0]
		if r.FromCard != c.from || r.ToCard != c.to || r.Identifying != c.identifies || r.From != "X" || r.To != "Y" {
			t.Errorf("%q: %v %v identifying %v (%s to %s), want %v %v %v", c.rel, r.FromCard, r.ToCard, r.Identifying, r.From, r.To, c.from, c.to, c.identifies)
		}
	}
}

// Attributes: a type and a name, keys in any combination, a comment; an alias; a block on one line.
func TestERAttributes(t *testing.T) {
	src := `erDiagram
    p[Person] {
        string firstName PK "the given name"
        string lastName
        int age UK
        varchar(255) email FK, UK
        string[] tags PK,FK "many"
    }
    "Delivery Address" { string street }
    p ||--o{ "Delivery Address" : lives`
	d := er(t, src)
	p := d.Entities[0]
	if p.ID != "p" || p.Label != "Person" || len(p.Attributes) != 5 {
		t.Fatalf("p: %q %q with %d attributes", p.ID, p.Label, len(p.Attributes))
	}
	want := []mermaid.Attribute{
		{Type: "string", Name: "firstName", Keys: []string{"PK"}, Comment: "the given name"},
		{Type: "string", Name: "lastName"},
		{Type: "int", Name: "age", Keys: []string{"UK"}},
		{Type: "varchar(255)", Name: "email", Keys: []string{"FK", "UK"}},
		{Type: "string[]", Name: "tags", Keys: []string{"PK", "FK"}, Comment: "many"},
	}
	for i, a := range p.Attributes {
		if a.Type != want[i].Type || a.Name != want[i].Name || !slices.Equal(a.Keys, want[i].Keys) || a.Comment != want[i].Comment {
			t.Errorf("attribute %d: %+v, want %+v", i, a, want[i])
		}
		if got := src[a.Span[0]:a.Span[1]]; !strings.Contains(got, a.Name) {
			t.Errorf("attribute %s spans %q", a.Name, got)
		}
	}
	if da := d.Entities[1]; da.ID != "Delivery Address" || len(da.Attributes) != 1 || da.Attributes[0].Name != "street" {
		t.Errorf("the quoted entity: %+v", da)
	}
	if r := d.Relationships[0]; r.From != "p" || r.To != "Delivery Address" {
		t.Errorf("the relationship joins %q and %q", r.From, r.To)
	}
	if got := src[p.Span[0]:p.Span[1]]; got != "p[Person]" {
		t.Errorf("p spans %q", got)
	}
}

// Styles resolve as a flowchart's do: default, then classes (class and :::), then style.
func TestERStyles(t *testing.T) {
	d := er(t, `erDiagram
    direction LR
    classDef default fill:#eee
    classDef hot fill:#f00,stroke:#333
    A:::hot ||--|| B : x
    class B hot
    style B fill:#0f0`)
	if d.Dir != mermaid.LR {
		t.Errorf("direction %v, want LR", d.Dir)
	}
	a, b := d.Entities[0], d.Entities[1]
	if !slices.Equal(a.Classes, []string{"hot"}) || prop(a.Style, "fill") != "#f00" || prop(a.Style, "stroke") != "#333" {
		t.Errorf("A: classes %v, style %v", a.Classes, a.Style)
	}
	if prop(b.Style, "fill") != "#0f0" || prop(b.Style, "stroke") != "#333" {
		t.Errorf("B: style %v", b.Style)
	}
}

func prop(st mermaid.Style, name string) string {
	for _, p := range st {
		if p.Name == name {
			return p.Value
		}
	}
	return ""
}

func TestERSyntaxErrors(t *testing.T) {
	for _, c := range []struct {
		src       string
		line, col int
		msg       string
	}{
		{"erDiagram\n  A ||--o{", 2, 11, "entity name"},
		{"erDiagram\n  A ||-o{ B : x", 2, 7, "a relationship was expected"},
		{"erDiagram\n  A <>--o{ B : x", 2, 5, "a cardinality"},
		{"erDiagram\n  A ||--o{ B x", 2, 14, "after the relationship"},
		{"erDiagram\n  A {\n    string\n  }", 3, 5, "a type and a name"},
		{"erDiagram\n  A {\n    string name PX\n  }", 3, 17, "not a key"},
		{"erDiagram\n  A {\n    string name \"open\n  }", 3, 17, "not closed"},
		{"erDiagram\n  A {\n    string name", 3, 16, "has no }"},
		{"erDiagram\n  }", 2, 3, "without an entity's block"},
		{"erDiagram\n  A[Alias", 2, 4, "alias is not closed"},
		{"erDiagram\n  ||--o{ B : x", 2, 3, "entity name"},
		{"erDiagram\n  direction UP", 2, 13, "unknown direction"},
		{"erDiagram\n  A {\n    9x name\n  }", 3, 5, "not an attribute type"},
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

// What the subset does not take is another renderer's, even beside an error.
func TestERUnsupported(t *testing.T) {
	for _, src := range []string{
		"erDiagram\n  A ||--o{ B : has\n  click A call x()",
		"erDiagram\n  A ||--o{ B : has\n  linkStyle 0 stroke:#ff3",
		"erDiagram\n  @{ x }",
		"erDiagram\n  A { string n \"<b>x</b>\" }",
		"erDiagram\n  A ||--o{\n  click A call x()",
	} {
		if _, err := mermaid.Parse(src); !errors.Is(err, mermaid.ErrUnsupported) {
			t.Errorf("%q: %v, want ErrUnsupported", src, err)
		}
	}
}
