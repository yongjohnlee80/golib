package widget

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // the rasters a page's images are decoded from
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// IMAGES IN CELLS — a paragraph of one <img> and nothing else (hblock.image), on a terminal that
// draws images (kitty's graphics protocol: tui.Capabilities.KittyGraphics), is the picture itself:
// rows of blank cells, the image's size at most the room's width (and cellImageRows tall), which
// the view reports the image over (tui.ImagesReporter) for the App to place. An image wider than
// the room is scaled down to it, never panned: a picture, unlike a diagram, reads whole at any size.
//
// It loads through the view's ImageResolver (a data: URI without one), off the loop, under caps; a
// raster (PNG, JPEG, GIF) is sent as a PNG. Until it loads, and where it can't (an SVG or another
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

const (
	cellImageData = 8 << 20  // a data: URI's payload
	cellImageFile = 32 << 20 // a resolved file, read through a limit
	cellImageSide = 8192     // either side, read before decoding
	cellImageRows = 40       // a tall picture's most rows: it is scaled down to them
	cellPixelsW   = 8        // a cell's pixels when the backend doesn't know them (most terminals):
	cellPixelsH   = 16       // a cell twice as tall as it is wide, as terminal fonts are near enough
	cellImageNoID = 0
)

var errCellImageCap = errors.New("htmlview: image over a size cap")

// cellImageHost is what the cell layout needs of its view to load and draw images: its context
// (the terminal's capabilities, its cell size, a task off the loop), its resolver, and where a
// refusal is told (HTMLView.Refused).
type cellImageHost interface {
	Context() *tui.Context
	Images() ImageResolver
	Refused(src string)
}

type cellImageState uint8

const (
	cellImageLoading cellImageState = iota + 1
	cellImageReady
	cellImageFailed
)

// cellImage is an image as loaded: its PNG, its size in pixels, its version (a reload's differs).
type cellImage struct {
	state   cellImageState
	png     []byte
	w, h    int
	version uint64
}

// cellImages are a page's images, by src, and the loads in flight.
type cellImages struct {
	host    cellImageHost
	entries map[string]*cellImage
	tasks   map[tui.TaskID]string
}

var cellImageVersions atomic.Uint64

func newCellImages(host cellImageHost) *cellImages {
	return &cellImages{host: host, entries: map[string]*cellImage{}, tasks: map[tui.TaskID]string{}}
}

// drawn reports whether images can be drawn here: the view is mounted on a terminal that verifiably
// draws kitty's graphics.
func (m *cellImages) drawn() bool {
	if m == nil || m.host == nil {
		return false
	}
	ctx := m.host.Context()
	return ctx != nil && ctx.Capabilities().KittyGraphics == tui.TriYes
}

// cellPixels is a cell's size in pixels: the backend's, or a terminal font's usual 8 × 16.
func (m *cellImages) cellPixels() (w, h float64) {
	if ctx := m.host.Context(); ctx != nil {
		if cp := ctx.CellPixels(); cp.W > 0 && cp.H > 0 {
			return float64(cp.W), float64(cp.H)
		}
	}
	return cellPixelsW, cellPixelsH
}

// entry is src's image, its load started on first sight.
func (m *cellImages) entry(src string) *cellImage {
	if e, ok := m.entries[src]; ok {
		return e
	}
	e := &cellImage{state: cellImageLoading}
	m.entries[src] = e
	ctx, resolve := m.host.Context(), m.host.Images()
	if ctx == nil || !strings.HasPrefix(src, "data:") && resolve == nil {
		e.state = cellImageFailed
		return e
	}
	id := ctx.Go(func(c context.Context) (any, error) { return loadCellImage(c, src, resolve) })
	m.tasks[id] = src
	return e
}

// reset drops every image and every load in flight: another resolver reads a src as another file.
func (m *cellImages) reset() {
	m.entries = map[string]*cellImage{}
	m.tasks = map[tui.TaskID]string{}
}

// done takes a load's result; it reports whether the result was an image's.
func (m *cellImages) done(r tui.TaskResult) bool {
	src, ok := m.tasks[r.ID]
	if !ok {
		return false
	}
	delete(m.tasks, r.ID)
	e := m.entries[src]
	if e == nil {
		return true // dropped by a reset
	}
	if errors.Is(r.Err, ErrImageRefused) {
		m.host.Refused(src)
	}
	l, ok := r.Value.(loadedCellImage)
	if r.Err != nil || !ok {
		e.state = cellImageFailed
		return true
	}
	e.png, e.w, e.h, e.version, e.state = l.png, l.w, l.h, cellImageVersions.Add(1), cellImageReady
	return true
}

// loadedCellImage is a load's value: the image as a PNG, and its size.
type loadedCellImage struct {
	png  []byte
	w, h int
}

// loadCellImage reads and decodes src off the loop, under the caps, and answers it as a PNG.
func loadCellImage(ctx context.Context, src string, resolve ImageResolver) (any, error) {
	data, err := readCellImage(ctx, src, resolve)
	if err != nil {
		return nil, err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err // an SVG or another format: its alt text
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > cellImageSide || cfg.Height > cellImageSide {
		return nil, errCellImageCap
	}
	if format == "png" {
		return loadedCellImage{data, cfg.Width, cfg.Height}, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return loadedCellImage{out.Bytes(), cfg.Width, cfg.Height}, nil
}

// readCellImage is src's bytes: a data: URI's payload, or what the resolver opens, under the caps.
func readCellImage(ctx context.Context, src string, resolve ImageResolver) ([]byte, error) {
	if rest, ok := strings.CutPrefix(src, "data:"); ok {
		meta, payload, ok := strings.Cut(rest, ",")
		if !ok || payload == "" {
			return nil, fmt.Errorf("htmlview: a data: URI without data")
		}
		if strings.HasSuffix(meta, ";base64") {
			if base64.StdEncoding.DecodedLen(len(payload)) > cellImageData {
				return nil, errCellImageCap
			}
			return base64.StdEncoding.DecodeString(payload)
		}
		s, err := url.PathUnescape(payload)
		if err != nil {
			return nil, err
		}
		if len(s) > cellImageData {
			return nil, errCellImageCap
		}
		return []byte(s), nil
	}
	rc, err := resolve(ctx, src)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, cellImageFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > cellImageFile {
		return nil, errCellImageCap
	}
	return data, nil
}

// imagePicture is a laid image: blank cells cols × rows, which the terminal draws the image over.
type imagePicture struct {
	e          *cellImage
	id         uint32
	cols, rows int
}

func (p *imagePicture) Size() tui.Size { return tui.Size{W: p.cols, H: p.rows} }

// Paint reserves the picture's cells: blank, in the page's colours, so no text shows through.
func (p *imagePicture) Paint(s tui.Surface, x0, y0 int) {
	s.Fill(tui.Rect{X: x0, Y: y0, W: p.cols, H: p.rows}, " ", style.PageText())
}

// imageCells is a picture's cells for an image w × h pixels in a room cols wide, with cells cw × ch
// pixels: its own size where that fits, else the room's width, and at most cellImageRows tall,
// keeping its shape.
func imageCells(w, h, room int, cw, ch float64) (cols, rows int) {
	cols = max(min(room, int(math.Ceil(float64(w)/cw))), 1)
	rows = max(int(math.Round(float64(cols)*cw*float64(h)/float64(w)/ch)), 1)
	if rows > cellImageRows {
		rows = cellImageRows
		cols = max(min(cols, int(math.Round(float64(rows)*ch*float64(w)/float64(h)/cw))), 1)
	}
	return cols, rows
}

// layImage lays block b out as its image when the terminal draws images and the image has loaded,
// and reports whether it did; otherwise b is laid as its alt text, and a load not yet started
// starts (its result lays the page out again).
func (c *cellHTML) layImage(bi int, b *hblock) bool {
	if b.image == nil || !c.imgs.drawn() {
		return false
	}
	e := c.imgs.entry(b.image.src)
	if e.state != cellImageReady {
		return false
	}
	cw, ch := c.imgs.cellPixels()
	cols, rows := imageCells(e.w, e.h, max(c.width-2*b.quote-b.indent, 1), cw, ch)
	key := cellImageKey{bi, b.image.src}
	id := c.imageIDs[key]
	if id == cellImageNoID {
		id = tui.NewImageID()
		c.imageIDs[key] = id
	}
	pic := &imagePicture{e: e, id: id, cols: cols, rows: rows}
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
		dst = append(dst, tui.PlacedImage{
			Image: tui.Image{ID: p.pic.id, PNG: p.pic.e.png, Version: p.pic.e.version},
			At:    tui.Rect{X: p.x, Y: p.y, W: p.pic.cols, H: p.pic.rows},
		})
	}
	return dst
}

var _ tui.ImagesReporter = (*HTMLView)(nil)

// shownImage is a picture the last paint showed, its top left cell in the view.
type shownImage struct {
	pic  *imagePicture
	x, y int
}

// HandleTask implements HTMLLayoutTasks: an image loaded, or failed.
func (c *cellHTML) HandleTask(r tui.TaskResult) bool {
	if c.imgs == nil || !c.imgs.done(r) {
		return false
	}
	c.layOut()
	return true
}

// ResetImages implements HTMLLayoutImages: another resolver, so every image loads again.
func (c *cellHTML) ResetImages() {
	if c.imgs != nil {
		c.imgs.reset()
		c.layOut()
	}
}

var (
	_ HTMLLayoutTasks  = (*cellHTML)(nil)
	_ HTMLLayoutImages = (*cellHTML)(nil)
)
