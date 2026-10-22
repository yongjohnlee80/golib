package decl_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// clock is a consumer's non-visual declaration: a timer, never on screen.
type clock struct{ widget.Base }

func (*clock) Layout(c tui.Constraints) tui.Size { return c.Constrain(tui.Size{}) }
func (*clock) Render(tui.Surface)                {}
func (*clock) HandleEvent(tui.Event) bool        { return false }
func (*clock) DeclarationOnly()                  {}

// A consumer's types can say what the built-ins say. A container's Adopt reads each child's
// attached properties of a schema it honours: its Build calls it for the children it is built
// with, as Flex's does, and the adapter calls it for a child a reload inserts. A non-visual
// declaration, marked DeclarationOnly, is refused by forceActiveFocus as having nothing on screen.
func TestAConsumersTypeCanAdoptAndBeDeclarationOnly(t *testing.T) {
	var sides []string
	adopt := func(parent, child tui.Component, attached map[string]qml.SpecValue) error {
		sides = append(sides, attached["Slot.side"].Raw)
		parent.(*tui.Flex).Add(child)
		return nil
	}
	shelf := tuidecl.Type{Name: "Shelf", Adopt: adopt,
		Build: func(b tuidecl.Build) (tui.Component, []string, error) {
			f := tui.NewFlex(tui.Vertical)
			for i, c := range b.Children {
				if err := adopt(f, c, b.ChildAttached[i]); err != nil {
					return nil, nil, err
				}
			}
			return f, nil, nil
		}}
	clockType := tuidecl.Type{Name: "Clock", Children: tuidecl.NoChildren(),
		Build: func(tuidecl.Build) (tui.Component, []string, error) { return &clock{}, nil, nil }}
	reg := tuidecl.StdRegistry()
	tuidecl.RegisterAttached(reg, "Slot", "side")
	tuidecl.Honour(reg, "Shelf", "Slot")
	a := tuidecl.New(reg, append(tuidecl.StdProperties(), tuidecl.WithTypes(shelf, clockType),
		tuidecl.WithErrorSink(func(error) {}))...)
	tr := decl.New(a)
	parse := func(src string) qml.SpecTree {
		spec, err := qml.QML{File: "main.qml"}.Parse([]byte("import tui 1.0\n" + src))
		if err != nil {
			t.Fatal(err)
		}
		return spec
	}
	if err := tr.Mount(parse(`Shelf { Clock { id: tick }
 Text { Slot.side: "left"; text: "a" } }`)); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sides, "left") {
		t.Fatalf("Adopt saw %q at construction, want the child's Slot.side, left", sides)
	}
	if _, err := tr.Reconcile(parse(`Shelf { Clock { id: tick }
 Text { Slot.side: "left"; text: "a" }
 Text { Slot.side: "right"; text: "b" } }`)); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(sides, "right") {
		t.Errorf("Adopt saw %q after a reload inserted a child, want right among them", sides)
	}
	id, _ := tr.NodeByID("tick")
	if err := a.Invoke(id, "forceActiveFocus", nil); err == nil || !strings.Contains(err.Error(), "nothing on screen to focus") {
		t.Errorf("forceActiveFocus on a DeclarationOnly Clock: %v, want refused as nothing on screen", err)
	}
}
