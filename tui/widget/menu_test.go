package widget_test

// A Menu's rows are DATA, not nodes, and almost every interesting failure comes
// from that. The runtime cannot hit-test a row, cannot arm one, and cannot tell
// two rows of the same menu apart — so the Menu does all three itself, and these
// tests go after the places where its own answer could drift from the runtime's:
// where a row was painted versus where a click lands, which level owns the keys,
// and whether a keyboard activation is the same event a mouse one produces.

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// saveAction is a distinguishable action for a row.
type saveAction struct{ id string }

func (saveAction) ActionID() tui.ActionID { return "test.save" }

// menuFixture mounts a Menu inside an OverlayHost — the arrangement a cascading
// menu actually needs, since its levels are anchored overlay layers.
func menuFixture(t *testing.T, m *widget.Menu, w, h int) (*harness, *widget.OverlayHost) {
	t.Helper()
	host := widget.NewOverlayHost(m)
	hh := startApp(t, host, w, h)
	hh.onLoop(func() { m.Context().RequestFocus() })
	hh.settle()
	return hh, host
}

// openLevelsOn and selectedOn read loop-owned Menu state the sanctioned way.
//
// Component state belongs to the event loop, and that ownership is exactly what
// lets widget authors write mutex-free Go. A test that reads it directly is the
// one thing wrong in that arrangement, and -race says so.
func openLevelsOn(t *testing.T, h *harness, m *widget.Menu) int {
	t.Helper()
	var n int
	h.onLoop(func() { n = m.OpenLevels() })
	return n
}

func selectedOn(t *testing.T, h *harness, m *widget.Menu) widget.ItemID {
	t.Helper()
	var id widget.ItemID
	h.onLoop(func() { id, _ = m.Selected() })
	return id
}

func modelOn(t *testing.T, h *harness, m *widget.Menu) []widget.MenuItemModel {
	t.Helper()
	var items []widget.MenuItemModel
	h.onLoop(func() { items = m.Model() })
	return items
}

// ─── the model ───────────────────────────────────────────────────────────────

// TestTheZeroRowIsInertRatherThanAnEnabledNoOp.
//
// A zero MenuItemModel appearing in a menu as a blank, clickable, do-nothing row
// is the failure mode of a permissive zero value. It shows nothing and does
// nothing instead — and the constructors are what opt a row in.
func TestTheZeroRowIsInertRatherThanAnEnabledNoOp(t *testing.T) {
	var zero widget.MenuItemModel
	if zero.Kind != widget.ItemKindCommand {
		t.Errorf("zero Kind = %v, want command", zero.Kind)
	}
	if zero.Enabled || zero.Visible {
		t.Error("the zero row is enabled or visible; it must be inert until a constructor opts it in")
	}
	built := widget.NewCommand("c", "Copy", nil)
	if !built.Enabled || !built.Visible {
		t.Error("NewCommand did not produce an enabled, visible row")
	}
	if sep := widget.NewSeparator("s"); sep.Enabled {
		t.Error("a separator is enabled; it can never be selected, and marking it " +
			"enabled invites code to treat Enabled as the only selectability test")
	}
}

// TestSetModelRejectsWhatCannotBeAddressed.
//
// Every operation this widget offers names a row by ID alone. An empty or
// repeated ID makes those ambiguous, and an ambiguous mutation is worse than a
// rejected model because it silently picks one.
func TestSetModelRejectsWhatCannotBeAddressed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		leaf  error
		items []widget.MenuItemModel
	}{
		{"an empty ID", widget.ErrEmptyItemID, []widget.MenuItemModel{
			widget.NewCommand("", "Nameless", nil),
		}},
		{"a duplicate at one level", widget.ErrDuplicateItemID, []widget.MenuItemModel{
			widget.NewCommand("a", "One", nil),
			widget.NewCommand("a", "Two", nil),
		}},
		{"a duplicate across levels", widget.ErrDuplicateItemID, []widget.MenuItemModel{
			widget.NewCommand("a", "One", nil),
			widget.NewSubmenu("s", "More", []widget.MenuItemModel{
				widget.NewCommand("a", "Deep", nil),
			}),
		}},
		{"an undeclared kind", widget.ErrUnknownItemKind, []widget.MenuItemModel{
			{ID: "x", Kind: widget.ItemKindRadio + 1, Visible: true, Enabled: true},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := widget.NewMenu()
			good := []widget.MenuItemModel{widget.NewCommand("keep", "Keep", nil)}
			if err := m.SetModel(good); err != nil {
				t.Fatalf("the control model was rejected: %v", err)
			}

			err := m.SetModel(tc.items)
			if !errors.Is(err, tc.leaf) {
				t.Errorf("errors.Is(err, %v) = false; err = %v", tc.leaf, err)
			}
			if !errors.Is(err, widget.ErrInvalidMenuModel) {
				t.Errorf("errors.Is(err, ErrInvalidMenuModel) = false; err = %v", err)
			}
			// Nothing changed: the old model is still there.
			if got := m.Model(); len(got) != 1 || got[0].ID != "keep" {
				t.Errorf("a rejected SetModel changed the model to %v", got)
			}
		})
	}
}

// TestTheModelIsAValueOnBothSides.
//
// The Menu deep-copies on ingest and hands out copies, so a caller mutating its
// own slice cannot reach inside a mounted widget and the widget cannot leak its
// storage. Children are the part a shallow copy gets wrong.
func TestTheModelIsAValueOnBothSides(t *testing.T) {
	children := []widget.MenuItemModel{widget.NewCommand("deep", "Deep", nil)}
	items := []widget.MenuItemModel{widget.NewSubmenu("s", "More", children)}

	m := widget.NewMenu()
	if err := m.SetModel(items); err != nil {
		t.Fatalf("SetModel: %v", err)
	}

	// Mutate the caller's own data, at both levels.
	items[0].Label = "CHANGED"
	children[0].Label = "CHANGED"

	got := m.Model()
	if got[0].Label != "More" {
		t.Error("mutating the caller's slice reached the menu's own model")
	}
	if got[0].Children[0].Label != "Deep" {
		t.Error("mutating the caller's CHILDREN slice reached the menu's model; " +
			"the copy was shallow")
	}

	// And the other direction: writing through what Model() returned.
	got[0].Children[0].Label = "ALSO CHANGED"
	if again := m.Model(); again[0].Children[0].Label != "Deep" {
		t.Error("writing through Model()'s result reached the menu's own storage")
	}
}

// ─── selection and keys ──────────────────────────────────────────────────────

// TestArrowsSkipWhatCannotBeSelected.
//
// Separators, disabled rows and hidden rows are all unselectable, for three
// different reasons. Traversal that checked only Enabled would land on a
// separator, which is the bug this pins.
func TestArrowsSkipWhatCannotBeSelected(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("open", "Open", nil),
		widget.NewSeparator("sep"),
		disabled(widget.NewCommand("save", "Save", nil)),
		hidden(widget.NewCommand("ghost", "Ghost", nil)),
		widget.NewCommand("quit", "Quit", nil),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 12)
	defer h.stop()

	if id := selectedOn(t, h, m); id != "open" {
		t.Fatalf("initial selection is %q, want the first selectable row", id)
	}
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown})
	h.settle()
	if id := selectedOn(t, h, m); id != "quit" {
		t.Errorf("Down selected %q, want quit — separator, disabled and hidden rows "+
			"are all skipped", id)
	}
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown})
	h.settle()
	if id := selectedOn(t, h, m); id != "open" {
		t.Errorf("Down from the last row selected %q, want a wrap to open", id)
	}
}

// TestSelectRefusesAnUnselectableRow.
func TestSelectRefusesAnUnselectableRow(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("ok", "OK", nil),
		widget.NewSeparator("sep"),
		disabled(widget.NewCommand("no", "No", nil)),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	for _, id := range []widget.ItemID{"sep", "no", "absent"} {
		if m.Select(id) {
			t.Errorf("Select(%q) succeeded", id)
		}
	}
	if got, _ := m.Selected(); got != "ok" {
		t.Errorf("a refused Select moved the selection to %q", got)
	}
}

// ─── activation ──────────────────────────────────────────────────────────────

// TestActivationOrdersStateThenActionThenClose.
//
// The order is observable and matters: a check handler that reads its own row
// must see the toggle that triggered it. Reversing them would make every such
// handler read the previous state.
func TestActivationOrdersStateThenActionThenClose(t *testing.T) {
	var sawChecked atomic.Bool
	var ran atomic.Int64

	// The executor reads the row back through the menu, which is what an
	// application handler does. Declared before the Menu so the closure can
	// capture it; assigned after, because it needs the Menu.
	var m *widget.Menu
	m = widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool {
		ran.Add(1)
		for _, it := range m.Model() {
			if it.ID == "wrap" {
				sawChecked.Store(it.Checked)
			}
		}
		return true
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCheck("wrap", "Wrap lines", saveAction{}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}

	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.waitFor("the executor ran", func() bool { return ran.Load() == 1 })
	h.settle()

	if !sawChecked.Load() {
		t.Error("the action observed the OLD checked state; the toggle must happen " +
			"before the action, or every handler reading its own row is wrong")
	}
	var after bool
	h.onLoop(func() {
		for _, it := range m.Model() {
			if it.ID == "wrap" {
				after = it.Checked
			}
		}
	})
	if !after {
		t.Error("the toggle did not stick")
	}
}

// TestAnActivationEventIsEmittedEvenWhenNothingHandledIt.
//
// A command whose action was nil or refused still happened as far as the user is
// concerned, so a listener counting activations must see it — and the Handled
// flag is how the listener tells the two apart. The menu does NOT close on an
// unhandled activation, because a command that could not run must not look like
// one that did.
func TestAnActivationEventIsEmittedEvenWhenNothingHandledIt(t *testing.T) {
	for _, tc := range []struct {
		name        string
		exec        func(tui.ActionInvocation) bool
		action      tui.Action
		wantHandled bool
	}{
		{"handled", func(tui.ActionInvocation) bool { return true }, saveAction{}, true},
		{"refused by the executor", func(tui.ActionInvocation) bool { return false }, saveAction{}, false},
		{"no action on the row", func(tui.ActionInvocation) bool { return true }, nil, false},
		{"no executor at all", nil, saveAction{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := widget.NewMenu(widget.WithActionExecutor(tc.exec))
			if err := m.SetModel([]widget.MenuItemModel{
				widget.NewCommand("go", "Go", tc.action),
			}); err != nil {
				t.Fatalf("SetModel: %v", err)
			}
			h, _ := menuFixture(t, m, 30, 10)
			defer h.stop()

			var mu sync.Mutex
			var events []widget.MenuActivatedEvent
			unsub := tui.Subscribe(h.app.Bus(), func(ev widget.MenuActivatedEvent) {
				mu.Lock()
				events = append(events, ev)
				mu.Unlock()
			})
			defer unsub()

			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
			h.waitFor("one activation event", func() bool {
				mu.Lock()
				defer mu.Unlock()
				return len(events) == 1
			})
			h.settle()

			mu.Lock()
			got := append([]widget.MenuActivatedEvent(nil), events...)
			mu.Unlock()
			if len(got) != 1 {
				t.Fatalf("%d activation events, want exactly 1", len(got))
			}
			if got[0].ItemID != "go" {
				t.Errorf("event names %q, want go", got[0].ItemID)
			}
			if got[0].Handled != tc.wantHandled {
				t.Errorf("Handled = %v, want %v", got[0].Handled, tc.wantHandled)
			}
			if got[0].Origin != tui.OriginKey {
				t.Errorf("Origin = %v, want OriginKey", got[0].Origin)
			}
		})
	}
}

// TestASeparatorCannotBeActivated.
func TestASeparatorCannotBeActivated(t *testing.T) {
	var ran atomic.Int64
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool {
		ran.Add(1)
		return true
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSeparator("sep"),
		widget.NewCommand("go", "Go", saveAction{}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()

	var events atomic.Int64
	unsub := tui.Subscribe(h.app.Bus(), func(widget.MenuActivatedEvent) { events.Add(1) })
	defer unsub()

	// The selection cannot rest on the separator, so activate it by name.
	h.onLoop(func() { m.Context().DoAction(widget.MenuActivateAction{ItemID: "sep"}) })
	h.settle()
	h.settle()

	if events.Load() != 0 || ran.Load() != 0 {
		t.Errorf("a separator produced %d events and %d executor calls",
			events.Load(), ran.Load())
	}
}

// TestRadioActivationClearsItsGroupEverywhere.
//
// A group split across levels is unusual but expressible, and honouring
// exclusivity only within one level would leave two members checked — which is
// exactly the state a radio group exists to prevent.
func TestRadioActivationClearsItsGroupEverywhere(t *testing.T) {
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool { return true }))
	if err := m.SetModel([]widget.MenuItemModel{
		checked(widget.NewRadio("light", "Light", "theme", saveAction{})),
		widget.NewSubmenu("more", "More", []widget.MenuItemModel{
			widget.NewRadio("dark", "Dark", "theme", saveAction{}),
		}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 12)
	defer h.stop()

	h.onLoop(func() { m.SetChecked("dark", true) })
	h.settle()

	for _, it := range flatten(modelOn(t, h, m)) {
		switch it.ID {
		case "dark":
			if !it.Checked {
				t.Error("the newly set radio is not checked")
			}
		case "light":
			if it.Checked {
				t.Error("a radio in another LEVEL of the same group stayed checked")
			}
		}
	}
}

// ─── cascading levels ────────────────────────────────────────────────────────

// TestOpeningASubmenuAnchorsALevelAndMovesTheKeys.
func TestOpeningASubmenuAnchorsALevelAndMovesTheKeys(t *testing.T) {
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool { return true }))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", saveAction{}),
			widget.NewCommand("open", "Open", saveAction{}),
		}),
		widget.NewCommand("quit", "Quit", saveAction{}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 40, 14)
	defer h.stop()

	var err error
	h.onLoop(func() { err = m.Open("file") })
	h.settle()
	h.settle()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if openLevelsOn(t, h, m) != 1 {
		t.Fatalf("OpenLevels() = %d, want 1", openLevelsOn(t, h, m))
	}
	if got := h.grid(); !strings.Contains(got, "New") || !strings.Contains(got, "Open") {
		t.Errorf("the submenu's rows are not on screen:\n%s", got)
	}
	// The open level owns the selection and the keys.
	if id := selectedOn(t, h, m); id != "new" {
		t.Errorf("selection is %q, want the new level's first row", id)
	}
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown})
	h.settle()
	if id := selectedOn(t, h, m); id != "open" {
		t.Errorf("Down moved to %q; the keys did not follow the open level", id)
	}
}

// TestLeftClosesOneLevelAndEscapeClosesAll.
func TestLeftClosesOneLevelAndEscapeClosesAll(t *testing.T) {
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool { return true }))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("a", "A", []widget.MenuItemModel{
			widget.NewSubmenu("b", "B", []widget.MenuItemModel{
				widget.NewCommand("deep", "Deep", saveAction{}),
			}),
		}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 40, 14)
	defer h.stop()

	h.onLoop(func() {
		if err := m.Open("a"); err != nil {
			t.Errorf("Open a: %v", err)
		}
	})
	h.settle()
	h.onLoop(func() {
		if err := m.Open("b"); err != nil {
			t.Errorf("Open b: %v", err)
		}
	})
	h.settle()
	h.settle()
	if openLevelsOn(t, h, m) != 2 {
		t.Fatalf("OpenLevels() = %d, want 2", openLevelsOn(t, h, m))
	}

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyLeft})
	h.waitFor("one level closed", func() bool { return openLevelsOn(t, h, m) == 1 })
	h.settle()
	if id := selectedOn(t, h, m); id != "b" {
		t.Errorf("selection after Left is %q, want the row that opened the closed level", id)
	}

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	h.waitFor("every level closed", func() bool { return openLevelsOn(t, h, m) == 0 })
	h.settle()
	if got := h.grid(); strings.Contains(got, "Deep") {
		t.Errorf("a level survived Escape:\n%s", got)
	}
}

// TestOpenRejectsARowItCannotOpen.
func TestOpenRejectsARowItCannotOpen(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("cmd", "Command", nil),
		disabled(widget.NewSubmenu("off", "Off", []widget.MenuItemModel{
			widget.NewCommand("x", "X", nil),
		})),
		hidden(widget.NewSubmenu("gone", "Gone", []widget.MenuItemModel{
			widget.NewCommand("y", "Y", nil),
		})),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 12)
	defer h.stop()

	for _, id := range []widget.ItemID{"cmd", "off", "gone", "absent"} {
		var err error
		h.onLoop(func() { err = m.Open(id) })
		if err == nil {
			t.Errorf("Open(%q) was accepted", id)
		}
		if openLevelsOn(t, h, m) != 0 {
			t.Fatalf("Open(%q) opened a level anyway", id)
		}
	}
}

// TestSetModelKeepsALevelItStillJustifies.
//
// Replacing a model is how an application updates a menu that is already open —
// enabling a row, renaming another. A level whose row survives must survive with
// it, or every update would collapse the cascade the user is working in.
func TestSetModelKeepsALevelItStillJustifies(t *testing.T) {
	sub := func(label string) []widget.MenuItemModel {
		return []widget.MenuItemModel{
			widget.NewSubmenu("file", label, []widget.MenuItemModel{
				widget.NewCommand("new", "New", nil),
			}),
		}
	}
	m := widget.NewMenu()
	if err := m.SetModel(sub("File")); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 40, 14)
	defer h.stop()
	h.onLoop(func() {
		if err := m.Open("file"); err != nil {
			t.Errorf("Open: %v", err)
		}
	})
	h.settle()
	h.settle()
	if openLevelsOn(t, h, m) != 1 {
		t.Fatalf("precondition failed: the level did not open")
	}

	// A relabelled but still-valid submenu keeps its level open.
	h.onLoop(func() {
		if err := m.SetModel(sub("Fichier")); err != nil {
			t.Errorf("SetModel: %v", err)
		}
	})
	h.settle()
	if openLevelsOn(t, h, m) != 1 {
		t.Errorf("OpenLevels() = %d after a model update that kept the row, want 1",
			openLevelsOn(t, h, m))
	}

	// A model where the row is gone closes it.
	h.onLoop(func() {
		if err := m.SetModel([]widget.MenuItemModel{
			widget.NewCommand("other", "Other", nil),
		}); err != nil {
			t.Errorf("SetModel: %v", err)
		}
	})
	h.waitFor("the orphaned level closed", func() bool { return openLevelsOn(t, h, m) == 0 })
}

// ─── the pointer machine ─────────────────────────────────────────────────────

// TestAMouseGestureActivatesOnReleaseOverThePressedRow.
//
// Press-arm, release-activate: activation happens on release, never on press, so
// a user who presses the wrong row can slide off and let go without running it.
func TestAMouseGestureActivatesOnReleaseOverThePressedRow(t *testing.T) {
	var ran atomic.Int64
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool {
		ran.Add(1)
		return true
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("one", "One", saveAction{}),
		widget.NewCommand("two", "Two", saveAction{}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()

	var events atomic.Int64
	unsub := tui.Subscribe(h.app.Bus(), func(widget.MenuActivatedEvent) { events.Add(1) })
	defer unsub()

	x1, y1 := cellOfLabel(t, h, "One")
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x1, Y: y1})
	h.settle()
	if ran.Load() != 0 || events.Load() != 0 {
		t.Fatal("a press alone activated the row; activation must wait for the release")
	}
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x1, Y: y1})
	h.waitFor("activated on release", func() bool { return events.Load() == 1 })
	if ran.Load() != 1 {
		t.Errorf("the executor ran %d times, want 1", ran.Load())
	}
}

// TestReleasingOffThePressedRowActivatesNothingAndKeepsTheMenuOpen.
//
// Sliding off a row and letting go is how a user cancels a click they have
// changed their mind about. The menu must stay open and usable afterwards, which
// is what distinguishes cancelling a gesture from closing a menu.
func TestReleasingOffThePressedRowActivatesNothingAndKeepsTheMenuOpen(t *testing.T) {
	var ran atomic.Int64
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool {
		ran.Add(1)
		return true
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("one", "One", saveAction{}),
		widget.NewCommand("two", "Two", saveAction{}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()

	x1, y1 := cellOfLabel(t, h, "One")
	x2, y2 := cellOfLabel(t, h, "Two")
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x1, Y: y1})
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: x2, Y: y2})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x2, Y: y2})
	h.settle()
	h.settle()

	if ran.Load() != 0 {
		t.Errorf("releasing over a different row activated something (%d runs)", ran.Load())
	}
	// Still usable: the keyboard works immediately afterwards.
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.waitFor("the menu still responds", func() bool { return ran.Load() == 1 })
}

// TestDraggingOffAndBackReArmsTheOriginalRow.
//
// The reason disarming retains the pointer. A user who presses, wanders off the
// row and comes back expects the click to still count — and that only works if
// the menu kept the grab while the pointer was away.
func TestDraggingOffAndBackReArmsTheOriginalRow(t *testing.T) {
	var ran atomic.Int64
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool {
		ran.Add(1)
		return true
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("one", "One", saveAction{}),
		widget.NewCommand("two", "Two", saveAction{}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()

	x1, y1 := cellOfLabel(t, h, "One")
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x1, Y: y1})
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: 28, Y: 9}) // off every row
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: x1, Y: y1})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x1, Y: y1})
	h.waitFor("re-armed and activated", func() bool { return ran.Load() == 1 })
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func disabled(m widget.MenuItemModel) widget.MenuItemModel { m.Enabled = false; return m }
func hidden(m widget.MenuItemModel) widget.MenuItemModel   { m.Visible = false; return m }
func checked(m widget.MenuItemModel) widget.MenuItemModel  { m.Checked = true; return m }

// flatten yields every row at every depth, for assertions about the whole model.
func flatten(items []widget.MenuItemModel) []widget.MenuItemModel {
	var out []widget.MenuItemModel
	for _, it := range items {
		out = append(out, it)
		out = append(out, flatten(it.Children)...)
	}
	return out
}

// nilableAction is a POINTER-shaped action, so a nil *nilableAction still
// satisfies tui.Action with a live type descriptor. That is the shape `== nil`
// cannot see.
type nilableAction struct{}

func (*nilableAction) ActionID() tui.ActionID { return "test.nilable" }

// TestATypedNilRowActionIsNoAction.
//
// tui defines a typed-nil Action as NO action, and the runtime's own seams
// enforce it. A Menu that checked only `it.Action == nil` disagreed: a row
// carrying (*T)(nil) looked like it had an action, so the executor was handed
// one, and a consumer switching on the concrete type reached a nil receiver in
// its own code for a row the model says does nothing.
func TestATypedNilRowActionIsNoAction(t *testing.T) {
	var ran atomic.Int64
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool {
		ran.Add(1)
		return true
	}))
	var nilAction *nilableAction
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("dead", "Dead", nilAction),
		widget.NewCommand("live", "Live", saveAction{}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.settle()
	h.settle()
	if n := ran.Load(); n != 0 {
		t.Errorf("the executor ran %d time(s) for a row whose Action is a typed nil; "+
			"a typed nil is no action", n)
	}

	// The positive control: the instrument does see a real action on the next
	// row, so the zero above is a refusal rather than a silent test.
	h.onLoop(func() { m.Select("live") })
	h.settle()
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.waitFor("the real action ran", func() bool { return ran.Load() == 1 })
}
