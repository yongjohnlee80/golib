package tui

import "sync/atomic"

// GRAPHICS — a raster image over a component's cells, on a terminal that draws images (kitty's
// graphics protocol; Capabilities.KittyGraphics).
//
// A component never writes to the terminal. It reports the image it wants shown (ImageReporter),
// and the App, after each frame's paint, compares what every VISIBLE reporter wants with what it
// placed last: a new or changed image, or one whose cells moved (a resize, a relayout), is placed
// again; one whose component was hidden, unmounted or reports none any more is deleted. Placements
// ride in the frame's one write, after its cells, through a backend that implements
// GraphicsBackend. On a backend that does not, nothing is placed, and the component's cells
// (whatever it paints there, a fallback) are what shows.
//
// Only the images this program placed are ever deleted: each placement is by its own id.

// Image is a raster image a component shows over its cells: PNG bytes, the id the component keeps
// for it (NewImageID), and a version that changes whenever the bytes do.
type Image struct {
	ID      uint32
	PNG     []byte
	Version uint64
}

// ImageReporter is a component that shows an Image over its whole rect. ok=false shows none.
// Consulted after each frame's paint, for a component that is visible.
type ImageReporter interface {
	Component
	Image() (img Image, ok bool)
}

// ImagePlacement is an image placed on the screen, over the cells X, Y to X+Cols, Y+Rows.
type ImagePlacement struct {
	Image
	X, Y, Cols, Rows int
}

// GraphicsBackend is a Backend that draws images. Both calls are latched: the next Flush writes
// them, in the order made, in the frame's one write.
type GraphicsBackend interface {
	PlaceImage(p ImagePlacement)
	DeleteImage(id uint32)
}

var imageIDs atomic.Uint32

// NewImageID is an image id no other in this process has.
func NewImageID() uint32 { return 1000 + imageIDs.Add(1) }

// Capabilities is the attached terminal's negotiated profile (Backend.Capabilities), so a host can
// choose what to show: an image where the terminal draws one, a fallback where it does not.
func (a *App) Capabilities() Capabilities { return a.backend.Capabilities() }

// noteImage records a visible reporter's image during the paint.
func (a *App) noteImage(n *node) {
	ir, ok := n.comp.(ImageReporter)
	if !ok {
		return
	}
	img, ok := ir.Image()
	if !ok || img.ID == 0 || len(img.PNG) == 0 {
		return
	}
	r := n.absRect.Intersect(Rect{W: a.size.W, H: a.size.H})
	if r.Empty() {
		return
	}
	a.frameImages = append(a.frameImages, ImagePlacement{Image: img, X: r.X, Y: r.Y, Cols: r.W, Rows: r.H})
}

// applyImages places what changed since the last frame and deletes what went.
func (a *App) applyImages() {
	g, ok := a.backend.(GraphicsBackend)
	want := a.frameImages
	a.frameImages = a.frameImages[:0]
	if !ok {
		return
	}
	if a.placed == nil {
		a.placed = map[uint32]ImagePlacement{}
	}
	seen := make(map[uint32]bool, len(want))
	for _, p := range want {
		seen[p.ID] = true
		old, had := a.placed[p.ID]
		if had && old.Version == p.Version && old.X == p.X && old.Y == p.Y && old.Cols == p.Cols && old.Rows == p.Rows {
			continue
		}
		g.PlaceImage(p)
		a.placed[p.ID] = p
	}
	for id := range a.placed {
		if !seen[id] {
			g.DeleteImage(id)
			delete(a.placed, id)
		}
	}
}
