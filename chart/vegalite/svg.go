package vegalite

import (
	"bytes"
	"fmt"
	"image/color"
	"strconv"
	"strings"
)

// SVG is the chart as a standalone <svg> element, its viewBox its size: what gui.SVGCanvas writes
// for a chart painted on it, without a canvas. Shapes are SVG's own (rect, polygon, polyline,
// ellipse), each label a <text> in its font, placed by text-anchor and a central baseline so the
// browser's copy of the font sits where the label was measured, an upright one turned about its
// box's bottom middle. A chart scaled to fit is one group scaled by the fit. A colour's alpha
// is its fill- or stroke-opacity.
func (l *Laid) SVG() []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %s %s" width="%s" height="%s">`,
		num(l.size.W), num(l.size.H), num(l.size.W), num(l.size.H))
	if l.fit != 1 {
		fmt.Fprintf(&b, `<g transform="scale(%s)">`, strconv.FormatFloat(float64(l.fit), 'f', -1, 32))
	}
	for _, it := range l.items {
		writeItem(&b, it)
	}
	if l.fit != 1 {
		b.WriteString("</g>")
	}
	b.WriteString("</svg>")
	return b.Bytes()
}

// writeItem writes one item as its SVG element.
func writeItem(b *bytes.Buffer, it item) {
	r := it.rect
	switch it.kind {
	case itemRect:
		fmt.Fprintf(b, `<rect x="%s" y="%s" width="%s" height="%s"%s/>`, num(r.X), num(r.Y), num(r.W), num(r.H), svgPaint("fill", it.color))
	case itemFill:
		fmt.Fprintf(b, `<polygon points="%s"%s/>`, points(it.pts), svgPaint("fill", it.color))
	case itemLine:
		fmt.Fprintf(b, `<polyline points="%s" fill="none" stroke-width="%s"%s/>`, points(it.pts), num(it.width), svgPaint("stroke", it.color))
	case itemDot:
		fmt.Fprintf(b, `<ellipse cx="%s" cy="%s" rx="%s" ry="%s"%s/>`, num(r.X+r.W/2), num(r.Y+r.H/2), num(r.W/2), num(r.H/2), svgPaint("fill", it.color))
	case itemRing:
		fmt.Fprintf(b, `<ellipse cx="%s" cy="%s" rx="%s" ry="%s" fill="none" stroke-width="%s"%s/>`,
			num(r.X+r.W/2), num(r.Y+r.H/2), num(r.W/2), num(r.H/2), num(it.width), svgPaint("stroke", it.color))
	case itemText:
		writeText(b, it)
	}
}

// writeText writes a label: anchored at its box's left, middle or right, on its box's middle
// line; an upright one starts at its box's bottom middle and reads upward.
func writeText(b *bytes.Buffer, it item) {
	r := it.rect
	var x, y float32
	anchor := "middle"
	transform := ""
	if it.up {
		x, y = r.X+r.W/2, r.Y+r.H
		anchor = "start"
		transform = fmt.Sprintf(` transform="rotate(-90 %s %s)"`, num(x), num(y))
	} else {
		y = r.Y + r.H/2
		switch it.align {
		case -1:
			x, anchor = r.X, "start"
		case 1:
			x, anchor = r.X+r.W, "end"
		default:
			x = r.X + r.W/2
		}
	}
	fmt.Fprintf(b, `<text x="%s" y="%s" text-anchor="%s" dominant-baseline="central" xml:space="preserve"%s%s%s>%s</text>`,
		num(x), num(y), anchor, transform, fontAttrs(it.font), svgPaint("fill", it.color), textEscaper.Replace(it.text))
}

// fontAttrs is f as SVG text attributes: the proportional face ("") is sans-serif.
func fontAttrs(f Font) string {
	family := f.Family
	if family == "" {
		family = "sans-serif"
	}
	s := fmt.Sprintf(` font-family="%s" font-size="%s"`, attrEscaper.Replace(family), num(f.Size))
	if f.Bold {
		s += ` font-weight="bold"`
	}
	if f.Italic {
		s += ` font-style="italic"`
	}
	return s
}

// svgPaint is attr (fill or stroke) in c, with its opacity when c is not opaque.
func svgPaint(attr string, c color.NRGBA) string {
	s := fmt.Sprintf(` %s="#%02x%02x%02x"`, attr, c.R, c.G, c.B)
	if c.A != 0xff {
		s += fmt.Sprintf(` %s-opacity="%s"`, attr, num(float32(c.A)/255))
	}
	return s
}

// points is pts as SVG's points list.
func points(pts []Point) string {
	var b strings.Builder
	for i, p := range pts {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(num(p.X))
		b.WriteByte(',')
		b.WriteString(num(p.Y))
	}
	return b.String()
}

// num is f in SVG's number syntax, to a hundredth; NaN or out of range is 0.
func num(f float32) string {
	if f != f || f > 1e9 || f < -1e9 {
		return "0"
	}
	r := float32(int64(f*100+copySign(0.5, f))) / 100
	return strconv.FormatFloat(float64(r), 'f', -1, 32)
}

func copySign(m, s float32) float32 {
	if s < 0 {
		return -m
	}
	return m
}

var (
	textEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	attrEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
)
