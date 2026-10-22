package decl_test

import (
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// Every type answers the same for the universal properties and method (universal.go), whichever
// hook is asked: ClassifyProperty, Resettable, ReadablesOf and MethodsOf agree with one another and
// across the vocabulary. Before, each hook carried its own if-ladder of the names.
func TestEveryTypeAgreesOnTheUniversalProperties(t *testing.T) {
	_, a := mount(t, listScreen, tuidecl.HostFuncs{},
		func(err error) { t.Errorf("unexpected handler error: %v", err) })

	cases := []struct {
		prop       string
		kind       decl.PropertyKind
		resettable bool
	}{
		{"visible", decl.PropRuntime, true},
		{"toolTipText", decl.PropRuntime, true},
		{"palette.window", decl.PropRuntime, true},
		{"syntax.keyword", decl.PropRuntime, true},
		{"focus", decl.PropConstructorOnly, false},
		// a misspelt role is the palette's, refused by name when applied, but no property
		{"palette.nosuchrole", decl.PropUnknown, false},
	}
	types := a.TypeNames()
	if len(types) == 0 {
		t.Fatal("the adapter names no types")
	}
	for _, typ := range types {
		for _, c := range cases {
			if got := a.ClassifyProperty(typ, c.prop); got != c.kind {
				t.Errorf("ClassifyProperty(%q, %q) = %s, want %s", typ, c.prop, got, c.kind)
			}
			if got := a.Resettable(typ, c.prop); got != c.resettable {
				t.Errorf("Resettable(%q, %q) = %v, want %v", typ, c.prop, got, c.resettable)
			}
		}
		if a.Resettable(typ, "keyScope") {
			t.Errorf("Resettable(%q, keyScope) = true; it is read only", typ)
		}
		if !slices.Contains(a.ReadablesOf(typ), "keyScope") {
			t.Errorf("ReadablesOf(%q) lacks keyScope: %v", typ, a.ReadablesOf(typ))
		}
		if !slices.Contains(a.MethodsOf(typ), "forceActiveFocus") {
			t.Errorf("MethodsOf(%q) lacks forceActiveFocus: %v", typ, a.MethodsOf(typ))
		}
	}
}

// `focus: false` is QML's default, written out, and mounts as `focus: true` does. Before, Create
// took it out of the builder's properties but reported it consumed only when true, so the engine
// applied a false one, and Apply refused it: "has no runtime property focus".
func TestAFalseFocusMounts(t *testing.T) {
	_, err := tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nFlex { Text { text: \"a\"; focus: false } }")),
		tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(10, 2))))
	if err != nil {
		t.Errorf("a Text with focus: false: %v; want mounted", err)
	}
}
