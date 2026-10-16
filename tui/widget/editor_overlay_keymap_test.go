package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

var normalQ = widget.KeyChord{Mode: widget.ModeNormal, Code: 'q'}

// A host unbinds a letter of an editor already built: the key goes unhandled to the host, under
// this keyset and after another one and back.
func TestOverlayKeymapUnbindsAKeyOfABuiltEditor(t *testing.T) {
	c, _ := coreWith(t, "abc")
	if !c.HandleKey(tui.KeyEvent{Code: 'q'}) {
		t.Fatal("q is not bound before the overlay: the cell would prove nothing")
	}
	keys(c, "a") // the register q waits for: the macro starts recording
	c.HandleKey(tui.KeyEvent{Code: 'q'})

	c.OverlayKeymap(widget.Keymap{normalQ: widget.ActUnbound})
	if c.HandleKey(tui.KeyEvent{Code: 'q'}) {
		t.Error("an unbound q was handled by the editor")
	}
	c.SetKeyset(widget.KeysetNano)
	c.SetKeyset(widget.KeysetVim)
	if c.HandleKey(tui.KeyEvent{Code: 'q'}) {
		t.Error("q came back after another keyset and back: the overlay was not replayed")
	}
	if got := c.Keymap()[normalQ]; got == widget.ActMacroRecord {
		t.Errorf("Keymap()[q] = %v; want it gone", got)
	}
}

// A key pending when the overlay arrives is dropped: the next key is read under the new bindings.
func TestOverlayKeymapDropsAPendingKey(t *testing.T) {
	c, _ := coreWith(t, "abc def")
	keys(c, "d") // an operator waiting for its motion
	c.OverlayKeymap(widget.Keymap{normalQ: widget.ActUnbound})
	keys(c, "w")
	if got := c.Value(); got != "abc def" {
		t.Errorf("d, the overlay, then w: %q; want nothing deleted, the d dropped", got)
	}
}

// A bad overlay panics before any of it applies.
func TestOverlayKeymapValidatesTheWholeOverlayFirst(t *testing.T) {
	c, _ := coreWith(t, "abc")
	func() {
		defer func() {
			if recover() == nil {
				t.Error("an unknown action did not panic")
			}
		}()
		c.OverlayKeymap(widget.Keymap{normalQ: widget.ActUnbound, {Mode: widget.ModeNormal, Code: 'Z'}: widget.Action(255)})
	}()
	if !c.HandleKey(tui.KeyEvent{Code: 'q'}) {
		t.Error("half of a refused overlay applied: q was unbound")
	}
}

// The Editor passes it to its core.
func TestEditorOverlayKeymap(t *testing.T) {
	e := widget.NewEditor(widget.WithInitialText("abc"))
	e.OverlayKeymap(widget.Keymap{normalQ: widget.ActUnbound})
	if _, ok := e.Keymap()[normalQ]; ok && e.Keymap()[normalQ] != widget.ActUnbound {
		t.Errorf("the Editor's keymap still binds q to %v", e.Keymap()[normalQ])
	}
}
