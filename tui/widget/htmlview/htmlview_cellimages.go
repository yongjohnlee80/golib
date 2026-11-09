package htmlview

import (
	"errors"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget/internal/cellimages"
)

// IMAGES IN CELLS — a paragraph of one <img> and nothing else (hblock.image), on a terminal that
// draws images (kitty's graphics protocol: tui.Capabilities.KittyGraphics), is the picture itself:
// rows of blank cells, the image's size at most the room's width (and cellImageRows tall), which
// the view reports the image over (tui.ImagesReporter) for the App to place. An image wider than
// the room is scaled down to it, never panned: a picture, unlike a diagram, reads whole at any size.
//
// It loads through the view's ImageResolver (a data: URI without one), off the loop, under caps
// (internal/cellimages, the loader the cell Editor's Rendered view shares); a raster is sent as a PNG. Until it loads, and where it can't (an SVG or another
// format, over a cap, a src refused, a terminal without kitty graphics, or a link to it that does
// not carry the protocol, which the terminal's probe then answers no to), the paragraph is its alt
// text, as it always was.
//
//	Some text before it.
//
//	┌──────────────┐  ← blank cells, reserved: the terminal draws the picture over them
//	│   (image)    │
//	└──────────────┘
//
//	Some text after it.

// pageImages is the page as its images' host: its context, its resolver, and a refusal told.
type pageImages struct{ v *HTMLView }

func (h pageImages) Context() *tui.Context { return h.v.Context() }

func (h pageImages) Resolve() cellimages.Resolver {
	if r := h.v.Images(); r != nil {
		return cellimages.Resolver(r)
	}
	return nil
}

func (h pageImages) Failed(src string, err error) {
	if errors.Is(err, ErrImageRefused) {
		h.v.Refused(src)
	}
}

// layImage lays block b out as its image when the terminal draws images and the image has loaded,
// and reports whether it did; otherwise b is laid as its alt text, and a load not yet started
// starts (its result lays the page out again).
func (c *cellHTML) layImage(bi int, b *hblock) bool {
	if b.image == nil || !c.imgs.Drawn() {
		return false
	}
	e := c.imgs.Entry(b.image.src)
	if e.State != cellimages.Ready {
		return false
	}
	cw, ch := c.imgs.CellPixels()
	cols, rows := cellimages.Cells(e.W, e.H, max(c.width-2*b.quote-b.indent, 1), cw, ch)
	key := cellImageKey{bi, b.image.src}
	id := c.imageIDs[key]
	if id == cellimages.NoID {
		id = tui.NewImageID()
		c.imageIDs[key] = id
	}
	pic := &cellimages.Picture{Img: e, ID: id, Cols: cols, Rows: rows, Look: style.PageText()}
	for i := range rows {
		c.rows = append(c.rows, hrow{block: bi, first: i == 0, grid: pic, gridRow: i})
	}
	return true
}

// cellImageKey names a laid image: its block and its src. A page laid out again keeps each one's
// id, so the terminal moves an image it has rather than being sent it again.
type cellImageKey struct {
	block int
	src   string
}

// PlacedImages implements tui.ImagesReporter for the cell layout: the pictures the last paint showed,
// each over its cells (one partly above the view's top starts above it, which the App crops).
func (v *HTMLView) PlacedImages(dst []tui.PlacedImage) []tui.PlacedImage {
	c, ok := v.layout.(*cellHTML)
	if !ok {
		return dst
	}
	for _, p := range c.shownImages {
		dst = append(dst, p.pic.Placed(p.x, p.y))
	}
	return dst
}

var _ tui.ImagesReporter = (*HTMLView)(nil)

// shownImage is a picture the last paint showed, its top left cell in the view.
type shownImage struct {
	pic  *cellimages.Picture
	x, y int
}

// HandleTask implements HTMLLayoutTasks: an image loaded, or failed.
func (c *cellHTML) HandleTask(r tui.TaskResult) bool {
	if c.imgs == nil || !c.imgs.Done(r) {
		return false
	}
	c.layOut()
	return true
}

// ResetImages implements HTMLLayoutImages: another resolver, so every image loads again.
func (c *cellHTML) ResetImages() {
	if c.imgs != nil {
		c.imgs.Reset()
		c.layOut()
	}
}

var (
	_ HTMLLayoutTasks  = (*cellHTML)(nil)
	_ HTMLLayoutImages = (*cellHTML)(nil)
)
