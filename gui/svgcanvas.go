package gui

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/color"
	"image/png"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
)

// SVG CANVAS — a [Canvas] that writes SVG: a still drawing written out rather than shown, such as
// a chart in an exported page. Paths keep their commands (M, L, Q, C, Z are SVG's own),
// rectangles and ellipses are SVG's own shapes, text is <text> in its font (the words a
// [TextLayout] was shaped from, not its outlines), and transforms and opacity are groups. A view
// lays its text out with this canvas's shaper, as on a window; the browser draws the words in its
// own copy of the font, so a label's width can differ a little from the box it was measured for.
// It has no grid: the cell methods answer zero and draw nothing. Not safe for concurrent use.

// SVGCanvas writes what is drawn on it as an SVG document (Bytes). As a window's canvas does, each
// canvas keeps its own state (its origin, its clip, its transform and opacity, and their stack)
// and applies it to each thing it draws: a sub-canvas and its parent may draw in any order.
type SVGCanvas struct {
	doc   *svgDoc
	off   Point    // the canvas's origin in the drawing's coordinates
	size  Size     // logical
	clips []string // the clip paths it draws inside, its parents' first: their intersection
	xform Affine   // the Pushes' transforms, composed; the zero Affine is none
	alpha float32  // the Pushes' opacities, multiplied, and its parent's when it was made
	stack []svgState
}

// svgState is what a Push saves and its Pop restores.
type svgState struct {
	xform Affine
	alpha float32
}

// svgDoc is what every canvas of one drawing writes into.
type svgDoc struct {
	size   Size
	shaper *TextShaper
	prefix string // the drawing's own, before every id: drawings inlined in one page never share one
	defs   bytes.Buffer
	body   bytes.Buffer
	ids    int
	textPx float32 // what TextSize answers
}

// svgDocs numbers the drawings made, for their ids' prefixes.
var svgDocs atomic.Uint64

// NewSVGCanvas is a canvas of size, in logical pixels, laying its text out with shaper (a new one
// at scale 1 when nil).
func NewSVGCanvas(size Size, shaper *TextShaper) *SVGCanvas {
	if shaper == nil {
		shaper = NewTextShaper(1)
	}
	d := &svgDoc{size: size, shaper: shaper, textPx: 14, prefix: "svg" + strconv.FormatUint(svgDocs.Add(1), 10) + "-"}
	return &SVGCanvas{doc: d, size: size, alpha: 1}
}

// SetTextSize sets what TextSize answers, the text size a view sizes its own by (14 unless set).
func (c *SVGCanvas) SetTextSize(px float32) {
	if px > 0 {
		c.doc.textPx = px
	}
}

// Bytes is the drawing as a standalone <svg> element, its viewBox the canvas's size.
func (c *SVGCanvas) Bytes() []byte {
	d := c.doc
	var out bytes.Buffer
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %s %s" width="%s" height="%s">`,
		num(d.size.W), num(d.size.H), num(d.size.W), num(d.size.H))
	if d.defs.Len() > 0 {
		out.WriteString("<defs>")
		out.Write(d.defs.Bytes())
		out.WriteString("</defs>")
	}
	out.Write(d.body.Bytes())
	out.WriteString("</svg>")
	return out.Bytes()
}

// id is a new id in the drawing: its prefix, then kind and a number.
func (d *svgDoc) id(kind string) string {
	d.ids++
	return d.prefix + kind + strconv.Itoa(d.ids)
}

// Sub is a canvas over r: its origin r's top left, clipped to r inside its parent's clip, at its
// parent's opacity, its transforms its own (as a window's Sub).
func (c *SVGCanvas) Sub(r Rect) Canvas {
	d := c.doc
	at := Pt(c.off.X+r.X, c.off.Y+r.Y)
	id := d.id("c")
	// in the drawing's coordinates: the clip group is outside every transform
	fmt.Fprintf(&d.defs, `<clipPath id="%s"><rect x="%s" y="%s" width="%s" height="%s"/></clipPath>`,
		id, num(at.X), num(at.Y), num(r.W), num(r.H))
	return &SVGCanvas{doc: d, off: at, size: r.Size(), clips: append(slices.Clip(c.clips), id), alpha: c.alpha}
}

// Size is the canvas's size in logical pixels.
func (c *SVGCanvas) Size() Size { return c.size }

// draw writes element in this canvas's state: inside its clips, then at its origin under its
// transform, at its opacity.
func (c *SVGCanvas) draw(element string) {
	b := &c.doc.body
	for _, id := range c.clips {
		fmt.Fprintf(b, `<g clip-path="url(#%s)">`, id)
	}
	var tr []string
	if c.off != (Point{}) {
		tr = append(tr, fmt.Sprintf("translate(%s %s)", num(c.off.X), num(c.off.Y)))
	}
	if c.xform != (Affine{}) {
		sx, hx, ox, hy, sy, oy := c.xform.Elems() // SVG's matrix(a b c d e f) is (sx hy hx sy ox oy)
		tr = append(tr, fmt.Sprintf("matrix(%s %s %s %s %s %s)", num(sx), num(hy), num(hx), num(sy), num(ox), num(oy)))
	}
	group := len(tr) > 0 || c.alpha < 1
	if group {
		b.WriteString("<g")
		if len(tr) > 0 {
			fmt.Fprintf(b, ` transform="%s"`, strings.Join(tr, " "))
		}
		if c.alpha < 1 {
			fmt.Fprintf(b, ` opacity="%s"`, num(c.alpha))
		}
		b.WriteString(">")
	}
	b.WriteString(element)
	if group {
		b.WriteString("</g>")
	}
	b.WriteString(strings.Repeat("</g>", len(c.clips)))
}

// PushTransform applies m to what this canvas draws until the matching Pop.
func (c *SVGCanvas) PushTransform(m Affine) {
	c.stack = append(c.stack, svgState{c.xform, c.alpha})
	c.xform = c.xform.Mul(m)
}

// PushOpacity multiplies what this canvas draws until the matching Pop by a, clamped to [0, 1].
func (c *SVGCanvas) PushOpacity(a float32) {
	c.stack = append(c.stack, svgState{c.xform, c.alpha})
	c.alpha *= min(max(a, 0), 1)
}

// Pop restores the state the latest Push saved; with none, it does nothing.
func (c *SVGCanvas) Pop() {
	if n := len(c.stack); n > 0 {
		c.xform, c.alpha = c.stack[n-1].xform, c.stack[n-1].alpha
		c.stack = c.stack[:n-1]
	}
}

// FillRect fills r.
func (c *SVGCanvas) FillRect(r Rect, b Brush) {
	c.draw(fmt.Sprintf(`<rect x="%s" y="%s" width="%s" height="%s"%s/>`, num(r.X), num(r.Y), num(r.W), num(r.H), c.paint("fill", b)))
}

// FillRRect fills r with corners of radius, clamped as every canvas clamps it (cornerRadius).
func (c *SVGCanvas) FillRRect(r Rect, radius float32, b Brush) {
	rad := num(cornerRadius(r, radius))
	c.draw(fmt.Sprintf(`<rect x="%s" y="%s" width="%s" height="%s" rx="%s" ry="%s"%s/>`, num(r.X), num(r.Y), num(r.W), num(r.H), rad, rad, c.paint("fill", b)))
}

// StrokeRRect strokes r's outline, with corners of radius (cornerRadius), width wide.
func (c *SVGCanvas) StrokeRRect(r Rect, radius, width float32, b Brush) {
	rad := num(cornerRadius(r, radius))
	c.draw(fmt.Sprintf(`<rect x="%s" y="%s" width="%s" height="%s" rx="%s" ry="%s" fill="none" stroke-width="%s"%s/>`,
		num(r.X), num(r.Y), num(r.W), num(r.H), rad, rad, num(width), c.paint("stroke", b)))
}

// FillEllipse fills the ellipse r bounds.
func (c *SVGCanvas) FillEllipse(r Rect, b Brush) {
	c.draw(fmt.Sprintf(`<ellipse cx="%s" cy="%s" rx="%s" ry="%s"%s/>`, num(r.X+r.W/2), num(r.Y+r.H/2), num(r.W/2), num(r.H/2), c.paint("fill", b)))
}

// FillPath fills p.
func (c *SVGCanvas) FillPath(p *Path, b Brush) {
	if p == nil || len(p.cmds) == 0 {
		return
	}
	c.draw(fmt.Sprintf(`<path d="%s"%s/>`, pathData(p), c.paint("fill", b)))
}

// StrokePath strokes p's outline, width wide.
func (c *SVGCanvas) StrokePath(p *Path, width float32, b Brush) {
	if p == nil || len(p.cmds) == 0 {
		return
	}
	c.draw(fmt.Sprintf(`<path d="%s" fill="none" stroke-width="%s"%s/>`, pathData(p), num(width), c.paint("stroke", b)))
}

// DrawText writes t's words with its top left at at: a <text> per run, on its line's baseline,
// in its font.
func (c *SVGCanvas) DrawText(t *TextLayout, at Point, b Brush) {
	if t == nil || len(t.lines) == 0 {
		return
	}
	var els strings.Builder
	for _, ln := range t.lines {
		y := at.Y + t.Ascent + ln.y/t.scale
		for _, run := range ln.runs {
			if strings.TrimSpace(run.text) == "" {
				continue
			}
			// xml:space: the words as shaped, their spaces kept, not collapsed as SVG's text would
			fmt.Fprintf(&els, `<text x="%s" y="%s" xml:space="preserve"%s%s>%s</text>`,
				num(at.X+run.x/t.scale), num(y), fontAttrs(t), c.paint("fill", b), xmlText(run.text))
		}
	}
	if els.Len() > 0 {
		c.draw(els.String())
	}
}

// DrawImage draws img's src (all of it when zero) into dst, as a PNG data: URI.
func (c *SVGCanvas) DrawImage(img *Image, dst, src Rect) {
	if img == nil || img.src == nil || dst.W <= 0 || dst.H <= 0 {
		return
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img.src); err != nil {
		return
	}
	href := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data.Bytes())
	full := img.Size()
	if src.W <= 0 || src.H <= 0 {
		src = Rect{W: full.W, H: full.H}
	}
	c.draw(fmt.Sprintf(`<svg x="%s" y="%s" width="%s" height="%s" viewBox="%s %s %s %s" preserveAspectRatio="none"><image width="%s" height="%s" href="%s"/></svg>`,
		num(dst.X), num(dst.Y), num(dst.W), num(dst.H), num(src.X), num(src.Y), num(src.W), num(src.H), num(full.W), num(full.H), href))
}

// paint is a brush as attr's paint (fill or stroke), and its opacity when not opaque; a gradient
// is a <linearGradient> of its own.
func (c *SVGCanvas) paint(attr string, b Brush) string {
	if g := b.Gradient; g != nil {
		d := c.doc
		id := d.id("g")
		fmt.Fprintf(&d.defs, `<linearGradient id="%s" gradientUnits="userSpaceOnUse" x1="%s" y1="%s" x2="%s" y2="%s"><stop offset="0" stop-color="%s"%s/><stop offset="1" stop-color="%s"%s/></linearGradient>`,
			id, num(g.From.X), num(g.From.Y), num(g.To.X), num(g.To.Y),
			hexColor(g.FromColor), stopOpacity(g.FromColor), hexColor(g.ToColor), stopOpacity(g.ToColor))
		return fmt.Sprintf(` %s="url(#%s)"`, attr, id)
	}
	s := fmt.Sprintf(` %s="%s"`, attr, hexColor(b.Color))
	if b.Color.A != 0xff {
		s += fmt.Sprintf(` %s-opacity="%s"`, attr, num(float32(b.Color.A)/255))
	}
	return s
}

// Text is the shaper a view lays its text out with.
func (c *SVGCanvas) Text() *TextShaper { return c.doc.shaper }

// Scale is 1: SVG's units are logical pixels.
func (c *SVGCanvas) Scale() float32 { return 1 }

// Fonts are the shaper's families.
func (c *SVGCanvas) Fonts() Fonts { return c.doc.shaper.fonts }

// TextSize is the text size set (SetTextSize): an SVG canvas has no window's.
func (c *SVGCanvas) TextSize() float32 { return c.doc.textPx }

// The grid's methods: an SVG canvas has none.
func (c *SVGCanvas) WindowMoveArea(Rect)                      {}
func (c *SVGCanvas) CellSize() Size                           { return Size{} }
func (c *SVGCanvas) CellColors(int, int) (fg, bg color.NRGBA) { return }
func (c *SVGCanvas) CellText(int, int) string                 { return "" }
func (c *SVGCanvas) Backdrop() color.NRGBA                    { return color.NRGBA{} }
func (c *SVGCanvas) PaintCells(CellRect, bool)                {}

// pathData is p's commands as SVG writes them.
func pathData(p *Path) string {
	var b strings.Builder
	for i, cmd := range p.cmds {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte(cmd.kind)
		for _, pt := range cmd.pts[:cmd.npoints] {
			b.WriteByte(' ')
			b.WriteString(num(pt.X))
			b.WriteByte(' ')
			b.WriteString(num(pt.Y))
		}
	}
	return b.String()
}

// fontAttrs is t's font as SVG text attributes.
func fontAttrs(t *TextLayout) string {
	s := fmt.Sprintf(` font-family="%s" font-size="%s"`, xmlAttr(t.family), num(t.font.Size))
	if t.font.Bold {
		s += ` font-weight="bold"`
	}
	if t.font.Italic {
		s += ` font-style="italic"`
	}
	return s
}

// num is f in SVG's number syntax, to a hundredth.
func num(f float32) string {
	return strconv.FormatFloat(float64(f32Round(f)), 'f', -1, 32)
}

func f32Round(f float32) float32 {
	if f != f || f > 1e9 || f < -1e9 { // NaN or out of range: nothing sensible to draw
		return 0
	}
	return float32(int64(f*100+copySign(0.5, f))) / 100
}

func copySign(m, s float32) float32 {
	if s < 0 {
		return -m
	}
	return m
}

func hexColor(c color.NRGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

func stopOpacity(c color.NRGBA) string {
	if c.A == 0xff {
		return ""
	}
	return fmt.Sprintf(` stop-opacity="%s"`, num(float32(c.A)/255))
}

var (
	textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
)

func xmlText(s string) string { return textEscaper.Replace(s) }
func xmlAttr(s string) string { return attrEscaper.Replace(s) }

var _ Canvas = (*SVGCanvas)(nil)
