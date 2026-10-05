package widget

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

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
	id         uint32
	png        []byte
	version    uint64
	scrollable bool
	view       imageView
	strips     []Strip // a PNG too large to place whole, cut; nil when png is placed whole
	strip      int     // the strip placed
}

// MaxImagePixels is the widest and tallest image a terminal takes: kitty's and Ghostty's limit
// (Ghostty: "max_dimension = 10000", an error the placement's q=2 keeps quiet).
const MaxImagePixels = 10000

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
	sub, ok := img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok {
		return Strips{}, fmt.Errorf("widget: a %T cannot be cut", img)
	}
	b0 := img.Bounds()
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	for top := 0; ; top += MaxImagePixels / 2 {
		top = min(top, cfg.Height-MaxImagePixels)
		var buf bytes.Buffer
		r := image.Rect(b0.Min.X, b0.Min.Y+top, b0.Max.X, b0.Min.Y+top+MaxImagePixels)
		if err := enc.Encode(&buf, sub.SubImage(r)); err != nil {
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
	if cfg, err := png.DecodeConfig(bytes.NewReader(b)); err == nil && m.scrollable && cfg.Height > MaxImagePixels {
		if s, err := SplitPNG(b); err == nil {
			m.SetStrips(s)
			return
		}
	}
	m.png, m.strips = b, nil
	m.view.width, m.view.height = 0, 0
	if cfg, err := png.DecodeConfig(bytes.NewReader(b)); err == nil {
		m.view.width, m.view.height = cfg.Width, cfg.Height
	}
	m.version++
	m.view.to(m.view.left, m.view.top)
	m.MarkDirty()
}

// SetStrips shows a PNG SplitPNG cut, as SetPNG shows a whole one: a scrollable Image places the
// strip the part it shows lies in. One strip is the PNG itself.
func (m *Image) SetStrips(s Strips) {
	if len(s.Cuts) == 0 {
		m.Clear()
		return
	}
	if len(s.Cuts) == 1 {
		m.strips = nil
		m.SetPNG(s.Cuts[0].PNG)
		return
	}
	m.png, m.strips, m.strip = s.Cuts[0].PNG, s.Cuts, 0
	m.view.width, m.view.height = s.Width, s.Height
	m.version++
	m.view.to(m.view.left, m.view.top)
	m.pickStrip()
	m.MarkDirty()
}

// pickStrip places the strip the part shown lies in, keeping the one placed while it still holds
// it: a scroll within a strip sends nothing. A part taller than the overlap is shown from the
// strip it starts in.
func (m *Image) pickStrip() {
	if len(m.strips) == 0 {
		return
	}
	v := m.view.shown()
	holds := func(i int) bool {
		top := m.strips[i].Top
		return v.Y >= top && v.Y+v.H <= top+MaxImagePixels
	}
	if holds(m.strip) {
		return
	}
	pick := -1
	for i := range m.strips {
		if holds(i) {
			pick = i
			break
		}
	}
	if pick < 0 { // taller than the overlap: the last strip starting at or above it
		pick = 0
		for i := range m.strips {
			if m.strips[i].Top <= v.Y {
				pick = i
			}
		}
	}
	if pick != m.strip {
		m.strip, m.png = pick, m.strips[pick].PNG
		m.version++
	}
}

// Clear shows no image.
func (m *Image) Clear() {
	m.png, m.strips = nil, nil
	m.version++
	m.MarkDirty()
}

// HasImage reports whether a PNG is set.
func (m *Image) HasImage() bool { return len(m.png) > 0 }

// SetScrollable sets whether the Image scrolls; see SCROLLABLE.
func (m *Image) SetScrollable(on bool) {
	m.scrollable = on
	m.view.to(0, 0)
	if on && len(m.strips) == 0 && m.view.height > MaxImagePixels {
		m.SetPNG(m.png) // too tall to place whole: cut it now it scrolls
	}
	m.MarkDirty()
}

// Scroll is the part of the PNG a scrollable Image shows, in its pixels, and the PNG's size.
func (m *Image) Scroll() (shown tui.Rect, width, height int) {
	return m.view.shown(), m.view.width, m.view.height
}

// ScrollTo puts the PNG's pixel x, y at the top left, as far as the PNG reaches.
func (m *Image) ScrollTo(x, y int) {
	m.view.to(x, y)
	m.pickStrip()
	m.MarkDirty()
}

// AcceptsFocus implements tui.Focusable: a scrollable Image takes the keys that scroll it.
func (m *Image) AcceptsFocus() bool { return m.scrollable }

// HandleEvent scrolls a scrollable Image; see SCROLLABLE.
func (m *Image) HandleEvent(ev tui.Event) bool {
	if !m.scrollable {
		return false
	}
	dx, dy, ok := scrollStep(ev, m.view.rows, m.view.height)
	if ok && m.view.to(m.view.left+dx, m.view.top+dy) {
		m.pickStrip()
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
func (m *Image) Cells() (cols, rows int) { return m.view.cols, m.view.rows }

// Render paints the cells blank: the image is placed over them.
func (m *Image) Render(s tui.Surface) {
	sz := s.Size()
	m.view.cols, m.view.rows = sz.W, sz.H
	m.view.to(m.view.left, m.view.top) // the cells may hold more now
	m.pickStrip()
	for y := 0; y < sz.H; y++ {
		for x := 0; x < sz.W; x++ {
			s.SetCell(x, y, " ", style.New())
		}
	}
}

// Image implements tui.ImageReporter.
func (m *Image) Image() (tui.Image, bool) {
	if len(m.png) == 0 {
		return tui.Image{}, false
	}
	img := tui.Image{ID: m.id, PNG: m.png, Version: m.version}
	if m.scrollable && m.view.cols > 0 && m.view.width > 0 {
		img.Clip = m.view.shown()
		if len(m.strips) > 0 { // the strip's own pixels: the whole's, less where it starts
			top := m.strips[m.strip].Top
			img.Clip.Y -= top
			img.Clip.H = min(img.Clip.H, MaxImagePixels-img.Clip.Y)
		}
	}
	return img, true
}

var _ tui.ImageReporter = (*Image)(nil)
