package widget

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // the formats a page's images are decoded from
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	"github.com/yongjohnlee80/golib/gui/svg"
	phtml "github.com/yongjohnlee80/golib/parse/html"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// IMAGES — a page's <img>s, loaded only through the view's ImageResolver (data: URIs aside), and
// decoded off the loop: a raster (PNG, JPEG, GIF) as an image, an SVG as a native drawing
// (gui/svg). Until an image is decoded its box is its width and height attributes, or nothing; one
// that fails or is over a cap shows its alt text, and one the resolver refused is reported to the
// view (HTMLView.Refused).
const (
	maxImageData   = 8 << 20  // a data: URI's payload
	maxImageFile   = 32 << 20 // a resolved file, read through a limit
	maxImageSide   = 8192     // either side, read before decoding
	maxImagePixels = 40_000_000
)

var errImageCap = errors.New("htmlview: image over a size cap")

type imgState uint8

const (
	imgLoading imgState = iota + 1
	imgReady
	imgFailed
)

type imgEntry struct {
	state imgState
	img   *gui.Image   // a raster's
	svg   *svg.Drawing // an SVG's
	w, h  int
}

type htmlImages struct {
	l       *htmlLayout
	entries map[string]*imgEntry
	tasks   map[tui.TaskID]string
	stale   map[tui.TaskID]struct{} // loads started before the last reset: their results are dropped
}

func newHTMLImages(l *htmlLayout) *htmlImages {
	return &htmlImages{l: l, entries: map[string]*imgEntry{}, tasks: map[tui.TaskID]string{},
		stale: map[tui.TaskID]struct{}{}}
}

// reset forgets every image and every load on its way: a new resolver reads a page's relative src
// as another file, so neither an entry nor a load still in flight under the old one may be kept.
func (m *htmlImages) reset() {
	for id := range m.tasks {
		m.stale[id] = struct{}{}
	}
	m.entries, m.tasks = map[string]*imgEntry{}, map[tui.TaskID]string{}
}

// gen is an image's state as a number, for its block's key: a decode that lands changes it.
func (m *htmlImages) gen(src string) int {
	if e, ok := m.entries[src]; ok {
		return int(e.state)
	}
	return 0
}

// entry is src's image, its load started on first sight.
func (m *htmlImages) entry(src string) *imgEntry {
	if e, ok := m.entries[src]; ok {
		return e
	}
	e := &imgEntry{state: imgLoading}
	m.entries[src] = e
	ctx := m.l.v.Context()
	resolve := m.l.v.Images()
	if ctx == nil || !strings.HasPrefix(src, "data:") && resolve == nil {
		e.state = imgFailed
		return e
	}
	id := ctx.Go(func(c context.Context) (any, error) { return loadImage(c, src, resolve) })
	m.tasks[id] = src
	return e
}

// done takes a decode's result; it reports whether the result was an image's.
func (m *htmlImages) done(r tui.TaskResult) bool {
	if _, ok := m.stale[r.ID]; ok {
		delete(m.stale, r.ID)
		return true // the old resolver's: dropped
	}
	src, ok := m.tasks[r.ID]
	if !ok {
		return false
	}
	delete(m.tasks, r.ID)
	e := m.entries[src]
	if errors.Is(r.Err, tuiwidget.ErrImageRefused) {
		m.l.v.Refused(src)
	}
	if e == nil || r.Err != nil {
		if e != nil {
			e.state = imgFailed
		}
		return true
	}
	switch v := r.Value.(type) {
	case image.Image:
		e.img = gui.NewImage(v)
		e.w, e.h = v.Bounds().Dx(), v.Bounds().Dy()
	case *svg.Drawing:
		e.svg = v
		sz := v.Size()
		e.w, e.h = max(int(sz.W+0.5), 1), max(int(sz.H+0.5), 1)
	default:
		e.state = imgFailed
		return true
	}
	e.state = imgReady
	return true
}

// sniffSVG reports whether data starts as an SVG document does: an XML declaration, a comment or
// a doctype, then <svg; for an SVG served under a name that does not say so.
func sniffSVG(data []byte) bool {
	head := strings.ToLower(string(data[:min(len(data), 1024)]))
	i := strings.Index(head, "<svg")
	if i < 0 {
		return false
	}
	pre := strings.TrimSpace(head[:i])
	return pre == "" || strings.HasPrefix(pre, "<?xml") || strings.HasPrefix(pre, "<!--") || strings.HasPrefix(pre, "<!doctype")
}

func isSVG(src string) bool {
	s := strings.ToLower(src)
	if strings.HasPrefix(s, "data:image/svg") {
		return true
	}
	if u, err := url.Parse(s); err == nil {
		return strings.HasSuffix(u.Path, ".svg")
	}
	return false
}

// loadImage reads and decodes src off the loop, under the caps.
func loadImage(ctx context.Context, src string, resolve func(context.Context, string) (io.ReadCloser, error)) (any, error) {
	var data []byte
	if strings.HasPrefix(src, "data:") {
		meta, payload, ok := strings.Cut(src[len("data:"):], ",")
		if !ok {
			return nil, fmt.Errorf("htmlview: a data: URI without data")
		}
		if strings.HasSuffix(meta, ";base64") {
			if base64.StdEncoding.DecodedLen(len(payload)) > maxImageData {
				return nil, errImageCap
			}
			b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(payload))
			if err != nil {
				return nil, err
			}
			data = b
		} else {
			if len(payload) > maxImageData {
				return nil, errImageCap
			}
			s, err := url.PathUnescape(payload)
			if err != nil {
				return nil, err
			}
			data = []byte(s)
		}
	} else {
		rc, err := resolve(ctx, src)
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		data, err = io.ReadAll(io.LimitReader(rc, maxImageFile+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxImageFile {
			return nil, errImageCap
		}
	}
	if isSVG(src) || sniffSVG(data) {
		if len(data) > svg.MaxBytes {
			return nil, errImageCap
		}
		return svg.Parse(bytes.NewReader(data))
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width > maxImageSide || cfg.Height > maxImageSide || cfg.Width*cfg.Height > maxImagePixels {
		return nil, errImageCap
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return img, nil
}

// attrPx reads a width or height attribute in pixels; 0 when absent or not a number.
func attrPx(n *phtml.Node, name string) float32 {
	v, ok := n.Attr(name)
	if !ok {
		return 0
	}
	f, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(v), "px"), 32)
	if err != nil || f < 0 {
		return 0
	}
	return float32(f)
}

// imageSpan is an <img> in the flow: its picture as an inline box once decoded, sized by its
// attributes and kept inside the page's width; its attributes' box until then; its alt text when
// it cannot be shown.
func (l *htmlLayout) imageSpan(n *phtml.Node, st *computed, src [2]int) flow.Span {
	ref, _ := n.Attr("src")
	alt, _ := n.Attr("alt")
	e := l.images.entry(strings.TrimSpace(ref))
	aw, ah := attrPx(n, "width"), attrPx(n, "height")
	switch e.state {
	case imgReady:
		w, h := float32(e.w), float32(e.h)
		switch {
		case aw > 0 && ah > 0:
			w, h = aw, ah
		case aw > 0:
			w, h = aw, h*aw/max(w, 1)
		case ah > 0:
			w, h = w*ah/max(h, 1), ah
		}
		if limit := l.contentWidth(); w > limit {
			w, h = limit, h*limit/w
		}
		img, drawing, ink := e.img, e.svg, st.color
		return flow.Span{Atom: &flow.Atom{W: w, H: h, Baseline: h, Paint: func(c gui.Canvas) {
			sz := c.Size()
			if drawing != nil {
				drawing.Draw(c, gui.Rect{W: sz.W, H: sz.H}, ink)
				return
			}
			c.DrawImage(img, gui.Rect{W: sz.W, H: sz.H}, gui.Rect{})
		}}, Link: st.link, Line: -1, Src: src}
	case imgLoading:
		return flow.Span{Atom: &flow.Atom{W: aw, H: ah, Baseline: ah}, Link: st.link, Line: -1, Src: src}
	}
	alt = strings.TrimSpace(alt)
	if alt == "" {
		return flow.Span{Line: -1, Src: src}
	}
	f := fontOf(st)
	f.Italic = true
	return flow.Span{Text: alt, Font: f, Color: st.color, Link: st.link, Line: -1, Src: src}
}
