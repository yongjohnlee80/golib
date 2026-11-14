package gui

import (
	"fmt"
	"image"
	"image/color"
	"testing"

	"github.com/yongjohnlee80/gio/gpu/headless"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/paint"
)

// Each block element fills its share of the cell, solid, on gio's headless GPU. The cell is 16x32
// at (8, 8): eighths are 2 px across and 4 px down, so every share is exact.
func TestBlockElementsFillTheirShareOfTheCell(t *testing.T) {
	hw, err := headless.NewWindow(32, 48)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	cell := image.Rect(8, 8, 24, 40)
	white := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	shot := func(r rune) (*image.RGBA, bool) {
		ops := new(op.Ops)
		paint.Fill(ops, color.NRGBA{A: 0xff})
		ok := drawBlock(ops, r, cell, white)
		if err := hw.Frame(ops); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rect(0, 0, 32, 48))
		if err := hw.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img, ok
	}
	// lit is the bounding box of the pixels brighter than half, and how many there are.
	lit := func(img *image.RGBA) (image.Rectangle, int) {
		var box image.Rectangle
		n := 0
		for y := range 48 {
			for x := range 32 {
				if img.RGBAAt(x, y).R > 0x80 {
					box = box.Union(image.Rect(x, y, x+1, y+1))
					n++
				}
			}
		}
		return box, n
	}
	type want struct {
		r    rune
		rect image.Rectangle
	}
	cases := []want{
		{'▀', image.Rect(8, 8, 24, 24)},  // upper half
		{'▐', image.Rect(16, 8, 24, 40)}, // right half
		{'▔', image.Rect(8, 8, 24, 12)},  // upper eighth
		{'▕', image.Rect(22, 8, 24, 40)}, // right eighth
	}
	for n := 1; n <= 8; n++ { // lower n/8: U+2581..U+2588
		cases = append(cases, want{rune('▁' + n - 1), image.Rect(8, 40-4*n, 24, 40)})
	}
	for n := 7; n >= 1; n-- { // left n/8: U+2589 (7/8)..U+258F (1/8)
		cases = append(cases, want{rune('▉' + 7 - n), image.Rect(8, 8, 8+2*n, 40)})
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%U", c.r), func(t *testing.T) {
			img, ok := shot(c.r)
			if !ok {
				t.Fatalf("%q is not drawn as a block", c.r)
			}
			box, n := lit(img)
			if box != c.rect || n != c.rect.Dx()*c.rect.Dy() {
				t.Fatalf("%q lit %v (%d px); want %v solid (%d px)", c.r, box, n, c.rect, c.rect.Dx()*c.rect.Dy())
			}
		})
	}

	// The shades fill the whole cell, evenly, each darker than the next and than full.
	prev := uint8(0)
	for _, r := range []rune{'░', '▒', '▓'} {
		img, ok := shot(r)
		if !ok {
			t.Fatalf("%q is not drawn as a block", r)
		}
		lo, hi := uint8(0xff), uint8(0)
		for y := cell.Min.Y; y < cell.Max.Y; y++ {
			for x := cell.Min.X; x < cell.Max.X; x++ {
				v := img.RGBAAt(x, y).R
				lo, hi = min(lo, v), max(hi, v)
			}
		}
		if out := img.RGBAAt(4, 4).R; out != 0 {
			t.Errorf("%q drew outside its cell: (4,4) is %d", r, out)
		}
		if hi-lo > 2 || lo <= prev || hi == 0xff {
			t.Errorf("%q is %d..%d; want one level within 2, above the lighter shade's %d and below full", r, lo, hi, prev)
		}
		prev = hi
	}

	// A rune that is not a block element is left to the font: nothing drawn.
	if img, ok := shot('A'); ok {
		t.Fatal("'A' was drawn as a block")
	} else if box, _ := lit(img); !box.Empty() {
		t.Fatalf("'A' lit %v", box)
	}
}
