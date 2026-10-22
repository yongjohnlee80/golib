package decl

import (
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// coreOnly is an editor widget that offers its core and nothing else: no SetKeyset, SetReadOnly
// or SetCursorPosition of its own, as the cell Editor will be once its deprecated facade goes.
type coreOnly struct {
	*widget.Text
	core *widget.EditorCore
}

func (c *coreOnly) Core() *widget.EditorCore { return c.core }

// keyset, readOnly and cursorPosition reach the core of a widget that offers only Core(), and a
// widget with neither the method nor a core is refused.
func TestTheBehaviourSettersReachACoreOnlyEditorsCore(t *testing.T) {
	ed := &coreOnly{Text: widget.NewText("x"), core: widget.NewEditorCore(widget.CoreInitialText("one\ntwo"))}
	set := EditorSetters()
	for prop, v := range map[string]qml.SpecValue{
		"keyset":         {Kind: qml.SpecValueString, Raw: constantValue("Nano")},
		"readOnly":       {Kind: qml.SpecValueBool, Raw: "true"},
		"cursorPosition": {Kind: qml.SpecValueNumber, Raw: "5"},
	} {
		if err := set[prop](ed, v); err != nil {
			t.Fatalf("%s on a core-only editor: %v", prop, err)
		}
	}
	if row, col := ed.core.Line(); ed.core.Keyset() != widget.KeysetNano || !ed.core.ReadOnly() || row != 1 || col != 1 {
		t.Errorf("the core: keyset %v, read-only %v, cursor (%d, %d); want Nano, true, (1, 1)",
			ed.core.Keyset(), ed.core.ReadOnly(), row, col)
	}
	if err := set["readOnly"](widget.NewText("x"), qml.SpecValue{Kind: qml.SpecValueBool, Raw: "true"}); err == nil {
		t.Error("readOnly on a widget with neither SetReadOnly nor a core was not refused")
	}
}
