package decl

import (
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// style_hooks_internal_test.go: a standard type's getters, methods and destroy hook keep their contract
// on any component. StandardType hands them to a style whose Build may make a different widget, so
// each must answer "not that widget" (an error, or nothing to release) instead of panicking.

// calm runs fn and reports a panic as an error.
func calm(fn func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panicked: %v", r)
		}
	}()
	fn()
	return nil
}

func TestAStandardTypesHooksRefuseAForeignWidgetWithoutPanicking(t *testing.T) {
	var foreign tui.Component = widget.NewText("a style's own widget")
	for _, std := range stdTypes() {
		copied, ok := StandardType(std.Name)
		if !ok {
			t.Fatalf("StandardType(%q) is missing", std.Name)
		}
		for name, get := range copied.Getters {
			var gerr error
			if p := calm(func() { _, gerr = get(foreign) }); p != nil {
				t.Errorf("%s.%s getter on a foreign widget %v", std.Name, name, p)
			} else if gerr == nil {
				t.Errorf("%s.%s getter read a foreign widget without an error", std.Name, name)
			}
		}
		for name, m := range copied.Methods {
			var merr error
			if p := calm(func() { merr = m(foreign, nil) }); p != nil {
				t.Errorf("%s.%s() on a foreign widget %v", std.Name, name, p)
			} else if merr == nil {
				t.Errorf("%s.%s() ran on a foreign widget without an error", std.Name, name)
			}
		}
		if copied.Destroyed != nil {
			if p := calm(func() { copied.Destroyed(foreign) }); p != nil {
				t.Errorf("%s's destroy hook on a foreign widget %v", std.Name, p)
			}
		}
	}
}
