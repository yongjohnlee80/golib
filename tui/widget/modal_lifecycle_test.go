package widget_test

// A dialog's LIFECYCLE, as distinct from how it looks. Everything here is a
// state the rendered output cannot distinguish: a modal that was remounted looks
// exactly like one that was reordered, a dialog closed out of order looks like
// one closed in order until you ask what is still open, and an activation that
// published no event looks like one that did until something is listening.
//
// That is the common shape of these cases and the reason they are grouped: a
// screen comparison is the wrong instrument for all of them, so each test names
// the identity, the event or the ordering it is actually pinning.

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// nodeIDOf reads a mounted component's node identity on the loop.
func nodeIDOf(t *testing.T, h *harness, c interface{ NodeID() tui.NodeID }) tui.NodeID {
	t.Helper()
	var id tui.NodeID
	h.onLoop(func() { id = c.NodeID() })
	return id
}

// focusedOn reports whether the given button holds focus, read on the loop.
func focusedOn(t *testing.T, h *harness, b *widget.Button) bool {
	t.Helper()
	var f bool
	h.onLoop(func() { f = b.Context() != nil && b.Context().Focused() })
	return f
}

// cellOfLabel finds where a label is painted, so a pointer test can aim at the
// control a user would actually click rather than at a hardcoded coordinate
// that silently stops pointing at it when the layout shifts.
func cellOfLabel(t *testing.T, h *harness, label string) (x, y int) {
	t.Helper()
	grid := h.tb.Snapshot()
	for row := range grid {
		line := ""
		for _, c := range grid[row] {
			if c.Continuation() {
				continue
			}
			if c.Content == "" {
				line += " "
				continue
			}
			line += c.Content
		}
		if i := strings.Index(line, label); i >= 0 {
			return i, row
		}
	}
	t.Fatalf("label %q is not on screen:\n%s", label, h.grid())
	return 0, 0
}

// ─── the focus provider ──────────────────────────────────────────────────────

// TestButtonsAddedToAnEmptyDialogTakeFocusInTabOrder. Opened with no buttons,
// the Modal node itself holds focus; once buttons arrive it is no longer the
// fallback, and focus moves to the first of them.
func TestButtonsAddedToAnEmptyDialogTakeFocusInTabOrder(t *testing.T) {
	m := widget.NewModal(widget.NewText("Body"))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	normal := widget.NewButton("Normal")
	def := widget.NewButton("OK", widget.WithRole(widget.ButtonRoleAccept), widget.WithDefault(true))
	var err error
	h.onLoop(func() { err = m.SetButtons(normal, def) })
	if err != nil {
		t.Fatalf("SetButtons: %v", err)
	}
	h.settle()

	if !focusedOn(t, h, normal) {
		t.Error("focus did not move to the first button once the dialog had buttons")
	}
	var sel int
	h.onLoop(func() { sel = m.SelectedButton() })
	if sel != 0 {
		t.Errorf("SelectedButton() = %d, want 0 (the first button)", sel)
	}
}

// TestSetButtonsKeepsSelectionForAStillFocusedButton.
//
// Replacing a list with an identical one changes nothing a user can see, and
// must therefore change nothing they can query. Clearing selection optimistically
// and waiting for a focus event to restore it does not work: focus never moved,
// so no event is emitted and the dialog is left reporting that nothing is
// selected while a button is visibly focused.
func TestSetButtonsKeepsSelectionForAStillFocusedButton(t *testing.T) {
	a := widget.NewButton("A")
	b := widget.NewButton("B")
	m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(a, b))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	h.onLoop(func() { a.Context().RequestFocus() })
	h.settle()
	if !focusedOn(t, h, a) {
		t.Fatal("precondition failed: A does not hold focus, so nothing has to be preserved")
	}

	var err error
	h.onLoop(func() { err = m.SetButtons(a, b) }) // the identical list
	if err != nil {
		t.Fatalf("SetButtons: %v", err)
	}
	h.settle()

	if !focusedOn(t, h, a) {
		t.Error("an identity SetButtons moved focus off the focused button")
	}
	var sel int
	h.onLoop(func() { sel = m.SelectedButton() })
	if sel != 0 {
		t.Errorf("SelectedButton() = %d, want 0; the focused button and the reported "+
			"selection disagree", sel)
	}
}

// TestAddingAButtonLeavesTheFocusedOneFocused. A button added AHEAD of the
// focused one is first in Tab order now, and focus stays where it is: the
// dialog's nomination says where focus starts, and where it goes when it has
// to move — never off a control that can still hold it.
func TestAddingAButtonLeavesTheFocusedOneFocused(t *testing.T) {
	plain := widget.NewButton("Plain")
	m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(plain))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)
	if !focusedOn(t, h, plain) {
		t.Fatal("precondition failed: the only button never took focus")
	}

	def := widget.NewButton("OK", widget.WithRole(widget.ButtonRoleAccept), widget.WithDefault(true))
	var err error
	h.onLoop(func() { err = m.SetButtons(def, plain) })
	if err != nil {
		t.Fatalf("SetButtons: %v", err)
	}
	h.settle()

	if !focusedOn(t, h, plain) {
		t.Error("focus moved off the focused button when a button was added ahead of it")
	}
}

// ─── list validation ─────────────────────────────────────────────────────────

// TestSetButtonsRejectsAnUnusableListWithoutChangingAnything.
//
// Every rejected call must leave the dialog exactly as it was — the list, the
// selection, the focus and the mounted tree. A validator that checked while
// installing would apply the acceptable entries first, which is how a refused
// call ends up doing half its work.
func TestSetButtonsRejectsAnUnusableListWithoutChangingAnything(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
		list func(host *widget.OverlayHost, base *widget.Button, keep *widget.Button) []*widget.Button
	}{
		{
			name: "a nil entry",
			want: widget.ErrNilButton,
			list: func(_ *widget.OverlayHost, _ *widget.Button, keep *widget.Button) []*widget.Button {
				return []*widget.Button{keep, nil}
			},
		},
		{
			name: "the same button twice",
			want: widget.ErrRepeatedButton,
			list: func(_ *widget.OverlayHost, _ *widget.Button, keep *widget.Button) []*widget.Button {
				return []*widget.Button{keep, keep}
			},
		},
		{
			name: "a button mounted elsewhere in the tree",
			want: widget.ErrForeignButton,
			list: func(_ *widget.OverlayHost, base *widget.Button, keep *widget.Button) []*widget.Button {
				return []*widget.Button{keep, base}
			},
		},
		{
			name: "two default buttons",
			want: widget.ErrDuplicateDefault,
			list: func(_ *widget.OverlayHost, _ *widget.Button, keep *widget.Button) []*widget.Button {
				return []*widget.Button{
					keep,
					widget.NewButton("X", widget.WithRole(widget.ButtonRoleAccept), widget.WithDefault(true)),
					widget.NewButton("Y", widget.WithRole(widget.ButtonRoleAccept), widget.WithDefault(true)),
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keep := widget.NewButton("Keep")
			m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(keep))
			h, host, base := modalFixture(t, m, 40, 12)
			defer h.stop()
			openOn(t, h, m, host)

			beforeID := nodeIDOf(t, h, keep)
			var before []*widget.Button
			h.onLoop(func() { before = m.Buttons() })

			var err error
			h.onLoop(func() { err = m.SetButtons(tc.list(host, base, keep)...) })
			h.settle()

			if !errors.Is(err, tc.want) {
				t.Fatalf("SetButtons returned %v, want %v", err, tc.want)
			}
			var after []*widget.Button
			var sel int
			h.onLoop(func() {
				after = m.Buttons()
				sel = m.SelectedButton()
			})
			if len(after) != len(before) || (len(after) > 0 && after[0] != before[0]) {
				t.Errorf("a rejected SetButtons changed the list: %d buttons, want %d",
					len(after), len(before))
			}
			if got := nodeIDOf(t, h, keep); got != beforeID {
				t.Errorf("the surviving button was remounted by a REJECTED call "+
					"(NodeID %d -> %d)", beforeID, got)
			}
			if !focusedOn(t, h, keep) {
				t.Error("a rejected SetButtons moved focus")
			}
			if sel != 0 {
				t.Errorf("SelectedButton() = %d after a rejected call, want 0", sel)
			}
		})
	}
}

// TestReorderingButtonsMovesThemWithoutRemounting.
//
// A caller reordering the controls asked for a cosmetic change. Remounting to
// achieve it gives every button a new NodeID, cancels its lifetime context and
// throws away its focus — and the screen looks identical either way, so nothing
// about the appearance can catch it. Document order is also TAB order, so the
// reorder has to reach the tree rather than only the paint.
func TestReorderingButtonsMovesThemWithoutRemounting(t *testing.T) {
	a := widget.NewButton("A")
	b := widget.NewButton("B")
	c := widget.NewButton("C")
	m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(a, b, c))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	ids := [3]tui.NodeID{nodeIDOf(t, h, a), nodeIDOf(t, h, b), nodeIDOf(t, h, c)}

	// REVERSED, deliberately not rotated. Tab traversal is cyclic, so a rotation
	// of the list produces the same sequence from the same starting point as the
	// original order does — an assertion over c,a,b starting at c passes whether
	// or not the tree was reordered at all. Reversal is the cheapest arrangement
	// the two orders disagree about.
	var err error
	h.onLoop(func() { err = m.SetButtons(c, b, a) })
	if err != nil {
		t.Fatalf("SetButtons: %v", err)
	}
	h.settle()

	for i, pair := range []struct {
		b  *widget.Button
		id tui.NodeID
	}{{a, ids[0]}, {b, ids[1]}, {c, ids[2]}} {
		if got := nodeIDOf(t, h, pair.b); got != pair.id {
			t.Errorf("button %d was remounted by a reorder (NodeID %d -> %d)",
				i, pair.id, got)
		}
	}

	// Tab order must follow the NEW visual order: c, b, a. Under the old tree
	// order (a, b, c) the same walk from c would visit a then b.
	h.onLoop(func() { c.Context().RequestFocus() })
	h.settle()
	for _, want := range []*widget.Button{b, a, c} {
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
		h.settle()
		if !focusedOn(t, h, want) {
			var order []string
			h.onLoop(func() {
				for _, bb := range m.Buttons() {
					order = append(order, bb.Label())
				}
			})
			t.Fatalf("Tab did not reach %q next; list order is %v but the tree was "+
				"not reordered to match", want.Label(), order)
		}
	}
}

// ─── stacking ────────────────────────────────────────────────────────────────

// TestClosingAStackedDialogDoesNotRemountTheSurvivor.
//
// Restoring the lower dialog's backdrop means inserting a layer BENEATH it.
// Doing that by removing and re-adding the dialog produces exactly the right
// picture and destroys the dialog on the way: new NodeID, cancelled lifetime
// context, unmount hooks fired, focus gone, Init re-run on everything inside.
// None of that is visible on screen, which is why a rendered-output test —
// including the one this package already had — cannot see it.
func TestClosingAStackedDialogDoesNotRemountTheSurvivor(t *testing.T) {
	lowerBtn := widget.NewButton("Lower")
	lower := widget.NewModal(widget.NewText("first"), widget.WithButtons(lowerBtn))
	upper := widget.NewModal(widget.NewText("later"), widget.WithModalTitle("Bravo"))

	h, host, _ := modalFixture(t, lower, 40, 12)
	defer h.stop()

	openOn(t, h, lower, host)
	lowerID := nodeIDOf(t, h, lower)
	btnID := nodeIDOf(t, h, lowerBtn)
	if !focusedOn(t, h, lowerBtn) {
		t.Fatal("precondition failed: the lower dialog's button never took focus")
	}

	openOn(t, h, upper, host)
	h.onLoop(func() { upper.Dismiss(widget.DismissProgrammatic) })
	h.settle()

	if got := nodeIDOf(t, h, lower); got != lowerID {
		t.Errorf("the surviving dialog was remounted (NodeID %d -> %d); the scrim "+
			"was reinserted by removing and re-adding it", lowerID, got)
	}
	if got := nodeIDOf(t, h, lowerBtn); got != btnID {
		t.Errorf("the survivor's button was remounted (NodeID %d -> %d)", btnID, got)
	}
	if !focusedOn(t, h, lowerBtn) {
		t.Error("the survivor lost focus when the dialog above it closed")
	}
	// And it is still usable: Escape must still reach it.
	var dismissed atomic.Int64
	unsub := tui.Subscribe(h.app.Bus(), func(widget.OverlayDismissedEvent) { dismissed.Add(1) })
	defer unsub()
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	h.waitFor("the survivor still responds to Escape", func() bool { return dismissed.Load() == 1 })
}

// TestDismissingANonTopDialogUnwindsEverythingAboveIt.
//
// Dialogs are a stack. Closing one in the middle while leaving those above open
// produces a state the model does not have — a dialog on top of nothing, still
// trapping focus — and the ones above are then unreachable and uncloseable.
func TestDismissingANonTopDialogUnwindsEverythingAboveIt(t *testing.T) {
	bottom := widget.NewModal(widget.NewText("1"), widget.WithModalTitle("One"))
	middle := widget.NewModal(widget.NewText("2"), widget.WithModalTitle("Two"))
	top := widget.NewModal(widget.NewText("3"), widget.WithModalTitle("Three"))

	h, host, _ := modalFixture(t, bottom, 40, 14)
	defer h.stop()
	openOn(t, h, bottom, host)
	openOn(t, h, middle, host)
	openOn(t, h, top, host)

	var mu sync.Mutex
	var reasons []widget.DismissReason
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.OverlayDismissedEvent) {
		mu.Lock()
		reasons = append(reasons, ev.Reason)
		mu.Unlock()
	})
	defer unsub()

	h.onLoop(func() { bottom.Dismiss(widget.DismissAccept) })
	h.waitFor("three dismissals published", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(reasons) == 3
	})
	h.settle()

	for _, m := range []struct {
		name string
		m    *widget.Modal
	}{{"bottom", bottom}, {"middle", middle}, {"top", top}} {
		if m.m.IsOpen() {
			t.Errorf("%s is still open after the bottom dialog was dismissed", m.name)
		}
	}
	var left *widget.Modal
	h.onLoop(func() { left = host.TopModal() })
	if left != nil {
		t.Error("the host still holds an open dialog")
	}

	mu.Lock()
	got := append([]widget.DismissReason(nil), reasons...)
	mu.Unlock()
	// Topmost first, and the ones the user did not act on are REPLACED: reporting
	// the caller's Accept for all three would attribute an answer to two dialogs
	// nobody answered.
	want := []widget.DismissReason{
		widget.DismissReplaced, widget.DismissReplaced, widget.DismissAccept,
	}
	if len(got) != len(want) {
		t.Fatalf("%d dismissal events, want %d (one per dialog)", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dismissal %d reported %v, want %v", i, got[i], want[i])
		}
	}
}

// ─── dismissal ordering ──────────────────────────────────────────────────────

// TestAnOnDismissCallbackMayReopenTheDialog.
//
// "Are you sure?" that reopens itself is ordinary application code. Running the
// callback while the component is still mounted makes it a crash instead: the
// reopen passes the open guard and the runtime refuses to mount one component
// twice.
func TestAnOnDismissCallbackMayReopenTheDialog(t *testing.T) {
	var host *widget.OverlayHost
	var m *widget.Modal
	var reopens atomic.Int64
	var reopenErr error

	m = widget.NewModal(widget.NewText("Body"), widget.WithOnDismiss(func(widget.DismissReason) {
		if reopens.Add(1) == 1 {
			reopenErr = m.Open(host)
		}
	}))
	h, hh, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	host = hh
	openOn(t, h, m, host)

	h.onLoop(func() { m.Dismiss(widget.DismissProgrammatic) })
	h.settle()

	if reopenErr != nil {
		t.Fatalf("reopening from the dismissal callback failed: %v", reopenErr)
	}
	if !m.IsOpen() {
		t.Error("the dialog did not reopen; the callback ran but the reopen did not take")
	}
	var top *widget.Modal
	h.onLoop(func() { top = host.TopModal() })
	if top != m {
		t.Error("the reopened dialog is not on the host's stack")
	}
}

// TestOpenMovesFocusBeforeItReturns.
//
// A dialog that is mounted and covering the UI while the control underneath
// still holds focus is a real window, however short: input already queued behind
// Open reaches content the dialog exists to trap. Deferring the focus move to a
// scheduled step created that window for no benefit — direct RequestFocus is
// legal before layout.
func TestOpenMovesFocusBeforeItReturns(t *testing.T) {
	ok := widget.NewButton("OK", widget.WithRole(widget.ButtonRoleAccept), widget.WithDefault(true))
	m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(ok))
	h, host, base := modalFixture(t, m, 40, 12)
	defer h.stop()

	var baseStillFocused, dialogFocused bool
	// Everything inside ONE loop turn: the assertion is about the state the
	// instant Open returns, and settling first would let a scheduled step run
	// and hide exactly the defect under test.
	h.onLoop(func() {
		if err := m.Open(host); err != nil {
			t.Errorf("Open: %v", err)
			return
		}
		baseStillFocused = base.Context().Focused()
		dialogFocused = ok.Context() != nil && ok.Context().Focused()
	})
	h.settle()

	if baseStillFocused {
		t.Error("Open returned with the covered control still focused")
	}
	if !dialogFocused {
		t.Error("Open returned before focus reached the dialog")
	}
}

// TestOpenRollsBackWhenTheDialogCannotMount.
//
// The documented guarantee is that a failed Open leaves no trace. Committing
// open/host state before the mount broke it for the one reachable case: a
// descendant already mounted elsewhere, which leaves a dialog marked open with
// nothing on screen and no way to close it.
func TestOpenRollsBackWhenTheDialogCannotMount(t *testing.T) {
	// The BODY is the reachable case. Button lists are validated at both
	// construction and Open, so a foreign button never reaches the mount; a body
	// is the caller's arbitrary component and is not, which is exactly why the
	// mount itself has to be able to fail safely.
	shared := widget.NewText("shared")
	baseRow := tui.NewFlex(tui.Vertical)
	base := widget.NewButton("base")
	baseRow.Add(base, shared)
	host := widget.NewOverlayHost(baseRow)
	h := startApp(t, host, 40, 12)
	defer h.stop()
	h.onLoop(func() { base.Context().RequestFocus() })
	h.settle()

	first := widget.NewModal(widget.NewText("first"), widget.WithModalTitle("Alpha"))
	openOn(t, h, first, host)
	before := h.grid()

	bad := widget.NewModal(shared) // already mounted under baseRow
	var openErr error
	h.onLoop(func() { openErr = bad.Open(host) })
	h.settle()

	if openErr == nil {
		t.Fatal("Open accepted a dialog whose body is mounted elsewhere")
	}
	if !errors.Is(openErr, widget.ErrModalNotMountable) {
		t.Errorf("Open returned %v, want ErrModalNotMountable", openErr)
	}
	if bad.IsOpen() {
		t.Error("a refused Open left the dialog marked open")
	}
	var top *widget.Modal
	h.onLoop(func() { top = host.TopModal() })
	if top != first {
		t.Error("a refused Open changed which dialog is topmost")
	}
	if got := h.grid(); got != before {
		t.Errorf("a refused Open changed the screen.\nwant:\n%s\ngot:\n%s", before, got)
	}
}

// ─── Escape and provenance ───────────────────────────────────────────────────

// TestEscapeActivatesTheCancelButtonThroughTheRuntime.
//
// "Escape means exactly what pressing the Cancel button means" is a claim about
// what OBSERVERS see, not only about which callback runs. The runtime is the
// sole publisher of ControlActivatedEvent, so calling Activate directly runs the
// callback and publishes nothing: a command log, an undo stack or a test
// watching the bus sees a keypress that activated nothing.
func TestEscapeActivatesTheCancelButtonThroughTheRuntime(t *testing.T) {
	var ran atomic.Int64
	cancel := widget.NewButton("Nope",
		widget.WithRole(widget.ButtonRoleReject),
		widget.WithOnActivate(func() { ran.Add(1) }))
	m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(cancel))

	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	var mu sync.Mutex
	var acts []tui.ControlActivatedEvent
	var dismissals []widget.OverlayDismissedEvent
	unsubA := tui.Subscribe(h.app.Bus(), func(ev tui.ControlActivatedEvent) {
		mu.Lock()
		acts = append(acts, ev)
		mu.Unlock()
	})
	defer unsubA()
	unsubD := tui.Subscribe(h.app.Bus(), func(ev widget.OverlayDismissedEvent) {
		mu.Lock()
		dismissals = append(dismissals, ev)
		mu.Unlock()
	})
	defer unsubD()

	cancelID := nodeIDOf(t, h, cancel)
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	h.waitFor("dismissed", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(dismissals) == 1
	})
	h.settle()

	mu.Lock()
	gotActs := append([]tui.ControlActivatedEvent(nil), acts...)
	gotDis := append([]widget.OverlayDismissedEvent(nil), dismissals...)
	mu.Unlock()

	if ran.Load() != 1 {
		t.Errorf("the Cancel callback ran %d times, want 1", ran.Load())
	}
	if len(gotActs) != 1 {
		t.Fatalf("%d ControlActivatedEvent, want exactly 1; Escape activated the "+
			"button without the runtime recording it", len(gotActs))
	}
	if gotActs[0].Owner != cancelID {
		t.Errorf("activation names node %d, want the Cancel button %d",
			gotActs[0].Owner, cancelID)
	}
	if gotActs[0].Origin != tui.OriginKey {
		t.Errorf("activation origin = %v, want OriginKey: the provenance of the "+
			"keypress must survive the forward", gotActs[0].Origin)
	}
	if len(gotDis) != 1 || gotDis[0].Reason != widget.DismissCancel {
		t.Errorf("dismissals = %v, want exactly one with reason cancel", gotDis)
	}
}

// ─── the new public controls ─────────────────────────────────────────────────

// TestModalPointerPolicyReachesTheWholeSubtree.
//
// "This dialog is keyboard-only" is one decision, so it is stated once on the
// dialog rather than repeated on every control. Set before mount it must still
// apply, because NewModal(...).WithPointerPolicy(...) is the natural way to
// write it and runs before there is any Context.
func TestModalPointerPolicyReachesTheWholeSubtree(t *testing.T) {
	for _, tc := range []struct {
		name     string
		preMount bool
	}{{"set before mount", true}, {"set while open", false}} {
		t.Run(tc.name, func(t *testing.T) {
			var acts atomic.Int64
			ok := widget.NewButton("OK",
				widget.WithRole(widget.ButtonRoleAccept), widget.WithDefault(true),
				widget.WithOnActivate(func() { acts.Add(1) }))
			m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(ok))
			if tc.preMount {
				m.WithPointerPolicy(tui.PointerDisabled)
			}
			h, host, _ := modalFixture(t, m, 40, 12)
			defer h.stop()
			openOn(t, h, m, host)
			if !tc.preMount {
				h.onLoop(func() { m.WithPointerPolicy(tui.PointerDisabled) })
				h.settle()
			}

			// A press and release on the button's own cell. With the pointer
			// disabled for the subtree, neither reaches it.
			bx, by := cellOfLabel(t, h, "OK")
			h.inject(
				tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: bx, Y: by},
				tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: bx, Y: by},
			)
			h.settle()
			h.settle()

			if got := acts.Load(); got != 0 {
				t.Errorf("the button activated %d times through a disabled pointer "+
					"policy set on the dialog", got)
			}
			// The control: the keyboard still works, so the test is observing a
			// pointer policy rather than a broken button.
			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
			h.waitFor("keyboard activation still works", func() bool { return acts.Load() == 1 })
		})
	}
}

// TestModalWithStyleRestylesTheLiveCardAndScrim.
//
// A theme swap has to reach a dialog that is already open, not merely the next
// one. The scrim is the host's layer but wears this dialog's look, so a restyle
// that stopped at the card would leave the backdrop in the old theme.
func TestModalWithStyleRestylesTheLiveCardAndScrim(t *testing.T) {
	m := widget.NewModal(widget.NewText("Body"), widget.WithModalTitle("T"))
	h, host, _ := modalFixture(t, m, 30, 8)
	defer h.stop()
	openOn(t, h, m, host)

	underlined := widget.NewModalStyle(styleOf(1), styleOf(2)).
		WithScrim(style.New().Underline(true)).
		WithCard(style.New().Underline(true))
	h.onLoop(func() { m.WithStyle(underlined) })
	h.settle()

	grid := h.tb.Snapshot()
	corner := grid[len(grid)-1][0] // outside any centred card: the backdrop
	if corner.Attrs.Mask&tui.AttrUnderline == 0 {
		t.Error("the live scrim kept its old style; a restyle of an open dialog " +
			"stopped at the card")
	}
	x0, y0, _, _ := cardBounds(t, h)
	// SAMPLED ON THE CARD'S OWN FILL, one cell inside the frame. The card's
	// centre is not a safe probe: a child painting there covers it, and since
	// the title moved onto the border the body sits on the middle row of a
	// short card. That would report the card unstyled while the cell simply
	// belonged to the Text.
	pad := grid[y0+1][x0+1]
	if pad.Attrs.Mask&tui.AttrUnderline == 0 {
		t.Error("the live card kept its old style")
	}
}

// TestDetachRemovesAnAttachedFloat.
//
// Attach used to be permanent, which made a Float usable only by an owner that
// lived as long as the application. Detaching runs the ordinary unmount cascade,
// so a Float holding focus has it returned rather than stranded.
func TestDetachRemovesAnAttachedFloat(t *testing.T) {
	inner := widget.NewButton("Inside")
	f := widget.NewFloat(inner)
	base := widget.NewButton("base")
	host := widget.NewOverlayHost(base)
	h := startApp(t, host, 30, 10)
	defer h.stop()

	h.onLoop(func() {
		host.Attach(f)
		host.Attach(f) // twice: one layer, not two
		f.Show()
	})
	h.settle()
	h.onLoop(func() { inner.Context().RequestFocus() })
	h.settle()
	if !focusedOn(t, h, inner) {
		t.Fatal("precondition failed: the Float's control never took focus")
	}

	h.onLoop(func() { host.Detach(f) })
	h.settle()

	// Read the SCREEN, not the widget's cached context pointer: Base keeps that
	// pointer across an unmount, so it reports "mounted" for a component the
	// runtime has already forgotten and would pass whether or not Detach worked.
	if got := h.grid(); strings.Contains(got, "Inside") {
		t.Errorf("Detach left the Float's content on screen:\n%s", got)
	}
	if f.Shown() {
		t.Error("Detach left the Float believing it was still shown; a later Show " +
			"would take the already-shown early return and mount nothing")
	}
	if !focusedOn(t, h, base) {
		t.Error("focus was not returned to the base after the Float was detached")
	}
	// Detaching again is a no-op rather than a panic.
	h.onLoop(func() { host.Detach(f) })
	h.settle()

	// And it can be used again: a Float that can only be detached once is not
	// reusable, which was the whole point of adding Detach.
	h.onLoop(func() {
		host.Attach(f)
		f.Show()
	})
	h.settle()
	if got := h.grid(); !strings.Contains(got, "Inside") {
		t.Errorf("the Float could not be re-attached and shown:\n%s", got)
	}
}

// TestModalPointerPolicyRejectsAnOutOfRangeValue.
//
// Probing the FIRST invalid value, not a far one: an off-by-one bound rejects
// 200 exactly as readily as a correct bound does, so a far probe cannot see the
// edge move. The last valid value is asserted accepted in the same breath, which
// is the half that catches a bound that moved the other way.
func TestModalPointerPolicyRejectsAnOutOfRangeValue(t *testing.T) {
	m := widget.NewModal(widget.NewText("Body"))
	if f := fatalFromWidgetExt(func() { m.WithPointerPolicy(tui.PointerDisabled) }); f != nil {
		t.Errorf("the last valid policy was rejected: %v", f.Rule)
	}
	if f := fatalFromWidgetExt(func() { m.WithPointerPolicy(tui.PointerDisabled + 1) }); f == nil {
		t.Error("Modal.WithPointerPolicy accepted the first value past the declared set")
	}
}

// TestASurvivorThatWantsNoBackdropGetsNone.
//
// The backdrop belongs to whichever dialog is on top, and "on top" changes when
// one closes — so the survivor's own preference decides, not the preference of
// the dialog that just went away. A restore that assumed every dialog wants one
// would dim the screen behind a dialog that explicitly asked not to.
func TestASurvivorThatWantsNoBackdropGetsNone(t *testing.T) {
	lower := widget.NewModal(widget.NewText("plain"),
		widget.WithModalTitle("Alpha"), widget.WithScrim(false))
	upper := widget.NewModal(widget.NewText("later"), widget.WithModalTitle("Bravo"))

	h, host, base := modalFixture(t, lower, 40, 12)
	defer h.stop()
	openOn(t, h, lower, host)
	alone := h.grid()
	if !strings.Contains(alone, base.Label()) {
		t.Fatalf("precondition failed: an unscrimmed dialog should leave the base "+
			"visible, but %q is not on screen:\n%s", base.Label(), alone)
	}

	openOn(t, h, upper, host)
	h.onLoop(func() { upper.Dismiss(widget.DismissProgrammatic) })
	h.settle()

	if got := h.grid(); got != alone {
		t.Errorf("closing the upper dialog did not restore the unscrimmed screen.\n"+
			"want:\n%s\ngot:\n%s", alone, got)
	}
}

// TestRestylingADialogThatIsNotOnTopLeavesTheBackdropAlone.
//
// Only the topmost dialog owns the backdrop. A lower dialog restyling itself
// must repaint its own card and nothing else — repainting the scrim would let a
// covered dialog change the look of the one covering it.
func TestRestylingADialogThatIsNotOnTopLeavesTheBackdropAlone(t *testing.T) {
	lower := widget.NewModal(widget.NewText("low"), widget.WithModalTitle("Alpha"))
	upper := widget.NewModal(widget.NewText("up"), widget.WithModalTitle("Bravo"))
	h, host, _ := modalFixture(t, lower, 30, 10)
	defer h.stop()
	openOn(t, h, lower, host)
	openOn(t, h, upper, host)

	underlined := widget.NewModalStyle(styleOf(1), styleOf(2)).
		WithScrim(style.New().Underline(true))
	h.onLoop(func() { lower.WithStyle(underlined) })
	h.settle()

	grid := h.tb.Snapshot()
	corner := grid[len(grid)-1][0]
	if corner.Attrs.Mask&tui.AttrUnderline != 0 {
		t.Error("a dialog that is not on top restyled the backdrop it does not own")
	}

	// The control: the same style on the TOP dialog does reach the scrim, so the
	// assertion above is observing ownership rather than a restyle that never
	// works from this fixture at all.
	h.onLoop(func() { upper.WithStyle(underlined) })
	h.settle()
	grid = h.tb.Snapshot()
	if grid[len(grid)-1][0].Attrs.Mask&tui.AttrUnderline == 0 {
		t.Error("the topmost dialog's restyle did not reach the backdrop either")
	}
}

// TestACallbackDismissingADialogTheUnwindWillReachClosesItOnce.
//
// Unwinding a stack runs each dialog's callback, and one of those may dismiss a
// dialog further down that the same unwind is about to reach. Without a guard
// the second visit unmounts nothing and publishes a duplicate event, so a
// listener counting closures counts one dialog twice.
func TestACallbackDismissingADialogTheUnwindWillReachClosesItOnce(t *testing.T) {
	bottom := widget.NewModal(widget.NewText("1"), widget.WithModalTitle("One"))
	middle := widget.NewModal(widget.NewText("2"), widget.WithModalTitle("Two"))
	// The callback dismisses BOTH a dialog the unwind has yet to reach and the
	// TARGET of the transition itself. The target is the case a re-derived
	// position cannot handle on its own: its own close is still pending at the
	// bottom of dismissModal, so without an already-closed guard it is closed
	// once by the callback and once again on the way out.
	top := widget.NewModal(widget.NewText("3"), widget.WithModalTitle("Three"),
		widget.WithOnDismiss(func(widget.DismissReason) {
			middle.Dismiss(widget.DismissProgrammatic)
			bottom.Dismiss(widget.DismissProgrammatic)
		}))

	h, host, _ := modalFixture(t, bottom, 40, 14)
	defer h.stop()
	openOn(t, h, bottom, host)
	openOn(t, h, middle, host)
	openOn(t, h, top, host)

	var mu sync.Mutex
	var seen []widget.OverlayDismissedEvent
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.OverlayDismissedEvent) {
		mu.Lock()
		seen = append(seen, ev)
		mu.Unlock()
	})
	defer unsub()

	h.onLoop(func() { bottom.Dismiss(widget.DismissAccept) })
	h.waitFor("three dismissals published", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen) >= 3
	})
	h.settle()
	h.settle()

	mu.Lock()
	got := append([]widget.OverlayDismissedEvent(nil), seen...)
	mu.Unlock()
	if len(got) != 3 {
		t.Errorf("%d dismissal events for three dialogs, want exactly 3", len(got))
	}
	owners := map[tui.NodeID]int{}
	for _, ev := range got {
		owners[ev.Owner]++
	}
	for owner, n := range owners {
		if n != 1 {
			t.Errorf("dialog %d was reported closed %d times", owner, n)
		}
	}
}

// TestADialogWithNoBodyIsStillUsable.
//
// A confirmation whose whole content is its buttons is an ordinary dialog, not
// a degenerate one. The card lays out and paints around an absent body rather
// than requiring callers to pass an empty placeholder.
func TestADialogWithNoBodyIsStillUsable(t *testing.T) {
	ok := widget.NewButton("OK", widget.WithRole(widget.ButtonRoleAccept), widget.WithDefault(true))
	m := widget.NewModal(nil, widget.WithModalTitle("Sure?"), widget.WithButtons(ok))
	h, host, _ := modalFixture(t, m, 30, 10)
	defer h.stop()
	openOn(t, h, m, host)

	if got := h.grid(); !strings.Contains(got, "Sure?") || !strings.Contains(got, "OK") {
		t.Errorf("a bodiless dialog did not paint its title and button:\n%s", got)
	}
	if !focusedOn(t, h, ok) {
		t.Error("focus did not reach the only button of a bodiless dialog")
	}
	// And a RECONCILE places them correctly too. Construction mounts the list
	// directly, so only SetButtons exercises the offset the card applies when
	// there is no body ahead of its buttons — with one, a wrong offset puts the
	// first button past the end of the child list.
	more := widget.NewButton("More")
	var err error
	h.onLoop(func() { err = m.SetButtons(more, ok) })
	if err != nil {
		t.Fatalf("SetButtons on a bodiless dialog: %v", err)
	}
	h.settle()
	if got := h.grid(); !strings.Contains(got, "More") {
		t.Errorf("the reconciled button is not on screen:\n%s", got)
	}
	// Focus stays on OK, which is still focusable, now second.
	var sel int
	h.onLoop(func() { sel = m.SelectedButton() })
	if sel != 1 {
		t.Errorf("SelectedButton() = %d, want 1 (OK, still focused, after the reorder)", sel)
	}
}

// ─── component reuse ─────────────────────────────────────────────────────────

// TestAReopenedDialogMayKeepItsButtons.
//
// A dialog is opened, dismissed and opened again with the same controls — the
// ordinary shape of "ask the user, then ask again". An unmounted component is
// reusable by design, so nothing here should be refused.
//
// The trap this pins is that a widget REMEMBERS its last Context after the
// runtime has forgotten the node, so "has a Context" is not "is mounted". A
// validator built on the retained pointer reads a perfectly reusable button as
// belonging to someone else.
func TestAReopenedDialogMayKeepItsButtons(t *testing.T) {
	ok := widget.NewButton("OK", widget.WithRole(widget.ButtonRoleAccept), widget.WithDefault(true))
	m := widget.NewModal(widget.NewText("Again?"), widget.WithButtons(ok))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()

	openOn(t, h, m, host)
	h.onLoop(func() { m.Dismiss(widget.DismissProgrammatic) })
	h.settle()

	var err error
	h.onLoop(func() { err = m.Open(host) })
	h.settle()
	if err != nil {
		t.Fatalf("reopening a dialog with its own retained button failed: %v", err)
	}
	if !m.IsOpen() {
		t.Error("the dialog did not reopen")
	}
	if !focusedOn(t, h, ok) {
		t.Error("the reopened dialog did not take focus on its own button")
	}
}

// TestADismissCallbackMayReopenADialogThatHasButtons.
//
// The reopen-from-callback guarantee, exercised on a dialog with controls. The
// existing callback test uses a buttonless dialog, so it cannot observe a
// validation rule that only fires once there is a button to validate.
func TestADismissCallbackMayReopenADialogThatHasButtons(t *testing.T) {
	var host *widget.OverlayHost
	var m *widget.Modal
	var reopens atomic.Int64
	var reopenErr error

	ok := widget.NewButton("OK", widget.WithRole(widget.ButtonRoleAccept), widget.WithDefault(true))
	m = widget.NewModal(widget.NewText("Body"),
		widget.WithButtons(ok),
		widget.WithOnDismiss(func(widget.DismissReason) {
			if reopens.Add(1) == 1 {
				reopenErr = m.Open(host)
			}
		}))
	h, hh, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	host = hh
	openOn(t, h, m, host)

	h.onLoop(func() { m.Dismiss(widget.DismissProgrammatic) })
	h.settle()

	if reopenErr != nil {
		t.Fatalf("reopening from the dismissal callback failed: %v", reopenErr)
	}
	if !m.IsOpen() {
		t.Error("the dialog with buttons did not reopen from its own callback")
	}
}

// TestAButtonMayBeRemovedAndAddedBackAgain.
//
// Taking a control out of a dialog and putting the same value back is ordinary
// list editing, and the intervening unmount must not make it someone else's
// component.
func TestAButtonMayBeRemovedAndAddedBackAgain(t *testing.T) {
	keep := widget.NewButton("Keep")
	gone := widget.NewButton("Gone")
	m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(keep, gone))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	var errOut, errBack error
	h.onLoop(func() { errOut = m.SetButtons(keep) })
	h.settle()
	if errOut != nil {
		t.Fatalf("removing a button: %v", errOut)
	}
	h.onLoop(func() { errBack = m.SetButtons(keep, gone) })
	h.settle()
	if errBack != nil {
		t.Fatalf("adding the same button back: %v", errBack)
	}
	if got := h.grid(); !strings.Contains(got, "Gone") {
		t.Errorf("the re-added button is not on screen:\n%s", got)
	}
}

// TestEveryValidationErrorMatchesBothTheUmbrellaAndItsOwnSentinel.
//
// The exported documentation promises callers can match either level. A caller
// writing errors.Is(err, ErrInvalidButtonList) to mean "the list was bad" gets
// false for every list, so the general handler never runs and the failure looks
// like an unrelated error class.
func TestEveryValidationErrorMatchesBothTheUmbrellaAndItsOwnSentinel(t *testing.T) {
	for _, tc := range []struct {
		name string
		leaf error
		list func(base, keep *widget.Button) []*widget.Button
	}{
		{"nil entry", widget.ErrNilButton,
			func(_, keep *widget.Button) []*widget.Button { return []*widget.Button{keep, nil} }},
		{"repeated button", widget.ErrRepeatedButton,
			func(_, keep *widget.Button) []*widget.Button { return []*widget.Button{keep, keep} }},
		{"foreign button", widget.ErrForeignButton,
			func(base, keep *widget.Button) []*widget.Button { return []*widget.Button{keep, base} }},
		{"duplicate role", widget.ErrDuplicateButtonRole,
			func(_, keep *widget.Button) []*widget.Button {
				return []*widget.Button{
					widget.NewButton("X", widget.WithRole(widget.ButtonRoleReject)),
					widget.NewButton("Y", widget.WithRole(widget.ButtonRoleReject)),
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keep := widget.NewButton("Keep")
			m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(keep))
			h, host, base := modalFixture(t, m, 40, 12)
			defer h.stop()
			openOn(t, h, m, host)

			var err error
			h.onLoop(func() { err = m.SetButtons(tc.list(base, keep)...) })
			h.settle()

			if err == nil {
				t.Fatal("the list was accepted")
			}
			if !errors.Is(err, tc.leaf) {
				t.Errorf("errors.Is(err, %v) = false; err = %v", tc.leaf, err)
			}
			if !errors.Is(err, widget.ErrInvalidButtonList) {
				t.Errorf("errors.Is(err, ErrInvalidButtonList) = false, so a caller "+
					"handling the whole class never matches; err = %v", err)
			}
		})
	}
}

// TestOpeningADialogThatIsAlreadyMountedElsewhereIsRefused.
//
// A Modal used as ordinary content and then opened would have to be mounted
// twice. Refused BEFORE the host adds it, because the unwind for a failed add
// removes the layer — and removing a component mounted elsewhere unmounts it
// from where it legitimately lives.
func TestOpeningADialogThatIsAlreadyMountedElsewhereIsRefused(t *testing.T) {
	inline := widget.NewModal(widget.NewText("inline"), widget.WithModalTitle("Inline"))
	base := widget.NewButton("base")
	root := tui.NewFlex(tui.Vertical)
	root.Add(base, inline) // the dialog is ordinary content here
	host := widget.NewOverlayHost(root)
	h := startApp(t, host, 40, 14)
	defer h.stop()
	h.settle()
	before := h.grid()

	var err error
	h.onLoop(func() { err = inline.Open(host) })
	h.settle()

	if !errors.Is(err, widget.ErrModalNotMountable) {
		t.Errorf("Open returned %v, want ErrModalNotMountable", err)
	}
	if inline.IsOpen() {
		t.Error("a refused Open left the dialog marked open")
	}
	if got := h.grid(); got != before {
		t.Errorf("a refused Open changed the screen.\nwant:\n%s\ngot:\n%s", before, got)
	}
}
