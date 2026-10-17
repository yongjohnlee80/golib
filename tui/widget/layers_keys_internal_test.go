package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/keymap"
)

// A modal Float's layer is its own scope and lists its Esc; a modeless one lists nothing, since its
// Esc is not taken. An anchored popup's layer lists its Esc.
func TestTheFloatAndPopupLayersScopesAndBindings(t *testing.T) {
	ids := func(bs []keymap.Binding) []string {
		var out []string
		for _, b := range bs {
			out = append(out, b.Command.ID)
		}
		return out
	}
	for _, modal := range []bool{false, true} {
		l := &floatLayer{owner: &Float{modal: modal}}
		if l.KeyScope() != FloatScope {
			t.Errorf("modal %v: scope %q", modal, l.KeyScope())
		}
		got := ids(l.KeyBindings(nil))
		if want := map[bool]int{false: 0, true: 1}[modal]; len(got) != want || want == 1 && got[0] != FloatClose.ID {
			t.Errorf("modal %v: bindings %v", modal, got)
		}
	}
	p := &popupLayer{}
	if p.KeyScope() != PopupScope {
		t.Errorf("popup scope %q", p.KeyScope())
	}
	if got := ids(p.KeyBindings(nil)); len(got) != 1 || got[0] != PopupClose.ID {
		t.Errorf("popup bindings %v", got)
	}
}
