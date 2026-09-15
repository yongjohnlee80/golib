package widget_test

// THE RATIFIED RESIZE SURFACE, as a compile contract.
//
// Every declaration below is written the way the design record ratifies it. If
// a field is renamed, a handle renumbered, an action removed or an ID changed,
// THIS FILE STOPS COMPILING — which is the point. A behavioural test cannot
// notice that `ResizeDragAction` was published where `ResizeUpdateAction` was
// promised, because the behaviour is identical and only the name a consumer
// writes is wrong; by the time anyone notices, it is in a release.
//
// The composite literals use FIELD NAMES deliberately, so a reordering is
// caught, and the handle constants are asserted by VALUE, so renumbering is too
// — a consumer persisting a handle, or sending one over a wire, is entitled to
// the numbering not moving under them.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TestTheRatifiedResizeActionsExistWithTheirRatifiedShapes.
func TestTheRatifiedResizeActionsExistWithTheirRatifiedShapes(t *testing.T) {
	// The declarations, exactly as ratified.
	var (
		begin  = widget.ResizeBeginAction{Handle: widget.HandleTopLeft, At: tui.Point{X: 1, Y: 2}}
		update = widget.ResizeUpdateAction{At: tui.Point{X: 3, Y: 4}}
		step   = widget.ResizeStepAction{DX: 1, DY: -1, Unit: widget.StepCells}
		set    = widget.ResizeSetAction{Size: tui.Size{W: 10, H: 5}}
		end    = widget.ResizeEndAction{}
		cancel = widget.ResizeCancelAction{}
	)
	// Every one is a tui.Action — the seam a consumer's resolver returns them
	// through — and each carries its stable published id.
	for _, tc := range []struct {
		action tui.Action
		want   tui.ActionID
	}{
		{begin, "resize.begin"},
		{update, "resize.update"},
		{step, "resize.step"},
		{set, "resize.set"},
		{end, "resize.end"},
		{cancel, "resize.cancel"},
	} {
		if got := tc.action.ActionID(); got != tc.want {
			t.Errorf("%T.ActionID() = %q, want %q — the id is the name a consumer's "+
				"dispatch table is keyed by", tc.action, got, tc.want)
		}
	}
}

// TestTheRatifiedHandleSetIsCompleteAndStablyNumbered.
//
// Ten handles: four edges, four corners, two dividers. The VALUES are asserted,
// not just the names, because a consumer may persist one or send it over a
// wire, and renumbering would silently turn a saved top-left grip into a
// bottom-right one.
func TestTheRatifiedHandleSetIsCompleteAndStablyNumbered(t *testing.T) {
	for want, h := range []widget.Handle{
		widget.HandleLeft,
		widget.HandleRight,
		widget.HandleTop,
		widget.HandleBottom,
		widget.HandleTopLeft,
		widget.HandleTopRight,
		widget.HandleBottomLeft,
		widget.HandleBottomRight,
		widget.HandleVerticalDivider,
		widget.HandleHorizontalDivider,
	} {
		if int(h) != want {
			t.Errorf("%v has value %d, want %d — the numbering is public", h, h, want)
		}
		if !h.Valid() {
			t.Errorf("%v reports itself invalid", h)
		}
	}
	// The set is CLOSED at exactly ten, checked at its edge.
	if (widget.HandleHorizontalDivider + 1).Valid() {
		t.Error("Handle.Valid does not bound the declared set at its edge")
	}
	// And the divider handles are the ones a Split uses, so they must not be
	// confusable with a corner: a consumer reading a trace needs the names.
	if widget.HandleVerticalDivider.String() != "vertical-divider" ||
		widget.HandleHorizontalDivider.String() != "horizontal-divider" {
		t.Error("the divider handles do not name themselves distinctly")
	}
}

// TestOneVocabularyServesBothWidgets.
//
// The claim that makes the shared surface worth having: a consumer holds ONE
// action and does not know or care which of the two widgets will answer it.
// Asserted by driving the identical value into both and seeing each act.
func TestOneVocabularyServesBothWidgets(t *testing.T) {
	grow := widget.ResizeStepAction{DX: 1, Unit: widget.StepCells}

	// The wrapper grows by a cell.
	child := &sizedChild{pref: tui.Size{W: 8, H: 4}}
	r := widget.NewResizable(child, widget.WithMaxSize(tui.Size{W: 30, H: 15}))
	rh := startApp(t, widget.NewOverlayHost(&looseCeiling{child: r, w: 30, hh: 15}), 40, 20)
	defer rh.stop()
	rh.settle()
	before := sizeOn(t, rh, r)
	rh.onLoop(func() { r.Context().DoAction(grow) })
	rh.waitFor("the wrapper answered the shared action", func() bool {
		return sizeOn(t, rh, r).W == before.W+1
	})

	// The SAME action value moves the divider of a horizontal split.
	sp := widget.NewSplit(widget.Horizontal, &pane{fill: "a"}, &pane{fill: "b"})
	sh := newShell(sp)
	ph := startApp(t, sh, 40, 8)
	defer ph.stop()
	ph.settle()
	a0 := aCellsOn(t, ph, sp)
	ph.onLoop(func() { sp.Context().DoAction(grow) })
	ph.waitFor("the split answered the same shared action", func() bool {
		return aCellsOn(t, ph, sp) == a0+1
	})
}
