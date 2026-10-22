package picture_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/internal/picture"
)

// A picture wider than the width asked is scaled down to it, its height with it; one that fits, or
// a width that asks nothing, keeps its size at a scale of 1.
func TestFitToScalesOnlyAWiderPicture(t *testing.T) {
	for _, tc := range []struct {
		width float32
		want  gui.Size
		scale float32
	}{
		{100, gui.Size{W: 100, H: 50}, 0.5},
		{300, gui.Size{W: 200, H: 100}, 1},
		{0, gui.Size{W: 200, H: 100}, 1},
	} {
		got, s := picture.FitTo(gui.Size{W: 200, H: 100}, tc.width)
		if got != tc.want || s != tc.scale {
			t.Errorf("width %v: %v at %v, want %v at %v", tc.width, got, s, tc.want, tc.scale)
		}
	}
}

// Push scales the canvas, and its pop undoes it; at a scale of 1 it pushes nothing.
func TestPushScalesOnlyWhenTheFitDoes(t *testing.T) {
	c := gui.NewRecordingCanvas(gui.Size{W: 100, H: 100}, gui.Size{W: 8, H: 16})
	picture.Push(c, 1)()
	if ops := c.Ops(); len(ops) != 0 {
		t.Fatalf("a scale of 1 drew %v", ops)
	}
	picture.Push(c, 0.5)()
	if got := strings.Join(c.Ops(), " "); got != "PushTransform Pop" {
		t.Fatalf("a scale of 0.5: %q, want PushTransform Pop", got)
	}
}

// A label is shaped once: asked for again in the same font, it is the same layout; another font is
// another.
func TestALabelIsShapedOnce(t *testing.T) {
	c := gui.NewRecordingCanvas(gui.Size{W: 100, H: 100}, gui.Size{W: 8, H: 16})
	var l picture.Labels
	f := gui.Font{Size: 12}
	a := l.Shape(c, "label", f)
	if b := l.Shape(c, "label", f); b != a {
		t.Fatal("the same label in the same font was shaped twice")
	}
	if b := l.Shape(c, "label", gui.Font{Size: 14}); b == a {
		t.Fatal("another font gave the same layout")
	}
}
