package gui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/yongjohnlee80/gio/f32"
	"github.com/yongjohnlee80/gio/font/gofont"
	"github.com/yongjohnlee80/gio/gpu/headless"
	"github.com/yongjohnlee80/gio/op"
	"github.com/yongjohnlee80/gio/text"
	"github.com/yongjohnlee80/golib/tui"
)

// goldenUpdate rewrites the goldens from this machine's drawing instead of comparing with them.
// Goldens are Linux's (Mesa's llvmpipe, as the gate runs it); another platform's GPU rounds edges
// its own way, and the platform matrix checks it there.
var goldenUpdate = os.Getenv("GOLIB_GUI_GOLDEN_UPDATE") == "1"

// goldenScenes are the Canvas primitives, each drawn on white in a 96x64 dp view.
var goldenScenes = []struct {
	name string
	draw func(c Canvas)
}{
	{"fill-rect", func(c Canvas) { c.FillRect(Rect{X: 10, Y: 10, W: 40, H: 20}, Solid(color.NRGBA{R: 0xc0, A: 0xff})) }},
	{"fill-rrect", func(c Canvas) {
		c.FillRRect(Rect{X: 8, Y: 8, W: 60, H: 40}, 10, Solid(color.NRGBA{B: 0xc0, A: 0xff}))
	}},
	{"stroke-rrect", func(c Canvas) {
		c.StrokeRRect(Rect{X: 8, Y: 8, W: 60, H: 40}, 8, 2, Solid(color.NRGBA{G: 0x80, A: 0xff}))
	}},
	{"ellipse", func(c Canvas) { c.FillEllipse(Rect{X: 20, Y: 6, W: 56, H: 40}, Solid(color.NRGBA{G: 0xa0, A: 0xff})) }},
	{"path-fill", func(c Canvas) {
		p := new(Path).MoveTo(Pt(10, 54)).LineTo(Pt(48, 6)).LineTo(Pt(86, 54)).Close()
		c.FillPath(p, Solid(color.NRGBA{R: 0x80, B: 0x80, A: 0xff}))
	}},
	{"path-stroke", func(c Canvas) {
		p := new(Path).MoveTo(Pt(6, 50)).CubeTo(Pt(30, -10), Pt(60, 74), Pt(90, 14))
		c.StrokePath(p, 3, Solid(color.NRGBA{A: 0xff}))
	}},
	{"gradient", func(c Canvas) {
		c.FillRect(Rect{X: 8, Y: 8, W: 80, H: 48}, Brush{Gradient: &LinearGradient{
			From: Pt(8, 0), To: Pt(88, 0), FromColor: color.NRGBA{R: 0xff, A: 0xff}, ToColor: color.NRGBA{B: 0xff, A: 0xff},
		}})
	}},
	{"opacity", func(c Canvas) {
		c.PushOpacity(0.5)
		c.FillRect(Rect{X: 10, Y: 10, W: 76, H: 44}, Solid(color.NRGBA{A: 0xff}))
		c.Pop()
	}},
	{"transform", func(c Canvas) {
		c.PushTransform(Affine{}.Rotate(f32.Pt(48, 32), math.Pi/6))
		c.FillRect(Rect{X: 28, Y: 22, W: 40, H: 20}, Solid(color.NRGBA{R: 0xc0, G: 0x60, A: 0xff}))
		c.Pop()
	}},
	{"image", func(c Canvas) {
		img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		for y := range 4 {
			for x := range 4 {
				if (x+y)%2 == 0 {
					img.SetNRGBA(x, y, color.NRGBA{A: 0xff})
				} else {
					img.SetNRGBA(x, y, color.NRGBA{R: 0xff, G: 0xff, A: 0xff})
				}
			}
		}
		c.DrawImage(NewImage(img), Rect{X: 16, Y: 8, W: 48, H: 48}, Rect{W: 4, H: 4})
	}},
	{"text", func(c Canvas) {
		c.DrawText(c.Text().Layout("Agj 0O", Font{Family: CellFamily, Size: 16}, 0), Pt(6, 20), Solid(color.NRGBA{A: 0xff}))
	}},
}

// goldenShot draws scene into a 96x64 dp view at scale s, through the window's own path (a native
// view placed over the grid), on hw, gio's headless GPU window of 96s x 64s pixels.
func goldenShot(t *testing.T, hw *headless.Window, draw func(Canvas), s int) *image.RGBA {
	t.Helper()
	w, h := 96*s, 64*s
	m := metrics{window: image.Pt(w, h), cell: image.Pt(8*s, 16*s), ppem: float32(14 * s), baseline: 12 * s, scale: float32(s), grid: tui.Size{W: 12, H: 4}}
	b := NewBackend()
	b.render = newRenderer(text.NewShaper(text.WithCollection(gofont.Collection()), text.NoSystemFonts()), "Go Mono", DefaultTheme())
	view := ViewFunc(func(c Canvas) {
		c.FillRect(Rect{W: 96, H: 64}, Solid(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}))
		draw(c)
	})
	g := &grid{}
	g.resize(12, 4)
	b.render.m = m
	call := b.render.frame(g, m, cursorState{}, []tui.NativePlacement{{View: view, Cols: 12, Rows: 4}}, nil, true)
	ops := new(op.Ops)
	call.Add(ops)
	if err := hw.Frame(ops); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	if err := hw.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	return img
}

// Each Canvas primitive, at scale 1 and 2, draws as its committed golden does: no byte more than
// pixelTolerance apart and the same pixels inked (compareShots). Each scene is drawn twice first:
// the two must be the same, or no golden means anything.
func TestCanvasPrimitivesMatchTheirGoldens(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the goldens are Linux's (llvmpipe); the platform matrix checks other GPUs")
	}
	// One window a scale, for every scene: EGL refuses some of many windows made in a row
	// (eglInitialize 0x3001), and a refused one would skip its scene unseen.
	for _, s := range []int{1, 2} {
		hw, err := headless.NewWindow(96*s, 64*s)
		if err != nil {
			t.Skipf("no headless GPU here: %v", err)
		}
		defer hw.Release()
		for _, sc := range goldenScenes {
			name := fmt.Sprintf("%s@%dx", sc.name, s)
			t.Run(name, func(t *testing.T) {
				got := goldenShot(t, hw, sc.draw, s)
				again := goldenShot(t, hw, sc.draw, s)
				if !bytes.Equal(got.Pix, again.Pix) {
					t.Fatalf("%s drawn twice differs: the drawing isn't deterministic", name)
				}
				path := filepath.Join("testdata", "golden", name+".png")
				if goldenUpdate {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					var buf bytes.Buffer
					if err := png.Encode(&buf, got); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				f, err := os.Open(path)
				if err != nil {
					t.Fatalf("no golden: %v (GOLIB_GUI_GOLDEN_UPDATE=1 writes them)", err)
				}
				defer f.Close()
				dec, err := png.Decode(f)
				if err != nil {
					t.Fatal(err)
				}
				want := image.NewRGBA(dec.Bounds())
				for y := dec.Bounds().Min.Y; y < dec.Bounds().Max.Y; y++ {
					for x := dec.Bounds().Min.X; x < dec.Bounds().Max.X; x++ {
						want.Set(x, y, dec.At(x, y))
					}
				}
				if want.Bounds() != got.Bounds() {
					t.Fatalf("golden is %v, the drawing %v", want.Bounds(), got.Bounds())
				}
				compareShots(t, name, got, want, want)
			})
		}
	}
}
