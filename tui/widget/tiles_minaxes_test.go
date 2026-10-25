package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// tiles_minaxes_test.go: a tile's minimum on each axis (TilePlace's MinW and MinH). A strip keeps
// the one across its edge, the centre both, and Min holds on an axis whose own is unset.

// A centre that wants 40 columns but only 8 rows lets a member tile in a 100x30 manager. With Min
// 40, which is 40 rows too, nothing beside it ever fits, so the member is never governed.
func TestACentresMinimumIsItsOwnOnEachAxis(t *testing.T) {
	for _, c := range []struct {
		name     string
		place    widget.TilePlace
		governed bool
	}{
		{"Min 40 on both axes", widget.TilePlace{Edge: tui.DockCenter, Min: 40}, false},
		{"MinW 40, MinH 8", widget.TilePlace{Edge: tui.DockCenter, MinW: 40, MinH: 8}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			tiles := widget.NewTiles()
			tiles.Add("page", widget.NewBox(widget.NewText(""), widget.WithTitle("PG")), c.place)
			a := newMember("AA", widget.AtRect(tui.Rect{X: 50, Y: 3, W: 12, H: 4}), widget.Left)
			tiles.AddMember("a", a, widget.TilePlace{Edge: tui.DockLeft, Size: 30, MinW: 20, MinH: 6}, true)
			r := startTiles(t, 100, 30, tiles, tiles, map[string]*member{"a": a})
			if got := r.governed("a"); got != c.governed {
				t.Fatalf("the member governed %v beside a centre of %+v, want %v:\n%s", got, c.place, c.governed, r.h.tb.String())
			}
		})
	}
}

// A strip keeps the minimum across its edge: 20 columns at the left, and re-docked to the bottom,
// 6 rows, not 20. Its Size asks less on either axis, so the minimum is what places it.
func TestAStripKeepsTheMinimumAcrossItsEdgeWhereverItDocks(t *testing.T) {
	f := newTiles(t, 100, 30, []tileSpec{
		{"strip", widget.TilePlace{Edge: tui.DockLeft, Edges: []tui.DockEdge{tui.DockLeft, tui.DockBottom}, Size: 10, MinW: 20, MinH: 6}},
		{"page", widget.TilePlace{Edge: tui.DockCenter, Min: 5}},
	})
	f.wantRects("at the left", map[string]tui.Rect{"strip": {X: 0, Y: 0, W: 20, H: 30}})
	f.h.onLoop(func() { f.m.SetEdge("strip", tui.DockBottom) })
	f.h.settle()
	f.wantRects("re-docked to the bottom", map[string]tui.Rect{"strip": {X: 0, Y: 24, W: 100, H: 6}})
}

// Min holds on an axis whose own minimum is unset, so a document that writes Min alone is
// unchanged: Min 20 with MinH 6 keeps 20 columns at the left and 6 rows at the bottom, and Min 20
// alone 20 of each.
func TestMinHoldsOnAnAxisWhoseOwnMinimumIsUnset(t *testing.T) {
	for _, c := range []struct {
		name         string
		min, minH    int
		left, bottom int
	}{
		{"Min 20, MinH 6", 20, 6, 20, 6},
		{"Min 20 alone", 20, 0, 20, 20},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newTiles(t, 100, 30, []tileSpec{
				{"strip", widget.TilePlace{Edge: tui.DockLeft, Edges: []tui.DockEdge{tui.DockLeft, tui.DockBottom}, Size: 10, Min: c.min, MinH: c.minH}},
				{"page", widget.TilePlace{Edge: tui.DockCenter, Min: 5}},
			})
			f.wantRects("at the left", map[string]tui.Rect{"strip": {X: 0, Y: 0, W: c.left, H: 30}})
			f.h.onLoop(func() { f.m.SetEdge("strip", tui.DockBottom) })
			f.h.settle()
			f.wantRects("at the bottom", map[string]tui.Rect{"strip": {X: 0, Y: 30 - c.bottom, W: 100, H: c.bottom}})
		})
	}
}

// SetMinSize moves the clamp live, as SetMin does: a strip given 30 columns is 30 wide at once, and
// 0 there gives the width back to its Min.
func TestSetMinSizeMovesTheClampLive(t *testing.T) {
	f := newTiles(t, 100, 30, []tileSpec{
		{"strip", widget.TilePlace{Edge: tui.DockLeft, Size: 10, Min: 15}},
		{"page", widget.TilePlace{Edge: tui.DockCenter, Min: 5}},
	})
	f.wantRects("Min alone", map[string]tui.Rect{"strip": {X: 0, Y: 0, W: 15, H: 30}})
	f.h.onLoop(func() { f.m.SetMinSize("strip", tui.Size{W: 30}) })
	f.h.settle()
	f.wantRects("MinW 30", map[string]tui.Rect{"strip": {X: 0, Y: 0, W: 30, H: 30}})
	f.h.onLoop(func() { f.m.SetMinSize("strip", tui.Size{}) })
	f.h.settle()
	f.wantRects("MinW unset again", map[string]tui.Rect{"strip": {X: 0, Y: 0, W: 15, H: 30}})
}
