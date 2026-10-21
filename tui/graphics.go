package tui

import (
	"bytes"
	"image"
	_ "image/png" // DecodeConfig reads a PNG's size, to crop an image partly hidden
	"slices"
	"sync/atomic"
)

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

// imagePlacements is the images visible reporters asked for in this frame's paint (frameImages), and
// what the backend was last told to show, by id (placed).
type imagePlacements struct {
	frameImages []ImagePlacement
	placed      map[uint32]ImagePlacement
}

// Image is a raster image a component shows over its cells: PNG bytes, the id the component keeps
// for it (NewImageID), and a version that changes whenever the bytes do. Clip is the part of the
// PNG shown, in its pixels (Qt's Image.sourceClipRect): empty, the whole of it. A change of Clip
// alone places the image again without sending its bytes, so scrolling a large image is cheap.
type Image struct {
	ID      uint32
	PNG     []byte
	Version uint64
	Clip    Rect
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
	// Layer and Covered are set for a backend that is also a NativeHost, which composites images
	// and native views in one paint order: Layer is the reporter's paint order this frame, as a
	// NativePlacement's, and Covered the cells in it written after it, which are drawn over the
	// image. Zero and nil on any other backend.
	Layer   uint32
	Covered []Rect
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
	r := n.visRect // the part that shows: the screen and every ancestor cut it
	if r.Empty() {
		return // not placed: applyImages deletes it, and places it again when it shows again
	}
	if r != n.absRect {
		// partly hidden (scrolled out of a ScrollView, or past the screen's edge): show the same
		// part of the picture, cropped through its source rect
		src := img.Clip
		if src.Empty() {
			cfg, _, err := image.DecodeConfig(bytes.NewReader(img.PNG))
			if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
				return // no size, so no crop: not placed while it is cut
			}
			src = Rect{W: cfg.Width, H: cfg.Height}
		}
		img.Clip = cropTo(src, n.absRect, r) // never empty: r and src are not, and it rounds outward
	}
	p := ImagePlacement{Image: img, X: r.X, Y: r.Y, Cols: r.W, Rows: r.H}
	if a.buf.stamping {
		p.Layer = a.buf.order // the node's own paint order: noteImage runs before its children
	}
	a.frameImages = append(a.frameImages, p)
}

// applyImages places what changed since the last frame and deletes what went.
func (a *App) applyImages() {
	g := a.caps.graphics
	want := a.frameImages
	a.frameImages = a.frameImages[:0]
	if g == nil {
		return
	}
	if a.placed == nil {
		a.placed = map[uint32]ImagePlacement{}
	}
	seen := make(map[uint32]bool, len(want))
	for _, p := range want {
		seen[p.ID] = true
		if a.buf.stamping {
			p.Covered = a.coveredAfter(Rect{X: p.X, Y: p.Y, W: p.Cols, H: p.Rows}, p.Layer)
		}
		old, had := a.placed[p.ID]
		if had && old.Version == p.Version && old.Clip == p.Clip && old.X == p.X && old.Y == p.Y && old.Cols == p.Cols && old.Rows == p.Rows &&
			old.Layer == p.Layer && slices.Equal(old.Covered, p.Covered) {
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

// cropTo is the part of src (pixels) that shows when whole, a node's cells, is cut to vis: the same
// fractions of its width and height come off each side, rounded outward to whole pixels. A visible
// cell always keeps the pixels under it, so an image scaled up over many cells (one pixel over a
// whole pane) is never cropped to nothing; a pixel a cut runs through is shown whole, a little
// stretched, rather than dropped.
func cropTo(src, whole, vis Rect) Rect {
	if whole.W <= 0 || whole.H <= 0 || src.Empty() || vis.Empty() {
		return Rect{}
	}
	x0, x1 := cropSpan(src.X, src.W, vis.X-whole.X, vis.X+vis.W-whole.X, whole.W)
	y0, y1 := cropSpan(src.Y, src.H, vis.Y-whole.Y, vis.Y+vis.H-whole.Y, whole.H)
	return Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// cropSpan is the source pixels, from off, of size pixels laid over n cells, under the cells a to b:
// the first one cell a touches, through the last one cell b-1 does. Non-empty whenever a < b.
func cropSpan(off, size, a, b, n int) (lo, hi int) {
	return off + size*a/n, off + (size*b+n-1)/n
}
