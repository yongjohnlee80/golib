package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// ctor_declared_test.go: a property a builder reads is one its Type declares. A reload decides
// whether a changed property rebuilds the node or calls a setter from the Type's declaration, so a
// property the builder reads and the Type forgot is refused on reload though it mounts.

// A reload may add or change any property the Dialog's builder reads: closeOnQ and the two maximum
// percentages are constructor properties, and a reload rebuilds the dialog with them.
func TestAReloadTakesEveryDialogConstructorProperty(t *testing.T) {
	s := decltest.Run(t, 40, 8, tuidecl.LayoutSource("main.qml", []byte(windowDoc(
		" Dialog { id: dlg; title: \"Late\"; Text { text: \"arrived\" } }"))),
		tuidecl.Singleton("demo", "1.0", "App"))
	s.WaitForText(t, "status")
	for _, prop := range []string{"closeOnQ: true", "maxWidthPercent: 50", "maxHeightPercent: 50"} {
		var err error
		onScreenLoop(t, s, func() {
			_, err = s.Program.Reload([]byte(windowDoc(
				" Dialog { id: dlg; title: \"Late\"; " + prop + "; Text { text: \"arrived\" } }")))
		})
		if err != nil {
			t.Errorf("a reload adding %s: %v", prop, err)
		}
	}
}

// A builder that reads a property its Type does not declare fails the first time it builds, naming
// the type and the property: the drift is caught by any test that mounts it, not first by a reload.
func TestABuilderReadingAnUndeclaredPropertyIsRefused(t *testing.T) {
	meter := tuidecl.Type{
		Name: "Meter",
		Ctor: []string{"label"},
		Build: func(b tuidecl.Build) (tui.Component, []string, error) {
			var label, unit string
			consumed, err := b.ReadProps(map[string]tuidecl.Field{
				"label": tuidecl.StringField(&label),
				"unit":  tuidecl.StringField(&unit),
			})
			return widget.NewText(label), consumed, err
		},
	}
	_, _, _, err := extended(t, "import tui 1.0\nMeter { label: \"m\" }", meter)
	if err == nil || !strings.Contains(err.Error(), "Meter") || !strings.Contains(err.Error(), `"unit"`) {
		t.Fatalf("a builder reading the undeclared unit: %v", err)
	}
}
