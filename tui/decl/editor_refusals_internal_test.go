package decl

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// The standard Editor's hand-written setters and its method refuse what they are not given, naming
// it: a value of the wrong kind, a widget that is no editor, too many arguments.
func TestTheEditorSettersAndMethodRefuseWhatTheyAreNotGiven(t *testing.T) {
	ed, other := widget.NewEditor(), widget.NewText("x")
	num := qml.SpecValue{Kind: qml.SpecValueNumber, Raw: "1"}
	set := EditorSetters()
	for _, c := range []struct {
		prop string
		on   tui.Component
		v    qml.SpecValue
		want string
	}{
		{"text", ed, num, "string"},
		{"text", other, strValue("x"), "not an Editor"},
		{"indentUnit", ed, num, "string"},
		{"indentUnit", other, strValue("  "), "indentUnit needs an Editor"},
	} {
		if err := set[c.prop](c.on, c.v); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s on a %T with %s: %v, want %q", c.prop, c.on, c.v.Kind, err, c.want)
		}
	}
	open := EditorMethods()["openCommandLine"]
	for _, c := range []struct {
		on   tui.Component
		args []qml.SpecValue
		want string
	}{
		{ed, []qml.SpecValue{strValue("a"), strValue("b")}, "at most one argument"},
		{ed, []qml.SpecValue{num}, "string"},
		{other, nil, "openCommandLine needs an Editor"},
	} {
		if err := open(c.on, c.args); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("openCommandLine(%d args) on a %T: %v, want %q", len(c.args), c.on, err, c.want)
		}
	}
}
