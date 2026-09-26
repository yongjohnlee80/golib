package widget_test

// KEYBOARD PARITY WITH THE EDITOR THIS PACKAGE REPLACED.
//
// Three vocabularies the original had and the first migration dropped: Vim
// aliases, directional movement and mnemonics inside a dialog, and an Escape
// that unwinds one stage at a time. Each is tested for the thing that makes it
// safe to offer as well as the thing it does — an alias that shadowed a
// declared mnemonic, or a mnemonic a disabled control still answered, would be
// worse than not having them.

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func pressOn(h *harness, code rune) {
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: code})
	h.settle()
	h.settle()
}

func selectionOf(t *testing.T, h *harness, m *widget.Menu) widget.ItemID {
	t.Helper()
	var id widget.ItemID
	h.onLoop(func() { id, _ = m.Selected() })
	return id
}

// vimBarModel has a row whose mnemonic IS an alias key, which is the collision
// the precedence rule exists for.
func vimBarModel() []widget.MenuItemModel {
	file := widget.NewSubmenu("file", "File", []widget.MenuItemModel{
		widget.NewCommand("new", "New", nil),
		widget.NewCommand("open", "Open", nil),
	})
	file.Hotkey = 'f'
	opt := widget.NewSubmenu("option", "Option", []widget.MenuItemModel{
		// Hotkey 'k' — the same key the alias would use to move up.
		hotkeyRow(widget.NewSubmenu("km", "Keymaps", []widget.MenuItemModel{
			widget.NewCommand("vim", "Vim", nil),
		}), 'k'),
	})
	opt.Hotkey = 'o'
	return []widget.MenuItemModel{file, opt}
}

func hotkeyRow(m widget.MenuItemModel, key rune) widget.MenuItemModel {
	m.Hotkey = key
	return m
}

// TestVimAliasesMoveOnlyWhereNoMnemonicAnswers.
//
// The aliases stand for the physical directions, so they inherit whatever each
// direction already means at that level rather than forming a second
// vocabulary. And a declared mnemonic always wins: a row whose hotkey is 'k'
// stays reachable, or enabling the aliases would silently strand it.
func TestVimAliasesMoveOnlyWhereNoMnemonicAnswers(t *testing.T) {
	t.Run("j opens a category and j/k move rows", func(t *testing.T) {
		m := widget.NewMenu(widget.WithMenuVimNavigation(true))
		h, _ := barFixture(t, m, vimBarModel(), 50, 14)
		defer h.stop()
		h.onLoop(func() { m.Context().RequestFocus(); m.Select("file") })
		h.settle()

		pressOn(h, 'j') // stands for Down: opens the category
		if n := openLevelsOn(t, h, m); n != 1 {
			t.Fatalf("j left %d levels open, want the category opened", n)
		}
		pressOn(h, 'j')
		if got := selectionOf(t, h, m); got != "open" {
			t.Errorf("j inside the dropdown selected %q, want the second row", got)
		}
		pressOn(h, 'k')
		if got := selectionOf(t, h, m); got != "new" {
			t.Errorf("k selected %q, want back to the first row", got)
		}
	})

	t.Run("h and l walk the bar", func(t *testing.T) {
		m := widget.NewMenu(widget.WithMenuVimNavigation(true))
		h, _ := barFixture(t, m, vimBarModel(), 50, 14)
		defer h.stop()
		h.onLoop(func() { m.Context().RequestFocus(); m.Select("file") })
		h.settle()
		pressOn(h, 'l')
		if got := selectionOf(t, h, m); got != "option" {
			t.Errorf("l selected %q, want the next category", got)
		}
		pressOn(h, 'h')
		if got := selectionOf(t, h, m); got != "file" {
			t.Errorf("h selected %q, want the previous category", got)
		}
	})

	t.Run("a row's own k mnemonic still wins", func(t *testing.T) {
		// THE COLLISION. Inside Option the only row answers to 'k', which is
		// also the alias for Up. The mnemonic is specific intent and must win,
		// or enabling the aliases makes that row unreachable by its own key.
		m := widget.NewMenu(widget.WithMenuVimNavigation(true))
		h, _ := barFixture(t, m, vimBarModel(), 50, 14)
		defer h.stop()
		h.onLoop(func() { m.Context().RequestFocus() })
		h.settle()
		h.onLoop(func() { _ = m.Open("option") })
		h.settle()
		h.settle()

		pressOn(h, 'k')
		if n := openLevelsOn(t, h, m); n != 2 {
			t.Errorf("k left %d levels open; the row's own mnemonic should have "+
				"opened the cascade rather than moving up\n%s", n, h.grid())
		}
	})

	t.Run("off by default", func(t *testing.T) {
		// The control: without the option the same keys are inert, so the
		// assertions above are about the option and not about hjkl being bound
		// somewhere else.
		m := widget.NewMenu()
		h, _ := barFixture(t, m, vimBarModel(), 50, 14)
		defer h.stop()
		h.onLoop(func() { m.Context().RequestFocus(); m.Select("file") })
		h.settle()
		pressOn(h, 'l')
		if got := selectionOf(t, h, m); got != "file" {
			t.Errorf("l moved the selection to %q with the aliases off", got)
		}
	})
}

// dialogFixture opens a two-button dialog with mnemonics, as the editor's exit
// confirmation has.
func dialogFixture(t *testing.T, opts ...widget.ModalOption) (*harness, *widget.Modal, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	var yes, no atomic.Int64
	yb := widget.NewButton("Yes", widget.WithRole(widget.ButtonRoleAccept), widget.WithDefault(true),
		widget.WithMnemonic('y'), widget.WithOnActivate(func() { yes.Add(1) }))
	nb := widget.NewButton("No", widget.WithRole(widget.ButtonRoleReject),
		widget.WithMnemonic('n'), widget.WithOnActivate(func() { no.Add(1) }))
	base := []widget.ModalOption{widget.WithModalTitle("Confirm"), widget.WithButtons(yb, nb)}
	md := widget.NewModal(widget.NewText("Sure?"), append(base, opts...)...)
	host := widget.NewOverlayHost(widget.NewText(""))
	h := startApp(t, host, 50, 12)
	h.onLoop(func() {
		if err := md.Open(host); err != nil {
			t.Fatalf("Open: %v", err)
		}
	})
	h.settle()
	h.settle()
	return h, md, &yes, &no
}

// TestADialogIsNavigableWithTheArrowsAndItsMnemonics.
//
// Tab alone is not how a two-button confirmation is used. Arrows are
// conventional and are the default; the mnemonics are what the original had.
func TestADialogIsNavigableWithTheArrowsAndItsMnemonics(t *testing.T) {
	// Space and Enter activate the focused button. The dialog's default only
	// answers Enter when the focused control leaves it unclaimed.
	t.Run("arrows move between the buttons", func(t *testing.T) {
		h, _, yes, no := dialogFixture(t)
		defer h.stop()
		// The default takes initial focus; Right moves to No.
		pressOn(h, tui.KeyRight)
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: ' '})
		h.settle()
		h.settle()
		if no.Load() != 1 || yes.Load() != 0 {
			t.Errorf("after Right, Space gave yes=%d no=%d; want the second button",
				yes.Load(), no.Load())
		}
	})

	t.Run("and back again", func(t *testing.T) {
		h, _, yes, no := dialogFixture(t)
		defer h.stop()
		pressOn(h, tui.KeyRight)
		pressOn(h, tui.KeyLeft)
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: ' '})
		h.settle()
		h.settle()
		if yes.Load() != 1 || no.Load() != 0 {
			t.Errorf("after Right then Left, Space gave yes=%d no=%d; want the first",
				yes.Load(), no.Load())
		}
	})

	t.Run("Enter presses the focused button before the default", func(t *testing.T) {
		h, _, yes, no := dialogFixture(t)
		defer h.stop()
		pressOn(h, tui.KeyRight) // focus on No
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
		h.settle()
		h.settle()
		if yes.Load() != 0 || no.Load() != 1 {
			t.Errorf("Enter on No gave yes=%d no=%d; want the focused No", yes.Load(), no.Load())
		}
	})

	t.Run("a mnemonic activates from either focus", func(t *testing.T) {
		h, _, yes, no := dialogFixture(t)
		defer h.stop()
		pressOn(h, 'n') // focus is on Yes; n must still reach No
		if no.Load() != 1 {
			t.Errorf("n from the other button gave no=%d, want 1", no.Load())
		}
		if yes.Load() != 0 {
			t.Errorf("n activated Yes (%d)", yes.Load())
		}
	})

	t.Run("vim aliases are opt-in", func(t *testing.T) {
		h, _, yes, no := dialogFixture(t, widget.WithModalVimNavigation(true))
		defer h.stop()
		pressOn(h, 'j')
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: ' '})
		h.settle()
		h.settle()
		if no.Load() != 1 {
			t.Errorf("j then Space gave yes=%d no=%d; want the second button",
				yes.Load(), no.Load())
		}
	})

	t.Run("the mnemonic is underlined", func(t *testing.T) {
		h, _, _, _ := dialogFixture(t)
		defer h.stop()
		// A key the user cannot see is a key they will not press.
		grid := h.tb.Snapshot()
		found := false
		for y := range grid {
			for _, c := range grid[y] {
				if c.Content == "Y" && c.Attrs.Mask&tui.AttrUnderline != 0 {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("the mnemonic is not marked in the label:\n%s", h.grid())
		}
	})
}

// TestADisabledButtonDoesNotAnswerItsMnemonic, and the navigation skips it —
// a greyed control answering a key is worse than one that is simply absent.
func TestADisabledButtonDoesNotAnswerItsMnemonic(t *testing.T) {
	var yes, no atomic.Int64
	yb := widget.NewButton("Yes", widget.WithRole(widget.ButtonRoleAccept), widget.WithDefault(true),
		widget.WithMnemonic('y'), widget.WithOnActivate(func() { yes.Add(1) }))
	nb := widget.NewButton("No", widget.WithRole(widget.ButtonRoleReject),
		widget.WithMnemonic('n'), widget.WithOnActivate(func() { no.Add(1) }))
	md := widget.NewModal(widget.NewText("Sure?"), widget.WithButtons(yb, nb))
	host := widget.NewOverlayHost(widget.NewText(""))
	h := startApp(t, host, 50, 12)
	defer h.stop()
	h.onLoop(func() {
		nb.SetEnabled(false)
		if err := md.Open(host); err != nil {
			t.Fatalf("Open: %v", err)
		}
	})
	h.settle()
	h.settle()

	pressOn(h, 'n')
	if no.Load() != 0 {
		t.Errorf("a disabled button answered its mnemonic (%d activations)", no.Load())
	}
	// And stepping skips it rather than parking focus on something inert.
	pressOn(h, tui.KeyRight)
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.settle()
	h.settle()
	if yes.Load() != 1 {
		t.Errorf("Right then Enter gave yes=%d; the step should have skipped the "+
			"disabled button and wrapped", yes.Load())
	}
}

// TestTwoEnabledButtonsCannotShareAMnemonic. One keystroke cannot mean two
// controls, and choosing either silently would make the dialog behave
// differently from how it reads.
func TestTwoEnabledButtonsCannotShareAMnemonic(t *testing.T) {
	mk := func(label string, key rune) *widget.Button {
		return widget.NewButton(label, widget.WithMnemonic(key))
	}
	if f := fatalFromWidgetExt(func() {
		widget.NewModal(widget.NewText("x"),
			widget.WithButtons(mk("Save", 's'), mk("Send", 'S')))
	}); f == nil {
		t.Error("two enabled buttons sharing a key were accepted")
	}
	// A DISABLED TWIN IS REFUSED TOO, and that is the point of checking the
	// declaration rather than the current state. Enabled is mutable: accepting
	// two greyed twins means SetEnabled(true) on both later produces a dialog
	// with two controls answering one key and nothing revalidating it, where
	// which one wins depends on list order.
	dis := mk("Send", 's')
	dis.SetEnabled(false)
	if f := fatalFromWidgetExt(func() {
		widget.NewModal(widget.NewText("x"), widget.WithButtons(mk("Save", 's'), dis))
	}); f == nil {
		t.Error("an enabled and a disabled button sharing a key were accepted; " +
			"enabling the second later would make the dialog ambiguous")
	}
	bothOff := []*widget.Button{mk("Save", 's'), mk("Send", 's')}
	for _, b := range bothOff {
		b.SetEnabled(false)
	}
	if f := fatalFromWidgetExt(func() {
		widget.NewModal(widget.NewText("x"), widget.WithButtons(bothOff...))
	}); f == nil {
		t.Error("two disabled buttons sharing a key were accepted; uniqueness has " +
			"to hold for the list's lifetime, not just while they are greyed")
	}

	// The control: distinct keys are accepted, so the refusals above are about
	// the collision rather than about mnemonics being rejected outright.
	if f := fatalFromWidgetExt(func() {
		widget.NewModal(widget.NewText("x"),
			widget.WithButtons(mk("Save", 's'), mk("Quit", 'q')))
	}); f != nil {
		t.Errorf("distinct mnemonics were refused: %v", f)
	}
}

// TestAMnemonicActivationReachesTheBus.
//
// Activation goes through the runtime rather than by calling the button's
// method, so a mnemonic press is indistinguishable from Enter or a click to
// anything watching — a command log, an undo stack, a test.
func TestAMnemonicActivationReachesTheBus(t *testing.T) {
	h, _, _, _ := dialogFixture(t)
	defer h.stop()

	var seen atomic.Int64
	unsub := tui.Subscribe(h.app.Bus(), func(tui.ControlActivatedEvent) { seen.Add(1) })
	defer unsub()

	pressOn(h, 'n')
	if seen.Load() != 1 {
		t.Errorf("%d ControlActivatedEvent after a mnemonic press, want 1: the "+
			"activation bypassed the runtime", seen.Load())
	}
}

// TestEscapeIsStagedAndUnhandledAtTheRoot restates the contract from the
// consumer's side: the dialog's Escape is unaffected, and the menu's unwinds.
func TestEscapeIsStagedAndUnhandledAtTheRoot(t *testing.T) {
	m := widget.NewMenu()
	h, _ := barFixture(t, m, vimBarModel(), 50, 14)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()
	h.onLoop(func() { _ = m.Open("option") })
	h.settle()
	h.settle()
	h.onLoop(func() { _ = m.Open("km") })
	h.settle()
	h.settle()
	if n := openLevelsOn(t, h, m); n != 2 {
		t.Fatalf("precondition: %d levels open, want 2", n)
	}

	for want := 1; want >= 0; want-- {
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
		h.settle()
		h.settle()
		if n := openLevelsOn(t, h, m); n != want {
			t.Fatalf("after an Escape %d levels remain, want %d", n, want)
		}
	}
	if grid := h.grid(); strings.Contains(grid, "Vim") {
		t.Errorf("a level survived the unwind:\n%s", grid)
	}
}

// TestADialogIgnoresReleasesChordsAndUnclaimedKeys.
//
// The three ways a keystroke must NOT reach a dialog's buttons. A release is
// the tail of a press already handled; a chord belongs to whatever binds it;
// and a letter no button answers to is somebody else's. Each would otherwise
// move focus or press something on a key the user did not aim at the dialog.
func TestADialogIgnoresReleasesChordsAndUnclaimedKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		ev   tui.KeyEvent
	}{
		{"a key release", tui.KeyEvent{Kind: tui.KeyRelease, Code: 'n'}},
		{"a ctrl chord", tui.KeyEvent{Kind: tui.KeyPress, Code: 'n', Mods: tui.ModCtrl}},
		{"an alt chord", tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight, Mods: tui.ModAlt}},
		{"a letter no button claims", tui.KeyEvent{Kind: tui.KeyPress, Code: 'z'}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, yes, no := dialogFixture(t)
			defer h.stop()
			h.inject(tc.ev)
			h.settle()
			h.settle()
			if yes.Load() != 0 || no.Load() != 0 {
				t.Errorf("%s activated something: yes=%d no=%d", tc.name, yes.Load(), no.Load())
			}
			// Focus has not moved either: Enter still means the focused button.
			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
			h.settle()
			h.settle()
			if yes.Load() != 1 {
				t.Errorf("%s moved the focus; Enter gave yes=%d no=%d",
					tc.name, yes.Load(), no.Load())
			}
		})
	}
}

// TestVimKeysInADialogAreInertUnlessAsked is the control for the opt-in: with
// the option off, h/j/k/l are ordinary letters that no button claims.
func TestVimKeysInADialogAreInertUnlessAsked(t *testing.T) {
	h, _, yes, no := dialogFixture(t) // no WithModalVimNavigation
	defer h.stop()
	for _, r := range []rune{'h', 'j', 'k', 'l'} {
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: r})
		h.settle()
	}
	h.settle()
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.settle()
	h.settle()
	if yes.Load() != 1 || no.Load() != 0 {
		t.Errorf("hjkl moved focus with the aliases off: yes=%d no=%d",
			yes.Load(), no.Load())
	}
}

// TestADialogWithNothingToStepToIsQuiet.
//
// Two shapes with no destination: a dialog carrying no buttons at all, and one
// whose only button is disabled. Neither may panic, and neither may leave focus
// somewhere inert.
func TestADialogWithNothingToStepToIsQuiet(t *testing.T) {
	t.Run("no buttons", func(t *testing.T) {
		md := widget.NewModal(widget.NewText("Just a message"),
			widget.WithModalTitle("Notice"))
		host := widget.NewOverlayHost(widget.NewText(""))
		h := startApp(t, host, 40, 10)
		defer h.stop()
		h.onLoop(func() {
			if err := md.Open(host); err != nil {
				t.Fatalf("Open: %v", err)
			}
		})
		h.settle()
		h.settle()
		for _, k := range []rune{tui.KeyLeft, tui.KeyRight, tui.KeyUp, tui.KeyDown} {
			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: k})
			h.settle()
		}
		// Still open and still painting: the point is that nothing blew up.
		h.onLoop(func() {
			if !md.IsOpen() {
				t.Error("arrows closed a dialog that has no buttons")
			}
		})
	})

	t.Run("every button disabled", func(t *testing.T) {
		var fired atomic.Int64
		b := widget.NewButton("Only", widget.WithMnemonic('o'),
			widget.WithOnActivate(func() { fired.Add(1) }))
		md := widget.NewModal(widget.NewText("m"), widget.WithButtons(b))
		host := widget.NewOverlayHost(widget.NewText(""))
		h := startApp(t, host, 40, 10)
		defer h.stop()
		h.onLoop(func() {
			b.SetEnabled(false)
			if err := md.Open(host); err != nil {
				t.Fatalf("Open: %v", err)
			}
		})
		h.settle()
		h.settle()

		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight})
		h.settle()
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 'o'})
		h.settle()
		h.settle()
		if fired.Load() != 0 {
			t.Errorf("a disabled sole button was reached (%d activations)", fired.Load())
		}
	})
}

// TestACascadeOpensWithoutWaitingForAFrame.
//
// THE DEFECT, and it is one my own tests were hiding. A nested level anchors to
// a row of its parent, and that row has no rect until the parent popup has been
// laid out — so a second open issued in the same turn found no anchor, returned
// ErrAnchorUnusable, and was simply lost. A user pressing Alt+O then k faster
// than a frame got Option and never Keymaps.
//
// Every cascade test in this package settles between the two opens, which is
// what a human does and is exactly why none of them saw it. This one does NOT
// settle: both intents are issued in one turn, as a terminal delivers a burst
// of keystrokes, and the cascade must arrive anyway.
func TestACascadeOpensWithoutWaitingForAFrame(t *testing.T) {
	model := []widget.MenuItemModel{
		widget.NewSubmenu("option", "Option", []widget.MenuItemModel{
			widget.NewSubmenu("km", "Keymaps", []widget.MenuItemModel{
				widget.NewCommand("vim", "Vim", nil),
			}),
		}),
	}

	t.Run("two Opens in one turn", func(t *testing.T) {
		m := widget.NewMenu()
		h, _ := barFixture(t, m, model, 50, 14)
		defer h.stop()
		h.onLoop(func() { m.Context().RequestFocus() })
		h.settle()

		// NO SETTLE BETWEEN. The second open lands before the first level has
		// been laid out, which is the whole point.
		h.onLoop(func() {
			_ = m.Open("option")
			_ = m.Open("km")
		})
		h.settle()
		h.settle()

		if n := openLevelsOn(t, h, m); n != 2 {
			t.Errorf("OpenLevels() = %d after two opens in one turn, want 2 — the "+
				"nested open was dropped for want of a frame\n%s", n, h.grid())
		}
		if !strings.Contains(h.grid(), "Vim") {
			t.Errorf("the nested level never appeared:\n%s", h.grid())
		}
	})

	t.Run("a burst of keystrokes", func(t *testing.T) {
		// The same thing through real input: the mnemonic that opens Option and
		// the one that opens Keymaps, injected together.
		opt := model[0]
		opt.Hotkey = 'o'
		km := opt.Children[0]
		km.Hotkey = 'k'
		opt.Children = []widget.MenuItemModel{km}
		burst := []widget.MenuItemModel{opt}

		m := widget.NewMenu()
		h, _ := barFixture(t, m, burst, 50, 14)
		defer h.stop()
		h.onLoop(func() { m.Context().RequestFocus() })
		h.settle()

		h.inject(
			tui.KeyEvent{Kind: tui.KeyPress, Code: 'o'},
			tui.KeyEvent{Kind: tui.KeyPress, Code: 'k'},
		)
		h.settle()
		h.settle()

		if n := openLevelsOn(t, h, m); n != 2 {
			t.Errorf("OpenLevels() = %d after o then k in one burst, want 2\n%s",
				n, h.grid())
		}
	})
}

// TestAnImpossibleAnchorIsStillAnError.
//
// The control for queuing. "Not laid out yet" is early and worth waiting a
// frame for; "not on screen at all" is not, and retrying it would leave the
// caller waiting for a frame that is never going to help — the failure mode
// queuing was introduced to avoid, arrived at from the other side.
func TestAnImpossibleAnchorIsStillAnError(t *testing.T) {
	model := []widget.MenuItemModel{
		widget.NewSubmenu("option", "Option", []widget.MenuItemModel{
			widget.NewSubmenu("km", "Keymaps", []widget.MenuItemModel{
				widget.NewCommand("vim", "Vim", nil),
			}),
		}),
	}
	m := widget.NewMenu()
	h, _ := barFixture(t, m, model, 50, 14)
	defer h.stop()

	// "km" lives two levels down and NOTHING is open, so its row is nowhere on
	// screen and no future layout will give it a rect.
	var err error
	h.onLoop(func() { err = m.Open("km") })
	h.settle()
	h.settle()
	if err == nil {
		t.Error("opening a row whose parent level is closed was accepted; it has " +
			"no anchor now and will not grow one")
	}
	if n := openLevelsOn(t, h, m); n != 0 {
		t.Errorf("OpenLevels() = %d after a refused open, want 0", n)
	}

	// And a row that is not in the model at all.
	h.onLoop(func() { err = m.Open("nosuchrow") })
	h.settle()
	if err == nil {
		t.Error("opening a row the model does not contain was accepted")
	}
}

// TestAQueuedOpenDoesNotSurviveItsCascade.
//
// An intent recorded against one arrangement of rows must not be carried into
// another: a queued open that outlived a close would reopen a level the user
// had just dismissed, a frame later and for no reason they could see.
func TestAQueuedOpenDoesNotSurviveItsCascade(t *testing.T) {
	model := []widget.MenuItemModel{
		widget.NewSubmenu("option", "Option", []widget.MenuItemModel{
			widget.NewSubmenu("km", "Keymaps", []widget.MenuItemModel{
				widget.NewCommand("vim", "Vim", nil),
			}),
		}),
	}
	m := widget.NewMenu()
	h, _ := barFixture(t, m, model, 50, 14)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()

	// Queue the nested open and close the whole cascade in the SAME turn, so
	// the intent is still outstanding when the levels go.
	h.onLoop(func() {
		_ = m.Open("option")
		_ = m.Open("km")
		m.Close()
	})
	h.settle()
	h.settle()

	if n := openLevelsOn(t, h, m); n != 0 {
		t.Errorf("OpenLevels() = %d after closing with an open queued; the queued "+
			"intent reopened a dismissed cascade\n%s", n, h.grid())
	}
}

// TestAClippedNestedRowIsRefusedRatherThanQueuedForever.
//
// THE DEFECT IN THE FIRST CUT OF THE QUEUE. "Early" was decided from the row's
// DEPTH alone — it belongs to a level that exists — but
// depth says nothing about whether that level has already been laid out. A row
// clipped out of a popup too short to hold it has the same depth as a row on a
// popup that has not been measured yet, so Open returned nil and parked an
// intent no future layout could satisfy: the caller was told the popup was
// coming, and it never came.
//
// Waiting is only honest while a frame can still change the answer. Once the
// owning level has laid out and left the row out, another frame will leave it
// out again, and the caller has to be told now.
func TestAClippedNestedRowIsRefusedRatherThanQueuedForever(t *testing.T) {
	// Four commands and then a submenu, in a terminal too short for the popup
	// to show all five rows. "deep" is the one that gets clipped.
	model := []widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("a", "One", nil),
			widget.NewCommand("b", "Two", nil),
			widget.NewCommand("c", "Three", nil),
			widget.NewCommand("d", "Four", nil),
			widget.NewSubmenu("deep", "Deeper", []widget.MenuItemModel{
				widget.NewCommand("x", "Leaf", nil),
			}),
		}),
	}
	m := widget.NewMenu()
	h, _ := barFixture(t, m, model, 40, 5)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()

	// SETTLED FIRST, so the level has laid out and made its decision about
	// which rows fit. That is what separates this from the burst case.
	h.onLoop(func() { _ = m.Open("file") })
	h.settle()
	h.settle()
	if strings.Contains(h.grid(), "Deeper") {
		t.Skipf("the fixture is not clipping: Deeper is on screen\n%s", h.grid())
	}

	var err error
	h.onLoop(func() { err = m.Open("deep") })
	h.settle()
	h.settle()

	if !errors.Is(err, widget.ErrAnchorUnusable) {
		t.Errorf("Open(deep) = %v for a row clipped out of a level that has "+
			"already laid out, want ErrAnchorUnusable — no later frame can give "+
			"it a rect\n%s", err, h.grid())
	}
	if n := openLevelsOn(t, h, m); n != 1 {
		t.Errorf("OpenLevels() = %d after a refused open, want 1", n)
	}
}

// TestARejectedSetModelKeepsAQueuedOpen.
//
// SetModel documents that a model it refuses changes nothing, and a queued open
// is part of what "nothing" has to cover: the old model is still the live one,
// so a valid open issued against it is still valid. Clearing the intent first
// and validating second threw away a request the caller had every right to
// expect, and did it only on the failure path — which is the path least likely
// to be exercised.
func TestARejectedSetModelKeepsAQueuedOpen(t *testing.T) {
	model := []widget.MenuItemModel{
		widget.NewSubmenu("option", "Option", []widget.MenuItemModel{
			widget.NewSubmenu("km", "Keymaps", []widget.MenuItemModel{
				widget.NewCommand("vim", "Vim", nil),
			}),
		}),
	}
	m := widget.NewMenu()
	h, _ := barFixture(t, m, model, 50, 14)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()

	// One turn: queue the nested open, then hand SetModel something it must
	// refuse. Duplicate ids are refused by validateItems.
	var setErr error
	h.onLoop(func() {
		_ = m.Open("option")
		_ = m.Open("km")
		setErr = m.SetModel([]widget.MenuItemModel{
			widget.NewCommand("dup", "One", nil),
			widget.NewCommand("dup", "Two", nil),
		})
	})
	h.settle()
	h.settle()

	if setErr == nil {
		t.Fatal("SetModel accepted duplicate ids; the fixture cannot show what a " +
			"rejected model leaves behind")
	}
	if n := openLevelsOn(t, h, m); n != 2 {
		t.Errorf("OpenLevels() = %d after a REFUSED SetModel, want 2 — the old "+
			"model is still live, so the open queued against it is still valid\n%s",
			n, h.grid())
	}
}
