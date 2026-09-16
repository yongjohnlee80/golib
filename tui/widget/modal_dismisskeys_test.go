package widget_test

// HOST DISMISS KEYS.
//
// A terminal application usually has a second dismiss key alongside Escape, and
// a dialog that traps focus swallows it — so a surface that honoured `q` as a
// Float stops honouring it as a Modal, with nothing to say so. The three cells
// that matter are the key working, and the TWO orderings the option's doc
// claims: a mnemonic still wins, and a focused text control still gets its
// letter. Each of those is a way for this option to break something that used
// to work.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestModalDismissKeyClosesTheDialog(t *testing.T) {
	md := widget.NewModal(widget.NewText("read me"),
		widget.WithModalTitle("notice"),
		widget.WithButtons(widget.NewButton("OK", widget.WithRole(widget.ButtonRoleDefault))),
		widget.WithModalDismissKeys('q'))
	h, host, _ := modalFixture(t, md, 40, 12)
	openOn(t, h, md, host)

	var open bool
	h.onLoop(func() { open = md.IsOpen() })
	if !open {
		t.Fatal("the dialog did not open")
	}

	h.inject(key('q'))
	h.settle()
	h.onLoop(func() { open = md.IsOpen() })
	if open {
		t.Error("`q` did not dismiss a dialog that declares it as a dismiss key")
	}
}

// A dialog that declares NO dismiss keys keeps `q`, so the cell above is about
// the option rather than about Modal closing on any stray letter.
func TestModalWithoutDismissKeysIgnoresQ(t *testing.T) {
	md := widget.NewModal(widget.NewText("read me"),
		widget.WithModalTitle("notice"),
		widget.WithButtons(widget.NewButton("OK", widget.WithRole(widget.ButtonRoleDefault))))
	h, host, _ := modalFixture(t, md, 40, 12)
	openOn(t, h, md, host)

	h.inject(key('q'))
	h.settle()
	var open bool
	h.onLoop(func() { open = md.IsOpen() })
	if !open {
		t.Error("`q` closed a dialog that never declared it")
	}
}

// THE MNEMONIC WINS. A declared key is specific intent; resolving the dismiss
// key first would make a button whose mnemonic is `q` unreachable, and the
// author who declared it would have no way to know why.
func TestModalDismissKeyLosesToAMnemonic(t *testing.T) {
	activated := false
	quit := widget.NewButton("Quit",
		widget.WithMnemonic('q'),
		widget.WithRole(widget.ButtonRoleDefault),
		widget.WithOnActivate(func() { activated = true }))
	md := widget.NewModal(widget.NewText("really?"),
		widget.WithModalTitle("quit?"),
		widget.WithButtons(quit),
		widget.WithModalDismissKeys('q'))
	h, host, _ := modalFixture(t, md, 40, 12)
	openOn(t, h, md, host)

	h.inject(key('q'))
	h.settle()

	var ran bool
	h.onLoop(func() { ran = activated })
	if !ran {
		t.Error("`q` dismissed instead of activating the button whose mnemonic it is")
	}
}

// AND A FOCUSED TEXT CONTROL STILL GETS ITS LETTER. A dismiss key that reached
// the dialog before the control would make `q` impossible to type into a form —
// a CIDR, a note name, a passphrase.
func TestModalDismissKeyDoesNotStealFromATextInput(t *testing.T) {
	in := widget.NewTextInput()
	md := widget.NewModal(in,
		widget.WithModalTitle("name it"),
		widget.WithButtons(widget.NewButton("OK", widget.WithRole(widget.ButtonRoleDefault))),
		widget.WithModalDismissKeys('q'))
	h, host, _ := modalFixture(t, md, 40, 12)
	openOn(t, h, md, host)
	h.onLoop(func() { in.Context().RequestFocus() })
	h.settle()

	h.inject(key('q'))
	h.settle()

	var got string
	var open bool
	h.onLoop(func() { got, open = in.Value(), md.IsOpen() })
	if got != "q" {
		t.Errorf("the input holds %q, want %q — the dismiss key was taken from a "+
			"control that was typing", got, "q")
	}
	if !open {
		t.Error("the dialog closed while a text input had focus")
	}
}

// A CONFIGURED KEY IS AN ESCAPE EQUIVALENT IN FULL, and these three cells are
// about what that costs and what it must not.

// With no Cancel role, `q` closes and reports DismissEscape — exactly one of
// them. The reason names the INTENTION, leaving without choosing, rather than
// the physical key that carried it; a separate constant would force every
// listener to learn a second name for one meaning, and the ones that did not
// would treat a `q` dismissal as unrecognised.
func TestModalDismissKeyReportsEscapeExactlyOnce(t *testing.T) {
	md := widget.NewModal(widget.NewText("read me"),
		widget.WithModalTitle("notice"),
		widget.WithButtons(widget.NewButton("OK", widget.WithRole(widget.ButtonRoleDefault))),
		widget.WithModalDismissKeys('q'))
	h, host, _ := modalFixture(t, md, 40, 12)
	rec := record[widget.OverlayDismissedEvent](h)
	openOn(t, h, md, host)

	h.inject(key('q'))
	h.settle()

	evs := rec.events()
	if len(evs) != 1 {
		t.Fatalf("published %d dismissals, want exactly 1: %+v", len(evs), evs)
	}
	if evs[0].Reason != widget.DismissEscape {
		t.Errorf("reason = %v, want DismissEscape", evs[0].Reason)
	}
}

// WITH a Cancel role, `q` activates that button and reports DismissCancel —
// the same resolution Escape gets. A key that closed the dialog WITHOUT running
// Cancel would be a second, quieter way out with different consequences.
func TestModalDismissKeyActivatesCancelAndReportsIt(t *testing.T) {
	cancelled := false
	no := widget.NewButton("Cancel",
		widget.WithRole(widget.ButtonRoleCancel),
		widget.WithOnActivate(func() { cancelled = true }))
	md := widget.NewModal(widget.NewText("really?"),
		widget.WithModalTitle("confirm"),
		widget.WithButtons(no, widget.NewButton("OK", widget.WithRole(widget.ButtonRoleDefault))),
		widget.WithModalDismissKeys('q'))
	h, host, _ := modalFixture(t, md, 40, 12)
	rec := record[widget.OverlayDismissedEvent](h)
	openOn(t, h, md, host)

	h.inject(key('q'))
	h.settle()

	var ran bool
	h.onLoop(func() { ran = cancelled })
	if !ran {
		t.Error("`q` closed the dialog without activating its Cancel button")
	}
	evs := rec.events()
	if len(evs) != 1 {
		t.Fatalf("published %d dismissals, want exactly 1: %+v", len(evs), evs)
	}
	if evs[0].Reason != widget.DismissCancel {
		t.Errorf("reason = %v, want DismissCancel — the same resolution Escape gets",
			evs[0].Reason)
	}
}

// A MODIFIED KEY IS NOT THE KEY. Ctrl-Q is not `q`, and a dialog that closed on
// it would take a chord the application may have bound to something else.
func TestModalDismissKeyIgnoresAModifiedKey(t *testing.T) {
	md := widget.NewModal(widget.NewText("read me"),
		widget.WithModalTitle("notice"),
		widget.WithButtons(widget.NewButton("OK", widget.WithRole(widget.ButtonRoleDefault))),
		widget.WithModalDismissKeys('q'))
	h, host, _ := modalFixture(t, md, 40, 12)
	openOn(t, h, md, host)

	h.inject(keyMod('q', tui.ModCtrl))
	h.settle()

	var open bool
	h.onLoop(func() { open = md.IsOpen() })
	if !open {
		t.Error("Ctrl-Q dismissed a dialog whose dismiss key is plain `q`")
	}
}
