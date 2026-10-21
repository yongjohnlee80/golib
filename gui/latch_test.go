package gui

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

func onePixelPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// The image calls are latched: nothing shows until a frame applies them, in the order made; a
// placement of the same version moves without a new decode; a delete removes the image.
func TestTheImageCallsApplyAtTheFrameInOrder(t *testing.T) {
	b := NewBackend()
	pic := onePixelPNG(t)
	b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: 1, PNG: pic, Version: 1}, Cols: 1, Rows: 1})
	b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: 2, PNG: pic, Version: 1}, Cols: 1, Rows: 1})
	if n := len(b.imageList()); n != 0 {
		t.Fatalf("%d images shown before a frame took the calls", n)
	}
	b.applyImages()
	first := b.placed[1].op

	b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: 1, PNG: pic, Version: 1}, X: 3, Cols: 1, Rows: 1})
	b.DeleteImage(2)
	b.applyImages()
	list := b.imageList()
	if len(list) != 1 || list[0].ID != 1 || list[0].X != 3 {
		t.Fatalf("after a move and a delete: %+v, want image 1 alone at x 3", list)
	}
	if b.placed[1].op != first {
		t.Fatal("moving a placement of the same version decoded it again")
	}
}

// The latched cursor is drawn as the App last set it, its colour resolved by the theme.
func TestTheCursorIsTakenAsLastSet(t *testing.T) {
	b := NewBackend()
	b.ShowCursor()
	b.SetCursor(4, 2)
	b.SetCursorShape(tui.CursorShapeBar)
	red := tui.CellColor{Kind: tui.CellColorRGB, R: 0xff}
	b.SetCursorColor(red, true)
	cur := b.takeCursor()
	want := cursorState{visible: true, x: 4, y: 2, shape: tui.CursorShapeBar, colored: true, color: b.cfg.theme.resolve(red, true)}
	if cur != want {
		t.Fatalf("taken cursor %+v, want %+v", cur, want)
	}
	b.SetCursorColor(red, false)
	b.HideCursor()
	if cur := b.takeCursor(); cur.colored || cur.visible {
		t.Fatalf("after HideCursor and the theme's colour back: %+v", cur)
	}
}
