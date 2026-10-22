package gui

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// wrappedCanvas is an application's canvas: golib's recorder, embedded, with its own additions.
type wrappedCanvas struct{ *RecordingCanvas }

// A CellPainter draws through its canvas's capability, not its type: a canvas embedding the
// recorder measures the recorder's cell and records the cells' frame, the caret's cell with it.
// Before, only the recorder itself (and the window's canvas) drew, and the wrapper drew nothing.
func TestACellPainterDrawsOnACanvasEmbeddingTheRecorder(t *testing.T) {
	rc := NewRecordingCanvas(Size{W: 200, H: 100}, Size{W: 8, H: 16})
	w := wrappedCanvas{rc}
	p := NewCellPainter()
	want, _ := headlessCell(16)
	if got, _ := p.Cell(w, 16); got != want {
		t.Errorf("Cell %+v, want the recorder's %+v", got, want)
	}
	p.Paint(w, 16, make([]tui.Cell, 6), 3, 2, CellCaret{X: 1, Y: 1, Show: true})
	if len(rc.Calls) != 1 || rc.Calls[0].Op != "PaintTerminalCells" || rc.Calls[0].Cells != (CellRect{W: 3, H: 2}) {
		t.Fatalf("recorded %+v, want one PaintTerminalCells of 3×2", rc.Calls)
	}
	if r := rc.Calls[0].Rect; r != (Rect{X: want.W, Y: want.H, W: want.W, H: want.H}) {
		t.Errorf("the caret's cell %+v, want the cell at (1, 1)", r)
	}
	p.Paint(w, 16, make([]tui.Cell, 5), 3, 2, CellCaret{})
	if len(rc.Calls) != 1 {
		t.Errorf("cells short of 3×2 recorded %+v", rc.Calls[1:])
	}
}
