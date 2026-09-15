package widget_test

// The L5 behaviours whose failure mode is SILENCE: a popup that stays attached
// to a row that no longer means what it did, a gesture the runtime took away
// that leaves the menu half-armed, a level that closes when it should not. None
// of these shows up as a wrong pixel; each shows up later as a menu that does
// the wrong thing once.

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// movingOwner declares a region whose POSITION it can change, and can retire
// every ref it has issued — the two things a scrolling list does.
type movingOwner struct {
	widget.Base
	y          int
	invalidate bool
	ref        tui.AnchorRef
}

func (o *movingOwner) Layout(cs tui.Constraints) tui.Size {
	if ctx := o.Context(); ctx != nil {
		if o.invalidate {
			o.invalidate = false
			ctx.InvalidateAnchors()
		}
		o.ref = ctx.DeclareRegion("row", tui.Rect{X: 0, Y: o.y, W: 6, H: 1})
	}
	return cs.Constrain(tui.Size{W: 12, H: 6})
}

func (o *movingOwner) Render(s tui.Surface) { s.SetCell(0, 0, "O", style.New()) }

// TestAnOrdinaryRectMoveKeepsTheLayerOpen.
//
// This is the distinction that makes anchoring usable at all. A terminal resize
// or a scroll MOVES a region; it does not destroy it. Treating every geometry
// change as anchor loss would close a menu whenever the window changed size,
// which is the failure the generation stamp exists to avoid conflating with a
// real loss.
func TestAnOrdinaryRectMoveKeepsTheLayerOpen(t *testing.T) {
	owner := &movingOwner{y: 1}
	host := widget.NewOverlayHost(owner)
	h := startApp(t, host, 30, 12)
	defer h.stop()
	h.settle()

	h.onLoop(func() {
		if err := host.OpenAnchored("p", widget.NewText("POPUP"),
			widget.AnchorSpec{Ref: owner.ref}, nil); err != nil {
			t.Errorf("OpenAnchored: %v", err)
		}
	})
	h.settle()
	_, before := cellOfLabel(t, h, "POPUP")

	h.onLoop(func() {
		owner.y = 3
		owner.Context().RequestLayout()
	})
	h.settle()
	h.settle()

	if got := h.grid(); !strings.Contains(got, "POPUP") {
		t.Fatalf("moving the region closed the layer; a scroll or a resize must not "+
			"be mistaken for anchor loss:\n%s", got)
	}
	_, after := cellOfLabel(t, h, "POPUP")
	if after == before {
		t.Errorf("the layer stayed at line %d; it should have followed its region", after)
	}
}

// TestAStaleGenerationResolvesToLostRatherThanToWhateverIsThereNow.
//
// InvalidateAnchors is the deliberate "my rows no longer mean what they did".
// The danger it guards against is not a missing popup but a WRONG one: a region
// id reused for different content would otherwise silently re-point the popup at
// it, and everything would look fine.
func TestAStaleGenerationResolvesToLostRatherThanToWhateverIsThereNow(t *testing.T) {
	owner := &movingOwner{y: 1}
	host := widget.NewOverlayHost(owner)
	h := startApp(t, host, 30, 12)
	defer h.stop()
	h.settle()

	h.onLoop(func() {
		if err := host.OpenAnchored("p", widget.NewText("POPUP"),
			widget.AnchorSpec{Ref: owner.ref}, nil); err != nil {
			t.Errorf("OpenAnchored: %v", err)
		}
	})
	h.settle()
	if !strings.Contains(h.grid(), "POPUP") {
		t.Fatal("precondition failed: the popup never appeared")
	}

	var reasons []widget.DismissReason
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.OverlayDismissedEvent) {
		reasons = append(reasons, ev.Reason)
	})
	defer unsub()

	// The owner retires its refs and immediately redeclares the SAME id at the
	// same place — the worst case, because nothing about the geometry changed.
	h.onLoop(func() {
		owner.invalidate = true
		owner.Context().RequestLayout()
	})
	h.waitFor("the stale layer closed", func() bool {
		return !strings.Contains(h.grid(), "POPUP")
	})
	h.settle()

	if len(reasons) != 1 || reasons[0] != widget.DismissAnchorLost {
		t.Errorf("dismissals = %v, want exactly one anchor-lost", reasons)
	}
}

// TestAnAnchorOutsideTheHostIsRefused.
//
// A host places layers against geometry in the tree it lays out. An anchor
// belonging to a node elsewhere is arithmetic without a meaning, and accepting
// it would put a popup at coordinates that mean nothing in this host's frame.
func TestAnAnchorOutsideTheHostIsRefused(t *testing.T) {
	inside := &movingOwner{y: 1}
	outside := &movingOwner{y: 1}
	host := widget.NewOverlayHost(inside)
	root := tui.NewFlex(tui.Vertical)
	root.Add(host, outside)
	h := startApp(t, root, 30, 16)
	defer h.stop()
	h.settle()

	var err error
	h.onLoop(func() {
		err = host.OpenAnchored("p", widget.NewText("POPUP"),
			widget.AnchorSpec{Ref: outside.ref}, nil)
	})
	h.settle()
	if !errors.Is(err, widget.ErrAnchorUnusable) {
		t.Errorf("OpenAnchored returned %v, want ErrAnchorUnusable", err)
	}
	// The control: the same call with an anchor INSIDE the host succeeds, so the
	// refusal above is about ownership rather than a broken fixture.
	h.onLoop(func() {
		err = host.OpenAnchored("p", widget.NewText("POPUP"),
			widget.AnchorSpec{Ref: inside.ref}, nil)
	})
	h.settle()
	if err != nil {
		t.Errorf("an anchor inside the host was refused: %v", err)
	}
}

// TestAFailedReplacementLeavesTheOldLayerOpen.
//
// Replacing an open id mounts the new layer before unmounting the old, so a
// failure leaves the id occupied by something that works rather than empty. An
// id that silently emptied on a bad replacement would look like a close the
// caller never asked for.
func TestAFailedReplacementLeavesTheOldLayerOpen(t *testing.T) {
	owner := &movingOwner{y: 1}
	host := widget.NewOverlayHost(owner)
	h := startApp(t, host, 30, 12)
	defer h.stop()
	h.settle()

	h.onLoop(func() {
		if err := host.OpenAnchored("p", widget.NewText("FIRST"),
			widget.AnchorSpec{Ref: owner.ref}, nil); err != nil {
			t.Errorf("first open: %v", err)
		}
	})
	h.settle()

	// A layer that cannot mount: already mounted as the host's own content.
	var err error
	h.onLoop(func() {
		err = host.OpenAnchored("p", owner, widget.AnchorSpec{Ref: owner.ref}, nil)
	})
	h.settle()

	if err == nil {
		t.Fatal("a replacement that cannot mount was accepted")
	}
	if got := h.grid(); !strings.Contains(got, "FIRST") {
		t.Errorf("the failed replacement closed the old layer:\n%s", got)
	}
	n := 0
	h.onLoop(func() {
		for range host.AnchoredLayers() {
			n++
		}
	})
	if n != 1 {
		t.Errorf("%d layers registered after a failed replacement, want 1", n)
	}
}

// TestHidingARowClosesTheLevelItOpened.
//
// A row that stops being visible stops declaring its region, and the level
// hanging off it has nothing left to point at. This is the model-level face of
// per-layout region lifetime.
func TestHidingARowClosesTheLevelItOpened(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", nil),
		}),
		widget.NewCommand("edit", "Edit", nil),
	}); err != nil {
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
		t.Fatal("precondition failed: the level did not open")
	}

	h.onLoop(func() { m.SetVisible("file", false) })
	h.waitFor("the level closed with its row", func() bool {
		return openLevelsOn(t, h, m) == 0
	})
	h.settle()
	if got := h.grid(); strings.Contains(got, "New") {
		t.Errorf("the orphaned level is still on screen:\n%s", got)
	}
}

// TestAHiddenOrDisabledRowKeepsItsCheckedState.
//
// Visibility is presentation; checkedness is state. Hiding a checked option does
// not uncheck it, and a caller that meant to uncheck it has SetChecked.
func TestAHiddenOrDisabledRowKeepsItsCheckedState(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		checked(widget.NewCheck("wrap", "Wrap", nil)),
		checked(widget.NewCheck("nums", "Numbers", nil)),
		widget.NewCommand("keep", "Keep", nil),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 12)
	defer h.stop()

	h.onLoop(func() {
		m.SetVisible("wrap", false)
		m.SetEnabled("nums", false)
	})
	h.settle()

	for _, it := range flatten(modelOn(t, h, m)) {
		switch it.ID {
		case "wrap":
			if !it.Checked {
				t.Error("hiding a checked row unchecked it; visibility is presentation")
			}
		case "nums":
			if !it.Checked {
				t.Error("disabling a checked row unchecked it")
			}
		}
	}
}

// TestAnUnhandledActionLeavesTheMenuOpenWithItsStateChanged.
//
// The two halves are separate on purpose. The state change already happened, so
// it stands; the command did not run, so the menu must not close as though it
// had. A caller watching a menu vanish concludes the command ran.
func TestAnUnhandledActionLeavesTheMenuOpenWithItsStateChanged(t *testing.T) {
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool {
		return false // refused
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("opts", "Options", []widget.MenuItemModel{
			widget.NewCheck("wrap", "Wrap", saveAction{}),
		}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 40, 14)
	defer h.stop()

	h.onLoop(func() {
		if err := m.Open("opts"); err != nil {
			t.Errorf("Open: %v", err)
		}
	})
	h.settle()
	h.settle()

	var events atomic.Int64
	unsub := tui.Subscribe(h.app.Bus(), func(widget.MenuActivatedEvent) { events.Add(1) })
	defer unsub()

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.waitFor("the activation was reported", func() bool { return events.Load() == 1 })
	h.settle()

	if openLevelsOn(t, h, m) != 1 {
		t.Error("a refused action closed the menu; a command that could not run " +
			"must not look like one that did")
	}
	for _, it := range flatten(modelOn(t, h, m)) {
		if it.ID == "wrap" && !it.Checked {
			t.Error("the toggle was rolled back; the state change happened first and stands")
		}
	}
}

// TestReleasingElsewhereLeavesTheOpenLevelsAlone.
//
// Cancelling a gesture and closing a menu are different things. Sliding off a
// row and letting go abandons the click; it does not dismiss the cascade the
// user is working in.
func TestReleasingElsewhereLeavesTheOpenLevelsAlone(t *testing.T) {
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool { return true }))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", saveAction{}),
			widget.NewCommand("open", "Open", saveAction{}),
		}),
	}); err != nil {
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

	nx, ny := cellOfLabel(t, h, "New")
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: nx, Y: ny})
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: 38, Y: 13})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 38, Y: 13})
	h.settle()
	h.settle()

	if got := openLevelsOn(t, h, m); got != 1 {
		t.Errorf("OpenLevels() = %d after releasing off the menu, want 1 — cancelling "+
			"a gesture is not closing a menu", got)
	}
}

// TestLosingTheCaptureClearsTheGestureWithoutClosingAnything.
//
// An involuntary loss is not a decision the user made about the menu. It must
// clear the half-finished gesture — so a later release cannot activate something
// the user has stopped pointing at — and leave the levels exactly as they were.
func TestLosingTheCaptureClearsTheGestureWithoutClosingAnything(t *testing.T) {
	var ran atomic.Int64
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool {
		ran.Add(1)
		return true
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", saveAction{}),
		}),
	}); err != nil {
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

	nx, ny := cellOfLabel(t, h, "New")
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: nx, Y: ny})
	h.settle()

	// The runtime takes the capture away, as it does on a scope change.
	h.onLoop(func() { m.Context().CancelGesture() })
	h.settle()

	if got := openLevelsOn(t, h, m); got != 1 {
		t.Errorf("OpenLevels() = %d after a capture loss, want 1", got)
	}
	// The release that follows must activate nothing: the gesture is over.
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: nx, Y: ny})
	h.settle()
	h.settle()
	if ran.Load() != 0 {
		t.Errorf("a release after the capture was lost activated something (%d runs)", ran.Load())
	}
}

// TestANonPrimaryReleaseDoesNotEndAPrimaryGesture.
//
// A user with a thumb button, or a trackpad reporting a stray middle click, must
// not lose the drag they are in the middle of. The primary release is what ends
// it, and it must still activate the row it started on.
func TestANonPrimaryReleaseDoesNotEndAPrimaryGesture(t *testing.T) {
	var ran atomic.Int64
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool {
		ran.Add(1)
		return true
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("one", "One", saveAction{}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()

	x, y := cellOfLabel(t, h, "One")
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseMiddle, X: x, Y: y})
	h.settle()
	if ran.Load() != 0 {
		t.Fatal("a non-primary release activated the row")
	}
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
	h.waitFor("the primary release still activates", func() bool { return ran.Load() == 1 })
	h.settle()
	if ran.Load() != 1 {
		t.Errorf("the row activated %d times, want exactly 1", ran.Load())
	}
}

// TestEscapeRestoresTheFocusTheMenuTookAndClosesEveryLevel.
func TestEscapeRestoresTheFocusTheMenuTookAndClosesEveryLevel(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("a", "A", []widget.MenuItemModel{
			widget.NewSubmenu("b", "B", []widget.MenuItemModel{
				widget.NewCommand("deep", "Deep", nil),
			}),
		}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 40, 16)
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
		t.Fatalf("precondition failed: %d levels open, want 2", openLevelsOn(t, h, m))
	}

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	h.waitFor("every level closed", func() bool { return openLevelsOn(t, h, m) == 0 })
	h.settle()

	// Focus is still on the menu itself, which is where it was before the
	// cascade opened: the levels never took it.
	var focused bool
	h.onLoop(func() { focused = m.Context().Focused() })
	if !focused {
		t.Error("the menu does not hold focus after Escape closed its levels")
	}
	if got := h.grid(); strings.Contains(got, "Deep") {
		t.Errorf("a level survived Escape:\n%s", got)
	}
}

// TestASubmenuRowIsClickable.
//
// A level is a separate NODE, so the runtime delivers its pointer events there
// rather than to the Menu. Without a resolver of its own a submenu's rows are
// simply not clickable — the cascade renders, highlights under the keyboard, and
// silently ignores the mouse. Nothing about the picture says so.
func TestASubmenuRowIsClickable(t *testing.T) {
	var ran atomic.Int64
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool {
		ran.Add(1)
		return true
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", saveAction{}),
			widget.NewCommand("open", "Open", saveAction{}),
		}),
	}); err != nil {
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

	var events []widget.MenuActivatedEvent
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.MenuActivatedEvent) {
		events = append(events, ev)
	})
	defer unsub()

	ox, oy := cellOfLabel(t, h, "Open")
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: ox, Y: oy})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: ox, Y: oy})
	h.waitFor("the submenu row activated", func() bool { return ran.Load() == 1 })
	h.settle()

	if len(events) != 1 || events[0].ItemID != "open" {
		t.Errorf("activation events = %v, want exactly one for open", events)
	}
	if events[0].Origin != tui.OriginPointer {
		t.Errorf("Origin = %v, want OriginPointer", events[0].Origin)
	}
}

// TestAnEnabledSeparatorIsStillNotSelectable.
//
// Three conditions make a row selectable and they fail differently: hidden is
// not on screen, disabled is temporary, and a separator is STRUCTURALLY not a
// control. The constructors leave a separator disabled, so nothing notices the
// third check until somebody sets Enabled on one — and then traversal lands the
// selection on a horizontal rule.
func TestAnEnabledSeparatorIsStillNotSelectable(t *testing.T) {
	sep := widget.NewSeparator("sep")
	sep.Enabled = true // what a caller building rows by hand eventually does

	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("one", "One", nil),
		sep,
		widget.NewCommand("two", "Two", nil),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()

	if m.Select("sep") {
		t.Error("an enabled separator accepted the selection")
	}
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown})
	h.settle()
	if id := selectedOn(t, h, m); id != "two" {
		t.Errorf("Down selected %q, want two; an enabled separator is still a rule", id)
	}
	// And it cannot be clicked either.
	sx, sy := cellOfLabel(t, h, "─")
	var ran atomic.Int64
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: sx, Y: sy})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: sx, Y: sy})
	h.settle()
	if ran.Load() != 0 {
		t.Error("an enabled separator was clickable")
	}
}

// TestARadioClearsMembersNestedDeeperThanItself.
//
// Exclusivity is a property of the whole model. Clearing only the level the
// radio lives on leaves a member checked inside a submenu — two radios of one
// group both set, which is the exact state a group exists to prevent.
func TestARadioClearsMembersNestedDeeperThanItself(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewRadio("light", "Light", "theme", nil),
		widget.NewSubmenu("more", "More", []widget.MenuItemModel{
			checked(widget.NewRadio("dark", "Dark", "theme", nil)),
			widget.NewSubmenu("deeper", "Deeper", []widget.MenuItemModel{
				checked(widget.NewRadio("midnight", "Midnight", "theme", nil)),
			}),
		}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 40, 16)
	defer h.stop()

	// Set the ROOT member: clearing the others requires descending.
	h.onLoop(func() { m.SetChecked("light", true) })
	h.settle()

	for _, it := range flatten(modelOn(t, h, m)) {
		switch it.ID {
		case "light":
			if !it.Checked {
				t.Error("the newly set radio is not checked")
			}
		case "dark", "midnight":
			if it.Checked {
				t.Errorf("%q stayed checked; a radio clears its group at every depth", it.ID)
			}
		}
	}
}

// TestTheExecutorSeesTheRuntimesOwnProvenance.
//
// Menu copies the trusted incoming invocation and replaces only its Action, so
// Origin and Source survive to the executor. Building a fresh invocation instead
// would hand every executor OriginProgrammatic and quietly destroy the ability
// to tell a keypress from a click.
func TestTheExecutorSeesTheRuntimesOwnProvenance(t *testing.T) {
	var origins []tui.ActionOrigin
	var got []tui.ActionID
	m := widget.NewMenu(widget.WithActionExecutor(func(inv tui.ActionInvocation) bool {
		origins = append(origins, inv.Origin)
		got = append(got, inv.Action.ActionID())
		return true
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("go", "Go", saveAction{}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 10)
	defer h.stop()

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	h.waitFor("the executor ran", func() bool {
		var n int
		h.onLoop(func() { n = len(origins) })
		return n == 1
	})
	h.settle()

	h.onLoop(func() {
		if len(origins) != 1 || origins[0] != tui.OriginKey {
			t.Errorf("the executor saw origins %v, want [key]", origins)
		}
		if len(got) != 1 || got[0] != (saveAction{}).ActionID() {
			t.Errorf("the executor saw actions %v, want the ROW's action", got)
		}
	})
}

// TestTheCaptureSurvivesThePointerLeavingTheMenuEntirely.
//
// The reason disarming retains the grab. Once the pointer is outside the menu's
// own rect, hit-testing would deliver its motion somewhere else — so without the
// capture the menu never learns the pointer came back, and a user who wandered
// off the edge and returned would find their click had evaporated.
func TestTheCaptureSurvivesThePointerLeavingTheMenuEntirely(t *testing.T) {
	var ran atomic.Int64
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool {
		ran.Add(1)
		return true
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("one", "One", saveAction{}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	// A wide host, so there is somewhere genuinely OUTSIDE the menu to go.
	other := widget.NewButton("far away")
	root := tui.NewFlex(tui.Horizontal)
	root.Add(m, other)
	host := widget.NewOverlayHost(root)
	h := startApp(t, host, 60, 10)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()

	x, y := cellOfLabel(t, h, "One")
	fx, fy := cellOfLabel(t, h, "far away")
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y})
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: fx, Y: fy}) // over another widget
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: x, Y: y})   // and back
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
	h.waitFor("the gesture survived leaving the menu", func() bool { return ran.Load() == 1 })
}

// TestADisabledItemRefusesAProgrammaticActivationToo.
//
// The pointer path is already gated by ActivationAvailable, so Activate's own
// check only shows itself on a path that does not consult it — a programmatic
// dispatch, or a consumer resolver that produced an activation directly. The
// guard is where the authority belongs: Activate is the sole decider of whether
// an activation happens, for every producer.
func TestADisabledItemRefusesAProgrammaticActivationToo(t *testing.T) {
	var ran atomic.Int64
	item := widget.NewMenuItem("Delete",
		widget.WithItemEnabled(false),
		widget.WithOnRun(func() { ran.Add(1) }))
	host := widget.NewOverlayHost(item)
	h := startApp(t, host, 30, 6)
	defer h.stop()
	h.settle()

	var events atomic.Int64
	unsub := tui.Subscribe(h.app.Bus(), func(tui.ControlActivatedEvent) { events.Add(1) })
	defer unsub()

	var handled bool
	h.onLoop(func() { handled = item.Context().DoAction(tui.ActivateAction{}) })
	h.settle()
	h.settle()

	if handled {
		t.Error("a disabled item reported a programmatic activation as handled")
	}
	if ran.Load() != 0 || events.Load() != 0 {
		t.Errorf("a disabled item ran %d times and published %d events",
			ran.Load(), events.Load())
	}

	// The control: enabled, the same call works, so the refusal above is about
	// the disabled state rather than a dispatch that never arrived.
	h.onLoop(func() { item.SetEnabled(true) })
	h.settle()
	h.onLoop(func() { handled = item.Context().DoAction(tui.ActivateAction{}) })
	h.waitFor("the enabled item accepts it", func() bool { return ran.Load() == 1 })
	if !handled {
		t.Error("an enabled item refused a programmatic activation")
	}
}

// TestTheHostBoundsAConsumerPolicyThatIgnoresTheViewport.
//
// FlipClipPolicy clamps its own answer, so the host's bound looks redundant
// until a CONSUMER policy is supplied — and a consumer policy is arbitrary code
// that can return anything. The host is the last word, which is what makes
// "whatever policy is used, the rect is bounded" a promise rather than a hope.
func TestTheHostBoundsAConsumerPolicyThatIgnoresTheViewport(t *testing.T) {
	owner := &movingOwner{y: 1}
	host := widget.NewOverlayHost(owner)
	h := startApp(t, host, 30, 12)
	defer h.stop()
	h.settle()

	// A policy that places the layer far off-screen, deliberately.
	rogue := widget.AnchorPolicyFunc(func(_, _ tui.Rect, want tui.Size, _ widget.Placement) tui.Rect {
		return tui.Rect{X: 900, Y: 900, W: want.W, H: want.H}
	})
	h.onLoop(func() {
		if err := host.OpenAnchored("p", widget.NewText("POPUP"),
			widget.AnchorSpec{Ref: owner.ref}, rogue); err != nil {
			t.Errorf("OpenAnchored: %v", err)
		}
	})
	h.settle()

	if got := h.grid(); !strings.Contains(got, "POPUP") {
		t.Errorf("the layer was placed off-screen and vanished; the host must bound "+
			"whatever a policy returns:\n%s", got)
	}
}

// TestDraggingAcrossRowsArmsOnlyThePressedOne.
//
// Dragging through a menu highlights rows as it goes. Only the row the gesture
// STARTED on is armed, because the armed look is a promise about what releasing
// here would do — and releasing over any other row does nothing.
func TestDraggingAcrossRowsArmsOnlyThePressedOne(t *testing.T) {
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool { return true }))
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
	restingLook := rowStyleAt(t, h, x2, y2)

	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x1, Y: y1})
	h.settle()
	armedLook := rowStyleAt(t, h, x1, y1)
	if armedLook == restingLook {
		t.Fatal("precondition failed: the armed look is indistinguishable from an " +
			"ordinary row, so nothing below could observe it")
	}

	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: x2, Y: y2})
	h.settle()

	// The PRESSED row stops looking armed the moment the pointer leaves it. The
	// armed look is a promise about what releasing would do, and releasing here
	// would now do nothing — so leaving One lit is the menu lying about it.
	if got := rowStyleAt(t, h, x1, y1); got == armedLook {
		t.Error("the pressed row still looks armed after the pointer left it")
	}
	if id := selectedOn(t, h, m); id != "two" {
		t.Errorf("selection is %q, want two: dragging still moves the highlight", id)
	}
}

// rowStyleAt reads the painted attributes of one cell, so a test can tell two
// row states apart without reimplementing the style table.
func rowStyleAt(t *testing.T, h *harness, x, y int) tui.CellAttrs {
	t.Helper()
	grid := h.tb.Snapshot()
	if y >= len(grid) || x >= len(grid[y]) {
		t.Fatalf("cell (%d,%d) is off the grid", x, y)
	}
	return grid[y][x].Attrs
}

// TestAnInvoluntaryCaptureLossClearsTheGesture.
//
// The runtime takes a capture away when its owner can no longer own one — the
// focus scope changed, the owner was hidden. That is not a decision the user
// made about the menu, so the open levels stay; but the half-finished gesture
// must go, or a later release activates a row the user has stopped pointing at.
//
// Driven on a ROOT row rather than inside a level, because the assertion needs a
// press that provably armed and the root's armed look is directly observable.
func TestAnInvoluntaryCaptureLossClearsTheGesture(t *testing.T) {
	var ran atomic.Int64
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool {
		ran.Add(1)
		return true
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", saveAction{}),
		}),
		widget.NewCommand("quit", "Quit", saveAction{}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	other := widget.NewButton("Other")
	root := tui.NewFlex(tui.Vertical)
	root.Add(m, other)
	host := widget.NewOverlayHost(root)
	h := startApp(t, host, 50, 14)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()
	h.onLoop(func() {
		if err := m.Open("file"); err != nil {
			t.Errorf("Open: %v", err)
		}
	})
	h.settle()
	h.settle()

	qx, qy := cellOfLabel(t, h, "Quit")
	restingLook := rowStyleAt(t, h, qx, qy)
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: qx, Y: qy})
	h.settle()
	if rowStyleAt(t, h, qx, qy) == restingLook {
		t.Fatal("precondition failed: the press did not arm the row, so the capture " +
			"loss below has no gesture to clear and the assertions prove nothing")
	}

	// Focus leaves the capture owner's subtree: the runtime revokes the capture
	// and delivers PointerCaptureLostEvent.
	h.onLoop(func() { other.Context().RequestFocus() })
	h.settle()

	if got := openLevelsOn(t, h, m); got != 1 {
		t.Errorf("OpenLevels() = %d after an involuntary capture loss, want 1", got)
	}
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: qx, Y: qy})
	h.settle()
	h.settle()
	if ran.Load() != 0 {
		t.Errorf("a release after the capture was revoked activated something (%d runs)",
			ran.Load())
	}
}

// TestTheBarStatesItsDropSideRatherThanRelyingOnTheFlip.
//
// A bottom bar asks to open UPWARD. With the default policy that preference is
// invisible — a downward request would not fit and would be flipped up anyway —
// so the preference is checked against a policy that does NOT flip, which is the
// only arrangement in which the two answers differ.
func TestTheBarStatesItsDropSideRatherThanRelyingOnTheFlip(t *testing.T) {
	var asked []widget.PlacementSide
	recording := widget.AnchorPolicyFunc(
		func(anchor, viewport tui.Rect, want tui.Size, pref widget.Placement) tui.Rect {
			asked = append(asked, pref.Side)
			return widget.FlipClipPolicy{}.Place(anchor, viewport, want, pref)
		})

	m := widget.NewMenu(widget.WithAnchorPolicy(recording))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", nil),
		}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	bar := widget.NewMenuBar(m, widget.WithBarPlacement(widget.BarPlacementBottom))
	body := widget.NewText("body")
	root := tui.NewFlex(tui.Vertical)
	root.AddWeighted(body, 1)
	root.Add(bar)
	host := widget.NewOverlayHost(root)
	h := startApp(t, host, 40, 14)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()

	h.onLoop(func() {
		if err := m.Open("file"); err != nil {
			t.Errorf("Open: %v", err)
		}
	})
	h.settle()
	h.settle()

	h.onLoop(func() {
		if len(asked) == 0 {
			t.Fatal("the policy was never consulted")
		}
		if asked[0] != widget.PlacementAbove {
			t.Errorf("the bar asked for %v, want above; a bottom bar must STATE its "+
				"drop side rather than leave the flip to rescue it", asked[0])
		}
	})
}

// TestAbandoningADragOutsideTheMenuEndsItCleanly.
//
// The capture is what makes this possible at all: with the pointer outside the
// menu's own rect, ordinary hit-testing would deliver the release somewhere
// else, and the menu would never learn the gesture had ended. Its pressed state
// would then survive indefinitely — and a later HOVER, with no button down,
// would be treated as the continuation of a drag and move the selection.
func TestAbandoningADragOutsideTheMenuEndsItCleanly(t *testing.T) {
	m := widget.NewMenu(widget.WithActionExecutor(func(tui.ActionInvocation) bool { return true }))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewCommand("one", "One", saveAction{}),
		widget.NewCommand("two", "Two", saveAction{}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	other := widget.NewButton("far away")
	root := tui.NewFlex(tui.Horizontal)
	root.Add(m, other)
	host := widget.NewOverlayHost(root)
	h := startApp(t, host, 60, 10)
	defer h.stop()
	h.onLoop(func() { m.Context().RequestFocus() })
	h.settle()

	x1, y1 := cellOfLabel(t, h, "One")
	x2, y2 := cellOfLabel(t, h, "Two")
	fx, fy := cellOfLabel(t, h, "far away")

	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x1, Y: y1})
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: fx, Y: fy})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: fx, Y: fy})
	h.settle()
	h.settle()

	// A bare hover afterwards must NOT move the selection: there is no drag in
	// progress, and hovering is not selecting.
	if id := selectedOn(t, h, m); id != "one" {
		t.Fatalf("selection is %q before the hover, want one", id)
	}
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: x2, Y: y2})
	h.settle()
	if id := selectedOn(t, h, m); id != "one" {
		t.Errorf("a hover moved the selection to %q; the abandoned drag was never "+
			"ended, so the menu still believes a button is down", id)
	}
}
