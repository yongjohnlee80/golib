package widget_test

// WHAT A MENU AND A DIALOG LOOK LIKE.
//
// These are presentation contracts, and presentation is exactly the area where
// a test that only checks "the label is somewhere on screen" passes through a
// redesign that made the widget unusable. Each test below therefore reads the
// CELLS — which attributes a row was painted with, which column a row starts
// in, which line the title is on — and each carries the opposite case as a
// control, so a change that switches everything off cannot pass.

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// barFixture is a horizontal bar plus a second focusable, so focus has
// somewhere to be that is not the menu. Docked rather than stacked: a stack
// puts both layers at the same origin and the button paints over the bar.
func barFixture(t *testing.T, m *widget.Menu, items []widget.MenuItemModel, w, h int) (*harness, *widget.Button) {
	t.Helper()
	bar := widget.NewMenuBar(m, widget.WithBarPlacement(widget.BarPlacementTop))
	other := widget.NewButton("ELSEWHERE")
	dock := tui.NewDock()
	dock.Pin(tui.DockTop, bar)
	dock.Add(other)
	host := widget.NewOverlayHost(dock)
	hh := startApp(t, host, w, h)
	hh.onLoop(func() {
		if err := m.SetModel(items); err != nil {
			t.Fatalf("SetModel: %v", err)
		}
	})
	hh.settle()
	return hh, other
}

// twoCategories is the smallest bar that can show ordering and pegging.
func twoCategories(pegHelp bool) []widget.MenuItemModel {
	file := widget.NewSubmenu("file", "File", []widget.MenuItemModel{
		widget.NewCommand("new", "New", nil),
	})
	help := widget.NewSubmenu("help", "Help", []widget.MenuItemModel{
		widget.NewCommand("about", "About", nil),
	})
	help.PegRight = pegHelp
	return []widget.MenuItemModel{file, help}
}

// TestAnUnfocusedMenuDoesNotPaintItsSelectionAsActive.
//
// THE DEFECT THIS EXISTS FOR: a menu bar keeps a selected row whether or not it
// has focus, and painting that row as the active one tells the user the
// keyboard is in the menu when it is in the document. There is no second cue to
// correct the impression — the caret is elsewhere on screen, and the bar looks
// exactly as it does when it IS being driven.
//
// Read from the cells rather than from a style object, because what matters is
// what was painted. The focused case is the control: if BOTH looked like the
// ordinary surface the widget would be equally broken in the other direction,
// and an assertion naming only the unfocused case would call that a pass.
func TestAnUnfocusedMenuDoesNotPaintItsSelectionAsActive(t *testing.T) {
	m := widget.NewMenu()
	h, other := barFixture(t, m, twoCategories(false), 50, 6)
	defer h.stop()

	h.onLoop(func() { m.Select("file") })
	h.settle()

	// Where "File" sits, and what an ordinary unselected row looks like, taken
	// from the OTHER category on the same line.
	fx, fy := cellOfLabel(t, h, "File")
	hx, _ := cellOfLabel(t, h, "Help")

	h.onLoop(func() { other.Context().RequestFocus() })
	h.settle()
	blurSel := rowStyleAt(t, h, fx, fy)
	blurOrd := rowStyleAt(t, h, hx, fy)
	if blurSel != blurOrd {
		t.Errorf("with focus elsewhere the selected row is painted %+v while an "+
			"ordinary row is %+v; the bar claims the keyboard is in the menu\n%s",
			blurSel, blurOrd, h.grid())
	}

	// THE CONTROL: with focus in the menu the selection must be visible, or the
	// assertion above is satisfied by a widget that never highlights anything.
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()
	focSel := rowStyleAt(t, h, fx, fy)
	focOrd := rowStyleAt(t, h, hx, fy)
	if focSel == focOrd {
		t.Errorf("with focus in the menu the selected row is painted exactly like "+
			"an ordinary one (%+v); the selection is invisible\n%s", focSel, h.grid())
	}
}

// TestABarDrawsNoSubmenuMarkerUnlessAsked.
//
// Every entry in a menu bar opens a dropdown, so an arrow on each one repeats
// what the bar already is and costs two cells per entry. Inside a popup the
// marker earns its place, distinguishing rows that cascade from rows that act —
// so the default is off in the bar and always on below it.
func TestABarDrawsNoSubmenuMarkerUnlessAsked(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		m := widget.NewMenu()
		h, _ := barFixture(t, m, twoCategories(false), 50, 6)
		defer h.stop()
		if line := h.row(0); strings.Contains(line, "▸") {
			t.Errorf("the bar drew a submenu marker with no option set: %q", line)
		}
	})

	t.Run("on when asked", func(t *testing.T) {
		m := widget.NewMenu(widget.WithBarSubmenuMarker(true))
		h, _ := barFixture(t, m, twoCategories(false), 50, 6)
		defer h.stop()
		if line := h.row(0); !strings.Contains(line, "▸") {
			t.Errorf("WithBarSubmenuMarker(true) drew no marker: %q", line)
		}
	})

	t.Run("a popup always marks its submenu rows", func(t *testing.T) {
		m := widget.NewMenu()
		items := []widget.MenuItemModel{
			widget.NewSubmenu("top", "Top", []widget.MenuItemModel{
				widget.NewSubmenu("deeper", "Deeper", []widget.MenuItemModel{
					widget.NewCommand("leaf", "Leaf", nil),
				}),
			}),
		}
		h, _ := barFixture(t, m, items, 50, 10)
		defer h.stop()
		h.onLoop(func() {
			if err := m.Open("top"); err != nil {
				t.Fatalf("Open: %v", err)
			}
		})
		h.settle()
		h.settle()
		if !strings.Contains(h.grid(), "▸") {
			t.Errorf("a submenu row inside a dropdown has no marker:\n%s", h.grid())
		}
		if !strings.Contains(h.grid(), "Deeper") {
			t.Errorf("the marker ate the label:\n%s", h.grid())
		}
	})
}

// TestAPeggedRowSitsAtTheFarEndOfTheBar.
//
// Help at the right is a convention old enough that its absence reads as a bug.
// The alternative a consumer is left with otherwise is a spacer row it has to
// resize itself on every layout.
func TestAPeggedRowSitsAtTheFarEndOfTheBar(t *testing.T) {
	const width = 50

	m := widget.NewMenu()
	h, _ := barFixture(t, m, twoCategories(true), width, 6)
	defer h.stop()

	hx, _ := cellOfLabel(t, h, "Help")
	fx, _ := cellOfLabel(t, h, "File")
	if fx != 1 {
		t.Errorf("File starts at column %d, want 1 — the leading run should be "+
			"unaffected by pegging\n%s", fx, h.grid())
	}
	// Far end, not merely "after File": a row that simply followed would sit a
	// few columns in, which is what the old behaviour did.
	if hx < width-10 {
		t.Errorf("Help starts at column %d of %d, which is not the far end\n%s",
			hx, width, h.grid())
	}

	// THE CONTROL: without the flag the same model packs Help against File, so
	// the assertion above is about pegging and not about the width of the bar.
	m2 := widget.NewMenu()
	h2, _ := barFixture(t, m2, twoCategories(false), width, 6)
	defer h2.stop()
	if hx2, _ := cellOfLabel(t, h2, "Help"); hx2 >= width-10 {
		t.Errorf("an unpegged Help also sits at column %d; pegging is not what "+
			"moved it\n%s", hx2, h2.grid())
	}
}

// TestAPeggedRowDoesNotOverlapTheRowBeforeIt.
//
// The failure mode a naive implementation has: place the leading run first,
// then discover the pegged row needs columns that are already taken. Squeezing
// the bar until the two must collide is the case that exposes it.
func TestAPeggedRowDoesNotOverlapTheRowBeforeIt(t *testing.T) {
	m := widget.NewMenu()
	h, _ := barFixture(t, m, twoCategories(true), 16, 6)
	defer h.stop()

	line := h.row(0)
	// Both labels are short; at 16 columns they cannot both fit with the usual
	// padding, and the leading run must yield rather than be painted over.
	if strings.Count(line, "Help") > 1 {
		t.Errorf("Help appears twice, so the pegged row was painted over itself: %q", line)
	}
	if i, j := strings.Index(line, "File"), strings.Index(line, "Help"); i >= 0 && j >= 0 && i+4 > j {
		t.Errorf("File and Help overlap in %q", line)
	}
}

// TestADialogTitleSitsOnItsBorder.
//
// The title used to be painted on the first row INSIDE the frame, which spends
// a content line on a banner and reads as a highlighted first paragraph rather
// than as the dialog's name. On the border is where a framed panel is titled
// everywhere else.
func TestADialogTitleSitsOnItsBorder(t *testing.T) {
	md := widget.NewModal(widget.NewText("Are you sure to quit?"),
		widget.WithModalTitle("Exit Confirmation"),
		widget.WithButtons(widget.NewButton("Yes"), widget.NewButton("No")))
	host := widget.NewOverlayHost(widget.NewText(""))
	h := startApp(t, host, 56, 12)
	defer h.stop()
	h.onLoop(func() {
		if err := md.Open(host); err != nil {
			t.Fatalf("Open: %v", err)
		}
	})
	h.settle()
	h.settle()

	grid := h.grid()
	titleRow := rowContaining(grid, "Exit Confirmation")
	bodyRow := rowContaining(grid, "Are you sure to quit?")
	if titleRow < 0 || bodyRow < 0 {
		t.Fatalf("title or body missing:\n%s", grid)
	}
	// The title shares its line with the frame's top rule.
	if line := strings.Split(grid, "\n")[titleRow]; !strings.Contains(line, "─") {
		t.Errorf("the title's line carries no border rule, so it is a content "+
			"row rather than the frame: %q\n%s", line, grid)
	}
	// And the body is the first CONTENT line: the border's line, then the card's
	// one-cell pad, then the message. Two lines below the title rather than one,
	// because the pad is deliberate — it is what stops the text touching the
	// frame. Three or more would mean the title had kept a content row of its
	// own on top of the padding, which is the defect.
	if bodyRow != titleRow+2 {
		t.Errorf("the body is on line %d and the title on %d; want the border "+
			"line, one pad line, then the body\n%s", bodyRow, titleRow, grid)
	}
}

// TestADialogSeparatesItsMessageFromItsButtons.
//
// Without the blank line the controls sit directly under the last line of prose
// and read as part of the sentence.
func TestADialogSeparatesItsMessageFromItsButtons(t *testing.T) {
	md := widget.NewModal(widget.NewText("Are you sure to quit?"),
		widget.WithModalTitle("Exit Confirmation"),
		widget.WithButtons(widget.NewButton("Yes"), widget.NewButton("No")))
	host := widget.NewOverlayHost(widget.NewText(""))
	h := startApp(t, host, 56, 12)
	defer h.stop()
	h.onLoop(func() {
		if err := md.Open(host); err != nil {
			t.Fatalf("Open: %v", err)
		}
	})
	h.settle()
	h.settle()

	grid := h.grid()
	bodyRow := rowContaining(grid, "Are you sure to quit?")
	btnRow := rowContaining(grid, "Yes")
	if bodyRow < 0 || btnRow < 0 {
		t.Fatalf("body or buttons missing:\n%s", grid)
	}
	if btnRow != bodyRow+2 {
		t.Errorf("the buttons are on line %d and the message on %d; want one blank "+
			"line between them\n%s", btnRow, bodyRow, grid)
	}
}

// TestADialogPlacesItsButtonsWhereItWasTold.
//
// Centred by default. The three alignments are compared against each other
// rather than against fixed columns, so the test states the ORDERING the option
// promises and survives a change in padding.
func TestADialogPlacesItsButtonsWhereItWasTold(t *testing.T) {
	col := func(t *testing.T, opts ...widget.ModalOption) (x, cardX, cardW int) {
		t.Helper()
		base := []widget.ModalOption{
			widget.WithModalTitle("A Dialog With A Long Title"),
			widget.WithButtons(widget.NewButton("Yes"), widget.NewButton("No")),
		}
		md := widget.NewModal(widget.NewText("short"), append(base, opts...)...)
		host := widget.NewOverlayHost(widget.NewText(""))
		h := startApp(t, host, 64, 12)
		defer h.stop()
		h.onLoop(func() {
			if err := md.Open(host); err != nil {
				t.Fatalf("Open: %v", err)
			}
		})
		h.settle()
		h.settle()
		grid := h.grid()
		bx, _ := cellOfLabel(t, h, "Yes")
		line := strings.Split(grid, "\n")[rowContaining(grid, "─")]
		return bx, strings.Index(line, "┌"), strings.Count(line, "─")
	}

	centred, cx, cw := col(t)
	left, _, _ := col(t, widget.WithButtonAlign(widget.ButtonsLeft))
	right, _, _ := col(t, widget.WithButtonAlign(widget.ButtonsRight))

	if !(left < centred && centred < right) {
		t.Errorf("button columns are left=%d centred=%d right=%d; want strictly "+
			"increasing (card starts at %d, %d wide)", left, centred, right, cx, cw)
	}
	// Centred is the ZERO VALUE, so a caller who says nothing gets it.
	explicit, _, _ := col(t, widget.WithButtonAlign(widget.ButtonsCenter))
	if explicit != centred {
		t.Errorf("explicit ButtonsCenter put the buttons at %d but the default put "+
			"them at %d; the zero value is not the documented default",
			explicit, centred)
	}
}

// TestAnUndeclaredButtonAlignIsRefused keeps the closed set closed, matching
// every other enum option in this package.
func TestAnUndeclaredButtonAlignIsRefused(t *testing.T) {
	if f := fatalFromWidgetExt(func() {
		widget.NewModal(widget.NewText("x"),
			widget.WithButtonAlign(widget.ButtonsRight+1))
	}); f == nil {
		t.Error("an out-of-range ButtonAlign was accepted")
	}
	// The control: every declared value is accepted.
	for _, a := range []widget.ButtonAlign{
		widget.ButtonsCenter, widget.ButtonsLeft, widget.ButtonsRight,
	} {
		if f := fatalFromWidgetExt(func() {
			widget.NewModal(widget.NewText("x"), widget.WithButtonAlign(a))
		}); f != nil {
			t.Errorf("ButtonAlign %v was refused: %v", a, f)
		}
	}
}

// rowContaining is the index of the first line holding sub, or -1.
func rowContaining(grid, sub string) int {
	for i, line := range strings.Split(grid, "\n") {
		if strings.Contains(line, sub) {
			return i
		}
	}
	return -1
}

// TestOpeningACategoryClosesTheOneBeforeIt.
//
// THE DEFECT: opening a level appended to the cascade, so moving from File to
// Help left File's dropdown on screen beside Help's, and a third category added
// a third. What is on screen then disagrees with what the model calls open, and
// the arrow keys drive one cascade while the user is reading another.
//
// Driven through Open, and then again through a real pointer click, because the
// two reach openLevel by different routes and only one of them was exercised.
func TestOpeningACategoryClosesTheOneBeforeIt(t *testing.T) {
	model := []widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", nil),
		}),
		widget.NewSubmenu("help", "Help", []widget.MenuItemModel{
			widget.NewCommand("about", "About", nil),
		}),
	}

	t.Run("through Open", func(t *testing.T) {
		m := widget.NewMenu()
		h, _ := barFixture(t, m, model, 50, 10)
		defer h.stop()

		h.onLoop(func() {
			if err := m.Open("file"); err != nil {
				t.Fatalf("Open(file): %v", err)
			}
		})
		h.settle()
		h.settle()
		if n := openLevelsOn(t, h, m); n != 1 {
			t.Fatalf("OpenLevels() = %d after opening File, want 1", n)
		}

		h.onLoop(func() {
			if err := m.Open("help"); err != nil {
				t.Fatalf("Open(help): %v", err)
			}
		})
		h.settle()
		h.settle()
		if n := openLevelsOn(t, h, m); n != 1 {
			t.Errorf("OpenLevels() = %d after moving to Help, want 1", n)
		}
		grid := h.grid()
		if strings.Contains(grid, "New") {
			t.Errorf("File's dropdown is still on screen beside Help's:\n%s", grid)
		}
		if !strings.Contains(grid, "About") {
			t.Errorf("Help's dropdown did not open:\n%s", grid)
		}
	})

	t.Run("through a pointer click", func(t *testing.T) {
		m := widget.NewMenu()
		h, _ := barFixture(t, m, model, 50, 10)
		defer h.stop()

		click := func(label string) {
			t.Helper()
			x, y := cellOfLabel(t, h, label)
			h.inject(
				tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y},
				tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y},
			)
			h.settle()
			h.settle()
		}

		click("File")
		if n := openLevelsOn(t, h, m); n != 1 {
			t.Fatalf("OpenLevels() = %d after clicking File, want 1", n)
		}
		click("Help")
		if n := openLevelsOn(t, h, m); n != 1 {
			t.Errorf("OpenLevels() = %d after clicking Help, want 1 — a click on "+
				"another category must close the open one\n%s", n, h.grid())
		}
		if grid := h.grid(); strings.Contains(grid, "New") {
			t.Errorf("File's dropdown survived a click on Help:\n%s", grid)
		}
	})

	t.Run("a nested level keeps its parent", func(t *testing.T) {
		nested := []widget.MenuItemModel{
			widget.NewSubmenu("opt", "Option", []widget.MenuItemModel{
				widget.NewSubmenu("km", "Keymaps", []widget.MenuItemModel{
					widget.NewCommand("vim", "Vim", nil),
				}),
			}),
		}
		m := widget.NewMenu()
		h, _ := barFixture(t, m, nested, 50, 12)
		defer h.stop()

		h.onLoop(func() { _ = m.Open("opt") })
		h.settle()
		h.onLoop(func() { _ = m.Open("km") })
		h.settle()
		h.settle()
		// THE CONTROL for the two cases above: truncation must not be so eager
		// that a cascade cannot exist at all.
		if n := openLevelsOn(t, h, m); n != 2 {
			t.Errorf("OpenLevels() = %d for a two-deep cascade, want 2\n%s", n, h.grid())
		}
		if grid := h.grid(); !strings.Contains(grid, "Keymaps") || !strings.Contains(grid, "Vim") {
			t.Errorf("the cascade lost a level:\n%s", grid)
		}
	})
}

// TestALockKeyDoesNotDisableTheKeyboard.
//
// THE DEFECT, and it is invisible in most test setups. Under the kitty keyboard
// protocol a terminal reports Caps Lock and Num Lock as MODIFIER BITS, set on
// every keystroke while the lock is engaged. A binding that asks `Mods != 0`
// therefore rejects every arrow, Enter and mnemonic the moment Num Lock is on —
// which is its resting state on most keyboards.
//
// It cannot be reproduced under tmux or any terminal still speaking the legacy
// sequences, because those cannot encode a lock bit at all. That is exactly why
// it reached a user: it works on the developer's setup and the widget appears
// to have stopped responding to the keyboard on theirs.
func TestALockKeyDoesNotDisableTheKeyboard(t *testing.T) {
	for _, mods := range []tui.Mods{
		0,
		tui.ModNumLock,
		tui.ModCapsLock,
		tui.ModNumLock | tui.ModCapsLock,
	} {
		t.Run(mods.String(), func(t *testing.T) {
			m := widget.NewMenu()
			h, _ := barFixture(t, m, twoCategories(false), 50, 10)
			defer h.stop()
			h.onLoop(func() { m.Context().RequestFocus() })
			h.settle()
			h.onLoop(func() { m.Select("file") })
			h.settle()

			// Right steps along the bar.
			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight, Mods: mods})
			h.settle()
			var sel widget.ItemID
			h.onLoop(func() { sel, _ = m.Selected() })
			if sel != "help" {
				t.Errorf("Right with mods %v left the selection on %q; a lock key "+
					"is not a chord and must not disable the binding", mods, sel)
			}

			// And Enter still activates.
			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter, Mods: mods})
			h.settle()
			h.settle()
			if n := openLevelsOn(t, h, m); n != 1 {
				t.Errorf("Enter with mods %v opened %d levels, want 1", mods, n)
			}
		})
	}
}

// TestARealModifierIsStillRejected is the control for the test above: masking
// the lock bits must not turn every chord into a plain keystroke, or Ctrl-Right
// would step the menu while the user meant it for something else.
func TestARealModifierIsStillRejected(t *testing.T) {
	m := widget.NewMenu()
	h, _ := barFixture(t, m, twoCategories(false), 50, 10)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()
	h.onLoop(func() { m.Select("file") })
	h.settle()

	for _, mods := range []tui.Mods{tui.ModCtrl, tui.ModAlt, tui.ModCtrl | tui.ModNumLock} {
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight, Mods: mods})
		h.settle()
		var sel widget.ItemID
		h.onLoop(func() { sel, _ = m.Selected() })
		if sel != "file" {
			t.Errorf("Right with %v moved the selection to %q; a real chord is not "+
				"the menu's binding", mods, sel)
		}
	}
}

// TestLeftAndRightWalkTheBarWhileADropdownIsOpen.
//
// With a level open, Right was bound to "cascade into a submenu" and did
// NOTHING on an ordinary row, while Left closed the level — so the bar felt
// half-wired: one direction responded and the other was dead. Walking the
// categories with a dropdown following along is what a menu bar has always
// done.
func TestLeftAndRightWalkTheBarWhileADropdownIsOpen(t *testing.T) {
	m := widget.NewMenu()
	h, _ := barFixture(t, m, twoCategories(false), 50, 12)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()
	h.onLoop(func() {
		if err := m.Open("file"); err != nil {
			t.Fatalf("Open: %v", err)
		}
	})
	h.settle()
	h.settle()
	if !strings.Contains(h.grid(), "New") {
		t.Fatalf("File's dropdown did not open:\n%s", h.grid())
	}

	// Right moves to the next category AND brings the dropdown with it.
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight})
	h.settle()
	h.settle()
	grid := h.grid()
	if !strings.Contains(grid, "About") {
		t.Errorf("Right did not open the next category:\n%s", grid)
	}
	if strings.Contains(grid, "New") {
		t.Errorf("Right left the previous dropdown open:\n%s", grid)
	}
	if n := openLevelsOn(t, h, m); n != 1 {
		t.Errorf("OpenLevels() = %d after stepping the bar, want 1", n)
	}

	// Left comes back, so the two directions are symmetric.
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyLeft})
	h.settle()
	h.settle()
	if grid := h.grid(); !strings.Contains(grid, "New") {
		t.Errorf("Left did not walk back to File:\n%s", grid)
	}
}

// TestLeftStillClosesANestedLevel is the control: making Left walk the bar must
// not cost the cascade its way back out.
func TestLeftStillClosesANestedLevel(t *testing.T) {
	nested := []widget.MenuItemModel{
		widget.NewSubmenu("opt", "Option", []widget.MenuItemModel{
			widget.NewSubmenu("km", "Keymaps", []widget.MenuItemModel{
				widget.NewCommand("vim", "Vim", nil),
			}),
		}),
	}
	m := widget.NewMenu()
	h, _ := barFixture(t, m, nested, 50, 12)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()
	h.onLoop(func() { _ = m.Open("opt") })
	h.settle()
	h.onLoop(func() { _ = m.Open("km") })
	h.settle()
	h.settle()
	if n := openLevelsOn(t, h, m); n != 2 {
		t.Fatalf("OpenLevels() = %d, want a two-deep cascade", n)
	}

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyLeft})
	h.settle()
	h.settle()
	if n := openLevelsOn(t, h, m); n != 1 {
		t.Errorf("OpenLevels() = %d after Left inside a cascade, want 1 — Left must "+
			"still step back out before it walks the bar", n)
	}
}
