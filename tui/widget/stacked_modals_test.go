package widget_test

// DIALOGS STACKED ON ONE HOST, driven through the real widgets rather than
// through hand-built trapping scopes.
//
// The runtime cells for this live in package tui and build the shape out of a
// Stack directly. These exist because the shape consumers actually write is
// two Modals — or a Select inside one — on a single OverlayHost, and the
// composition has parts the runtime cells cannot see: Modal.Open seeding focus
// onto its default button, Float mounting its trapping layer INSIDE itself so
// the two traps are cousins rather than siblings, and the scope stack carrying
// focus back when the top one closes.
//
// THE RETURN PATH IS THE HALF THAT IS EASY TO MISS. Tab cannot reach a dialog
// behind the active one — a trap confines traversal both in and out, so the
// ring while the upper dialog is open contains only the upper dialog. What
// brings focus back is the scope stack: entering a trap records where focus
// came from, and the unmount cascade restores it. Before the stacked-entry fix
// that pairing could not even form, because focus was never granted into the
// second dialog, so nothing was ever recorded to restore.

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TestASecondDialogOpenedOverTheFirstTakesTheKeyboard.
//
// Two Modals on one OverlayHost: the shape every consumer writes, and the one
// that was inert. Each Modal's trapping layer is mounted inside its own Float,
// so neither trap is an ancestor of the other and ancestry alone could only
// ever refuse the second.
func TestASecondDialogOpenedOverTheFirstTakesTheKeyboard(t *testing.T) {
	firstOK := widget.NewButton("first-ok", widget.WithRole(widget.ButtonRoleDefault))
	first := widget.NewModal(widget.NewText("first"), widget.WithButtons(firstOK))
	secondOK := widget.NewButton("second-ok", widget.WithRole(widget.ButtonRoleDefault))
	second := widget.NewModal(widget.NewText("second"), widget.WithButtons(secondOK))

	h, host, base := modalFixture(t, first, 40, 12)
	defer h.stop()

	openOn(t, h, first, host)
	var firstFocused bool
	h.onLoop(func() { firstFocused = firstOK.Context() != nil && firstOK.Context().Focused() })
	if !firstFocused {
		t.Fatal("precondition failed: the first dialog never took focus, so the " +
			"second has nothing to take it from")
	}

	openOn(t, h, second, host)

	var secondFocused, stillFirst, baseFocused bool
	h.onLoop(func() {
		secondFocused = secondOK.Context() != nil && secondOK.Context().Focused()
		stillFirst = firstOK.Context() != nil && firstOK.Context().Focused()
		baseFocused = base.Context().Focused()
	})
	if !secondFocused {
		t.Errorf("the dialog opened on top did not take the keyboard "+
			"(first=%v base=%v); it paints and cannot be typed into",
			stillFirst, baseFocused)
	}
	if stillFirst {
		t.Error("focus stayed in the dialog underneath")
	}
}

// TestClosingTheTopDialogPutsTheKeyboardBackInTheOneBeneath.
//
// The return path, which is NOT Tab. Closing the upper dialog must land focus
// exactly where it was in the lower one, via the scope stack's restore — not
// on the base, and not nowhere.
func TestClosingTheTopDialogPutsTheKeyboardBackInTheOneBeneath(t *testing.T) {
	firstOK := widget.NewButton("first-ok", widget.WithRole(widget.ButtonRoleDefault))
	first := widget.NewModal(widget.NewText("first"), widget.WithButtons(firstOK))
	secondOK := widget.NewButton("second-ok", widget.WithRole(widget.ButtonRoleDefault))
	second := widget.NewModal(widget.NewText("second"), widget.WithButtons(secondOK))

	h, host, base := modalFixture(t, first, 40, 12)
	defer h.stop()

	openOn(t, h, first, host)
	openOn(t, h, second, host)
	var reached bool
	h.onLoop(func() { reached = secondOK.Context() != nil && secondOK.Context().Focused() })
	if !reached {
		t.Fatal("precondition failed: never reached the second dialog")
	}

	h.onLoop(func() { second.Dismiss(widget.DismissProgrammatic) })
	h.settle()
	h.settle()

	var backInFirst, onBase bool
	var firstOpen bool
	h.onLoop(func() {
		backInFirst = firstOK.Context() != nil && firstOK.Context().Focused()
		onBase = base.Context().Focused()
		firstOpen = first.IsOpen()
	})
	if !firstOpen {
		t.Fatal("closing the top dialog closed the one underneath too")
	}
	if !backInFirst {
		t.Errorf("focus did not return to the dialog beneath (onBase=%v). Tab "+
			"cannot reach it — a trap confines traversal both ways — so if the "+
			"scope restore does not land it there, that dialog is stranded", onBase)
	}
}

// TestADropdownInsideADialogStillOpens.
//
// Select's popup traps too, so a Select used inside a Modal is a trap opening
// over a trap — and STRUCTURALLY IT IS STACKED, not nested: the popup is
// attached to the same OverlayHost as the dialog, making the two traps layers
// of one host rather than one inside the other. It reads like the nested case
// and is not, which is why it earns a cell of its own.
//
// The genuine nested control lives in package tui
// (TestAnInnerTrapCannotEscapeIntoTheDialogAroundIt), where the inner scope is
// a real descendant of the outer one.
func TestADropdownInsideADialogStillOpens(t *testing.T) {
	sel := widget.NewSelect(widget.WithOptions([]widget.SelectItem[string]{
		{Label: "alpha", Value: "alpha"},
		{Label: "beta", Value: "beta"},
	}))
	ok := widget.NewButton("ok", widget.WithRole(widget.ButtonRoleDefault))
	m := widget.NewModal(sel, widget.WithButtons(ok))

	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	h.onLoop(func() { sel.Context().RequestFocus() })
	h.settle()
	var selFocused bool
	h.onLoop(func() { selFocused = sel.Context().Focused() })
	if !selFocused {
		t.Fatal("precondition failed: the Select inside the dialog never took focus")
	}
	// The unopened dropdown shows only its field, so the second option being
	// absent is the screen saying the popup is shut.
	if before := h.grid(); strings.Contains(before, "beta") {
		t.Fatalf("precondition failed: the popup is already open:\n%s", before)
	}

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown})
	h.settle()
	h.settle()

	if after := h.grid(); !strings.Contains(after, "beta") {
		t.Errorf("a Select inside a dialog could not open its popup; the topmost "+
			"stacked popup trap must still be enterable:\n%s", after)
	}
}
