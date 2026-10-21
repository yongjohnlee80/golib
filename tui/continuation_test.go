package tui

import "testing"

// A wide cell's continuation keeps its head's face as well as its attributes, so the second column
// draws in the same font: the frame buffer writes it so, and a mirror that rebuilds it from a diff
// (TestBackend here, the web grid and gui too) gets the same cell.
func TestAContinuationKeepsItsHeadsFace(t *testing.T) {
	head := Cell{Content: "漢", Width: 2, Attrs: CellAttrs{Mask: AttrBold}, Face: 3}
	want := Cell{Content: "", Width: 0, Attrs: head.Attrs, Face: 3}
	if got := ContinuationOf(head); got != want {
		t.Fatalf("ContinuationOf = %+v, want %+v", got, want)
	}
	tb := NewTestBackend(4, 1)
	if err := tb.Flush([]CellUpdate{{X: 1, Y: 0, Cell: head}}); err != nil {
		t.Fatal(err)
	}
	if got := tb.Snapshot()[0][2]; got != want {
		t.Errorf("the mirror's continuation is %+v, want %+v", got, want)
	}
}
