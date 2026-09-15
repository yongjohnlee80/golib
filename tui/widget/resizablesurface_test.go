package widget_test

// The Resizable family's PUBLISHED SURFACE: the style value, the enums, the
// options and the setters. Individually small, and collectively the part of a
// widget a consumer meets first — an option that silently does nothing, a style
// method that mutates the value two wrappers are sharing, or an enum that
// renders as a number in a failure message is a defect no behavioural test of
// the widget's logic will ever reach.
//
// It follows the same shape as the Button, Modal and Menu surface tests,
// because a consumer who has learned one has learned them all, and that claim
// is only true if the tests check the same things.

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TestResizableStyleIsNilSafeAndImmutable.
func TestResizableStyleIsNilSafeAndImmutable(t *testing.T) {
	var nilStyle *widget.ResizableStyle
	// Every accessor answers on a nil receiver, because a widget with no style
	// set holds exactly that and must still paint.
	if nilStyle.Handle() != widget.DefaultResizableStyle().Handle() {
		t.Error("Handle() on a nil style did not fall back to the default")
	}
	if nilStyle.Active() != widget.DefaultResizableStyle().Active() {
		t.Error("Active() on a nil style did not fall back to the default")
	}
	// A With* on nil yields a COMPLETE value, not one with empty looks beside
	// the field that was set.
	fromNil := nilStyle.WithHandle(styleOf(7))
	if fromNil.Handle() != styleOf(7) {
		t.Error("WithHandle on a nil style did not set the handle look")
	}
	if fromNil.Active() != widget.DefaultResizableStyle().Active() {
		t.Error("WithHandle on a nil style left the OTHER look empty; a clone of " +
			"nothing must be a clone of the defaults")
	}

	// Immutability: these values are shared between wrappers by design, so a
	// With* must copy rather than write through.
	base := widget.NewResizableStyle(styleOf(1))
	derived := base.WithActive(styleOf(2))
	if base.Active() == derived.Active() {
		t.Error("WithActive mutated the receiver; one style value dresses many wrappers")
	}
	if derived.Handle() != base.Handle() {
		t.Error("WithActive changed the handle look as well")
	}
	// And the constructor derives the active look rather than leaving it empty.
	if base.Active() == base.Handle() {
		t.Error("NewResizableStyle did not derive a distinct active look")
	}
}

// TestTheResizableEnumsNameEveryValue — these strings appear in traces and
// failure messages, so a value that renders as a number is a message nobody can
// read, and an out-of-range one that renders as a number twice is worse.
func TestTheResizableEnumsNameEveryValue(t *testing.T) {
	for m, want := range map[widget.SizeMode]string{
		widget.SizeAuto:     "auto",
		widget.SizeExplicit: "explicit",
	} {
		if got := m.String(); got != want {
			t.Errorf("SizeMode(%d).String() = %q, want %q", m, got, want)
		}
	}
	if got := (widget.SizeExplicit + 1).String(); got != "unknown" {
		t.Errorf("an undeclared SizeMode rendered as %q", got)
	}

	for h, want := range map[widget.Handle]string{
		widget.HandleLeft:              "left",
		widget.HandleRight:             "right",
		widget.HandleTop:               "top",
		widget.HandleBottom:            "bottom",
		widget.HandleTopLeft:           "top-left",
		widget.HandleTopRight:          "top-right",
		widget.HandleBottomLeft:        "bottom-left",
		widget.HandleBottomRight:       "bottom-right",
		widget.HandleVerticalDivider:   "vertical-divider",
		widget.HandleHorizontalDivider: "horizontal-divider",
	} {
		if got := h.String(); got != want {
			t.Errorf("Handle(%d).String() = %q, want %q", h, got, want)
		}
	}
	if got := (widget.HandleHorizontalDivider + 1).String(); got != "unknown" {
		t.Errorf("an undeclared Handle rendered as %q", got)
	}

	for p, want := range map[widget.PlacementMode]string{
		widget.PlacementOverlay: "overlay",
		widget.PlacementReserve: "reserve",
	} {
		if got := p.String(); got != want {
			t.Errorf("PlacementMode(%d).String() = %q, want %q", p, got, want)
		}
	}
	if got := (widget.PlacementReserve + 1).String(); got != "unknown" {
		t.Errorf("an undeclared PlacementMode rendered as %q", got)
	}
	// The FIRST invalid value, not a far one: an off-by-one bound rejects 200
	// as readily as a correct bound does.
	if !widget.PlacementReserve.Valid() || (widget.PlacementReserve + 1).Valid() {
		t.Error("PlacementMode.Valid does not bound the declared set at its edge")
	}

	for u, want := range map[widget.StepUnit]string{
		widget.StepCells:   "cells",
		widget.StepPercent: "percent",
	} {
		if got := u.String(); got != want {
			t.Errorf("StepUnit(%d).String() = %q, want %q", u, got, want)
		}
	}
	if got := (widget.StepPercent + 1).String(); got != "unknown" {
		t.Errorf("an undeclared StepUnit rendered as %q", got)
	}
	if !widget.StepPercent.Valid() || (widget.StepPercent + 1).Valid() {
		t.Error("StepUnit.Valid does not bound the declared set at its edge")
	}
}

// TestTheStyleReachesTheGripBothWays.
//
// Style is an ASSOCIATION, not something the widget owns: it can be supplied at
// construction or swapped at runtime, and either way it is the grip that
// changes. A setter that stored the value without repainting would pass any
// test that only read it back.
func TestTheStyleReachesTheGripBothWays(t *testing.T) {
	loud := widget.NewResizableStyle(style.New().Foreground(style.TokenError))
	child := &sizedChild{pref: tui.Size{W: 6, H: 3}}

	// At construction.
	r := widget.NewResizable(child, widget.WithResizableStyle(loud),
		widget.WithHandleGlyph("@"))
	host := widget.NewOverlayHost(r)
	h := startApp(t, host, 20, 10)
	defer h.stop()
	h.settle()

	if !strings.Contains(h.grid(), "@") {
		t.Fatalf("WithHandleGlyph did not reach the grip:\n%s", h.grid())
	}
	sz := sizeOn(t, h, r)
	atRest := cellAttrs(h, sz.W-1, sz.H-1)

	// And at runtime, on the SAME wrapper: the association is replaceable.
	quiet := widget.NewResizableStyle(style.New().Foreground(style.TokenTextMuted))
	h.onLoop(func() { r.WithStyle(quiet) })
	h.settle()
	if got := cellAttrs(h, sz.W-1, sz.H-1); got == atRest {
		t.Error("WithStyle did not change the grip's painted look; a style set at " +
			"runtime must reach the cells, not just the field")
	}

	// nil reverts to the default rather than painting nothing, so there is no
	// separate clear API to get wrong.
	h.onLoop(func() { r.WithStyle(nil) })
	h.settle()
	if !strings.Contains(h.grid(), "@") {
		t.Errorf("a nil style stopped the grip painting:\n%s", h.grid())
	}
}

// TestSetSizeAndSetAutoAreIdempotentAndSwitchModes.
//
// Both are state mutations a consumer drives, and both have a no-op path that a
// behavioural test of resizing never takes: setting the size already held, or
// returning to auto when already auto. A no-op that still requested layout
// would spend a frame per call for the rest of the application's life.
func TestSetSizeAndSetAutoAreIdempotentAndSwitchModes(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 6, H: 3}}
	r := widget.NewResizable(child, widget.WithMaxSize(tui.Size{W: 30, H: 15}))
	host := widget.NewOverlayHost(r)
	h := startApp(t, host, 40, 20)
	defer h.stop()
	h.settle()

	if mode := modeOn(t, h, r); mode != widget.SizeAuto {
		t.Fatalf("SizeMode() = %v at construction, want auto", mode)
	}
	// SetAuto while already auto changes nothing and is not an error.
	h.onLoop(func() { r.SetAuto() })
	h.settle()
	if mode := modeOn(t, h, r); mode != widget.SizeAuto {
		t.Errorf("SizeMode() = %v after a redundant SetAuto", mode)
	}

	h.onLoop(func() { r.SetSize(tui.Size{W: 12, H: 6}) })
	h.settle()
	if mode := modeOn(t, h, r); mode != widget.SizeExplicit {
		t.Errorf("SizeMode() = %v after SetSize, want explicit", mode)
	}
	want, ok := requestedOn(t, h, r)
	if !ok || want != (tui.Size{W: 12, H: 6}) {
		t.Errorf("RequestedSize() = (%+v, %v), want the 12x6 that was asked for", want, ok)
	}

	// AN IDENTICAL EXPLICIT SET COSTS NOTHING. Counted in child layout passes,
	// because that is the cost: a drag delivers a SetSize per pointer motion and
	// most of them land on the size already held, so a redundant request spends
	// a frame per motion event on geometry that cannot change. Asserting only
	// the resulting mode and value — which the previous version of this test did
	// — cannot see it, since both are correct either way.
	before := child.layouts()
	h.onLoop(func() { r.SetSize(tui.Size{W: 12, H: 6}) })
	h.settle()
	h.settle()
	if got := child.layouts(); got != before {
		t.Errorf("the child was measured %d more times for a repeated identical "+
			"SetSize, want 0", got-before)
	}
	// And through the ACTION, which routes to the same request path.
	h.onLoop(func() {
		r.Context().DoAction(widget.ResizeSetAction{Size: tui.Size{W: 12, H: 6}})
	})
	h.settle()
	h.settle()
	if got := child.layouts(); got != before {
		t.Errorf("the child was measured %d more times for a no-op ResizeSetAction, "+
			"want 0", got-before)
	}
	// The control: a DIFFERENT size does cost a pass, so the zeros above are a
	// no-op rather than a wrapper that has stopped laying out at all.
	h.onLoop(func() { r.SetSize(tui.Size{W: 14, H: 6}) })
	h.waitFor("a real change still re-measures", func() bool {
		return child.layouts() > before
	})

	// A NEGATIVE request is clamped to zero rather than rejected: the wrapper
	// has a minimum and the parent has constraints, so the useful answer is the
	// smallest legal size, not a panic in a setter a consumer drives from input.
	h.onLoop(func() { r.SetSize(tui.Size{W: -5, H: -5}) })
	h.settle()
	got, _ := requestedOn(t, h, r)
	if got.W < 0 || got.H < 0 {
		t.Errorf("RequestedSize() = %+v; a negative request is not a smaller box", got)
	}

	// Back to auto, and the wrapper tracks the child again.
	h.onLoop(func() { r.SetAuto() })
	h.settle()
	if _, ok := requestedOn(t, h, r); ok {
		t.Error("RequestedSize reported a request after returning to auto; auto has " +
			"none, and persisting one would mistake a tracked size for a choice")
	}
	if got := sizeOn(t, h, r); got != (tui.Size{W: 6, H: 3}) {
		t.Errorf("Size() = %+v after SetAuto, want the child's intrinsic 6x3", got)
	}
}

// TestEveryHandleSitsOnItsOwnEdgeAndDragsItsOwnWay.
//
// Six positions, and the two things each has to get right: WHERE the grip is
// placed, and WHICH WAY dragging it moves the size. They are easy to write and
// easy to get subtly wrong — a top-left grip that grows the box when dragged
// right is the kind of thing that looks fine until someone uses it — and the
// four rarely-exercised positions are exactly the ones no behavioural test of
// "resizing works" ever reaches.
func TestEveryHandleSitsOnItsOwnEdgeAndDragsItsOwnWay(t *testing.T) {
	for _, tc := range []struct {
		handle   widget.Handle
		name     string
		atRight  bool // the grip's column is the wrapper's last, not its first
		atBottom bool // ... and likewise for its row
		// dx and dy are the direction dragging by +1 cell moves the SIZE: a
		// left or top grip grows the box when dragged towards smaller
		// coordinates, so its sign is inverted.
		dx, dy int
	}{
		{widget.HandleBottomRight, "bottom-right", true, true, 1, 1},
		{widget.HandleBottomLeft, "bottom-left", false, true, -1, 1},
		{widget.HandleTopRight, "top-right", true, false, 1, -1},
		{widget.HandleTopLeft, "top-left", false, false, -1, -1},
		{widget.HandleRight, "right", true, false, 1, 0},
		{widget.HandleBottom, "bottom", false, true, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := &sizedChild{pref: tui.Size{W: 8, H: 4}}
			r := widget.NewResizable(child,
				widget.WithHandles(tc.handle),
				widget.WithHandleGlyph("#"),
				widget.WithMaxSize(tui.Size{W: 30, H: 15}))
			// A loose container at the ORIGIN, so the wrapper's own coordinates
			// are the grid's. Handing it straight to the host places it
			// wherever the host likes, and then every column in this test is
			// measuring the host's placement policy instead of the grip's.
			host := widget.NewOverlayHost(&looseCeiling{child: r, w: 30, hh: 15})
			h := startApp(t, host, 40, 20)
			defer h.stop()
			h.settle()

			sz := sizeOn(t, h, r)
			gx, gy := 0, 0
			if tc.atRight {
				gx = sz.W - 1
			}
			if tc.atBottom {
				gy = sz.H - 1
			}
			if got := h.row(gy); !strings.Contains(got, "#") {
				t.Fatalf("no grip painted on row %d:\n%s", gy, h.grid())
			}
			if x := columnOf(h.row(gy), "#"); tc.dy == 0 {
				// An edge grip runs the whole side, so only its column is fixed.
				if x != gx {
					t.Errorf("the %s grip starts at column %d, want %d", tc.name, x, gx)
				}
			} else if x != gx {
				t.Errorf("the %s grip is at column %d, want %d", tc.name, x, gx)
			}

			// Drag it one cell in the direction that should GROW the box, and
			// the size follows on the axes that grip controls.
			h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: gx, Y: gy})
			h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: gx + tc.dx, Y: gy + tc.dy})
			h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft,
				X: gx + tc.dx, Y: gy + tc.dy})
			want := tui.Size{W: sz.W, H: sz.H}
			if tc.dx != 0 {
				want.W++
			}
			if tc.dy != 0 {
				want.H++
			}
			h.waitFor("the drag grew the box on this grip's axes", func() bool {
				return sizeOn(t, h, r) == want
			})
		})
	}
}

// TestEveryArrowStepsItsOwnAxisAndOnlyWithShift.
//
// The keyboard half of the same surface. Bare arrows belong to whatever is
// inside the wrapper — a resizable wraps arbitrary content, and one that stole
// them would make every scrollable child unusable — so the modifier is part of
// the contract rather than a convenience.
func TestEveryArrowStepsItsOwnAxisAndOnlyWithShift(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   rune
		dw, dh int
	}{
		{"right grows the width", tui.KeyRight, 1, 0},
		{"left shrinks it", tui.KeyLeft, -1, 0},
		{"down grows the height", tui.KeyDown, 0, 1},
		{"up shrinks it", tui.KeyUp, 0, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			child := &sizedChild{pref: tui.Size{W: 8, H: 4}}
			r := widget.NewResizable(child, widget.WithMaxSize(tui.Size{W: 30, H: 15}))
			host := widget.NewOverlayHost(r)
			h := startApp(t, host, 40, 20)
			defer h.stop()
			h.onLoop(func() { child.Context().RequestFocus() })
			h.settle()
			before := sizeOn(t, h, r)

			// WITHOUT Shift: the wrapper must not touch it.
			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tc.code})
			h.settle()
			h.settle()
			if got := sizeOn(t, h, r); got != before {
				t.Errorf("a bare arrow resized the wrapper to %+v; bare arrows belong "+
					"to the content", got)
			}

			h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tc.code, Mods: tui.ModShift})
			want := tui.Size{W: before.W + tc.dw, H: before.H + tc.dh}
			h.waitFor("the Shift-arrow stepped its own axis", func() bool {
				return sizeOn(t, h, r) == want
			})
		})
	}
}

// columnOf is the CELL column of the first occurrence of sub in a grid row.
//
// strings.Index returns a BYTE offset, and a grid row is full of multi-byte
// glyphs — the "·" a test fixture fills with is two bytes, so a byte offset
// reads as double the column and the assertion fails against correct output.
// Every column assertion against the grid has to count runes.
func columnOf(row, sub string) int {
	i := strings.Index(row, sub)
	if i < 0 {
		return -1
	}
	return utf8.RuneCountInString(row[:i])
}
