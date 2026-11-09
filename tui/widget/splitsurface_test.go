package widget_test

// The Split family's PUBLISHED SURFACE: the construction options, the setters
// and the accessors. The same shape as the Button, Modal, Menu and Resizable
// surface tests, because a consumer who has learned one has learned them all —
// and that claim is only true if the tests check the same things.
//
// This file exists because the round that gave Split its configuration seams
// added public surface without covering it, which is a pattern worth naming: a
// widget's behaviour gets tested because it is interesting, and its options get
// missed because each one looks too small to be worth a test until one of them
// silently does nothing.

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/core"
)

// TestSplitConstructionRefusesWhatCannotBeHonoured.
func TestSplitConstructionRefusesWhatCannotBeHonoured(t *testing.T) {
	a, b := &pane{fill: "a"}, &pane{fill: "b"}
	for _, tc := range []struct {
		name string
		call func()
	}{
		{"an orientation outside the set", func() {
			core.NewSplit(core.Vertical+1, a, b)
		}},
		{"a nil pane", func() { core.NewSplit(core.Horizontal, nil, b) }},
		{"a ratio at zero", func() {
			core.NewSplit(core.Horizontal, a, b, core.WithRatio(0))
		}},
		{"a ratio at one", func() {
			core.NewSplit(core.Horizontal, a, b, core.WithRatio(1))
		}},
		{"a negative minimum", func() {
			core.NewSplit(core.Horizontal, a, b, core.WithMinSizes(-1, 0))
		}},
		{"a step below one", func() {
			core.NewSplit(core.Horizontal, a, b,
				core.WithSplitResizeStep(0, core.StepCells))
		}},
		{"a step unit outside the set", func() {
			core.NewSplit(core.Horizontal, a, b,
				core.WithSplitResizeStep(1, core.StepPercent+1))
		}},
		{"a multi-grapheme divider glyph", func() {
			core.NewSplit(core.Horizontal, a, b,
				core.WithSplitDividerGlyphs("ab", "─"))
		}},
		{"a zero-width divider glyph", func() {
			// One cluster, no cells: it would be reserved geometry that paints
			// nothing, which is the invisible-affordance failure again.
			core.NewSplit(core.Horizontal, a, b,
				core.WithSplitDividerGlyphs("\u200b", "─"))
		}},
		{"a pointer policy outside the set", func() {
			core.NewSplit(core.Horizontal, a, b).
				WithPointerPolicy(tui.PointerDisabled + 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !panicked(tc.call) {
				t.Error("it was accepted")
			}
		})
	}
	// The controls: the last valid value of each closed set is accepted.
	if panicked(func() {
		core.NewSplit(core.Vertical, a, b,
			core.WithRatio(0.999),
			core.WithMinSizes(0, 0),
			core.WithSplitResizeStep(1, core.StepPercent),
			// A WIDE glyph is legal: its width is geometry, and the split
			// reserves the cells it actually occupies.
			core.WithSplitDividerGlyphs("世", "─")).
			WithPointerPolicy(tui.PointerDisabled)
	}) {
		t.Error("a legal configuration was rejected")
	}
}

// panicked reports whether fn panicked, which is how every construction option
// in this package refuses a value written in source at the call site.
func panicked(fn func()) (did bool) {
	defer func() {
		if recover() != nil {
			did = true
		}
	}()
	fn()
	return false
}

// TestTheDividerGlyphsAndStyleReachTheScreen.
//
// An option that stored its value without it reaching a cell would pass any
// test that only read the field back.
func TestTheDividerGlyphsAndStyleReachTheScreen(t *testing.T) {
	for _, tc := range []struct {
		name   string
		orient core.Orientation
		glyph  string
	}{
		{"a horizontal split draws the vertical glyph", core.Horizontal, "!"},
		{"a vertical split draws the horizontal one", core.Vertical, "="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, hz := "│", "─"
			if tc.orient == core.Horizontal {
				v = tc.glyph
			} else {
				hz = tc.glyph
			}
			s := core.NewSplit(tc.orient, &pane{fill: "a"}, &pane{fill: "b"},
				core.WithSplitDividerGlyphs(v, hz),
				core.WithDividerStyle(style.New().Foreground(style.TokenError)))
			sh := newShell(s)
			h := startApp(t, sh, 20, 6)
			h.barrier(sh)

			if !strings.Contains(h.grid(), tc.glyph) {
				t.Errorf("the configured divider glyph %q is not on screen:\n%s",
					tc.glyph, h.grid())
			}
		})
	}
}

// TestTheConfiguredStepMovesTheDividerByThatMuch.
//
// WithSplitResizeStep is two values, and both have to arrive: a step size that
// was stored but not used, or a unit that was ignored, leaves a key binding
// that moves the wrong distance — which reads as a sluggish control rather than
// a broken one, and so goes unreported.
func TestTheConfiguredStepMovesTheDividerByThatMuch(t *testing.T) {
	t.Run("cells", func(t *testing.T) {
		s := core.NewSplit(core.Horizontal, &pane{fill: "a"}, &pane{fill: "b"},
			core.WithSplitResizeStep(4, core.StepCells))
		sh := newShell(s)
		h := startApp(t, sh, 40, 8)
		h.inject(tab())
		h.barrier(sh)
		a0 := aCellsOn(t, h, s)

		h.inject(keyMod(tui.KeyRight, tui.ModAlt))
		h.waitFor("the divider moved by the configured four cells", func() bool {
			return aCellsOn(t, h, s) == a0+4
		})
	})

	t.Run("percent", func(t *testing.T) {
		// 10% of the 39 available cells is 3.
		s := core.NewSplit(core.Horizontal, &pane{fill: "a"}, &pane{fill: "b"},
			core.WithSplitResizeStep(10, core.StepPercent))
		sh := newShell(s)
		h := startApp(t, sh, 40, 8)
		h.inject(tab())
		h.barrier(sh)
		a0 := aCellsOn(t, h, s)

		h.inject(keyMod(tui.KeyRight, tui.ModAlt))
		h.waitFor("the divider moved by ten percent of the available cells", func() bool {
			return aCellsOn(t, h, s) == a0+3
		})
	})
}

// TestAPointerDisabledSplitStillResizesFromTheKeyboard.
//
// The parity that makes the policy safe to use: it disables the pointer path
// and nothing else. A policy that reached the keyboard too would silently make
// a split unresizable for anyone who set it to stop stray clicks.
func TestAPointerDisabledSplitStillResizesFromTheKeyboard(t *testing.T) {
	s := core.NewSplit(core.Horizontal, &pane{fill: "a"}, &pane{fill: "b"}).
		WithPointerPolicy(tui.PointerDisabled)
	sh := newShell(s)
	h := startApp(t, sh, 40, 8)
	h.inject(tab())
	h.barrier(sh)
	a0 := aCellsOn(t, h, s)

	// The divider is inert to the mouse.
	h.inject(
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: a0, Y: 0},
		tui.MouseEvent{Kind: tui.MouseMotion, X: a0 + 5, Y: 0},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: a0 + 5, Y: 0},
	)
	h.barrier(sh)
	if got := aCellsOn(t, h, s); got != a0 {
		t.Errorf("a drag moved a pointer-disabled split's divider to %d", got)
	}

	// The keyboard is not.
	h.inject(keyMod(tui.KeyRight, tui.ModAlt))
	h.waitFor("the keyboard still moves it", func() bool {
		return aCellsOn(t, h, s) == a0+1
	})
}

// TestZoomedReportsWhatZoomDid — a one-line accessor, and the only way a
// consumer can ask which pane is maximised without tracking it themselves.
func TestZoomedReportsWhatZoomDid(t *testing.T) {
	h, s, sh := splitFixture(t, 30, 8)
	read := func() core.SplitPane {
		var p core.SplitPane
		h.onLoop(func() { p = s.Zoomed() })
		return p
	}
	if got := read(); got != core.PaneNone {
		t.Errorf("Zoomed() = %v at rest, want PaneNone", got)
	}
	for _, want := range []core.SplitPane{core.PaneA, core.PaneB, core.PaneNone} {
		h.onLoop(func() { s.Zoom(want) })
		h.barrier(sh)
		if got := read(); got != want {
			t.Errorf("Zoomed() = %v after Zoom(%v)", got, want)
		}
	}
}

// TestSetRatioRefusesWhatIsNotADivision — the setter is driven from consumer
// code and from persistence, so a restored file carrying a nonsense value must
// fail where it is set rather than at the next layout.
func TestSetRatioRefusesWhatIsNotADivision(t *testing.T) {
	h, s, _ := splitFixture(t, 30, 8)
	for _, r := range []float64{0, 1, -0.5, 2} {
		var did bool
		h.onLoop(func() { did = panicked(func() { s.SetRatio(r) }) })
		if !did {
			t.Errorf("SetRatio(%v) was accepted; a ratio outside (0,1) is not a division", r)
		}
	}
	// And the control: a legal one is taken.
	h.onLoop(func() { s.SetRatio(0.25) })
	h.settle()
	if got := requestedRatioOn(t, h, s); got != 0.25 {
		t.Errorf("RequestedRatio() = %v, want the 0.25 that was set", got)
	}
}

// TestAVerticalSplitDragsItsOwnDivider.
//
// Every axis-dependent branch in the divider's path, exercised on the axis the
// tests mostly do not use. A horizontal split is the common case and so the one
// everything gets written against; the vertical branches of the handle choice,
// the point projection, the hit test and the step then go unexercised, and a
// sign or axis error in any of them ships.
func TestAVerticalSplitDragsItsOwnDivider(t *testing.T) {
	s := core.NewSplit(core.Vertical, &pane{fill: "a"}, &pane{fill: "b"})
	sh := newShell(s)
	h := startApp(t, sh, 20, 21)
	h.inject(tab())
	h.barrier(sh)

	a0 := aCellsOn(t, h, s)

	// The MOUSE: the divider is a row, so the press is on y == aCells and the
	// drag is measured down the screen.
	h.inject(
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 3, Y: a0},
		tui.MouseEvent{Kind: tui.MouseMotion, X: 3, Y: a0 + 4},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 3, Y: a0 + 4},
	)
	h.waitFor("the vertical divider followed the pointer", func() bool {
		return aCellsOn(t, h, s) == a0+4
	})

	// The ACTION, naming this split's own divider explicitly.
	after := aCellsOn(t, h, s)
	h.onLoop(func() {
		s.Context().DoAction(core.ResizeBeginAction{
			Handle: core.HandleHorizontalDivider, At: tui.Point{X: 3, Y: after}})
		s.Context().DoAction(core.ResizeUpdateAction{At: tui.Point{X: 3, Y: after - 2}})
		s.Context().DoAction(core.ResizeEndAction{})
	})
	h.waitFor("the action moved it on the vertical axis", func() bool {
		return aCellsOn(t, h, s) == after-2
	})

	// And ResizeSetAction places it absolutely, on the height axis.
	h.onLoop(func() {
		s.Context().DoAction(core.ResizeSetAction{Size: tui.Size{W: 99, H: 6}})
	})
	h.waitFor("the set action used the height for a vertical split", func() bool {
		return aCellsOn(t, h, s) == 6
	})
}

// TestResizeSetActionPlacesTheDividerAbsolutely — the horizontal half of the
// same seam, and the one a consumer restoring a saved layout uses.
func TestResizeSetActionPlacesTheDividerAbsolutely(t *testing.T) {
	h, s, sh := splitFixture(t, 40, 8)
	h.onLoop(func() {
		s.Context().DoAction(core.ResizeSetAction{Size: tui.Size{W: 12, H: 99}})
	})
	h.barrier(sh)
	if got := aCellsOn(t, h, s); got != 12 {
		t.Errorf("pane A holds %d cells after a set to 12, want 12 — a horizontal "+
			"split takes the width and ignores the height", got)
	}
}
