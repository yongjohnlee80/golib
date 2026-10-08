package gui

import (
	"image"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

type caretView struct {
	r    Rect
	base float32
	ok   bool
}

func (caretView) Paint(Canvas)                   {}
func (v caretView) Caret() (Rect, float32, bool) { return v.r, v.base, v.ok }

func TestViewCaretInDevicePixels(t *testing.T) {
	m := metrics{origin: image.Pt(4, 6), scale: 2, cell: image.Pt(16, 32)}
	natives := []tui.NativePlacement{
		{View: "plain", X: 0, Y: 0, Cols: 3, Rows: 1},
		{View: caretView{ok: false}, X: 1, Y: 1, Cols: 3, Rows: 1},
		{View: caretView{r: Rect{X: 10, Y: 3, W: 1, H: 12}, base: 9, ok: true}, X: 2, Y: 1, Cols: 10, Rows: 2},
	}
	r, base, ok := viewCaret(m, natives)
	if !ok {
		t.Fatal("no caret found")
	}
	// cell (2, 1) is at 4+2*16, 6+1*32 = (36, 38); the view's (10, 3) is 2× that in device pixels
	if want := image.Rect(56, 44, 58, 68); r != want || base != 18 {
		t.Fatalf("caret %v base %d, want %v base 18", r, base, want)
	}
	if _, _, ok := viewCaret(m, natives[:2]); ok {
		t.Error("a view whose caret is off reported one")
	}
}
