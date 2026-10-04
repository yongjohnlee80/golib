package widget

import (
	"bytes"
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
type Image struct {
	Base
	id            uint32
	png           []byte
	version       uint64
	cols, rows    int // the cells the last paint had
	width, height int // the PNG's, read from its header
	scrollable    bool
	left, top     int // the corner of the PNG shown, when scrollable
}

// The pixels of a scrollable Image's PNG one cell shows: the size a host renders a PNG for.
const (
	CellPixelsW = 10
	CellPixelsH = 20
)

// NewImage returns an empty Image.
func NewImage() *Image { return &Image{id: tui.NewImageID()} }

// SetPNG shows png; nil or empty clears the image. A scrollable Image keeps its corner, as far as
// the new PNG reaches.
func (m *Image) SetPNG(b []byte) {
	m.png = b
	m.width, m.height = 0, 0
	if cfg, err := png.DecodeConfig(bytes.NewReader(b)); err == nil {
		m.width, m.height = cfg.Width, cfg.Height
	}
	m.version++
	m.scrollTo(m.left, m.top)
	m.MarkDirty()
}

// Clear shows no image.
func (m *Image) Clear() {
	m.png = nil
	m.version++
	m.MarkDirty()
}

// HasImage reports whether a PNG is set.
func (m *Image) HasImage() bool { return len(m.png) > 0 }

// SetScrollable sets whether the Image scrolls; see SCROLLABLE.
func (m *Image) SetScrollable(on bool) {
	m.scrollable = on
	m.scrollTo(0, 0)
	m.MarkDirty()
}

// Scroll is the part of the PNG a scrollable Image shows, in its pixels, and the PNG's size.
func (m *Image) Scroll() (shown tui.Rect, width, height int) {
	return tui.Rect{X: m.left, Y: m.top, W: min(m.cols*CellPixelsW, m.width), H: min(m.rows*CellPixelsH, m.height)}, m.width, m.height
}

// ScrollTo puts the PNG's pixel x, y at the top left, as far as the PNG reaches.
func (m *Image) ScrollTo(x, y int) {
	m.scrollTo(x, y)
	m.MarkDirty()
}

func (m *Image) scrollTo(x, y int) {
	m.left = max(min(x, m.width-m.cols*CellPixelsW), 0)
	m.top = max(min(y, m.height-m.rows*CellPixelsH), 0)
}

// scrollBy moves the corner by dx, dy and reports whether it is a scrollable Image's to handle.
func (m *Image) scrollBy(dx, dy int) bool {
	if !m.scrollable {
		return false
	}
	left, top := m.left, m.top
	if m.scrollTo(left+dx, top+dy); m.left != left || m.top != top {
		m.MarkDirty()
	}
	return true
}

// AcceptsFocus implements tui.Focusable: a scrollable Image takes the keys that scroll it.
func (m *Image) AcceptsFocus() bool { return m.scrollable }

// HandleEvent scrolls a scrollable Image; see SCROLLABLE.
func (m *Image) HandleEvent(ev tui.Event) bool {
	const col = 4 * CellPixelsW
	switch e := ev.(type) {
	case tui.KeyEvent:
		if e.Kind == tui.KeyRelease || e.Mods.Chord() != 0 {
			return false
		}
		page := max(m.rows-1, 1) * CellPixelsH
		switch e.Code {
		case tui.KeyDown, 'j':
			return m.scrollBy(0, CellPixelsH)
		case tui.KeyUp, 'k':
			return m.scrollBy(0, -CellPixelsH)
		case tui.KeyRight, 'l':
			return m.scrollBy(col, 0)
		case tui.KeyLeft, 'h':
			return m.scrollBy(-col, 0)
		case tui.KeyPageDown, ']', ' ':
			return m.scrollBy(0, page)
		case tui.KeyPageUp, '[':
			return m.scrollBy(0, -page)
		case tui.KeyEnd, 'G':
			return m.scrollBy(0, m.height)
		case tui.KeyHome, 'g':
			return m.scrollBy(0, -m.height)
		}
	case tui.MouseEvent:
		if e.Kind != tui.MouseWheel {
			return false
		}
		switch e.Button {
		case tui.WheelDown:
			return m.scrollBy(0, 3*CellPixelsH)
		case tui.WheelUp:
			return m.scrollBy(0, -3*CellPixelsH)
		case tui.WheelRight:
			return m.scrollBy(3*CellPixelsW, 0)
		case tui.WheelLeft:
			return m.scrollBy(-3*CellPixelsW, 0)
		}
	}
	return false
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
func (m *Image) Cells() (cols, rows int) { return m.cols, m.rows }

// Render paints the cells blank: the image is placed over them.
func (m *Image) Render(s tui.Surface) {
	sz := s.Size()
	m.cols, m.rows = sz.W, sz.H
	m.scrollTo(m.left, m.top) // the cells may hold more now
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
	if m.scrollable && m.cols > 0 && m.width > 0 {
		img.Clip, _, _ = m.Scroll()
	}
	return img, true
}

var _ tui.ImageReporter = (*Image)(nil)
