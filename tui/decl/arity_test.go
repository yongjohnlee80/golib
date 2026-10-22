package decl_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A type states how many children it takes (Type.Children), and the adapter refuses another count
// before its Build runs, naming the type, the count and the declaration's position, with the
// type's note. A consumer's type gets it as a built-in does; WindowMod, whose own check named no
// position, now does.
func TestATypesArityIsCheckedWithItsPosition(t *testing.T) {
	built := 0
	pair := tuidecl.Type{Name: "Pair", Children: tuidecl.Exactly(2).Because("a left and a right"),
		Build: func(b tuidecl.Build) (tui.Component, []string, error) {
			built++
			return widget.NewText("pair"), nil, nil
		}}
	for doc, want := range map[string]string{
		"Pair { Text { } }":                 "Pair needs exactly 2 children, got 1 (at main.qml:2:1); a left and a right",
		"WindowMod { Text { }\n Text { } }": "WindowMod needs exactly 1 child, got 2 (at main.qml:2:1); its content",
		"Flex { ScrollView { } }":           "ScrollView needs exactly 1 child, got 0 (at main.qml:2:8)",
		"Flex { ListView { Text { } } }":    "ListView takes no children, got 1 (at main.qml:2:8); its rows are its model's",
	} {
		_, err := tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\n"+doc)),
			tuidecl.Types(pair), tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(20, 4))))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", doc, err, want)
		}
	}
	if built != 0 {
		t.Errorf("Pair's Build ran %d times for a refused count", built)
	}
}

// An Enum without a scope is refused when its type is registered: on the enum engine an empty scope
// reads as Tui's, and its constants would overwrite Tui's.
func TestAnEnumWithoutAScopeIsRefused(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "an enum needs a scope") {
			t.Errorf("an Enum without a scope: recovered %v, want refused", r)
		}
	}()
	odd := tuidecl.Type{Name: "Odd", Enums: []tuidecl.Enum{{Values: []string{"A"}}},
		Build: func(tuidecl.Build) (tui.Component, []string, error) { return widget.NewText("odd"), nil, nil }}
	_, _ = tuidecl.NewProgram(tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nOdd { }")),
		tuidecl.Types(odd), tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(10, 2))))
}
