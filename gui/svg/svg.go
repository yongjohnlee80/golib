// Package svg draws SVG documents natively, through gui.Path: an icon or an illustration a page
// shows as an <img>, at any scale, with no rasteriser.
//
// It reads paths, the basic shapes (rect, circle, ellipse, line, polyline, polygon) and groups,
// with fill, stroke, stroke-width, the opacities and transforms, as attributes or in style="".
// Anything else (text, gradients, filters, masks, clip paths, use) is skipped, never an error: the
// drawing shows what it can. A document is bounded as a whole, by its bytes, its elements, its
// nesting and its path segments, and a number that is not finite refuses it.
package svg

import (
	"encoding/xml"
	"errors"
	"image/color"
	"io"
	"math"
	"strconv"
	"strings"

	"gioui.org/f32"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/internal/csscolor"
)

// The bounds of a document, each over the whole drawing.
const (
	MaxBytes    = 4 << 20 // the document
	MaxElements = 20_000  // elements, kept or skipped
	MaxDepth    = 64      // element nesting
	MaxSegments = 200_000 // path segments in all; an arc counts as the cubics it becomes
	maxCoord    = 1e7     // a number's magnitude, and a transform's translation
	maxScale    = 1e4     // a transform's scale and shear, composed: what keeps geometry finite
	minViewBox  = 1e-3    // a viewBox's smallest side: smaller scales its drawing past any bound
)

var (
	// ErrNotSVG is a document with no <svg> root element.
	ErrNotSVG = errors.New("svg: not an SVG document")
	// ErrTooLarge is a document over one of the bounds.
	ErrTooLarge = errors.New("svg: over a size bound")
	// ErrInvalid is a number that is not finite, or too large to draw.
	ErrInvalid = errors.New("svg: a number that cannot be drawn")
)

// paint is a fill or a stroke as written: a colour, none, or currentColor.
type paint struct {
	color   color.NRGBA
	none    bool
	current bool
}

// style is what an element draws with, inherited as SVG inherits it.
type style struct {
	fill, stroke   paint
	strokeWidth    float32
	fillOpacity    float32
	strokeOpacity  float32
	opacity        float32 // the group's, multiplied down: an element's alone is not inherited
	xform          f32.Affine2D
	skip           bool // inside something not drawn (defs, a gradient, text)
	shapeChildless bool // a shape's children (title, desc) are not drawn
}

type shape struct {
	path          *gui.Path
	xform         f32.Affine2D
	fill, stroke  paint
	strokeWidth   float32
	fillAlpha     float32
	strokeAlpha   float32
	filled, lined bool
}

// Drawing is a parsed SVG document: its shapes in viewBox units, in document order.
type Drawing struct {
	shapes []shape
	vb     [4]float32 // min-x, min-y, width, height
	size   gui.Size
}

// Size is the drawing's intrinsic size: its width and height, else its viewBox's, else 300 × 150.
func (d *Drawing) Size() gui.Size { return d.size }

// Shapes is how many shapes it draws.
func (d *Drawing) Shapes() int { return len(d.shapes) }

// Draw paints the drawing into r, its viewBox scaled to fit and centred (preserveAspectRatio
// xMidYMid meet); current is currentColor's.
func (d *Drawing) Draw(c gui.Canvas, r gui.Rect, current color.NRGBA) {
	w, h := d.vb[2], d.vb[3]
	if w <= 0 || h <= 0 || r.W <= 0 || r.H <= 0 {
		return
	}
	s := min(r.W/w, r.H/h)
	tx := r.X + (r.W-w*s)/2 - d.vb[0]*s
	ty := r.Y + (r.H-h*s)/2 - d.vb[1]*s
	base := f32.NewAffine2D(s, 0, tx, 0, s, ty)
	for _, sh := range d.shapes {
		m := base.Mul(sh.xform)
		if !finiteAffine(m) {
			continue // a scale past what a canvas can draw: nothing this shape draws is finite
		}
		c.PushTransform(m)
		if sh.filled {
			c.FillPath(sh.path, gui.Solid(alpha(sh.fill.colorOr(current), sh.fillAlpha)))
		}
		if sh.lined {
			c.StrokePath(sh.path, sh.strokeWidth, gui.Solid(alpha(sh.stroke.colorOr(current), sh.strokeAlpha)))
		}
		c.Pop()
	}
}

func (p paint) colorOr(current color.NRGBA) color.NRGBA {
	if p.current {
		return current
	}
	return p.color
}

func alpha(c color.NRGBA, a float32) color.NRGBA {
	c.A = uint8(float32(c.A) * min(max(a, 0), 1))
	return c
}

// Parse reads an SVG document, at most MaxBytes of it.
func Parse(r io.Reader) (*Drawing, error) {
	lr := &io.LimitedReader{R: r, N: MaxBytes + 1}
	dec := xml.NewDecoder(lr)
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity
	p := &parser{d: &Drawing{}}
	var stack []style
	root := false
	for {
		tok, err := dec.Token()
		if lr.N <= 0 {
			return nil, ErrTooLarge
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			if errors.Is(err, ErrTooLarge) || errors.Is(err, ErrInvalid) {
				return nil, err
			}
			break // a malformed tail: what came before is drawn
		}
		switch t := tok.(type) {
		case xml.StartElement:
			p.elements++
			if p.elements > MaxElements || len(stack) >= MaxDepth {
				return nil, ErrTooLarge
			}
			parent := style{fill: paint{color: color.NRGBA{A: 255}}, stroke: paint{none: true}, strokeWidth: 1,
				fillOpacity: 1, strokeOpacity: 1, opacity: 1, xform: f32.Affine2D{}}
			if len(stack) > 0 {
				parent = stack[len(stack)-1]
			}
			name := strings.ToLower(t.Name.Local)
			if !root {
				if name != "svg" {
					return nil, ErrNotSVG
				}
				root = true
				if err := p.root(t); err != nil {
					return nil, err
				}
			}
			st, err := p.element(name, t, parent)
			if err != nil {
				return nil, err
			}
			stack = append(stack, st)
		case xml.EndElement:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	if !root {
		return nil, ErrNotSVG
	}
	return p.d, nil
}

type parser struct {
	d        *Drawing
	elements int
	segments int
}

// root reads the <svg> element's viewBox, width and height.
func (p *parser) root(t xml.StartElement) error {
	w, wok, err := length(attr(t, "width"))
	if err != nil {
		return err
	}
	h, hok, err := length(attr(t, "height"))
	if err != nil {
		return err
	}
	if vb := attr(t, "viewBox"); vb != "" {
		nums, err := numbers(vb)
		if err != nil {
			return err
		}
		if len(nums) == 4 && nums[2] > 0 && nums[3] > 0 {
			if nums[2] < minViewBox || nums[3] < minViewBox {
				return ErrInvalid
			}
			copy(p.d.vb[:], nums)
		}
	}
	switch {
	case p.d.vb[2] <= 0 && wok && hok && w > 0 && h > 0:
		p.d.vb = [4]float32{0, 0, w, h}
	case p.d.vb[2] <= 0:
		p.d.vb = [4]float32{0, 0, 300, 150}
	}
	p.d.size = gui.Size{W: p.d.vb[2], H: p.d.vb[3]}
	switch {
	case wok && hok:
		p.d.size = gui.Size{W: w, H: h}
	case wok:
		p.d.size = gui.Size{W: w, H: w * p.d.vb[3] / p.d.vb[2]}
	case hok:
		p.d.size = gui.Size{W: h * p.d.vb[2] / p.d.vb[3], H: h}
	}
	return nil
}

// element is the style an element and its children draw with, its shape added when it is one.
func (p *parser) element(name string, t xml.StartElement, parent style) (style, error) {
	st := parent
	st.opacity = parent.opacity // multiplied below by the element's own
	if parent.skip || parent.shapeChildless {
		st.skip = true
		return st, nil
	}
	switch name {
	case "svg", "g", "a", "switch":
	case "path", "rect", "circle", "ellipse", "line", "polyline", "polygon":
		st.shapeChildless = true
	default:
		st.skip = true // defs, symbol, gradients, text, title, use, image, …: not drawn
		return st, nil
	}
	if err := p.styled(t, &st); err != nil {
		return st, err
	}
	if name == "svg" || name == "g" || name == "a" || name == "switch" {
		return st, nil
	}
	path, err := p.shapePath(name, t)
	if err != nil || path == nil {
		return st, err
	}
	sh := shape{path: path, xform: st.xform, fill: st.fill, stroke: st.stroke, strokeWidth: st.strokeWidth,
		fillAlpha: st.fillOpacity * st.opacity, strokeAlpha: st.strokeOpacity * st.opacity}
	sh.filled = !st.fill.none && name != "line" && name != "polyline" || name == "polyline" && !st.fill.none
	sh.lined = !st.stroke.none && st.strokeWidth > 0
	if sh.filled || sh.lined {
		p.d.shapes = append(p.d.shapes, sh)
	}
	return st, nil
}

// styled applies an element's presentation attributes, then its style="" declarations, which win.
func (p *parser) styled(t xml.StartElement, st *style) error {
	own := 1.0
	set := func(prop, v string) error {
		v = strings.TrimSpace(v)
		switch prop {
		case "fill":
			st.fill = parsePaint(v, st.fill)
		case "stroke":
			st.stroke = parsePaint(v, st.stroke)
		case "stroke-width":
			if w, ok, err := length(v); err != nil {
				return err
			} else if ok {
				st.strokeWidth = max(w, 0)
			}
		case "fill-opacity", "stroke-opacity", "opacity":
			f, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 32)
			if err != nil {
				return nil
			}
			if strings.HasSuffix(v, "%") {
				f /= 100
			}
			f = min(max(f, 0), 1)
			switch prop {
			case "fill-opacity":
				st.fillOpacity = float32(f)
			case "stroke-opacity":
				st.strokeOpacity = float32(f)
			default:
				own = f
			}
		case "transform":
			m, err := transform(v)
			if err != nil {
				return err
			}
			// bounded as composed, not only one by one: nested scales multiply
			if st.xform = st.xform.Mul(m); !bounded(st.xform) {
				return ErrInvalid
			}
		}
		return nil
	}
	for _, a := range t.Attr {
		if err := set(strings.ToLower(a.Name.Local), a.Value); err != nil {
			return err
		}
	}
	for _, decl := range strings.Split(attr(t, "style"), ";") {
		k, v, ok := strings.Cut(decl, ":")
		if !ok {
			continue
		}
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "transform" {
			continue // a CSS transform is not SVG's attribute; left alone
		}
		if err := set(k, v); err != nil {
			return err
		}
	}
	st.opacity *= float32(own)
	return nil
}

// parsePaint reads a fill or stroke value; an unreadable one (a gradient's url) keeps a fallback
// colour written after it, else draws nothing.
func parsePaint(v string, inherited paint) paint {
	lv := strings.ToLower(v)
	switch {
	case lv == "" || lv == "inherit":
		return inherited
	case lv == "none" || lv == "transparent":
		return paint{none: true}
	case lv == "currentcolor":
		return paint{current: true}
	case strings.HasPrefix(lv, "url("):
		if end := strings.IndexByte(lv, ')'); end >= 0 {
			if fb := strings.TrimSpace(v[end+1:]); fb != "" {
				return parsePaint(fb, paint{none: true})
			}
		}
		return paint{none: true}
	}
	if c, ok := csscolor.Parse(v, color.NRGBA{A: 255}); ok {
		return paint{color: c}
	}
	return inherited
}

// shapePath is a shape's outline in its own units; nil for one that draws nothing (a zero size).
func (p *parser) shapePath(name string, t xml.StartElement) (*gui.Path, error) {
	num := func(k string) (float32, error) {
		v, _, err := length(attr(t, k))
		return v, err
	}
	var vals [6]float32
	var keys []string
	switch name {
	case "rect":
		keys = []string{"x", "y", "width", "height", "rx", "ry"}
	case "circle":
		keys = []string{"cx", "cy", "r"}
	case "ellipse":
		keys = []string{"cx", "cy", "rx", "ry"}
	case "line":
		keys = []string{"x1", "y1", "x2", "y2"}
	}
	for i, k := range keys {
		v, err := num(k)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	path := &gui.Path{}
	switch name {
	case "path":
		if err := p.pathData(attr(t, "d"), path); err != nil {
			return nil, err
		}
		return path, nil
	case "rect":
		x, y, w, h, rx, ry := vals[0], vals[1], vals[2], vals[3], vals[4], vals[5]
		if w <= 0 || h <= 0 {
			return nil, nil
		}
		if attr(t, "rx") == "" {
			rx = ry
		}
		if attr(t, "ry") == "" {
			ry = rx
		}
		rx, ry = min(max(rx, 0), w/2), min(max(ry, 0), h/2)
		if err := p.count(10); err != nil {
			return nil, err
		}
		roundRect(path, x, y, w, h, rx, ry)
	case "circle":
		if vals[2] <= 0 {
			return nil, nil
		}
		if err := p.count(5); err != nil {
			return nil, err
		}
		ellipse(path, vals[0], vals[1], vals[2], vals[2])
	case "ellipse":
		if vals[2] <= 0 || vals[3] <= 0 {
			return nil, nil
		}
		if err := p.count(5); err != nil {
			return nil, err
		}
		ellipse(path, vals[0], vals[1], vals[2], vals[3])
	case "line":
		if err := p.count(2); err != nil {
			return nil, err
		}
		path.MoveTo(gui.Point{X: vals[0], Y: vals[1]}).LineTo(gui.Point{X: vals[2], Y: vals[3]})
	case "polyline", "polygon":
		nums, err := numbers(attr(t, "points"))
		if err != nil {
			return nil, err
		}
		if len(nums) < 4 {
			return nil, nil
		}
		if err := p.count(len(nums)/2 + 1); err != nil {
			return nil, err
		}
		path.MoveTo(gui.Point{X: nums[0], Y: nums[1]})
		for i := 2; i+1 < len(nums); i += 2 {
			path.LineTo(gui.Point{X: nums[i], Y: nums[i+1]})
		}
		if name == "polygon" {
			path.Close()
		}
	}
	return path, nil
}

// count takes n segments from the drawing's budget.
func (p *parser) count(n int) error {
	p.segments += n
	if p.segments > MaxSegments {
		return ErrTooLarge
	}
	return nil
}

// kappa is a quarter circle's cubic control distance, as a fraction of the radius.
const kappa = 0.5522847498

func ellipse(p *gui.Path, cx, cy, rx, ry float32) {
	kx, ky := rx*kappa, ry*kappa
	p.MoveTo(gui.Point{X: cx + rx, Y: cy})
	p.CubeTo(gui.Point{X: cx + rx, Y: cy + ky}, gui.Point{X: cx + kx, Y: cy + ry}, gui.Point{X: cx, Y: cy + ry})
	p.CubeTo(gui.Point{X: cx - kx, Y: cy + ry}, gui.Point{X: cx - rx, Y: cy + ky}, gui.Point{X: cx - rx, Y: cy})
	p.CubeTo(gui.Point{X: cx - rx, Y: cy - ky}, gui.Point{X: cx - kx, Y: cy - ry}, gui.Point{X: cx, Y: cy - ry})
	p.CubeTo(gui.Point{X: cx + kx, Y: cy - ry}, gui.Point{X: cx + rx, Y: cy - ky}, gui.Point{X: cx + rx, Y: cy})
	p.Close()
}

func roundRect(p *gui.Path, x, y, w, h, rx, ry float32) {
	if rx == 0 || ry == 0 {
		p.MoveTo(gui.Point{X: x, Y: y}).LineTo(gui.Point{X: x + w, Y: y}).LineTo(gui.Point{X: x + w, Y: y + h}).
			LineTo(gui.Point{X: x, Y: y + h}).Close()
		return
	}
	kx, ky := rx*kappa, ry*kappa
	p.MoveTo(gui.Point{X: x + rx, Y: y})
	p.LineTo(gui.Point{X: x + w - rx, Y: y})
	p.CubeTo(gui.Point{X: x + w - rx + kx, Y: y}, gui.Point{X: x + w, Y: y + ry - ky}, gui.Point{X: x + w, Y: y + ry})
	p.LineTo(gui.Point{X: x + w, Y: y + h - ry})
	p.CubeTo(gui.Point{X: x + w, Y: y + h - ry + ky}, gui.Point{X: x + w - rx + kx, Y: y + h}, gui.Point{X: x + w - rx, Y: y + h})
	p.LineTo(gui.Point{X: x + rx, Y: y + h})
	p.CubeTo(gui.Point{X: x + rx - kx, Y: y + h}, gui.Point{X: x, Y: y + h - ry + ky}, gui.Point{X: x, Y: y + h - ry})
	p.LineTo(gui.Point{X: x, Y: y + ry})
	p.CubeTo(gui.Point{X: x, Y: y + ry - ky}, gui.Point{X: x + rx - kx, Y: y}, gui.Point{X: x + rx, Y: y})
	p.Close()
}

func attr(t xml.StartElement, name string) string {
	for _, a := range t.Attr {
		if strings.EqualFold(a.Name.Local, name) {
			return a.Value
		}
	}
	return ""
}

// finite checks a number: not NaN, not infinite, within maxCoord.
func finite(f float64) (float32, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) || math.Abs(f) > maxCoord {
		return 0, ErrInvalid
	}
	return float32(f), nil
}

// length reads a length in user units: a number, px, pt, mm, cm, in; a percentage, or nothing,
// is not one (false).
func length(v string) (float32, bool, error) {
	v = strings.TrimSpace(strings.ToLower(v))
	if v == "" || strings.HasSuffix(v, "%") {
		return 0, false, nil
	}
	scale := 1.0
	for _, u := range []struct {
		s string
		k float64
	}{{"px", 1}, {"pt", 4.0 / 3}, {"mm", 96 / 25.4}, {"cm", 96 / 2.54}, {"in", 96}, {"em", 16}} {
		if strings.HasSuffix(v, u.s) {
			v, scale = strings.TrimSuffix(v, u.s), u.k
			break
		}
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return 0, false, nil
	}
	n, err := finite(f * scale)
	return n, err == nil, err
}

// numbers reads a list of numbers separated by spaces and commas.
func numbers(v string) ([]float32, error) {
	s := &scanner{s: v}
	var out []float32
	for {
		s.sep()
		if s.done() {
			return out, nil
		}
		n, ok, err := s.number()
		if err != nil {
			return nil, err
		}
		if !ok {
			return out, nil
		}
		out = append(out, n)
		if len(out) > 2*MaxSegments {
			return nil, ErrTooLarge
		}
	}
}

// transform reads a transform list into one matrix, applied right to left as SVG composes.
func transform(v string) (f32.Affine2D, error) {
	m := f32.Affine2D{}
	rest := v
	for {
		rest = strings.TrimLeft(rest, " \t\r\n,")
		open := strings.IndexByte(rest, '(')
		end := strings.IndexByte(rest, ')')
		if open < 0 || end < open {
			return m, nil
		}
		name := strings.ToLower(strings.TrimSpace(rest[:open]))
		a, err := numbers(rest[open+1 : end])
		if err != nil {
			return m, err
		}
		rest = rest[end+1:]
		arg := func(i int, def float32) float32 {
			if i < len(a) {
				return a[i]
			}
			return def
		}
		var t f32.Affine2D
		switch name {
		case "matrix":
			if len(a) != 6 {
				continue
			}
			t = f32.NewAffine2D(a[0], a[2], a[4], a[1], a[3], a[5])
		case "translate":
			t = f32.Affine2D{}.Offset(f32.Pt(arg(0, 0), arg(1, 0)))
		case "scale":
			sx := arg(0, 1)
			t = f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(sx, arg(1, sx)))
		case "rotate":
			rad := float64(arg(0, 0)) * math.Pi / 180
			t = f32.Affine2D{}.Rotate(f32.Pt(arg(1, 0), arg(2, 0)), float32(rad))
		case "skewx":
			t = f32.NewAffine2D(1, float32(math.Tan(float64(arg(0, 0))*math.Pi/180)), 0, 0, 1, 0)
		case "skewy":
			t = f32.NewAffine2D(1, 0, 0, float32(math.Tan(float64(arg(0, 0))*math.Pi/180)), 1, 0)
		default:
			continue
		}
		m = m.Mul(t)
	}
}

// bounded reports whether a composed transform keeps geometry finite: scale and shear within
// maxScale, translation within maxCoord, every element finite. A skew near 90° fails it.
func bounded(m f32.Affine2D) bool {
	sx, hx, ox, hy, sy, oy := m.Elems()
	for _, v := range []float32{sx, hx, hy, sy} {
		if !finite32(v) || math.Abs(float64(v)) > maxScale {
			return false
		}
	}
	return finite32(ox) && finite32(oy) && math.Abs(float64(ox)) <= maxCoord && math.Abs(float64(oy)) <= maxCoord
}

// finiteAffine reports whether every element of m is finite: what a canvas may be given.
func finiteAffine(m f32.Affine2D) bool {
	sx, hx, ox, hy, sy, oy := m.Elems()
	for _, v := range []float32{sx, hx, ox, hy, sy, oy} {
		if !finite32(v) || math.Abs(float64(v)) > 1e15 {
			return false
		}
	}
	return true
}

func finite32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}
