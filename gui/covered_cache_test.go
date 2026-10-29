package gui

import (
	"image"
	"testing"

	"github.com/yongjohnlee80/gio/unit"
	"github.com/yongjohnlee80/golib/tui"
)

// coveredFrame draws one frame of a native view at (2,1) 6x3 with a dialog's cells over it at
// (3,2) 3x2, and returns the recording of those covered cells.
func coveredFrame(t *testing.T, r *renderer, g *grid, m metrics, natives []tui.NativePlacement) *coveredRecording {
	t.Helper()
	r.frameParts(g, m, cursorState{}, natives, nil, true)
	return r.covered[CellRect{X: 3, Y: 2, W: 3, H: 2}]
}

func coveredSetup() (*renderer, *grid, metrics, []tui.NativePlacement) {
	m := measure(testFont, 14, 0, image.Pt(200, 100), unit.Metric{PxPerDp: 1, PxPerSp: 1})
	g := &grid{}
	g.resize(m.grid.W, m.grid.H)
	r := newRenderer(newTestShaper(), "Go Mono", DefaultTheme())
	natives := []tui.NativePlacement{{View: ViewFunc(func(Canvas) {}), X: 2, Y: 1, Cols: 6, Rows: 3,
		Covered: []tui.Rect{{X: 3, Y: 2, W: 3, H: 2}}}}
	return r, g, m, natives
}

// TestCoveredCellsReplayWhileTheirRowsHold: a frame that re-recorded no row under a dialog's
// cells replays their recording rather than drawing them again.
func TestCoveredCellsReplayWhileTheirRowsHold(t *testing.T) {
	r, g, m, natives := coveredSetup()
	first := coveredFrame(t, r, g, m, natives)
	if first == nil {
		t.Fatal("the covered cells were not recorded")
	}
	if again := coveredFrame(t, r, g, m, natives); again != first {
		t.Fatal("a frame that changed no row under the dialog recorded its cells again")
	}
}

// TestACoveredRecordingIsRedrawnWhenARowUnderItChanges: a cell written in a covered row makes a
// new recording, holding the row's new gen; a row outside it doesn't.
func TestACoveredRecordingIsRedrawnWhenARowUnderItChanges(t *testing.T) {
	r, g, m, natives := coveredSetup()
	first := coveredFrame(t, r, g, m, natives)
	g.apply([]tui.CellUpdate{{X: 0, Y: 0, Cell: tui.Cell{Content: "x", Width: 1}}}) // outside the covered rows (2 and 3)
	if again := coveredFrame(t, r, g, m, natives); again != first {
		t.Fatal("a change outside the covered rows made a new recording")
	}
	g.apply([]tui.CellUpdate{{X: 4, Y: 3, Cell: tui.Cell{Content: "y", Width: 1}}}) // under the dialog, in its second row
	redrawn := coveredFrame(t, r, g, m, natives)
	if redrawn == first {
		t.Fatal("a cell written under the dialog kept the old recording: it would show the old cell")
	}
	if redrawn.gens[1] != r.rowGen[3] {
		t.Fatalf("the new recording holds row 3's gen %d, the row has %d", redrawn.gens[1], r.rowGen[3])
	}
}

// TestAClosedDialogsRecordingIsDropped: a frame without the dialog forgets its recording.
func TestAClosedDialogsRecordingIsDropped(t *testing.T) {
	r, g, m, natives := coveredSetup()
	coveredFrame(t, r, g, m, natives)
	coveredFrame(t, r, g, m, nil)
	if len(r.covered) != 0 {
		t.Fatalf("%d covered recordings kept after the dialog closed", len(r.covered))
	}
}
