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
	if n := len(b.images.list()); n != 0 {
		t.Fatalf("%d images shown before a frame took the calls", n)
	}
	b.images.apply()
	first := b.images.placed[1].op

	b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: 1, PNG: pic, Version: 1}, X: 3, Cols: 1, Rows: 1})
	b.DeleteImage(2)
	b.images.apply()
	list := b.images.list()
	if len(list) != 1 || list[0].ID != 1 || list[0].X != 3 {
		t.Fatalf("after a move and a delete: %+v, want image 1 alone at x 3", list)
	}
	if b.images.placed[1].op != first {
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

// Flush is where the latches meet the frame: nothing shows before it; it applies the image calls
// in the order made (a place, a delete and a newer place of one id leave the newer one), and
// publishes the latched cursor as the frame's caret; the next Flush applies a delete and a hide.
func TestFlushAppliesTheLatchesToTheFrame(t *testing.T) {
	b := NewBackend()
	m := metrics{window: image.Pt(32, 32), cell: image.Pt(8, 16), ppem: 14, baseline: 12, scale: 1, grid: tui.Size{W: 4, H: 2}}
	b.metrics.Store(&m)
	b.grid.resize(4, 2)
	pic := onePixelPNG(t)
	b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: 1, PNG: pic, Version: 1}, Cols: 1, Rows: 1})
	b.DeleteImage(1)
	b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: 1, PNG: pic, Version: 2}, X: 1, Cols: 1, Rows: 1})
	b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: 2, PNG: pic, Version: 1}, X: 2, Cols: 1, Rows: 1})
	b.ShowCursor()
	b.SetCursor(3, 1)
	if n := len(b.images.list()); n != 0 || b.latest != nil {
		t.Fatalf("before any Flush: %d images shown, frame %v; want none", n, b.latest)
	}

	if err := b.Flush(nil); err != nil {
		t.Fatal(err)
	}
	list := b.images.list()
	if len(list) != 2 || list[0].ID != 1 || list[0].Version != 2 || list[0].X != 1 || list[1].ID != 2 {
		t.Fatalf("after Flush: %+v, want image 1 at version 2 and x 1, then image 2", list)
	}
	if b.latest == nil || b.latest.caret != image.Rect(24, 16, 32, 32) {
		t.Fatalf("after Flush the frame's caret is not the cursor's cell (3, 1): %+v", b.latest)
	}

	b.DeleteImage(1)
	b.HideCursor()
	if err := b.Flush(nil); err != nil {
		t.Fatal(err)
	}
	if list := b.images.list(); len(list) != 1 || list[0].ID != 2 {
		t.Fatalf("the next Flush kept a deleted image: %+v", list)
	}
	if !b.latest.caret.Empty() {
		t.Fatalf("the next Flush kept a hidden cursor's caret %v", b.latest.caret)
	}
}

// A placement of a new version decodes its image again; one that is not a PNG shows nothing, the
// cells beneath it showing as on a terminal that refuses it.
func TestANewVersionOfAnImageIsDecodedAgain(t *testing.T) {
	b := NewBackend()
	b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: 1, PNG: onePixelPNG(t), Version: 1}, Cols: 1, Rows: 1})
	b.images.apply()
	first := b.images.placed[1].op

	var two bytes.Buffer
	if err := png.Encode(&two, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: 1, PNG: two.Bytes(), Version: 2}, Cols: 1, Rows: 1})
	b.PlaceImage(tui.ImagePlacement{Image: tui.Image{ID: 3, PNG: []byte("not a png"), Version: 1}, Cols: 1, Rows: 1})
	b.images.apply()
	if got := b.images.placed[1]; got.op == first || got.Version != 2 {
		t.Errorf("version 2 of image 1: version %d, decoded again %v; want 2, true", got.Version, got.op != first)
	}
	if list := b.images.list(); len(list) != 1 || list[0].ID != 1 {
		t.Errorf("shown %+v, want image 1 alone: the one that is not a PNG shows nothing", list)
	}
}

// A clipboard write waits on the bridge for the Gio goroutine's next frame, which takes it once.
func TestAClipboardWriteIsTakenOnce(t *testing.T) {
	var c clipboardBridge
	if c.take() != nil {
		t.Fatal("a write taken before any was put")
	}
	c.put("one")
	c.put("two") // the newer write replaces one not yet taken
	if s := c.take(); s == nil || *s != "two" {
		t.Fatalf("took %v, want the newest write, two", s)
	}
	if c.take() != nil {
		t.Error("a write was taken twice")
	}
}
