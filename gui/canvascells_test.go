package gui

import "testing"

// A canvas's size in whole cells rounds to the nearest cell; a canvas with no cell size has none.
func TestCanvasCellsRoundsToWholeCells(t *testing.T) {
	if cols, rows := CanvasCells(NewRecordingCanvas(Size{W: 100, H: 40}, Size{W: 8, H: 16})); cols != 13 || rows != 3 {
		t.Errorf("100x40 in 8x16 cells: %dx%d, want 13x3 (12.5 and 2.5 round up)", cols, rows)
	}
	if cols, rows := CanvasCells(NewRecordingCanvas(Size{W: 100, H: 40}, Size{})); cols != 0 || rows != 0 {
		t.Errorf("no cell size: %dx%d, want 0x0", cols, rows)
	}
}
