package gui

import (
	"image"
	"image/color"
	"testing"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/gpu/headless"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/op/clip"
	"github.com/yongjohnlee80/gio/op/paint"
)

// arcAtItsPlace is drawArc as it was: the path at the cell's absolute place, without an offset.
// The test holds it as the reference the offset path must match.
func arcAtItsPlace(ops *op.Ops, a [2]int, rect image.Rectangle, c color.NRGBA, lw int) {
	cx := float32(rect.Min.X) + float32(rect.Dx()/2) + 0.5*float32(lw%2)
	cy := float32(rect.Min.Y) + float32(rect.Dy()/2) + 0.5*float32(lw%2)
	rad := float32(min(rect.Dx(), rect.Dy())) / 2
	end := func(arm int) (edge, near f32.Point) {
		switch arm {
		case 0:
			return f32.Pt(cx, float32(rect.Min.Y)), f32.Pt(cx, cy-rad)
		case 1:
			return f32.Pt(float32(rect.Max.X), cy), f32.Pt(cx+rad, cy)
		case 2:
			return f32.Pt(cx, float32(rect.Max.Y)), f32.Pt(cx, cy+rad)
		default:
			return f32.Pt(float32(rect.Min.X), cy), f32.Pt(cx-rad, cy)
		}
	}
	e0, n0 := end(a[0])
	e1, n1 := end(a[1])
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(e0)
	p.LineTo(n0)
	p.QuadTo(f32.Pt(cx, cy), n1)
	p.LineTo(e1)
	st := clip.Stroke{Path: p.End(), Width: float32(lw)}.Op().Push(ops)
	paint.ColorOp{Color: c}.Add(ops)
	paint.PaintOp{}.Add(ops)
	st.Pop()
}

// TestAnArcDrawnAtItsCellsOriginLandsWhereItDid: the four rounded corners, at line widths 1 to 3,
// drawn at their cell's origin under a whole-pixel offset, ink exactly the pixels the path at the
// cell's absolute place inked. Rendered on gio's headless GPU.
func TestAnArcDrawnAtItsCellsOriginLandsWhereItDid(t *testing.T) {
	hw, err := headless.NewWindow(120, 60)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	shot := func(draw func(ops *op.Ops)) *image.RGBA {
		ops := new(op.Ops)
		paint.Fill(ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
		draw(ops)
		if err := hw.Frame(ops); err != nil {
			t.Fatal(err)
		}
		img := image.NewRGBA(image.Rect(0, 0, 120, 60))
		if err := hw.Screenshot(img); err != nil {
			t.Fatal(err)
		}
		return img
	}
	ink := color.NRGBA{R: 0x30, G: 0x60, B: 0x90, A: 0xff}
	cell := image.Rect(37, 13, 46, 31) // an odd place and an odd width
	for _, a := range [][2]int{{1, 2}, {2, 3}, {0, 1}, {0, 3}} {
		for _, lw := range []int{1, 2, 3} {
			offset := shot(func(ops *op.Ops) { drawArc(ops, a, cell, ink, lw) })
			placed := shot(func(ops *op.Ops) { arcAtItsPlace(ops, a, cell, ink, lw) })
			differ, inked := 0, 0
			for i := range offset.Pix {
				if offset.Pix[i] != placed.Pix[i] {
					differ++
				}
				if placed.Pix[i] != 0xff {
					inked++
				}
			}
			if inked == 0 {
				t.Fatalf("arc %v at lw %d drew nothing: the comparison proves nothing", a, lw)
			}
			if differ != 0 {
				t.Errorf("arc %v at lw %d: %d bytes differ from the path at the cell's place", a, lw, differ)
			}
		}
	}
}
