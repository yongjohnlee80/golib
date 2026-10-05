package widget

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Image shows a PNG over the cells it is given, on a terminal that draws images (kitty's graphics
// protocol; tui.Capabilities.KittyGraphics). It reserves its cells: it paints them blank, so no
// text beneath shows through, and the App places the image over them (tui.ImageReporter), places
// it again when the cells move or the PNG changes, and deletes it when the widget is hidden,
// unmounted or cleared. On a terminal without images the cells stay blank: a host that cannot be
// sure of graphics shows its fallback instead of an Image.
//
// The PNG is the host's to make (RasterizeSVG, RasterizeHTML, or its own); the terminal scales it
// to the cells, keeping nothing of its own aspect, so a host renders it at the cells' aspect.
//
// SCROLLABLE (golib's; Qt puts an Image in a Flickable): a scrollable Image shows its PNG at one
// cell to CellPixelsW × CellPixelsH of its pixels — the scale a host renders at — and as much of
// it as the cells hold, from a corner the user moves: ↓ j and ↑ k by a row, ← h and → l by four
// columns, Page Down ] Space and Page Up [ by the cells less a row, End G and Home g to the bottom
// and the top, and the wheel by three rows (or columns, sideways). Only the part shown is placed
// (tui.Image.Clip), and a scroll places it again without sending the PNG. A host renders a PNG to
// scroll at least as large as the cells at that scale, so it is never stretched.
//
// TALL (golib's): a terminal refuses an image wider or taller than MaxImagePixels, and says so only
// in a reply the placement asks it to keep to itself, so a page longer than that would show
// nothing. A scrollable Image shows such a PNG as Strips: MaxImagePixels-tall cuts of it,
// overlapping by half, so the part the cells show lies whole in one, and the one it lies in is the
// one placed. Scrolling within it places it again without sending it; scrolling into another
// sends that one. SplitPNG cuts them (a second or so for a long page, off the UI thread); SetPNG
// cuts a PNG too large itself, on the caller's thread.
type Image struct {
	Base
	id uint32
	st imageState
}

// imageState is what an Image shows, and every change to it: the Image's methods only hand it on
// and mark the Image dirty, so none of them rests on another (a type embedding an Image overrides
// any of them alone).
type imageState struct {
	given      []byte // the PNG as the host set it, before any cut or fit
	png        []byte
	version    uint64
	scrollable bool
	view       imageView
	strips     []Strip // a PNG too large to place whole, cut; nil when png is placed whole
	strip      int     // the strip placed
}

// setPNG shows b, never past MaxImagePixels: cut into strips when it scrolls and is too tall to
// place whole, and scaled down to fit when it cannot be cut (it does not scroll, or it is too
// wide). One that neither decodes nor encodes is no PNG a terminal shows either, and is left as it
// is.
func (s *imageState) setPNG(b []byte) {
	s.given = b
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err == nil && (cfg.Width > MaxImagePixels || (cfg.Height > MaxImagePixels && !s.scrollable)) {
		if small, w, h, err := fitPNG(b); err == nil {
			b, cfg.Width, cfg.Height = small, w, h
		}
	}
	if err == nil && s.scrollable && cfg.Height > MaxImagePixels {
		// a cut fails only where a fit would too (the data does not decode, or no PNG encodes)
		if cut, err := SplitPNG(b); err == nil {
			s.setStrips(cut)
			return
		}
	}
	s.png, s.strips = b, nil
	s.view.width, s.view.height = 0, 0
	if err == nil {
		s.view.width, s.view.height = cfg.Width, cfg.Height
	}
	s.version++
	s.view.to(s.view.left, s.view.top)
}

// setStrips shows a cut PNG; one strip is the PNG itself, none clears it.
func (s *imageState) setStrips(cut Strips) {
	switch len(cut.Cuts) {
	case 0:
		s.clear()
		return
	case 1:
		s.strips = nil
		s.setPNG(cut.Cuts[0].PNG)
		return
	}
	s.given, s.png, s.strips, s.strip = nil, cut.Cuts[0].PNG, cut.Cuts, 0
	s.view.width, s.view.height = cut.Width, cut.Height
	s.version++
	s.view.to(s.view.left, s.view.top)
	s.pick()
}

func (s *imageState) clear() {
	s.given, s.png, s.strips = nil, nil, nil
	s.version++
}

// moved re-picks the strip after the part shown moved.
func (s *imageState) moved() { s.pick() }

// pick places the strip the part shown lies in, keeping the one placed while it still holds it: a
// scroll within a strip sends nothing. A part taller than the overlap is shown from the strip it
// starts in.
func (s *imageState) pick() {
	if len(s.strips) == 0 {
		return
	}
	v := s.view.shown()
	holds := func(i int) bool {
		top := s.strips[i].Top
		return v.Y >= top && v.Y+v.H <= top+MaxImagePixels
	}
	if holds(s.strip) {
		return
	}
	pick := -1
	for i := range s.strips {
		if holds(i) {
			pick = i
			break
		}
	}
	if pick < 0 { // taller than the overlap: the last strip starting at or above it
		pick = 0
		for i := range s.strips {
			if s.strips[i].Top <= v.Y {
				pick = i
			}
		}
	}
	if pick != s.strip {
		s.strip, s.png = pick, s.strips[pick].PNG
		s.version++
	}
}

// setScrollable sets whether it scrolls. A PNG the host set whole is laid out again for the new
// mode, from the bytes as given: cut once it scrolls, fitted once it does not.
func (s *imageState) setScrollable(on bool) {
	s.scrollable = on
	s.view.to(0, 0)
	if s.given != nil {
		s.setPNG(s.given)
	}
}

// image is what the terminal is given: the PNG placed, and the part shown in its pixels.
func (s *imageState) image(id uint32) (tui.Image, bool) {
	if len(s.png) == 0 {
		return tui.Image{}, false
	}
	img := tui.Image{ID: id, PNG: s.png, Version: s.version}
	if s.scrollable && s.view.cols > 0 && s.view.width > 0 {
		img.Clip = s.view.shown()
		if len(s.strips) > 0 { // the strip's own pixels: the whole's, less where it starts
			top := s.strips[s.strip].Top
			img.Clip.Y -= top
			img.Clip.H = min(img.Clip.H, MaxImagePixels-img.Clip.Y)
		}
	}
	return img, true
}

// MaxImagePixels is the widest and tallest image a terminal takes: kitty's and Ghostty's limit
// (Ghostty: "max_dimension = 10000", an error the placement's q=2 keeps quiet).
const MaxImagePixels = 10000

// encodePNG writes a strip or a fitted PNG: fast rather than small, as the terminal reads it once.
// A variable, so a test can make it fail.
var encodePNG = func(w io.Writer, img image.Image) error {
	return (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(w, img)
}

// fitPNG scales b down, nearest pixel, until neither side is past MaxImagePixels, keeping its
// aspect: an image that cannot be cut into strips shows smaller rather than not at all. It
// returns the new PNG and its size.
func fitPNG(b []byte) ([]byte, int, int, error) {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("widget: the PNG to fit: %w", err)
	}
	src := img.Bounds()
	w, h := src.Dx(), src.Dy()
	if w > MaxImagePixels {
		w, h = MaxImagePixels, max(h*MaxImagePixels/w, 1)
	}
	if h > MaxImagePixels {
		w, h = max(w*MaxImagePixels/h, 1), MaxImagePixels
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		sy := src.Min.Y + y*src.Dy()/h
		for x := range w {
			out.Set(x, y, img.At(src.Min.X+x*src.Dx()/w, sy))
		}
	}
	var buf bytes.Buffer
	if err := encodePNG(&buf, out); err != nil {
		return nil, 0, 0, fmt.Errorf("widget: the fitted PNG: %w", err)
	}
	return buf.Bytes(), w, h, nil
}

// Strip is a horizontal cut of a PNG too tall for a terminal: its PNG, and its first row's place
// in the whole.
type Strip struct {
	PNG []byte
	Top int
}

// Strips are a PNG cut for a scrollable Image (SetStrips): the whole's size, and its cuts.
type Strips struct {
	Width, Height int
	Cuts          []Strip
}

// SplitPNG cuts b into Strips: MaxImagePixels tall, a new one every half of that, the last ending
// at the bottom, so any part up to half of MaxImagePixels tall lies whole in one. A PNG that fits
// is one strip, itself. It decodes and encodes the image, so a host calls it off the UI thread. A
// PNG wider than MaxImagePixels is refused: nothing cuts it to fit.
func SplitPNG(b []byte) (Strips, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return Strips{}, fmt.Errorf("widget: the PNG to split: %w", err)
	}
	if cfg.Width > MaxImagePixels {
		return Strips{}, fmt.Errorf("widget: a PNG %d pixels wide is wider than a terminal shows (%d)", cfg.Width, MaxImagePixels)
	}
	s := Strips{Width: cfg.Width, Height: cfg.Height}
	if cfg.Height <= MaxImagePixels {
		s.Cuts = []Strip{{PNG: b}}
		return s, nil
	}
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return Strips{}, fmt.Errorf("widget: the PNG to split: %w", err)
	}
	b0 := img.Bounds()
	for top := 0; ; top += MaxImagePixels / 2 {
		top = min(top, cfg.Height-MaxImagePixels)
		strip := image.NewNRGBA(image.Rect(0, 0, b0.Dx(), MaxImagePixels))
		draw.Draw(strip, strip.Bounds(), img, image.Pt(b0.Min.X, b0.Min.Y+top), draw.Src)
		var buf bytes.Buffer
		if err := encodePNG(&buf, strip); err != nil {
			return Strips{}, fmt.Errorf("widget: a strip of the PNG: %w", err)
		}
		s.Cuts = append(s.Cuts, Strip{PNG: buf.Bytes(), Top: top})
		if top+MaxImagePixels >= cfg.Height {
			return s, nil
		}
	}
}

// The pixels of a scrollable Image's PNG one cell shows: the size a host renders a PNG for.
const (
	CellPixelsW = 10
	CellPixelsH = 20
)

// imageView is the arithmetic of the part of a PNG an Image shows: its cells, the PNG's size, and
// the corner shown, kept within the PNG.
type imageView struct {
	cols, rows    int // the cells the last paint had
	width, height int // the PNG's, read from its header
	left, top     int
}

// shown is the part of the PNG the cells show, in its pixels.
func (v imageView) shown() tui.Rect {
	return tui.Rect{X: v.left, Y: v.top, W: min(v.cols*CellPixelsW, v.width), H: min(v.rows*CellPixelsH, v.height)}
}

// to moves the corner to x, y, as far as the PNG reaches, and reports whether it moved.
func (v *imageView) to(x, y int) bool {
	left := max(min(x, v.width-v.cols*CellPixelsW), 0)
	top := max(min(y, v.height-v.rows*CellPixelsH), 0)
	moved := left != v.left || top != v.top
	v.left, v.top = left, top
	return moved
}

// scrollStep is how far ev moves a scrollable Image of rows cells, in its PNG's pixels; ok is
// whether ev is one of its keys or the wheel.
func scrollStep(ev tui.Event, rows, height int) (dx, dy int, ok bool) {
	const col = 4 * CellPixelsW
	switch e := ev.(type) {
	case tui.KeyEvent:
		if e.Kind == tui.KeyRelease || e.Mods.Chord() != 0 {
			return 0, 0, false
		}
		page := max(rows-1, 1) * CellPixelsH
		switch e.Code {
		case tui.KeyDown, 'j':
			return 0, CellPixelsH, true
		case tui.KeyUp, 'k':
			return 0, -CellPixelsH, true
		case tui.KeyRight, 'l':
			return col, 0, true
		case tui.KeyLeft, 'h':
			return -col, 0, true
		case tui.KeyPageDown, ']', ' ':
			return 0, page, true
		case tui.KeyPageUp, '[':
			return 0, -page, true
		case tui.KeyEnd, 'G':
			return 0, height, true
		case tui.KeyHome, 'g':
			return 0, -height, true
		}
	case tui.MouseEvent:
		if e.Kind != tui.MouseWheel {
			return 0, 0, false
		}
		switch e.Button {
		case tui.WheelDown:
			return 0, 3 * CellPixelsH, true
		case tui.WheelUp:
			return 0, -3 * CellPixelsH, true
		case tui.WheelRight:
			return 3 * CellPixelsW, 0, true
		case tui.WheelLeft:
			return -3 * CellPixelsW, 0, true
		}
	}
	return 0, 0, false
}

// NewImage returns an empty Image.
func NewImage() *Image { return &Image{id: tui.NewImageID()} }

// SetPNG shows png; nil or empty clears the image. A scrollable Image keeps its corner, as far as
// the new PNG reaches. One taller than MaxImagePixels is cut here, on the caller's thread (see
// SplitPNG, and TALL); one that cannot be cut is shown as it is.
func (m *Image) SetPNG(b []byte) {
	m.st.setPNG(b)
	m.MarkDirty()
}

// SetStrips shows a PNG SplitPNG cut, as SetPNG shows a whole one: a scrollable Image places the
// strip the part it shows lies in. One strip is the PNG itself.
func (m *Image) SetStrips(s Strips) {
	m.st.setStrips(s)
	m.MarkDirty()
}

// Clear shows no image.
func (m *Image) Clear() {
	m.st.clear()
	m.MarkDirty()
}

// HasImage reports whether a PNG is set.
func (m *Image) HasImage() bool { return len(m.st.png) > 0 }

// SetScrollable sets whether the Image scrolls; see SCROLLABLE.
func (m *Image) SetScrollable(on bool) {
	m.st.setScrollable(on)
	m.MarkDirty()
}

// Scroll is the part of the PNG a scrollable Image shows, in its pixels, and the PNG's size.
func (m *Image) Scroll() (shown tui.Rect, width, height int) {
	return m.st.view.shown(), m.st.view.width, m.st.view.height
}

// ScrollTo puts the PNG's pixel x, y at the top left, as far as the PNG reaches.
func (m *Image) ScrollTo(x, y int) {
	m.st.view.to(x, y)
	m.st.moved()
	m.MarkDirty()
}

// AcceptsFocus implements tui.Focusable: a scrollable Image takes the keys that scroll it.
func (m *Image) AcceptsFocus() bool { return m.st.scrollable }

// HandleEvent scrolls a scrollable Image; see SCROLLABLE.
func (m *Image) HandleEvent(ev tui.Event) bool {
	if !m.st.scrollable {
		return false
	}
	v := &m.st.view
	dx, dy, ok := scrollStep(ev, v.rows, v.height)
	if ok && v.to(v.left+dx, v.top+dy) {
		m.st.moved()
		m.MarkDirty()
	}
	return ok
}

// Layout takes every cell offered.
func (m *Image) Layout(c tui.Constraints) tui.Size {
	w, h := c.MaxW, c.MaxH
	if w == tui.Unbounded {
		w = c.MinW
	}
	if h == tui.Unbounded {
		h = c.MinH
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

// Cells are the columns and rows the Image last painted: the size a host renders its PNG for, so
// the terminal does not stretch it. 0, 0 before its first paint.
func (m *Image) Cells() (cols, rows int) { return m.st.view.cols, m.st.view.rows }

// Render paints the cells blank: the image is placed over them.
func (m *Image) Render(s tui.Surface) {
	sz := s.Size()
	v := &m.st.view
	v.cols, v.rows = sz.W, sz.H
	v.to(v.left, v.top) // the cells may hold more now
	m.st.moved()
	for y := 0; y < sz.H; y++ {
		for x := 0; x < sz.W; x++ {
			s.SetCell(x, y, " ", style.New())
		}
	}
}

// Image implements tui.ImageReporter.
func (m *Image) Image() (tui.Image, bool) { return m.st.image(m.id) }

var _ tui.ImageReporter = (*Image)(nil)
