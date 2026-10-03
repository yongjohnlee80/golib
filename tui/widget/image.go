package widget

import (
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
type Image struct {
	Base
	id      uint32
	png     []byte
	version uint64
}

// NewImage returns an empty Image.
func NewImage() *Image { return &Image{id: tui.NewImageID()} }

// SetPNG shows png; nil or empty clears the image.
func (m *Image) SetPNG(png []byte) {
	m.png = png
	m.version++
	m.MarkDirty()
}

// Clear shows no image.
func (m *Image) Clear() { m.SetPNG(nil) }

// HasImage reports whether a PNG is set.
func (m *Image) HasImage() bool { return len(m.png) > 0 }

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

// Render paints the cells blank: the image is placed over them.
func (m *Image) Render(s tui.Surface) {
	sz := s.Size()
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
	return tui.Image{ID: m.id, PNG: m.png, Version: m.version}, true
}

var _ tui.ImageReporter = (*Image)(nil)
