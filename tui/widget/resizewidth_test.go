package widget_test

// AFFORDANCE GEOMETRY UNDER BOTH WIDTH POLICIES.
//
// An East Asian Ambiguous character — which is what every box-drawing and
// geometric glyph is, including the default grip "◢" and the default dividers
// "│" and "─" — occupies ONE cell under WidthPolicyDefault and TWO under
// WidthPolicyAmbiguousWide. The policy is fixed per App by its author.
//
// Measuring a glyph at construction bakes in the default policy, and the
// failure is not cosmetic: layout reserves one cell, the surface paints two,
// and every cell after it in that row is misaligned. The affordance itself
// simply vanishes. These tests run the same widget under both policies and
// assert it is visible and correctly sized in each.

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// bothPolicies is the pair every affordance has to survive.
var bothPolicies = []struct {
	name   string
	policy tui.WidthPolicy
	cells  int // what an Ambiguous glyph measures under it
}{
	{"default", tui.WidthPolicyDefault, 1},
	{"ambiguous-wide", tui.WidthPolicyAmbiguousWide, 2},
}

// TestAGripIsVisibleAndCorrectlySizedUnderEitherWidthPolicy.
//
// "±" is Ambiguous, so it is the glyph that separates a policy-aware
// measurement from a hardcoded one. Under AmbiguousWide it disappeared
// entirely: one cell was reserved, two were painted, and the grip was clipped
// out of existence.
func TestAGripIsVisibleAndCorrectlySizedUnderEitherWidthPolicy(t *testing.T) {
	for _, p := range bothPolicies {
		t.Run(p.name, func(t *testing.T) {
			child := &sizedChild{pref: tui.Size{W: 8, H: 4}}
			r := widget.NewResizable(child,
				widget.WithHandleGlyph("±"),
				widget.WithHandles(widget.HandleBottomRight),
				widget.WithHandlePlacement(widget.PlacementReserve),
				widget.WithMaxSize(tui.Size{W: 30, H: 15}))
			host := widget.NewOverlayHost(&looseCeiling{child: r, w: 30, hh: 15})
			h := startAppOpts(t, host, 40, 20, tui.WithWidthPolicy(p.policy))
			defer h.stop()
			h.settle()

			if !strings.Contains(h.grid(), "±") {
				t.Fatalf("the grip is not on screen under %s:\n%s", p.name, h.grid())
			}
			// Reserve took the glyph's ACTUAL cells from the child, so the
			// wrapper is the child plus exactly that many.
			if got := sizeOn(t, h, r); got.W != child.pref.W+p.cells {
				t.Errorf("Size().W = %d under %s, want %d — the reserved band is the "+
					"glyph's measured width, not a constant",
					got.W, p.name, child.pref.W+p.cells)
			}
		})
	}
}

// TestTheDEFAULTAffordanceGlyphsSurviveEitherWidthPolicy.
//
// The defaults are the case that matters most, and the one a test using a
// deliberately chosen glyph does not reach: "◢", "│" and "─" are themselves East
// Asian Ambiguous, so an application that selects WidthPolicyAmbiguousWide and
// configures nothing at all still gets two-cell affordances. All three vanished
// before this round. The glyphs are named here because they ARE the defaults
// under test — if they change, this test is supposed to fail and be updated,
// not to keep passing against something else.
func TestTheDEFAULTAffordanceGlyphsSurviveEitherWidthPolicy(t *testing.T) {
	for _, p := range bothPolicies {
		t.Run(p.name+"/grip", func(t *testing.T) {
			child := &sizedChild{pref: tui.Size{W: 8, H: 4}}
			r := widget.NewResizable(child,
				widget.WithHandles(widget.HandleBottomRight),
				widget.WithHandlePlacement(widget.PlacementReserve),
				widget.WithMaxSize(tui.Size{W: 30, H: 15}))
			host := widget.NewOverlayHost(&looseCeiling{child: r, w: 30, hh: 15})
			h := startAppOpts(t, host, 40, 20, tui.WithWidthPolicy(p.policy))
			defer h.stop()
			h.settle()

			if !strings.Contains(h.grid(), "◢") {
				t.Errorf("the default grip is not on screen under %s:\n%s", p.name, h.grid())
			}
			if got := sizeOn(t, h, r); got.W != child.pref.W+p.cells {
				t.Errorf("Size().W = %d under %s, want %d — the reserved band is the "+
					"default grip's measured width", got.W, p.name, child.pref.W+p.cells)
			}
		})
		t.Run(p.name+"/divider", func(t *testing.T) {
			s := widget.NewSplit(widget.Horizontal, &pane{fill: "a"}, &pane{fill: "b"})
			sh := newShell(s)
			h := startAppOpts(t, sh, 40, 6, tui.WithWidthPolicy(p.policy))
			h.barrier(sh)

			if !strings.Contains(h.grid(), "│") {
				t.Errorf("the default divider is not on screen under %s:\n%s", p.name, h.grid())
			}
			a, b, ok := cellsOn(t, h, s)
			if !ok {
				t.Fatal("no committed division")
			}
			if want := 40 - p.cells; a+b != want {
				t.Errorf("the panes hold %d cells under %s, want %d — the default "+
					"divider occupies %d", a+b, p.name, want, p.cells)
			}
		})
	}
}

// TestADividerIsVisibleAndCorrectlySizedUnderEitherWidthPolicy.
//
// The same rule for Split, where the width is GEOMETRY: a two-cell divider
// takes two cells from the panes. Reserving one for it put the divider's second
// half over pane B.
func TestADividerIsVisibleAndCorrectlySizedUnderEitherWidthPolicy(t *testing.T) {
	for _, p := range bothPolicies {
		t.Run(p.name, func(t *testing.T) {
			s := widget.NewSplit(widget.Horizontal, &pane{fill: "a"}, &pane{fill: "b"},
				widget.WithSplitDividerGlyphs("±", "─"))
			sh := newShell(s)
			h := startAppOpts(t, sh, 40, 6, tui.WithWidthPolicy(p.policy))
			h.barrier(sh)

			if !strings.Contains(h.grid(), "±") {
				t.Fatalf("the divider is not on screen under %s:\n%s", p.name, h.grid())
			}
			// The panes share what is left after the divider's real width.
			a, b, ok := cellsOn(t, h, s)
			if !ok {
				t.Fatal("no committed division")
			}
			if want := 40 - p.cells; a+b != want {
				t.Errorf("the panes hold %d cells under %s, want %d — the divider "+
					"occupies %d", a+b, p.name, want, p.cells)
			}
			// And the whole divider is grabbable, not just its first column.
			for off := range p.cells {
				before := aCellsOn(t, h, s)
				h.inject(
					tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: before + off, Y: 0},
					tui.MouseEvent{Kind: tui.MouseMotion, X: before + off + 2, Y: 0},
					tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: before + off + 2, Y: 0},
				)
				h.barrier(sh)
				if got := aCellsOn(t, h, s); got == before {
					t.Errorf("a press on divider column %d of %d did nothing under %s",
						off, p.cells, p.name)
				}
			}
		})
	}
}

// TestAWideEdgeGripRepeatsAlongItsWholeEdge.
//
// A two-cell glyph written at every x overlaps its own previous pair and
// dissolves it, so a six-cell edge kept a single head. Surface.Fill steps by
// the measured width and handles the odd tail; the assertion is simply that the
// edge is covered rather than decorated at one end.
func TestAWideEdgeGripRepeatsAlongItsWholeEdge(t *testing.T) {
	child := &sizedChild{pref: tui.Size{W: 8, H: 4}}
	r := widget.NewResizable(child,
		widget.WithHandleGlyph("±"),
		widget.WithHandles(widget.HandleTop),
		widget.WithHandlePlacement(widget.PlacementReserve),
		widget.WithMaxSize(tui.Size{W: 30, H: 15}))
	host := widget.NewOverlayHost(&looseCeiling{child: r, w: 30, hh: 15})
	h := startAppOpts(t, host, 40, 20, tui.WithWidthPolicy(tui.WidthPolicyAmbiguousWide))
	defer h.stop()
	h.settle()

	// The top edge is the grip's row. Eight columns of a two-cell glyph is four
	// of them; one head would mean the fill collapsed.
	got := strings.Count(h.row(0), "±")
	if got < 4 {
		t.Errorf("the top edge carries %d grip glyphs, want 4 across its eight "+
			"columns; a wide glyph written per column overwrites its own pair\n%s",
			got, h.grid())
	}
}
