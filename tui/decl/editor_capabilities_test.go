package decl_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// styledEditor is a style's own editor widget: not a *widget.Editor, but with every view capability
// one has (here through the Editor it wraps).
type styledEditor struct{ *widget.Editor }

// A style's Editor, a copy of the standard type whose Build makes its own widget, takes the view
// properties as the standard one does: they set capabilities (widget.LineNumberer, ViewChooser and
// the rest), not a *widget.Editor. Before, each asserted *widget.Editor and refused the style's
// ("not an Editor"), so a style re-declared all eight (gui/decl's editorSetters).
func TestAStylesEditorTakesTheViewProperties(t *testing.T) {
	ed, ok := tuidecl.StandardType("Editor")
	if !ok {
		t.Fatal("no standard Editor")
	}
	ed.Name = "StyledEditor"
	ed.Enums = nil // the standard Editor publishes Editor.Rendered; a second scope of one name is refused
	var made *styledEditor
	ed.Build = func(b tuidecl.Build) (tui.Component, []string, error) {
		made = &styledEditor{widget.NewEditor()}
		return made, nil, nil
	}
	s := decltest.Run(t, 40, 4, tuidecl.LayoutSource("main.qml", []byte(`import tui 1.0
StyledEditor { lineNumbers: true; lineNumberColor: "red"; ruler: 30; wrap: true; cursorColor: "blue"
    contextMenu: true; renderedEnabled: false; view: Editor.Rendered }`)),
		tuidecl.Types(ed))
	var view widget.EditorView
	var built bool
	onScreenLoop(t, s, func() {
		if built = made != nil; built {
			view = made.View()
		}
	})
	if !built || view != widget.ViewRendered {
		t.Errorf("the style's Editor (built %v) has view %v; want Rendered, set through ViewChooser", built, view)
	}
}

// A setter typed on a capability names it when a widget lacks it. Before, the name came from a zero
// value's type, and an interface's zero is nil: "not a <nil>".
func TestASettersRefusalNamesItsCapability(t *testing.T) {
	set := tuidecl.EnumSetter(tuidecl.EditorViews, func(widget.ViewChooser, string) {})
	err := set(widget.NewText("x"), qml.SpecValue{Kind: qml.SpecValueString, Raw: "Editor.Rendered"})
	if err == nil || err.Error() != "not a widget.ViewChooser" {
		t.Errorf("a Text given view: %v; want \"not a widget.ViewChooser\"", err)
	}
}
