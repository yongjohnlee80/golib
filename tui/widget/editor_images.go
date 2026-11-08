package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/internal/cellimages"
)

// THE EDITOR'S IMAGES — an image-only paragraph in the Rendered view, drawn by the terminal over the
// rows the renderer reserves (kitty's graphics), loaded by the HTML page's loader
// (internal/cellimages): its alt text until it loads, and wherever it can't be drawn. The Editor
// reports each picture its last paint showed (tui.ImagesReporter), over its cells in the Editor; the
// App places it, crops one partly off the view, and removes one no longer reported. A buffer, a
// view switch or a renderer drops the images with the pictures (dropPictures).

// CellImageHost lays a Rendered block's image for a renderer that draws images: the Editor is one,
// handed to a renderer with a SetImageHost method when it is set (SetRenderer).
type CellImageHost interface {
	// Image is block b's picture of the image src within cols: ok false until it has loaded, or where
	// it can't be drawn; the renderer then lays the paragraph as text (its alt text).
	Image(b Block, src string, cols int) (p CellPicture, ok bool)
}

var (
	_ CellImageHost      = (*Editor)(nil)
	_ tui.ImagesReporter = (*Editor)(nil)
)

// SetImages is where the Rendered view's images are read: a src through r (a data: URI needs none).
// Another resolver reads a src as another file, so every image loads again.
func (e *Editor) SetImages(r ImageResolver) {
	e.resolve = r
	if e.imgs != nil {
		e.imgs.Reset()
	}
	e.picGen++
	e.MarkDirty()
}

// editorImages is the Editor as its images' host.
type editorImages struct{ e *Editor }

func (h editorImages) Context() *tui.Context { return h.e.Context() }

func (h editorImages) Resolve() cellimages.Resolver {
	if h.e.resolve != nil {
		return cellimages.Resolver(h.e.resolve)
	}
	return nil
}

func (h editorImages) Failed(string, error) {} // the paragraph stays its alt text

// Image implements CellImageHost.
func (e *Editor) Image(b Block, src string, cols int) (CellPicture, bool) {
	if e.imgs == nil {
		e.imgs = cellimages.New(editorImages{e})
	}
	if !e.imgs.Drawn() {
		return nil, false
	}
	img := e.imgs.Entry(src)
	if img.State != cellimages.Ready {
		return nil, false
	}
	cw, ch := e.imgs.CellPixels()
	c, r := cellimages.Cells(img.W, img.H, max(cols, 1), cw, ch)
	at := imageAt{from: b.From, src: src}
	id := e.imageIDs[at]
	if id == cellimages.NoID { // a layout laid again keeps each image's id: the terminal moves it, not sent again
		if e.imageIDs == nil {
			e.imageIDs = map[imageAt]uint32{}
		}
		id = tui.NewImageID()
		e.imageIDs[at] = id
	}
	return &cellimages.Picture{Img: img, ID: id, Cols: c, Rows: r, Look: e.cellTheme().Text}, true
}

// imageAt names a laid image: its block's first line and its src.
type imageAt struct {
	from int
	src  string
}

// imageDone takes an image's load, reporting whether it was one: the blocks are laid again with it
// (an arrival, as a picture's: the cursor revealed if it was on the view).
func (e *Editor) imageDone(r tui.TaskResult) bool {
	if e.imgs == nil || !e.imgs.Done(r) {
		return false
	}
	e.arrived()
	return true
}

// dropImages forgets the images laid and shown, and every load: another buffer, view or renderer.
func (e *Editor) dropImages() {
	if e.imgs != nil {
		e.imgs.Reset()
	}
	e.imageIDs, e.shownImages = nil, nil
}

// PlacedImages implements tui.ImagesReporter: the pictures the last paint showed, each over its
// cells in the Editor (one partly above the view starts above it, which the App crops).
func (e *Editor) PlacedImages(dst []tui.PlacedImage) []tui.PlacedImage {
	for _, p := range e.shownImages {
		dst = append(dst, p.pic.Placed(p.x, p.y))
	}
	return dst
}

// shownPicture is an image the last paint showed, its top left cell in the Editor.
type shownPicture struct {
	pic  *cellimages.Picture
	x, y int
}
