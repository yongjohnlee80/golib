package tui_test

import (
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// A CursorLatch reports a change only against the state the last Take gave: a fresh latch is a
// hidden cursor at no position, and unchanged; each call that moves the state is a change once; the
// same state latched again, or a change undone within the frame, is none.
func TestACursorLatchReportsChangesAgainstTheLastTake(t *testing.T) {
	var l tui.CursorLatch
	take := func(what string, want tui.CursorState, wantChanged bool) {
		t.Helper()
		got, changed := l.Take()
		if got != want || changed != wantChanged {
			t.Errorf("%s: Take = %+v, changed %v; want %+v, changed %v", what, got, changed, want, wantChanged)
		}
	}
	take("a fresh latch", tui.CursorState{}, false)

	l.Show()
	l.SetPos(3, 2)
	l.SetShape(tui.CursorShapeBar)
	shown := tui.CursorState{X: 3, Y: 2, PosSet: true, Visible: true, Shape: tui.CursorShapeBar}
	take("shown at 3,2 as a bar", shown, true)
	take("taken again with nothing latched", shown, false)

	l.Show()
	l.SetPos(3, 2)
	l.SetShape(tui.CursorShapeBar)
	take("the same state latched again", shown, false)

	l.Hide()
	l.Show()
	take("hidden, then shown again within the frame", shown, false)

	l.SetPos(4, 2)
	take("moved", tui.CursorState{X: 4, Y: 2, PosSet: true, Visible: true, Shape: tui.CursorShapeBar}, true)

	var at tui.CursorLatch
	at.SetPos(0, 0)
	if got, changed := at.Take(); !changed || !got.PosSet {
		t.Errorf("a first position at 0,0 is a change: Take = %+v, changed %v", got, changed)
	}
}

// A CursorLatch keeps the colour it is given, a palette index too: what a backend can show is the
// backend's decision, made before it latches. Unset, the colour is the terminal's own and zero.
func TestACursorLatchKeepsTheColourItIsGiven(t *testing.T) {
	var l tui.CursorLatch
	palette := tui.CellColor{Kind: tui.CellColorANSI256, Index: 208}
	l.SetColor(palette, true)
	if got, changed := l.Take(); !changed || !got.Colored || got.Color != palette {
		t.Fatalf("a palette colour latched: Take = %+v, changed %v; want it kept", got, changed)
	}
	l.SetColor(tui.CellColor{Kind: tui.CellColorRGB, R: 1, G: 2, B: 3}, false)
	got, changed := l.Take()
	if !changed || got.Colored || got.Color != (tui.CellColor{}) {
		t.Errorf("the terminal's own colour: Take = %+v, changed %v; want uncoloured and a zero colour", got, changed)
	}
	l.SetColor(tui.CellColor{Kind: tui.CellColorRGB, R: 9}, false)
	if _, changed := l.Take(); changed {
		t.Error("unset again with another colour is no change: an unset colour is the terminal's own whatever it was")
	}
}

// An ImageLatch gives each Take the calls made since the last one, in the order made, and then
// none.
func TestAnImageLatchGivesItsCallsInOrderOnce(t *testing.T) {
	var l tui.ImageLatch
	if ops := l.Take(); ops != nil {
		t.Fatalf("a fresh latch: Take = %v, want nil", ops)
	}
	a := tui.ImagePlacement{Image: tui.Image{ID: 1}, Cols: 2, Rows: 1}
	b := tui.ImagePlacement{Image: tui.Image{ID: 2}, X: 4, Cols: 3, Rows: 2}
	l.Place(a)
	l.Delete(7)
	l.Place(b)
	want := []tui.ImageOp{{Place: &a}, {Delete: 7}, {Place: &b}}
	if got := l.Take(); !reflect.DeepEqual(got, want) {
		t.Errorf("Take = %+v, want %+v", got, want)
	}
	if ops := l.Take(); ops != nil {
		t.Errorf("a second Take: %v, want nil", ops)
	}
}

// TestBackend applies the latched cursor at Flush and only then: a call between flushes shows
// nothing until the next one.
func TestTestBackendAppliesTheLatchedCursorAtFlush(t *testing.T) {
	tb := tui.NewTestBackend(10, 3)
	tb.ShowCursor()
	tb.SetCursor(2, 1)
	tb.SetCursorShape(tui.CursorShapeUnderline)
	tb.SetCursorColor(tui.CellColor{Kind: tui.CellColorANSI, Index: 3}, true)
	if _, _, visible := tb.CursorPos(); visible {
		t.Fatal("the cursor shows before a Flush")
	}
	if err := tb.Flush(nil); err != nil {
		t.Fatal(err)
	}
	x, y, visible := tb.CursorPos()
	c, colored := tb.CursorColor()
	if x != 2 || y != 1 || !visible || tb.CursorShape() != tui.CursorShapeUnderline || !colored || c != (tui.CellColor{Kind: tui.CellColorANSI, Index: 3}) {
		t.Errorf("after Flush: at %d,%d visible %v, shape %v, colour %+v (%v)", x, y, visible, tb.CursorShape(), c, colored)
	}
}
