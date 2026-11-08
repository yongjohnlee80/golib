// Package cellimages loads and sizes the images a terminal draws over a widget's cells (kitty's
// graphics protocol): the HTML page's and the cell Editor's Rendered view's, by one loader.
//
// A src loads through the view's resolver (a data: URI without one), off the loop, under caps; a
// raster (PNG, JPEG, GIF) is answered as a PNG. Until it loads, and where it can't (an SVG or
// another format, over a cap, a src refused, a terminal without kitty graphics), the view shows the
// image's alt text.
package cellimages

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // the rasters images are decoded from
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

const (
	dataCap  = 8 << 20  // a data: URI's payload
	fileCap  = 32 << 20 // a resolved file, read through a limit
	sideCap  = 8192     // either side, read before decoding
	MaxRows  = 40       // a tall picture's most rows: it is scaled down to them
	pixelsW  = 8        // a cell's pixels when the backend doesn't know them (most terminals):
	pixelsH  = 16       // a cell twice as tall as it is wide, as terminal fonts are near enough
	NoID     = 0        // no placement id yet
	errorTag = "cellimages"
)

// ErrCap is an image over a size cap.
var ErrCap = errors.New(errorTag + ": image over a size cap")

// Resolver opens the file an image's src names (a view's ImageResolver).
type Resolver func(ctx context.Context, src string) (io.ReadCloser, error)

// Host is what loading and drawing images needs of a view: its context (the terminal's
// capabilities, its cell size, a task off the loop), its resolver, and where a failed load is told
// (a refusal the view reports).
type Host interface {
	Context() *tui.Context
	Resolve() Resolver
	Failed(src string, err error)
}

// State is where an image's load is.
type State uint8

const (
	Loading State = iota + 1
	Ready
	Failed
)

// Image is an image as loaded: its PNG, its size in pixels, its version (a reload's differs).
type Image struct {
	State   State
	PNG     []byte
	W, H    int
	Version uint64
}

// Loader is a view's images, by src, and the loads in flight.
type Loader struct {
	host    Host
	entries map[string]*Image
	tasks   map[tui.TaskID]string
}

var versions atomic.Uint64

// New is a loader for host's images.
func New(host Host) *Loader {
	return &Loader{host: host, entries: map[string]*Image{}, tasks: map[tui.TaskID]string{}}
}

// Drawn reports whether images can be drawn: the view is mounted on a terminal that verifiably
// draws kitty's graphics.
func (m *Loader) Drawn() bool {
	if m == nil || m.host == nil {
		return false
	}
	ctx := m.host.Context()
	return ctx != nil && ctx.Capabilities().KittyGraphics == tui.TriYes
}

// CellPixels is a cell's size in pixels: the backend's, or a terminal font's usual 8 × 16.
func (m *Loader) CellPixels() (w, h float64) {
	if ctx := m.host.Context(); ctx != nil {
		if cp := ctx.CellPixels(); cp.W > 0 && cp.H > 0 {
			return float64(cp.W), float64(cp.H)
		}
	}
	return pixelsW, pixelsH
}

// Entry is src's image, its load started on first sight.
func (m *Loader) Entry(src string) *Image {
	if e, ok := m.entries[src]; ok {
		return e
	}
	e := &Image{State: Loading}
	m.entries[src] = e
	ctx, resolve := m.host.Context(), m.host.Resolve()
	if ctx == nil || !strings.HasPrefix(src, "data:") && resolve == nil {
		e.State = Failed
		return e
	}
	id := ctx.Go(func(c context.Context) (any, error) { return load(c, src, resolve) })
	m.tasks[id] = src
	return e
}

// Tasks is the loads in flight, each one's src by its task (a copy).
func (m *Loader) Tasks() map[tui.TaskID]string {
	out := make(map[tui.TaskID]string, len(m.tasks))
	for id, src := range m.tasks {
		out[id] = src
	}
	return out
}

// Reset drops every image and every load in flight: another resolver reads a src as another file.
func (m *Loader) Reset() {
	m.entries = map[string]*Image{}
	m.tasks = map[tui.TaskID]string{}
}

// Done takes a load's result; it reports whether the result was an image's.
func (m *Loader) Done(r tui.TaskResult) bool {
	src, ok := m.tasks[r.ID]
	if !ok {
		return false
	}
	delete(m.tasks, r.ID)
	e := m.entries[src]
	if e == nil {
		return true // dropped by a reset
	}
	l, ok := r.Value.(loaded)
	if r.Err != nil || !ok {
		m.host.Failed(src, r.Err)
		e.State = Failed
		return true
	}
	e.PNG, e.W, e.H, e.Version, e.State = l.png, l.w, l.h, versions.Add(1), Ready
	return true
}

// loaded is a load's value: the image as a PNG, and its size.
type loaded struct {
	png  []byte
	w, h int
}

// load reads and decodes src off the loop, under the caps, and answers it as a PNG.
func load(ctx context.Context, src string, resolve Resolver) (any, error) {
	data, err := read(ctx, src, resolve)
	if err != nil {
		return nil, err
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err // an SVG or another format: its alt text
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > sideCap || cfg.Height > sideCap {
		return nil, ErrCap
	}
	if format == "png" {
		return loaded{data, cfg.Width, cfg.Height}, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return loaded{out.Bytes(), cfg.Width, cfg.Height}, nil
}

// read is src's bytes: a data: URI's payload, or what the resolver opens, under the caps.
func read(ctx context.Context, src string, resolve Resolver) ([]byte, error) {
	if rest, ok := strings.CutPrefix(src, "data:"); ok {
		meta, payload, ok := strings.Cut(rest, ",")
		if !ok || payload == "" {
			return nil, fmt.Errorf("%s: a data: URI without data", errorTag)
		}
		if strings.HasSuffix(meta, ";base64") {
			if base64.StdEncoding.DecodedLen(len(payload)) > dataCap {
				return nil, ErrCap
			}
			return base64.StdEncoding.DecodeString(payload)
		}
		s, err := url.PathUnescape(payload)
		if err != nil {
			return nil, err
		}
		if len(s) > dataCap {
			return nil, ErrCap
		}
		return []byte(s), nil
	}
	rc, err := resolve(ctx, src)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, fileCap+1))
	if err != nil {
		return nil, err
	}
	if len(data) > fileCap {
		return nil, ErrCap
	}
	return data, nil
}

// Picture is a laid image: blank cells Cols × Rows in Look, which the terminal draws the image
// over.
type Picture struct {
	Img        *Image
	ID         uint32
	Cols, Rows int
	Look       style.Style
}

func (p *Picture) Size() tui.Size { return tui.Size{W: p.Cols, H: p.Rows} }

// Paint reserves the picture's cells: blank, in its look, so no text shows through.
func (p *Picture) Paint(s tui.Surface, x0, y0 int) {
	s.Fill(tui.Rect{X: x0, Y: y0, W: p.Cols, H: p.Rows}, " ", p.Look)
}

// Placed is the picture as the App places it, over its cells at (x, y).
func (p *Picture) Placed(x, y int) tui.PlacedImage {
	return tui.PlacedImage{
		Image: tui.Image{ID: p.ID, PNG: p.Img.PNG, Version: p.Img.Version},
		At:    tui.Rect{X: x, Y: y, W: p.Cols, H: p.Rows},
	}
}

// Cells is a picture's cells for an image w × h pixels in a room cols wide, with cells cw × ch
// pixels: its own size where that fits, else the room's width, and at most MaxRows tall, keeping
// its shape.
func Cells(w, h, room int, cw, ch float64) (cols, rows int) {
	cols = max(min(room, int(math.Ceil(float64(w)/cw))), 1)
	rows = max(int(math.Round(float64(cols)*cw*float64(h)/float64(w)/ch)), 1)
	if rows > MaxRows {
		rows = MaxRows
		cols = max(min(cols, int(math.Round(float64(rows)*ch*float64(w)/float64(h)/cw))), 1)
	}
	return cols, rows
}
