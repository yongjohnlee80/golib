package widget_test

// Anchoring is placement arithmetic plus a lifecycle, and the two fail in
// different ways. The arithmetic is pure and is tested as such — no App, no
// tree, just rectangles — because a policy that needs a running application to
// check is a policy nobody will check. The lifecycle needs the real runtime,
// because the property that matters is what happens to a mounted layer when the
// thing it hangs off disappears.

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// ─── the placement policy ────────────────────────────────────────────────────

// TestFlipClipPolicyPrefersTheAskedSideThenFlipsThenClips.
//
// The order is the whole behaviour. A dropdown near the bottom of the screen
// that CLIPPED would show two of its rows; the same dropdown FLIPPED above its
// field shows all of them. Clipping is the last resort, for a popup that fits on
// neither side.
func TestFlipClipPolicyPrefersTheAskedSideThenFlipsThenClips(t *testing.T) {
	viewport := tui.Rect{X: 0, Y: 0, W: 40, H: 20}
	var pol widget.FlipClipPolicy

	t.Run("the preferred side is used when it fits", func(t *testing.T) {
		anchor := tui.Rect{X: 5, Y: 4, W: 10, H: 1}
		got := pol.Place(anchor, viewport, tui.Size{W: 8, H: 6}, widget.Placement{})
		want := tui.Rect{X: 5, Y: 5, W: 8, H: 6} // directly below, start-aligned
		if got != want {
			t.Errorf("placed at %+v, want %+v", got, want)
		}
	})

	t.Run("it flips when the preferred side does not fit", func(t *testing.T) {
		anchor := tui.Rect{X: 5, Y: 17, W: 10, H: 1} // near the bottom edge
		got := pol.Place(anchor, viewport, tui.Size{W: 8, H: 6}, widget.Placement{})
		want := tui.Rect{X: 5, Y: 11, W: 8, H: 6} // above, whole popup visible
		if got != want {
			t.Errorf("placed at %+v, want %+v; a popup that fits above must flip "+
				"rather than lose rows to the bottom edge", got, want)
		}
	})

	t.Run("it clips only when neither side fits", func(t *testing.T) {
		anchor := tui.Rect{X: 5, Y: 9, W: 10, H: 1}
		got := pol.Place(anchor, viewport, tui.Size{W: 8, H: 30}, widget.Placement{})
		if got.H != viewport.H || got.Y != 0 {
			t.Errorf("placed at %+v, want the full viewport height at the top edge", got)
		}
	})

	t.Run("a horizontal preference flips on the horizontal axis", func(t *testing.T) {
		anchor := tui.Rect{X: 34, Y: 4, W: 6, H: 1} // hard against the right edge
		got := pol.Place(anchor, viewport, tui.Size{W: 12, H: 4},
			widget.Placement{Side: widget.PlacementRight})
		want := tui.Rect{X: 22, Y: 4, W: 12, H: 4} // flipped to the left of the anchor
		if got != want {
			t.Errorf("placed at %+v, want %+v", got, want)
		}
	})
}

// TestPlacementAlignmentShiftsAlongTheOtherAxis.
//
// Side chooses which edge the popup hangs off; alignment chooses where along
// that edge. They are independent, and a policy that conflated them would move
// a centred dropdown off its field whenever the field was near an edge.
func TestPlacementAlignmentShiftsAlongTheOtherAxis(t *testing.T) {
	viewport := tui.Rect{X: 0, Y: 0, W: 40, H: 20}
	anchor := tui.Rect{X: 10, Y: 4, W: 12, H: 1}
	want := tui.Size{W: 6, H: 3}
	var pol widget.FlipClipPolicy

	for _, tc := range []struct {
		align widget.PlacementAlign
		wantX int
	}{
		{widget.PlacementAlignStart, 10},  // the anchor's left edge
		{widget.PlacementAlignCenter, 13}, // 10 + (12-6)/2
		{widget.PlacementAlignEnd, 16},    // 10 + 12 - 6
	} {
		t.Run(tc.align.String(), func(t *testing.T) {
			got := pol.Place(anchor, viewport, want, widget.Placement{Align: tc.align})
			if got.X != tc.wantX {
				t.Errorf("x = %d, want %d", got.X, tc.wantX)
			}
			if got.Y != 5 {
				t.Errorf("y = %d, want 5; alignment must not move the popup off its side", got.Y)
			}
		})
	}
}

// TestTheOffsetNudgesAndTheViewportStillWins.
//
// An offset is a nudge the caller asked for, not permission to leave the screen.
// The host bounds whatever the policy returns, so the two cannot disagree about
// who has the last word.
func TestTheOffsetNudgesAndTheViewportStillWins(t *testing.T) {
	viewport := tui.Rect{X: 0, Y: 0, W: 40, H: 20}
	anchor := tui.Rect{X: 5, Y: 4, W: 10, H: 1}
	var pol widget.FlipClipPolicy

	nudged := pol.Place(anchor, viewport, tui.Size{W: 8, H: 4},
		widget.Placement{Offset: tui.Point{X: 2, Y: 1}})
	if nudged.X != 7 || nudged.Y != 6 {
		t.Errorf("nudged to %+v, want x=7 y=6", nudged)
	}

	shoved := pol.Place(anchor, viewport, tui.Size{W: 8, H: 4},
		widget.Placement{Offset: tui.Point{X: 500, Y: 500}})
	if shoved.X+shoved.W > viewport.W || shoved.Y+shoved.H > viewport.H {
		t.Errorf("an absurd offset escaped the viewport: %+v", shoved)
	}
}

// ─── the lifecycle ───────────────────────────────────────────────────────────

// anchorOwner declares one region per layout pass, so a test can make the
// region appear and disappear the way a menu's rows do.
type anchorOwner struct {
	widget.Base
	declare bool
	region  tui.RegionID
	local   tui.Rect
	ref     tui.AnchorRef
}

func (o *anchorOwner) Layout(cs tui.Constraints) tui.Size {
	if ctx := o.Context(); ctx != nil && o.declare {
		o.ref = ctx.DeclareRegion(o.region, o.local)
	}
	return cs.Constrain(tui.Size{W: 12, H: 3})
}

func (o *anchorOwner) Render(s tui.Surface) {
	s.SetCell(0, 0, "A", style.New())
}

// TestAnAnchoredLayerIsPlacedAgainstItsRegion.
func TestAnAnchoredLayerIsPlacedAgainstItsRegion(t *testing.T) {
	owner := &anchorOwner{declare: true, region: "row-1", local: tui.Rect{X: 2, Y: 1, W: 6, H: 1}}
	host := widget.NewOverlayHost(owner)
	h := startApp(t, host, 30, 12)
	defer h.stop()
	h.settle()

	popup := widget.NewText("POPUP")
	var err error
	h.onLoop(func() {
		err = host.OpenAnchored("p", popup,
			widget.AnchorSpec{Ref: owner.ref}, nil)
	})
	if err != nil {
		t.Fatalf("OpenAnchored: %v", err)
	}
	h.settle()

	// The region sits at (2,1) in the owner, which is at the host's origin, and
	// is one row tall — so a popup preferring "below" lands at y = 2, x = 2.
	x, y := cellOfLabel(t, h, "POPUP")
	if x != 2 || y != 2 {
		t.Errorf("popup painted at (%d,%d), want (2,2) — directly below its region:\n%s",
			x, y, h.grid())
	}
	var ids []widget.LayerID
	h.onLoop(func() {
		for id := range host.AnchoredLayers() {
			ids = append(ids, id)
		}
	})
	if len(ids) != 1 || ids[0] != "p" {
		t.Errorf("AnchoredLayers() = %v, want [p]", ids)
	}
}

// TestALayerWhoseAnchorVanishesIsDismissed.
//
// The popup exists to be attached to something. When that something stops being
// declared — a row scrolled away, a model replaced — leaving the popup floating
// where the anchor used to be is worse than closing it: it now points at
// whatever happens to occupy those cells.
func TestALayerWhoseAnchorVanishesIsDismissed(t *testing.T) {
	owner := &anchorOwner{declare: true, region: "row-1", local: tui.Rect{X: 2, Y: 1, W: 6, H: 1}}
	host := widget.NewOverlayHost(owner)
	h := startApp(t, host, 30, 12)
	defer h.stop()
	h.settle()

	popup := widget.NewText("POPUP")
	h.onLoop(func() {
		if err := host.OpenAnchored("p", popup, widget.AnchorSpec{Ref: owner.ref}, nil); err != nil {
			t.Errorf("OpenAnchored: %v", err)
		}
	})
	h.settle()
	if !strings.Contains(h.grid(), "POPUP") {
		t.Fatalf("precondition failed: the popup never appeared:\n%s", h.grid())
	}

	var reasons []widget.DismissReason
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.OverlayDismissedEvent) {
		reasons = append(reasons, ev.Reason)
	})
	defer unsub()

	// Stop declaring the region, exactly as a menu stops declaring a row that
	// left its model.
	h.onLoop(func() {
		owner.declare = false
		owner.Context().RequestLayout()
	})
	h.waitFor("the orphaned layer closed", func() bool {
		return !strings.Contains(h.grid(), "POPUP")
	})
	h.settle()

	var ids []widget.LayerID
	h.onLoop(func() {
		for id := range host.AnchoredLayers() {
			ids = append(ids, id)
		}
	})
	if len(ids) != 0 {
		t.Errorf("the host still holds %v after the anchor went", ids)
	}
	if len(reasons) != 1 || reasons[0] != widget.DismissAnchorLost {
		t.Errorf("dismissal reasons = %v, want exactly one anchor-lost", reasons)
	}
}

// TestReopeningAnIdReplacesTheLayerAtomically.
//
// Reopening a dropdown that is already showing is how a user toggles one, so a
// duplicate id is a replacement rather than an error. It must leave exactly one
// layer mounted under that id — not two, and not none.
func TestReopeningAnIdReplacesTheLayerAtomically(t *testing.T) {
	owner := &anchorOwner{declare: true, region: "r", local: tui.Rect{X: 0, Y: 1, W: 6, H: 1}}
	host := widget.NewOverlayHost(owner)
	h := startApp(t, host, 30, 12)
	defer h.stop()
	h.settle()

	first := widget.NewText("FIRST")
	second := widget.NewText("SECOND")
	h.onLoop(func() {
		if err := host.OpenAnchored("p", first, widget.AnchorSpec{Ref: owner.ref}, nil); err != nil {
			t.Errorf("first open: %v", err)
		}
	})
	h.settle()
	h.onLoop(func() {
		if err := host.OpenAnchored("p", second, widget.AnchorSpec{Ref: owner.ref}, nil); err != nil {
			t.Errorf("replacement: %v", err)
		}
	})
	h.settle()

	got := h.grid()
	if strings.Contains(got, "FIRST") {
		t.Errorf("the replaced layer is still on screen:\n%s", got)
	}
	if !strings.Contains(got, "SECOND") {
		t.Errorf("the replacement is not on screen:\n%s", got)
	}
	n := 0
	h.onLoop(func() {
		for range host.AnchoredLayers() {
			n++
		}
	})
	if n != 1 {
		t.Errorf("%d layers registered under one id, want 1", n)
	}
}

// TestOpenAnchoredRejectsWhatItCannotPlace.
//
// Each rejection leaves the host untouched, which is what lets a caller retry
// without first working out how far the previous attempt got.
func TestOpenAnchoredRejectsWhatItCannotPlace(t *testing.T) {
	owner := &anchorOwner{declare: true, region: "r", local: tui.Rect{X: 0, Y: 1, W: 6, H: 1}}
	host := widget.NewOverlayHost(owner)
	h := startApp(t, host, 30, 12)
	defer h.stop()
	h.settle()

	for _, tc := range []struct {
		name string
		want error
		call func() error
	}{
		{"an empty id", widget.ErrEmptyLayerID, func() error {
			return host.OpenAnchored("", widget.NewText("x"), widget.AnchorSpec{Ref: owner.ref}, nil)
		}},
		{"the zero anchor", widget.ErrAnchorUnusable, func() error {
			return host.OpenAnchored("p", widget.NewText("x"), widget.AnchorSpec{}, nil)
		}},
		{"an out-of-range placement", widget.ErrAnchorUnusable, func() error {
			return host.OpenAnchored("p", widget.NewText("x"), widget.AnchorSpec{
				Ref:  owner.ref,
				Pref: widget.Placement{Side: widget.PlacementLeft + 1},
			}, nil)
		}},
		{"a nil layer", widget.ErrAnchorUnusable, func() error {
			return host.OpenAnchored("p", nil, widget.AnchorSpec{Ref: owner.ref}, nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			h.onLoop(func() { err = tc.call() })
			h.settle()
			if !errors.Is(err, tc.want) {
				t.Errorf("returned %v, want %v", err, tc.want)
			}
			n := 0
			h.onLoop(func() {
				for range host.AnchoredLayers() {
					n++
				}
			})
			if n != 0 {
				t.Errorf("a rejected open registered %d layers", n)
			}
		})
	}
}

// TestTheSameLayerCannotBeOpenUnderTwoIds.
//
// One component is one node. Registering it twice would need it mounted twice,
// which the runtime refuses, and the second id could never be closed
// independently of the first.
func TestTheSameLayerCannotBeOpenUnderTwoIds(t *testing.T) {
	owner := &anchorOwner{declare: true, region: "r", local: tui.Rect{X: 0, Y: 1, W: 6, H: 1}}
	host := widget.NewOverlayHost(owner)
	h := startApp(t, host, 30, 12)
	defer h.stop()
	h.settle()

	shared := widget.NewText("SHARED")
	var first, second error
	h.onLoop(func() {
		first = host.OpenAnchored("a", shared, widget.AnchorSpec{Ref: owner.ref}, nil)
		second = host.OpenAnchored("b", shared, widget.AnchorSpec{Ref: owner.ref}, nil)
	})
	h.settle()

	if first != nil {
		t.Fatalf("the first registration failed: %v", first)
	}
	if !errors.Is(second, widget.ErrLayerAlreadyRegistered) {
		t.Errorf("the second registration returned %v, want ErrLayerAlreadyRegistered", second)
	}
}

// TestCloseAnchoredIsIdempotentAndReportsTheReason.
func TestCloseAnchoredIsIdempotentAndReportsTheReason(t *testing.T) {
	owner := &anchorOwner{declare: true, region: "r", local: tui.Rect{X: 0, Y: 1, W: 6, H: 1}}
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

	var reasons []widget.DismissReason
	unsub := tui.Subscribe(h.app.Bus(), func(ev widget.OverlayDismissedEvent) {
		reasons = append(reasons, ev.Reason)
	})
	defer unsub()

	h.onLoop(func() {
		host.CloseAnchored("p", widget.DismissProgrammatic)
		host.CloseAnchored("p", widget.DismissProgrammatic) // already closed
		host.CloseAnchored("never-opened", widget.DismissProgrammatic)
	})
	h.settle()
	h.settle()

	if strings.Contains(h.grid(), "POPUP") {
		t.Errorf("the layer is still on screen after being closed:\n%s", h.grid())
	}
	if len(reasons) != 1 {
		t.Errorf("%d dismissal events for one closure, want exactly 1", len(reasons))
	}
}
