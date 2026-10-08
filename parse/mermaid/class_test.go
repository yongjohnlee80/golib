package mermaid_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/mermaid"
)

func class(t *testing.T, src string) *mermaid.ClassDiagram {
	t.Helper()
	d, err := mermaid.Parse(src)
	if err != nil {
		t.Fatalf("%q: %v", src, err)
	}
	cd, ok := d.(*mermaid.ClassDiagram)
	if !ok || d.Kind() != mermaid.Class {
		t.Fatalf("%q: a %T, kind %v", src, d, d.Kind())
	}
	return cd
}

func classByID(t *testing.T, d *mermaid.ClassDiagram, id string) mermaid.ClassBox {
	t.Helper()
	for _, c := range d.Classes {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no class %q in %+v", id, d.Classes)
	return mermaid.ClassBox{}
}

// Mermaid's documented example: relations, members by "Name : member" and in { }, and an
// annotation written in a block.
func TestTheDocumentedAnimalExample(t *testing.T) {
	d := class(t, `classDiagram
    note "From Duck till Zebra"
    Animal <|-- Duck
    note for Duck "can fly\ncan swim\ncan dive\ncan help in debugging"
    Animal <|-- Fish
    Animal <|-- Zebra
    Animal : +int age
    Animal : +String gender
    Animal: +isMammal()
    Animal: +mate()
    class Duck{
        +String beakColor
        +swim()
        +quack()
    }
    class Fish{
        -int sizeInFeet
        -canEat()
    }
    class Zebra{
        +bool is_wild
        +run()
    }`)
	if got := len(d.Classes); got != 4 {
		t.Fatalf("%d classes, want 4: %+v", got, d.Classes)
	}
	a := classByID(t, d, "Animal")
	if !reflect.DeepEqual(a.Attributes, []string{"+int age", "+String gender"}) || !reflect.DeepEqual(a.Methods, []string{"+isMammal()", "+mate()"}) {
		t.Errorf("Animal's members %q %q", a.Attributes, a.Methods)
	}
	duck := classByID(t, d, "Duck")
	if !reflect.DeepEqual(duck.Attributes, []string{"+String beakColor"}) || !reflect.DeepEqual(duck.Methods, []string{"+swim()", "+quack()"}) {
		t.Errorf("Duck's members %q %q", duck.Attributes, duck.Methods)
	}
	if len(d.Relations) != 3 {
		t.Fatalf("%d relations", len(d.Relations))
	}
	r := d.Relations[0]
	if r.From != "Animal" || r.To != "Duck" || r.FromEnd != mermaid.RelInheritance || r.ToEnd != mermaid.RelNone || r.Line != mermaid.Solid {
		t.Errorf("Animal <|-- Duck read as %+v", r)
	}
	if len(d.Notes) != 2 || d.Notes[0].For != "" || d.Notes[1].For != "Duck" || d.Notes[1].Text != "can fly\ncan swim\ncan dive\ncan help in debugging" {
		t.Errorf("notes %+v", d.Notes)
	}
}

// Every arrow, both ways and two-sided, with cardinalities and labels.
func TestEveryRelation(t *testing.T) {
	cases := []struct {
		src            string
		from, to       mermaid.RelEnd
		line           mermaid.LineKind
		fromCard, card string
		label          string
	}{
		{"A <|-- B", mermaid.RelInheritance, mermaid.RelNone, mermaid.Solid, "", "", ""},
		{"A *-- B", mermaid.RelComposition, mermaid.RelNone, mermaid.Solid, "", "", ""},
		{"A o-- B", mermaid.RelAggregation, mermaid.RelNone, mermaid.Solid, "", "", ""},
		{"A --> B", mermaid.RelNone, mermaid.RelAssociation, mermaid.Solid, "", "", ""},
		{"A -- B", mermaid.RelNone, mermaid.RelNone, mermaid.Solid, "", "", ""},
		{"A ..> B", mermaid.RelNone, mermaid.RelAssociation, mermaid.Dotted, "", "", ""},
		{"A ..|> B", mermaid.RelNone, mermaid.RelInheritance, mermaid.Dotted, "", "", ""},
		{"A .. B", mermaid.RelNone, mermaid.RelNone, mermaid.Dotted, "", "", ""},
		{"A --|> B", mermaid.RelNone, mermaid.RelInheritance, mermaid.Solid, "", "", ""},
		{"A --* B", mermaid.RelNone, mermaid.RelComposition, mermaid.Solid, "", "", ""},
		{"A --o B", mermaid.RelNone, mermaid.RelAggregation, mermaid.Solid, "", "", ""},
		{"A <-- B", mermaid.RelAssociation, mermaid.RelNone, mermaid.Solid, "", "", ""},
		{"A <|--|> B", mermaid.RelInheritance, mermaid.RelInheritance, mermaid.Solid, "", "", ""},
		{"A *--o B", mermaid.RelComposition, mermaid.RelAggregation, mermaid.Solid, "", "", ""},
		{`Customer "1" --> "*" Ticket`, mermaid.RelNone, mermaid.RelAssociation, mermaid.Solid, "1", "*", ""},
		{`Galaxy "1" o-- "many" Star : contains`, mermaid.RelAggregation, mermaid.RelNone, mermaid.Solid, "1", "many", "contains"},
		{"A <|-- B : implements", mermaid.RelInheritance, mermaid.RelNone, mermaid.Solid, "", "", "implements"},
		{"A<|--B", mermaid.RelInheritance, mermaid.RelNone, mermaid.Solid, "", "", ""},
	}
	for _, c := range cases {
		d := class(t, "classDiagram\n  "+c.src)
		if len(d.Relations) != 1 {
			t.Errorf("%q: %d relations", c.src, len(d.Relations))
			continue
		}
		r := d.Relations[0]
		got := struct {
			from, to       mermaid.RelEnd
			line           mermaid.LineKind
			fromCard, card string
			label          string
		}{r.FromEnd, r.ToEnd, r.Line, r.FromCard, r.ToCard, r.Label}
		want := struct {
			from, to       mermaid.RelEnd
			line           mermaid.LineKind
			fromCard, card string
			label          string
		}{c.from, c.to, c.line, c.fromCard, c.card, c.label}
		if got != want || r.From == "" || r.To == "" || r.From == r.To {
			t.Errorf("%q: read %+v (%s → %s), want %+v", c.src, got, r.From, r.To, want)
		}
	}
}

// Generics, labels, annotations three ways, namespaces and css classes.
func TestNamesAnnotationsAndNamespaces(t *testing.T) {
	d := class(t, `classDiagram
    class Square~Shape~{
        int id
        List~int~ position
        setPoints(List~int~ points)
        getPoints() List~int~
    }
    Square : -List~string~ messages
    Square : +getDistanceMatrix() List~List~int~~
    class Animal["Animal with a label"]
    class Shape
    <<interface>> Shape
    class Color{
        <<enumeration>>
        RED
        BLUE
    }
    class Thing <<abstract>>
    namespace BaseShapes {
        class Triangle
        class Rectangle {
          double width
        }
    }
    class Duck:::hot
    classDef hot fill:#f96
    cssClass "Shape,Color" cool
    classDef cool stroke:#00f
    style Animal fill:#fff,stroke:#333`)
	sq := classByID(t, d, "Square")
	if sq.Label != "Square<Shape>" {
		t.Errorf("Square drawn as %q", sq.Label)
	}
	if !reflect.DeepEqual(sq.Attributes, []string{"int id", "List<int> position", "-List<string> messages"}) {
		t.Errorf("Square's attributes %q", sq.Attributes)
	}
	if !reflect.DeepEqual(sq.Methods, []string{"setPoints(List<int> points)", "getPoints() List<int>", "+getDistanceMatrix() List<List<int>>"}) {
		t.Errorf("Square's methods %q", sq.Methods)
	}
	if a := classByID(t, d, "Animal"); a.Label != "Animal with a label" || a.Style == nil {
		t.Errorf("Animal %+v", a)
	}
	if s := classByID(t, d, "Shape"); !reflect.DeepEqual(s.Annotations, []string{"interface"}) || !reflect.DeepEqual(s.Classes, []string{"cool"}) {
		t.Errorf("Shape %+v", s)
	}
	if c := classByID(t, d, "Color"); !reflect.DeepEqual(c.Annotations, []string{"enumeration"}) || !reflect.DeepEqual(c.Attributes, []string{"RED", "BLUE"}) {
		t.Errorf("Color %+v", c)
	}
	if c := classByID(t, d, "Thing"); !reflect.DeepEqual(c.Annotations, []string{"abstract"}) {
		t.Errorf("Thing %+v", c)
	}
	if len(d.Namespaces) != 1 || d.Namespaces[0].Name != "BaseShapes" {
		t.Errorf("namespaces %+v", d.Namespaces)
	}
	for _, id := range []string{"Triangle", "Rectangle"} {
		if c := classByID(t, d, id); c.Namespace != "BaseShapes" {
			t.Errorf("%s in namespace %q", id, c.Namespace)
		}
	}
	if c := classByID(t, d, "Duck"); c.Namespace != "" || !reflect.DeepEqual(c.Classes, []string{"hot"}) || len(c.Style) != 1 || c.Style[0].Value != "#f96" {
		t.Errorf("Duck %+v", c)
	}
}

func TestClassDirectionAndSpans(t *testing.T) {
	src := "classDiagram\n  direction RL\n  class A\n  A --> B"
	d := class(t, src)
	if d.Dir != mermaid.RL {
		t.Errorf("direction %v", d.Dir)
	}
	a, r := classByID(t, d, "A"), d.Relations[0]
	if got := src[a.Span[0]:a.Span[1]]; got != "class A" {
		t.Errorf("A's span %q", got)
	}
	if got := src[r.Span[0]:r.Span[1]]; got != "A --> B" {
		t.Errorf("the relation's span %q", got)
	}
}

func TestClassSyntaxErrors(t *testing.T) {
	for _, c := range []struct {
		src, msg string
		line     int
	}{
		{"classDiagram\n  class A {\n  +x", "no closing }", 3},
		{"classDiagram\n  }", "closes nothing", 2},
		{"classDiagram\n  A <|-- ", "class after its arrow", 2},
		{"classDiagram\n  <|-- B", "class before its arrow", 2},
		{"classDiagram\n  direction XY", "unknown direction", 2},
		{"classDiagram\n  class A[\"x\"", "not closed", 2},
		{"classDiagram\n  note for A unquoted", "quoted", 2},
		{"classDiagram\n  namespace N {\n  class A", "namespace", 3},
		{"classDiagram\n  A :", "empty", 2},
	} {
		_, err := mermaid.Parse(c.src)
		var se *mermaid.SyntaxError
		if !errors.As(err, &se) || !strings.Contains(se.Msg, c.msg) || se.Line != c.line {
			t.Errorf("%q: %v, want a SyntaxError on line %d with %q", c.src, err, c.line, c.msg)
		}
	}
}

func TestClassUnsupported(t *testing.T) {
	for _, src := range []string{
		"classDiagram\n  A --> B\n  click A call cb()",
		"classDiagram\n  callback A \"cb\"",
		"classDiagram\n  link A \"https://x\"",
		"classDiagram\n  A ()-- B",
		"classDiagram\n  namespace N {\n  namespace M {\n  }\n  }",
		"classDiagram\n  just some words",
		"classDiagram\n  class A[<b>x</b>]",
	} {
		if _, err := mermaid.Parse(src); !errors.Is(err, mermaid.ErrUnsupported) {
			t.Errorf("%q: %v, want ErrUnsupported", src, err)
		}
	}
}
