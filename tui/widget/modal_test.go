package widget_test

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

// A dialog is mostly lifecycle and focus policy, and both are the kind of thing
// that looks right on screen while being wrong underneath. These tests go after
// the states that are easy to get almost-right: opening twice, dismissing
// twice, and what holds focus when every control is unavailable.

// modalFixture builds host(base + dialog-ready) at a known size and returns
// both. The base is a focusable leaf, so "focus moved into the dialog" is
// distinguishable from "focus was already there".
func modalFixture(t *testing.T, m *widget.Modal, w, h int) (*harness, *widget.OverlayHost, *widget.Button) {
	t.Helper()
	base := widget.NewButton("base")
	host := widget.NewOverlayHost(base)
	hh := startApp(t, host, w, h)
	hh.onLoop(func() { base.Context().RequestFocus() })
	hh.settle()
	return hh, host, base
}

func openOn(t *testing.T, h *harness, m *widget.Modal, host *widget.OverlayHost) {
	t.Helper()
	var err error
	h.onLoop(func() { err = m.Open(host) })
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	h.settle()
}

// TestOpeningADialogTrapsFocusAndPrefersTheDefaultButton.
//
// The default-role button is preferred UNCONDITIONALLY, not as a tie-break: a
// dialog's affirmative action is where a user expects to land, and choosing it
// only when nothing else qualified would make the landing spot depend on the
// order the buttons were listed in.
func TestOpeningADialogTrapsFocusAndPrefersTheDefaultButton(t *testing.T) {
	cancel := widget.NewButton("Cancel", widget.WithRole(widget.ButtonRoleCancel))
	ok := widget.NewButton("OK", widget.WithRole(widget.ButtonRoleDefault))
	// Cancel is listed FIRST, so "the first enabled button" would pick it.
	m := widget.NewModal(widget.NewText("Sure?"), widget.WithButtons(cancel, ok))

	h, host, base := modalFixture(t, m, 40, 12)
	defer h.stop()

	var baseFocusedBefore bool
	h.onLoop(func() { baseFocusedBefore = base.Context().Focused() })
	if !baseFocusedBefore {
		t.Fatal("precondition failed: the base never held focus, so nothing has to move")
	}

	openOn(t, h, m, host)

	var okFocused, baseFocused bool
	var sel int
	h.onLoop(func() {
		okFocused = ok.Context() != nil && ok.Context().Focused()
		baseFocused = base.Context().Focused()
		sel = m.SelectedButton()
	})
	if baseFocused {
		t.Error("focus stayed outside the dialog; opening one must move focus into it")
	}
	if !okFocused {
		t.Error("focus did not land on the Default-role button, although it was listed second")
	}
	if sel != 1 {
		t.Errorf("SelectedButton() = %d, want 1 (the Default button's index)", sel)
	}
	if !m.IsOpen() {
		t.Error("IsOpen() is false after a successful Open")
	}
}

// TestOpeningATwiceOpenedDialogChangesNothing. Two code paths opening the same
// dialog would otherwise mount it twice and leave one copy unreachable.
func TestOpeningATwiceOpenedDialogChangesNothing(t *testing.T) {
	ok := widget.NewButton("OK")
	m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(ok))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()

	openOn(t, h, m, host)

	var err error
	var top *widget.Modal
	h.onLoop(func() {
		err = m.Open(host)
		top = host.TopModal()
	})
	h.settle()

	if !errors.Is(err, widget.ErrModalAlreadyOpen) {
		t.Errorf("second Open returned %v, want ErrModalAlreadyOpen", err)
	}
	if top != m {
		t.Error("the stack no longer holds the dialog as its top layer")
	}
	if !m.IsOpen() {
		t.Error("the dialog closed itself on a refused second Open")
	}
}

// TestOpeningWithoutAHostIsRefused.
func TestOpeningWithoutAHostIsRefused(t *testing.T) {
	m := widget.NewModal(widget.NewText("Body"))
	if err := m.Open(nil); !errors.Is(err, widget.ErrNilOverlayHost) {
		t.Errorf("Open(nil) returned %v, want ErrNilOverlayHost", err)
	}
	if m.IsOpen() {
		t.Error("a refused Open left the dialog marked open")
	}
}

// TestDismissPublishesExactlyOnceAndRunsTheCallbackFirst.
func TestDismissPublishesExactlyOnceAndRunsTheCallbackFirst(t *testing.T) {
	var order []string
	var mu sync.Mutex
	note := func(s string) { mu.Lock(); order = append(order, s); mu.Unlock() }

	m := widget.NewModal(widget.NewText("Body"),
		widget.WithOnDismiss(func(widget.DismissReason) { note("callback") }))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	var got []widget.OverlayDismissedEvent
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.OverlayDismissedEvent) {
		note("event")
		mu.Lock()
		got = append(got, ev)
		mu.Unlock()
	})
	defer unsub()

	h.onLoop(func() { m.Dismiss(widget.DismissProgrammatic) })
	h.waitFor("dismissal published", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(got) == 1
	})
	h.settle()

	mu.Lock()
	seq := append([]string(nil), order...)
	evs := append([]widget.OverlayDismissedEvent(nil), got...)
	mu.Unlock()

	if len(evs) != 1 {
		t.Fatalf("%d dismissal events, want exactly 1", len(evs))
	}
	if evs[0].Reason != widget.DismissProgrammatic {
		t.Errorf("reason = %v, want %v", evs[0].Reason, widget.DismissProgrammatic)
	}
	// The callback completes before any observer sees the event. Worth stating,
	// but note WHY: Bus.Publish is enqueue-only, so a subscriber always runs in
	// a later drain than the Dismiss that published. The guarantee comes from
	// the bus rather than from the order of these two statements, which is why
	// no mutation of that order can fail this assertion.
	if len(seq) != 2 || seq[0] != "callback" || seq[1] != "event" {
		t.Errorf("order = %v, want [callback event]", seq)
	}
	if m.IsOpen() {
		t.Error("IsOpen() is true after dismissal")
	}
}

// TestDismissingAClosedDialogIsSilent. Dismissal arrives from several
// directions at once — Escape, a Cancel button, the program — so idempotence is
// what stops one closure producing three events.
func TestDismissingAClosedDialogIsSilent(t *testing.T) {
	var callbacks atomic.Int64
	m := widget.NewModal(widget.NewText("Body"),
		widget.WithOnDismiss(func(widget.DismissReason) { callbacks.Add(1) }))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	var events atomic.Int64
	unsub := tui.Subscribe(h.app.Bus(), func(widget.OverlayDismissedEvent) { events.Add(1) })
	defer unsub()

	h.onLoop(func() {
		m.Dismiss(widget.DismissProgrammatic)
		m.Dismiss(widget.DismissProgrammatic) // already closed
		m.Dismiss(widget.DismissEscape)       // still closed
	})
	h.waitFor("first dismissal published", func() bool { return events.Load() >= 1 })
	h.settle()

	if got := callbacks.Load(); got != 1 {
		t.Errorf("callback ran %d times, want exactly 1", got)
	}
	if got := events.Load(); got != 1 {
		t.Errorf("%d dismissal events, want exactly 1", got)
	}
}

// TestEscapeResolvesTheCancelRoleRatherThanALabel.
//
// Matching "Cancel" or "No" by text breaks the moment an application is
// translated; matching the last button breaks the moment the list is reordered.
// The role is stated once by the author and survives both.
func TestEscapeResolvesTheCancelRoleRatherThanALabel(t *testing.T) {
	var cancelled atomic.Int64
	// Deliberately misleading: the CANCEL role is on the button labelled "Nope",
	// and a button labelled "Cancel" carries no role at all.
	decoy := widget.NewButton("Cancel")
	real := widget.NewButton("Nope", widget.WithRole(widget.ButtonRoleCancel),
		widget.WithOnActivate(func() { cancelled.Add(1) }))
	m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(decoy, real))

	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	var reason widget.DismissReason
	var seen atomic.Int64
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.OverlayDismissedEvent) {
		reason = ev.Reason
		seen.Add(1)
	})
	defer unsub()

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	h.waitFor("dismissed by escape", func() bool { return seen.Load() == 1 })
	h.settle()

	if got := cancelled.Load(); got != 1 {
		t.Errorf("the Cancel-ROLE button activated %d times, want 1", got)
	}
	if reason != widget.DismissCancel {
		t.Errorf("reason = %v, want %v: a dialog with a cancel role reports that role",
			reason, widget.DismissCancel)
	}
}

// TestEscapeWithoutACancelRoleStillCloses.
func TestEscapeWithoutACancelRoleStillCloses(t *testing.T) {
	m := widget.NewModal(widget.NewText("Body"),
		widget.WithButtons(widget.NewButton("OK", widget.WithRole(widget.ButtonRoleDefault))))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	var reason widget.DismissReason
	var seen atomic.Int64
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.OverlayDismissedEvent) {
		reason = ev.Reason
		seen.Add(1)
	})
	defer unsub()

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	h.waitFor("dismissed", func() bool { return seen.Load() == 1 })

	if reason != widget.DismissEscape {
		t.Errorf("reason = %v, want %v", reason, widget.DismissEscape)
	}
}

// TestTheDialogItselfTakesFocusOnlyWhenNoButtonCan.
//
// The ring inside a trap must never be empty. With every button disabled and
// the dialog also refusing focus there would be nothing focusable inside the
// trap at all — Escape would become unreachable and the dialog uncloseable by
// keyboard. So the dialog is the target of last resort, and steps aside as soon
// as a real control is available.
func TestTheDialogItselfTakesFocusOnlyWhenNoButtonCan(t *testing.T) {
	t.Run("zero buttons", func(t *testing.T) {
		m := widget.NewModal(widget.NewText("Body"))
		if !m.AcceptsFocus() {
			t.Error("a dialog with no buttons refuses focus, leaving the trap empty")
		}
	})

	t.Run("one enabled button", func(t *testing.T) {
		m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(widget.NewButton("OK")))
		if m.AcceptsFocus() {
			t.Error("a dialog with an enabled button takes focus itself; the button should")
		}
	})

	t.Run("all buttons disabled", func(t *testing.T) {
		b := widget.NewButton("OK")
		m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(b))
		h, host, _ := modalFixture(t, m, 40, 12)
		defer h.stop()
		openOn(t, h, m, host)

		h.onLoop(func() { b.SetEnabled(false) })
		h.settle()

		if !m.AcceptsFocus() {
			t.Error("with every button disabled the dialog must take focus itself, or " +
				"the trap has no focusable node and Escape becomes unreachable")
		}
		var modalFocused bool
		var sel int
		h.onLoop(func() {
			modalFocused = m.Context().Focused()
			sel = m.SelectedButton()
		})
		if !modalFocused {
			t.Error("focus was not repaired onto the dialog node")
		}
		if sel != -1 {
			t.Errorf("SelectedButton() = %d, want -1 when focus is on the dialog itself", sel)
		}

		// And Escape still works, which is the whole reason for the rule.
		var seen atomic.Int64
		unsub := tui.Subscribe(h.app.Bus(), func(widget.OverlayDismissedEvent) { seen.Add(1) })
		defer unsub()
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
		h.waitFor("escape still closes an all-disabled dialog", func() bool { return seen.Load() == 1 })
	})
}

// TestSetButtonsRejectsADuplicateRoleWithoutChangingAnything.
func TestSetButtonsRejectsADuplicateRoleWithoutChangingAnything(t *testing.T) {
	ok := widget.NewButton("OK", widget.WithRole(widget.ButtonRoleDefault))
	m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(ok))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	var err error
	h.onLoop(func() {
		err = m.SetButtons(
			widget.NewButton("A", widget.WithRole(widget.ButtonRoleDefault)),
			widget.NewButton("B", widget.WithRole(widget.ButtonRoleDefault)),
		)
	})
	h.settle()

	if !errors.Is(err, widget.ErrDuplicateButtonRole) {
		t.Fatalf("SetButtons returned %v, want ErrDuplicateButtonRole", err)
	}
	// ATOMIC: the original list survives a rejected call.
	got := m.Buttons()
	if len(got) != 1 || got[0] != ok {
		t.Errorf("the button list changed after a rejected SetButtons (%d buttons); "+
			"validation must complete before any mutation", len(got))
	}
}

// TestSetButtonsReplacesAValidList.
func TestSetButtonsReplacesAValidList(t *testing.T) {
	m := widget.NewModal(widget.NewText("Body"),
		widget.WithButtons(widget.NewButton("Old")))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	a := widget.NewButton("A", widget.WithRole(widget.ButtonRoleDefault))
	b := widget.NewButton("B", widget.WithRole(widget.ButtonRoleCancel))
	var err error
	h.onLoop(func() { err = m.SetButtons(a, b) })
	h.settle()

	if err != nil {
		t.Fatalf("SetButtons rejected a valid list: %v", err)
	}
	got := m.Buttons()
	if len(got) != 2 || got[0] != a || got[1] != b {
		t.Errorf("the button list was not replaced (%d buttons)", len(got))
	}
}

// TestConstructingWithADuplicateRolePanics — the construction adapter over the
// same rule the runtime setter returns an error for.
func TestConstructingWithADuplicateRolePanics(t *testing.T) {
	fatal := fatalFromWidgetExt(func() {
		widget.NewModal(widget.NewText("Body"), widget.WithButtons(
			widget.NewButton("A", widget.WithRole(widget.ButtonRoleCancel)),
			widget.NewButton("B", widget.WithRole(widget.ButtonRoleCancel)),
		))
	})
	if fatal == nil {
		t.Error("NewModal accepted two Cancel-role buttons")
	}

	// The control: one of each is fine, so the panic above is about the
	// duplicate rather than about roles in general.
	if f := fatalFromWidgetExt(func() {
		widget.NewModal(widget.NewText("Body"), widget.WithButtons(
			widget.NewButton("A", widget.WithRole(widget.ButtonRoleCancel)),
			widget.NewButton("B", widget.WithRole(widget.ButtonRoleDefault)),
		))
	}); f != nil {
		t.Errorf("NewModal rejected a legal list (%v)", f.Rule)
	}
}

// TestTheCardIsCentredWithinTheHost. A stack places an unaligned layer
// top-left, so centring is something the Modal must do rather than something it
// inherits — which is exactly why the card is a separate placed child.
func TestTheCardIsCentredWithinTheHost(t *testing.T) {
	m := widget.NewModal(widget.NewText("Hi"), widget.WithModalTitle("T"))
	h, host, _ := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	// The card's border is the leftmost non-blank cell on its row. Centred, it
	// cannot start at column 0.
	x0, _, _, _ := cardBounds(t, h)
	if x0 <= 0 {
		t.Errorf("the card's top-left corner is at column %d; a centred card in a "+
			"40-column host cannot start at the left edge", x0)
	}
}

// cardBounds locates the card's frame corners on screen, as inclusive
// coordinates. It reads the border glyphs rather than asking the widget where
// it placed itself, so a placement test observes what a user would see rather
// than the number the code under test computed.
func cardBounds(t *testing.T, h *harness) (x0, y0, x1, y1 int) {
	t.Helper()
	grid := h.tb.Snapshot()
	x0, y0, x1, y1 = -1, -1, -1, -1
	for y := range grid {
		for x, c := range grid[y] {
			switch c.Content {
			case "┌":
				x0, y0 = x, y
			case "┘":
				x1, y1 = x, y
			}
		}
	}
	if x0 < 0 || x1 < 0 {
		t.Fatalf("no card frame on screen:\n%s", h.grid())
	}
	return x0, y0, x1, y1
}

// TestEachPlacementPutsTheCardWhereItSays.
//
// The four corners are one switch away from each other, and a swapped axis or
// a dropped clamp reads as plausible in the source. The expectation here is
// derived from the card's own measured size rather than a hardcoded column, so
// the test says "flush against that edge" rather than restating an arithmetic
// the code already performs.
func TestEachPlacementPutsTheCardWhereItSays(t *testing.T) {
	const hostW, hostH = 40, 12
	for _, tc := range []struct {
		p                 widget.ModalPlacement
		wantLeft, wantTop bool
	}{
		{widget.PlacementTopLeft, true, true},
		{widget.PlacementTopRight, false, true},
		{widget.PlacementBottomLeft, true, false},
		{widget.PlacementBottomRight, false, false},
	} {
		t.Run(tc.p.String(), func(t *testing.T) {
			m := widget.NewModal(widget.NewText("Hi"),
				widget.WithModalTitle("T"), widget.WithPlacement(tc.p))
			h, host, _ := modalFixture(t, m, hostW, hostH)
			defer h.stop()
			openOn(t, h, m, host)

			x0, y0, x1, y1 := cardBounds(t, h)
			wantX0, wantY0 := hostW-1-(x1-x0), hostH-1-(y1-y0)
			if tc.wantLeft {
				wantX0 = 0
			}
			if tc.wantTop {
				wantY0 = 0
			}
			if x0 != wantX0 || y0 != wantY0 {
				t.Errorf("card at (%d,%d), want (%d,%d) for %s in a %dx%d host:\n%s",
					x0, y0, wantX0, wantY0, tc.p, hostW, hostH, h.grid())
			}
		})
	}
}

// TestClosingAStackedDialogRestoresTheBackdropBeneathTheOneBelow.
//
// Two dialogs share ONE backdrop, because the backdrop belongs to whichever is
// on top. Closing the upper one therefore has to give the scrim back to the
// lower one and put it UNDERNEATH — and both halves fail invisibly on their
// own: a scrim that is never restored just looks undimmed, and one restored on
// top of the surviving dialog just looks blank. So the assertion is the whole
// screen: after the upper dialog closes, what is left must be exactly what was
// there before it opened.
func TestClosingAStackedDialogRestoresTheBackdropBeneathTheOneBelow(t *testing.T) {
	lower := widget.NewModal(widget.NewText("first"), widget.WithModalTitle("Alpha"))
	upper := widget.NewModal(widget.NewText("later"), widget.WithModalTitle("Bravo"))

	h, host, _ := modalFixture(t, lower, 40, 12)
	defer h.stop()

	openOn(t, h, lower, host)
	alone := h.grid()
	if !strings.Contains(alone, "Alpha") {
		t.Fatalf("precondition failed: the first dialog is not on screen:\n%s", alone)
	}

	openOn(t, h, upper, host)
	if h.grid() == alone {
		t.Fatal("opening a second dialog changed nothing on screen, so the " +
			"comparison below cannot observe the restore either")
	}

	var top *widget.Modal
	h.onLoop(func() { top = host.TopModal() })
	if top != upper {
		t.Error("TopModal() is not the dialog opened last")
	}

	h.onLoop(func() { upper.Dismiss(widget.DismissProgrammatic) })
	h.settle()

	if got := h.grid(); got != alone {
		t.Errorf("closing the upper dialog did not restore the screen beneath it.\n"+
			"want:\n%s\ngot:\n%s", alone, got)
	}
	h.onLoop(func() { top = host.TopModal() })
	if top != lower {
		t.Error("the dialog underneath did not become topmost after the one above closed")
	}

	h.onLoop(func() { lower.Dismiss(widget.DismissProgrammatic) })
	h.settle()
	h.onLoop(func() { top = host.TopModal() })
	if top != nil {
		t.Errorf("TopModal() = %v with every dialog closed, want nil", top)
	}
}

// TestAnAssociatedStyleDressesTheScrim.
//
// Style is an association rather than something a widget hard-codes, and the
// scrim is where that is easiest to get wrong: it is painted by the HOST on the
// dialog's behalf, so a style that stopped at the card would leave the backdrop
// in its default look while everything else changed with the theme.
func TestAnAssociatedStyleDressesTheScrim(t *testing.T) {
	// NewModalStyle states the two looks a caller cares about and derives the
	// rest; the scrim is then replaced so it is distinguishable from the default.
	st := widget.NewModalStyle(styleOf(1), styleOf(2)).
		WithScrim(style.New().Underline(true))
	m := widget.NewModal(widget.NewText("Body"),
		widget.WithModalTitle("T"), widget.WithModalStyle(st))

	h, host, _ := modalFixture(t, m, 30, 8)
	defer h.stop()
	openOn(t, h, m, host)

	// The bottom-left cell is outside any centred card, so whatever occupies it
	// is the backdrop.
	grid := h.tb.Snapshot()
	corner := grid[len(grid)-1][0]
	if corner.Content != "" && corner.Content != " " {
		t.Fatalf("precondition failed: the bottom-left cell holds %q, so it is not "+
			"backdrop:\n%s", corner.Content, h.grid())
	}
	if corner.Attrs.Mask&tui.AttrUnderline == 0 {
		t.Error("the backdrop does not carry the associated scrim style; the " +
			"dialog's style is not reaching the host that paints it")
	}
}

// TestScrimIsPaintedByDefaultAndSuppressible. The dialog states the preference;
// the host decides where the dimming goes, because only the host knows which
// dialog is on top.
func TestScrimIsPaintedByDefaultAndSuppressible(t *testing.T) {
	withScrim := func(want bool) string {
		opts := []widget.ModalOption{widget.WithModalTitle("T")}
		if !want {
			opts = append(opts, widget.WithScrim(false))
		}
		m := widget.NewModal(widget.NewText("Hi"), opts...)
		h, host, _ := modalFixture(t, m, 30, 8)
		defer h.stop()
		openOn(t, h, m, host)
		return h.grid()
	}

	dimmed := withScrim(true)
	plain := withScrim(false)
	if dimmed == plain {
		t.Error("the rendered grid is identical with and without a scrim; the backdrop " +
			"is not being painted")
	}
}

// TestDismissReasonNamesEveryValue, including the unknown arm — an application
// may dismiss with a reason of its own, and it must render rather than vanish.
func TestDismissReasonNamesEveryValue(t *testing.T) {
	for r, want := range map[widget.DismissReason]string{
		widget.DismissProgrammatic: "programmatic",
		widget.DismissEscape:       "escape",
		widget.DismissAccept:       "accept",
		widget.DismissCancel:       "cancel",
		widget.DismissAnchorLost:   "anchor-lost",
		widget.DismissReplaced:     "replaced",
	} {
		if got := r.String(); got != want {
			t.Errorf("DismissReason(%d).String() = %q, want %q", r, got, want)
		}
	}
	if got := widget.DismissReason(200).String(); got != "unknown" {
		t.Errorf("a consumer-defined reason rendered as %q, want %q", got, "unknown")
	}
}

// TestPlacementEnumsValidateAndName.
func TestPlacementEnumsValidateAndName(t *testing.T) {
	// The FIRST invalid value of each, not a far-out one: an off-by-one bound
	// rejects 200 exactly as readily as a correct bound does, so probing only
	// that cannot see the edge move.
	if !widget.PlacementBottomRight.Valid() || (widget.PlacementBottomRight + 1).Valid() {
		t.Error("ModalPlacement.Valid does not bound the declared set at its edge")
	}
	if !widget.PlacementLeft.Valid() || (widget.PlacementLeft + 1).Valid() {
		t.Error("PlacementSide.Valid does not bound the declared set at its edge")
	}
	if !widget.PlacementAlignEnd.Valid() || (widget.PlacementAlignEnd + 1).Valid() {
		t.Error("PlacementAlign.Valid does not bound the declared set at its edge")
	}
	// The zero Placement is the common case and must be valid unconfigured.
	if !(widget.Placement{}).Valid() {
		t.Error("the zero Placement is invalid; below/start/no-offset is the default")
	}
	if (widget.Placement{Side: widget.PlacementSide(9)}).Valid() {
		t.Error("Placement.Valid accepted an out-of-range side")
	}
	for p, want := range map[widget.ModalPlacement]string{
		widget.PlacementCenter:      "center",
		widget.PlacementTopLeft:     "top-left",
		widget.PlacementBottomRight: "bottom-right",
	} {
		if got := p.String(); got != want {
			t.Errorf("ModalPlacement(%d).String() = %q, want %q", p, got, want)
		}
	}
	if got := widget.ModalPlacement(200).String(); got != "unknown" {
		t.Errorf("an undefined placement rendered as %q", got)
	}
	for s, want := range map[widget.PlacementSide]string{
		widget.PlacementBelow: "below",
		widget.PlacementAbove: "above",
		widget.PlacementRight: "right",
		widget.PlacementLeft:  "left",
	} {
		if got := s.String(); got != want {
			t.Errorf("PlacementSide(%d).String() = %q, want %q", s, got, want)
		}
	}
	if got := widget.PlacementSide(9).String(); got != "unknown" {
		t.Errorf("an undefined side rendered as %q", got)
	}
	for a, want := range map[widget.PlacementAlign]string{
		widget.PlacementAlignStart:  "start",
		widget.PlacementAlignCenter: "center",
		widget.PlacementAlignEnd:    "end",
	} {
		if got := a.String(); got != want {
			t.Errorf("PlacementAlign(%d).String() = %q, want %q", a, got, want)
		}
	}
	if got := widget.PlacementAlign(9).String(); got != "unknown" {
		t.Errorf("an undefined alignment rendered as %q", got)
	}
}

// TestAnInvalidPlacementIsRefusedAtConstruction.
func TestAnInvalidPlacementIsRefusedAtConstruction(t *testing.T) {
	fatal := fatalFromWidgetExt(func() {
		widget.NewModal(widget.NewText("Body"), widget.WithPlacement(widget.ModalPlacement(200)))
	})
	if fatal == nil {
		t.Error("an out-of-range ModalPlacement was accepted")
	}
}

// TestModalStyleIsNilSafeAndImmutable — the same contract as ButtonStyle, since
// a consumer who has learned one has learned them all.
func TestModalStyleIsNilSafeAndImmutable(t *testing.T) {
	var s *widget.ModalStyle
	def := widget.DefaultModalStyle()
	if s.Card() != def.Card() || s.Title() != def.Title() ||
		s.Border() != def.Border() || s.Scrim() != def.Scrim() {
		t.Error("a nil ModalStyle does not fall back to the defaults")
	}

	base := widget.NewModalStyleFull(
		styleOf(1), styleOf(2), styleOf(3), styleOf(4))
	before := base.Card()
	derived := base.WithCard(styleOf(5))
	if base.Card() != before {
		t.Error("WithCard mutated the receiver; styles are shared and must be copied")
	}
	if derived.Card() == before {
		t.Error("WithCard did not change the copy")
	}
	if derived.Title() != base.Title() {
		t.Error("WithCard also changed an unrelated look")
	}

	// Every other With* accessor owes the same contract. Checking only one of
	// four would leave three setters free to mutate in place, which is a defect
	// that surfaces on the SECOND dialog sharing the value rather than the first.
	for name, tc := range map[string]struct {
		derive func(*widget.ModalStyle) *widget.ModalStyle
		read   func(*widget.ModalStyle) style.Style
	}{
		"WithTitle": {
			func(s *widget.ModalStyle) *widget.ModalStyle { return s.WithTitle(styleOf(6)) },
			(*widget.ModalStyle).Title,
		},
		"WithBorder": {
			func(s *widget.ModalStyle) *widget.ModalStyle { return s.WithBorder(styleOf(6)) },
			(*widget.ModalStyle).Border,
		},
		"WithScrim": {
			func(s *widget.ModalStyle) *widget.ModalStyle { return s.WithScrim(styleOf(6)) },
			(*widget.ModalStyle).Scrim,
		},
	} {
		was := tc.read(base)
		got := tc.derive(base)
		if tc.read(got) != styleOf(6) {
			t.Errorf("%s did not change the copy", name)
		}
		if tc.read(base) != was {
			t.Errorf("%s mutated the receiver; styles are shared and must be copied", name)
		}
		if got.Card() != base.Card() {
			t.Errorf("%s also changed an unrelated look", name)
		}
	}

	// NewModalStyle states the two looks a caller has an opinion about and
	// derives the other two, so a partial statement still yields a complete
	// style rather than two empty surfaces.
	derivedFull := widget.NewModalStyle(styleOf(5), styleOf(6))
	if derivedFull.Border() != derivedFull.Card() {
		t.Error("NewModalStyle did not derive the border from the card")
	}
	if derivedFull.Scrim() != widget.DefaultModalStyle().Scrim() {
		t.Error("NewModalStyle did not fall back to the default scrim")
	}
}

// styleOf builds a distinguishable style value for the immutability checks.
func styleOf(n int) style.Style {
	st := style.New()
	if n&1 != 0 {
		st = st.Bold(true)
	}
	if n&2 != 0 {
		st = st.Italic(true)
	}
	if n&4 != 0 {
		st = st.Underline(true)
	}
	return st
}

// TestTabTraversalStaysAmongTheDialogsButtons.
//
// Two separate guarantees meet here, and neither is visible from where focus
// merely LANDS on opening: the trap must stop Tab escaping to the controls
// underneath, and the card must not be a tab stop of its own. A card that
// accepted focus would insert a dead stop between the dialog and its buttons —
// one press of Tab going nowhere visible — which no test of the initial focus
// target can detect.
func TestTabTraversalStaysAmongTheDialogsButtons(t *testing.T) {
	a := widget.NewButton("A")
	b := widget.NewButton("B")
	m := widget.NewModal(widget.NewText("Body"), widget.WithButtons(a, b))
	h, host, base := modalFixture(t, m, 40, 12)
	defer h.stop()
	openOn(t, h, m, host)

	// Tab several times — more than there are buttons, so the ring must wrap
	// rather than run out — and record where focus is each time.
	seen := map[string]int{}
	for i := 0; i < 6; i++ {
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
		h.settle()
		var where string
		h.onLoop(func() {
			switch {
			case a.Context() != nil && a.Context().Focused():
				where = "A"
			case b.Context() != nil && b.Context().Focused():
				where = "B"
			case base.Context() != nil && base.Context().Focused():
				where = "base"
			case m.Context() != nil && m.Context().Focused():
				where = "modal"
			default:
				where = "nowhere"
			}
		})
		seen[where]++
	}

	if seen["base"] > 0 {
		t.Errorf("Tab reached the control BEHIND the dialog %d times; a trap must "+
			"confine traversal to itself", seen["base"])
	}
	if seen["nowhere"] > 0 {
		t.Errorf("Tab landed on no focusable node %d times; the card is acting as a "+
			"dead tab stop between the dialog and its buttons", seen["nowhere"])
	}
	if seen["A"] == 0 || seen["B"] == 0 {
		t.Errorf("Tab did not visit both buttons (A=%d B=%d); the ring inside the "+
			"dialog is not cycling", seen["A"], seen["B"])
	}
}
