package widget

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// rowsPNG is a PNG w wide and h tall whose row y has its own colour (rowColour).
func rowsPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		c := rowColour(y)
		for x := range w {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func rowColour(y int) color.NRGBA {
	return color.NRGBA{R: uint8(y >> 16), G: uint8(y >> 8), B: uint8(y), A: 255}
}

// rowOf is the row a pixel of rowsPNG's colour came from.
func rowOf(c color.Color) int {
	r, g, b, _ := c.RGBA()
	return int(r>>8)<<16 | int(g>>8)<<8 | int(b>>8)
}

func decoded(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// SplitPNG cuts a PNG taller than a terminal shows into strips no taller than MaxImagePixels, a new
// one every half of it and the last at the bottom, each row the whole's at its place; a PNG that
// fits is itself; one too wide is refused.
func TestSplitPNGCutsATallPNG(t *testing.T) {
	const h = 25000
	s, err := SplitPNG(rowsPNG(t, 8, h))
	if err != nil {
		t.Fatal(err)
	}
	if s.Width != 8 || s.Height != h {
		t.Fatalf("size %dx%d", s.Width, s.Height)
	}
	var tops []int
	for _, c := range s.Cuts {
		tops = append(tops, c.Top)
		img := decoded(t, c.PNG)
		if img.Bounds().Dy() != MaxImagePixels {
			t.Errorf("the strip at %d is %d tall", c.Top, img.Bounds().Dy())
		}
		for _, r := range []int{0, 1, MaxImagePixels / 2, MaxImagePixels - 1} {
			if got := rowOf(img.At(0, r)); got != c.Top+r {
				t.Errorf("the strip at %d: its row %d is the whole's %d, want %d", c.Top, r, got, c.Top+r)
			}
		}
	}
	if want := []int{0, 5000, 10000, 15000}; !equalInts(tops, want) {
		t.Errorf("tops %v, want %v", tops, want)
	}

	small := rowsPNG(t, 8, 300)
	if s, err := SplitPNG(small); err != nil || len(s.Cuts) != 1 || !bytes.Equal(s.Cuts[0].PNG, small) {
		t.Errorf("a PNG that fits: %d cuts, %v", len(s.Cuts), err)
	}
	if _, err := SplitPNG(rowsPNG(t, MaxImagePixels+1, 2)); err == nil {
		t.Error("a PNG too wide to show was cut")
	}
	if _, err := SplitPNG([]byte("not a png")); err == nil {
		t.Error("not a PNG was cut")
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// placed is what a scrollable Image of cols×rows cells places: the PNG it sends, decoded, the part
// of it shown, and its version.
func placed(t *testing.T, m *Image) (image.Image, tui.Rect, uint64) {
	t.Helper()
	img, ok := m.Image()
	if !ok {
		t.Fatal("no image placed")
	}
	return decoded(t, img.PNG), img.Clip, img.Version
}

// A scrollable Image of a PNG taller than a terminal shows places a strip no taller than
// MaxImagePixels, its clip the part shown in the strip's own pixels: the rows shown are the whole's
// rows at the scroll. A scroll within the strip sends nothing new; one past it sends the next.
// SetPNG cuts such a PNG itself, and SetScrollable cuts one set before it scrolled.
func TestATallPNGIsPlacedAsAStrip(t *testing.T) {
	const h = 25000
	tall := rowsPNG(t, 8, h)
	for name, set := range map[string]func(m *Image){
		"SetStrips": func(m *Image) {
			s, err := SplitPNG(tall)
			if err != nil {
				t.Fatal(err)
			}
			m.SetScrollable(true)
			m.SetStrips(s)
		},
		"SetPNG":                  func(m *Image) { m.SetScrollable(true); m.SetPNG(tall) },
		"SetPNG, then scrollable": func(m *Image) { m.SetPNG(tall); m.SetScrollable(true) },
	} {
		m := NewImage()
		set(m)
		m.st.view.cols, m.st.view.rows = 4, 30 // as a paint leaves them: 600 pixels shown
		if _, w, hh := m.Scroll(); w != 8 || hh != h {
			t.Fatalf("%s: the whole is %dx%d", name, w, hh)
		}
		check := func(y int) uint64 {
			t.Helper()
			m.ScrollTo(0, y)
			img, clip, v := placed(t, m)
			if img.Bounds().Dy() > MaxImagePixels {
				t.Fatalf("%s at %d: a PNG %d tall was placed", name, y, img.Bounds().Dy())
			}
			if clip.Y < 0 || clip.Y+clip.H > img.Bounds().Dy() {
				t.Fatalf("%s at %d: the clip %+v is outside the strip", name, y, clip)
			}
			for _, r := range []int{0, clip.H - 1} {
				if got := rowOf(img.At(0, clip.Y+r)); got != y+r {
					t.Fatalf("%s at %d: row %d shown is the whole's %d, want %d", name, y, r, got, y+r)
				}
			}
			return v
		}
		v0 := check(0)
		if v := check(4000); v != v0 {
			t.Errorf("%s: a scroll within the strip sent it again", name)
		}
		v1 := check(12345)
		if v1 == v0 {
			t.Errorf("%s: a scroll into another strip sent nothing", name)
		}
		// back into rows the first strip holds too: the strip placed holds them, and stays
		if v := check(7000); v != v1 {
			t.Errorf("%s: a scroll the placed strip still holds sent another", name)
		}
		check(h - 600) // the bottom
		check(0)       // and back
	}
}

// A PNG that fits is placed whole, as before.
func TestAPNGThatFitsIsPlacedWhole(t *testing.T) {
	small := rowsPNG(t, 8, 1200)
	m := NewImage()
	m.SetScrollable(true)
	m.SetPNG(small)
	m.st.view.cols, m.st.view.rows = 4, 30
	m.ScrollTo(0, 300)
	img, ok := m.Image()
	if !ok || !bytes.Equal(img.PNG, small) || img.Clip.Y != 300 {
		t.Fatalf("placed %d bytes at clip %+v, want the PNG itself at 300", len(img.PNG), img.Clip)
	}
}

// An Image never places a PNG past MaxImagePixels. One it cannot cut (it does not scroll, or it is
// too wide) is scaled down to fit, keeping its aspect; one turned scrollable afterwards is cut from
// the PNG as given, not from the fitted one.
func TestAPNGThatCannotBeCutIsFitted(t *testing.T) {
	for name, tc := range map[string]struct {
		w, h       int
		scrollable bool
	}{
		"tall, not scrollable": {w: 40, h: 25000},
		"too wide, scrolling":  {w: 12000, h: 6, scrollable: true},
		"too wide, not":        {w: 12000, h: 6},
	} {
		m := NewImage()
		m.SetScrollable(tc.scrollable)
		m.SetPNG(rowsPNG(t, tc.w, tc.h))
		m.st.view.cols, m.st.view.rows = 4, 3
		img, ok := m.Image()
		if !ok {
			t.Fatalf("%s: nothing placed", name)
		}
		got := decoded(t, img.PNG).Bounds()
		if got.Dx() > MaxImagePixels || got.Dy() > MaxImagePixels {
			t.Errorf("%s: placed %dx%d, past the terminal's %d", name, got.Dx(), got.Dy(), MaxImagePixels)
		}
		want := float64(tc.w) / float64(tc.h)
		if r := float64(got.Dx()) / float64(got.Dy()); r < want*0.9 || r > want*1.1 {
			t.Errorf("%s: placed %dx%d, its aspect %.4f, want %.4f", name, got.Dx(), got.Dy(), r, want)
		}
	}
	m := NewImage()
	m.SetPNG(rowsPNG(t, 4, 25000)) // fitted while it does not scroll
	m.SetScrollable(true)          // then cut from the PNG as given
	if _, _, h := m.Scroll(); h != 25000 {
		t.Errorf("turned scrollable, the whole is %d tall, want the given 25000", h)
	}
}

// Strips of none clear the Image, and of one are the PNG itself, placed whole.
func TestStripsOfNoneOrOne(t *testing.T) {
	m := NewImage()
	m.SetScrollable(true)
	m.SetPNG(rowsPNG(t, 4, 100))
	m.SetStrips(Strips{})
	if m.HasImage() {
		t.Error("no strips left an image")
	}
	one := rowsPNG(t, 4, 300)
	m.SetStrips(Strips{Width: 4, Height: 300, Cuts: []Strip{{PNG: one}}})
	if img, ok := m.Image(); !ok || !bytes.Equal(img.PNG, one) {
		t.Error("one strip is not the PNG itself")
	}
}

// A part shown taller than the strips' overlap is shown from the last strip starting at or above
// it, its clip kept within the strip.
func TestAViewTallerThanTheOverlap(t *testing.T) {
	m := NewImage()
	m.SetScrollable(true)
	m.SetPNG(rowsPNG(t, 4, 25000))
	m.st.view.cols, m.st.view.rows = 4, 300 // 6000 pixels, past the overlap's 5000
	m.ScrollTo(0, 4500)                     // rows 4500 to 10500: whole in no strip (they start at 0 and 5000)
	img, clip, _ := placed(t, m)
	if clip.Y+clip.H > img.Bounds().Dy() {
		t.Fatalf("the clip %+v is outside the strip", clip)
	}
	if got := rowOf(img.At(0, clip.Y)); got != 4500 {
		t.Errorf("the first row shown is the whole's %d, want 4500 (from the strip at 0)", got)
	}
}

// A PNG whose header reads but whose data does not, and an encoder that fails, are errors, never a
// strip of garbage.
func TestSplitPNGFailsAsItsSteps(t *testing.T) {
	tall := rowsPNG(t, 4, 25000)
	broken := append([]byte(nil), tall[:len(tall)/2]...) // the header intact, the data cut short
	if _, err := SplitPNG(broken); err == nil {
		t.Error("a PNG cut short was split")
	}
	if _, _, _, err := fitPNG(broken); err == nil {
		t.Error("a PNG cut short was fitted")
	}
	failing := errors.New("disk full")
	old := encodePNG
	encodePNG = func(io.Writer, image.Image) error { return failing }
	defer func() { encodePNG = old }()
	if _, err := SplitPNG(tall); !errors.Is(err, failing) {
		t.Errorf("a strip whose encoding failed: %v", err)
	}
	if _, _, _, err := fitPNG(tall); !errors.Is(err, failing) {
		t.Errorf("a fit whose encoding failed: %v", err)
	}
	// neither cut nor fitted: shown as given, the only bytes there are
	m := NewImage()
	m.SetScrollable(true)
	m.SetPNG(tall)
	if img, ok := m.Image(); !ok || !bytes.Equal(img.PNG, tall) {
		t.Error("a PNG that could be neither cut nor fitted was not shown as given")
	}
}
