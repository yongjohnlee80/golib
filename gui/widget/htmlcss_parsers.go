package widget

import (
	"image/color"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/internal/css"
	"github.com/yongjohnlee80/golib/gui/internal/csscolor"
)

// VALUE PARSERS — a declaration's value read as the computed style holds it: lengths, colours,
// sides, borders, font sizes and families, line heights, and the font and flex shorthands.

// parseColor reads a colour value; cur is currentColor's.
func parseColor(s string, cur color.NRGBA) (color.NRGBA, bool) { return csscolor.Parse(s, cur) }

// parseLen reads a length; allowAuto takes "auto".
func parseLen(s string) (cssLen, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "auto" {
		return cssLen{unit: 'a'}, true
	}
	if s == "0" {
		return cssLen{unit: 'p'}, true
	}
	if strings.HasSuffix(s, ")") {
		if e, ok := parseExpr(s); ok {
			return cssLen{unit: 'x', expr: e}, true
		}
		return cssLen{}, false
	}
	units := []struct {
		suf  string
		unit byte
	}{{"px", 'p'}, {"rem", 'r'}, {"em", 'e'}, {"ch", 'c'}, {"%", '%'}, {"vw", 'w'}, {"vh", 'h'}}
	for _, u := range units {
		if strings.HasSuffix(s, u.suf) {
			n, err := strconv.ParseFloat(strings.TrimSuffix(s, u.suf), 32)
			if err != nil {
				return cssLen{}, false
			}
			return cssLen{v: float32(n), unit: u.unit}, true
		}
	}
	return cssLen{}, false
}

// substituteVars replaces var(--x[, fallback]) in v from vars, to a bounded depth.
func substituteVars(v string, vars map[string]string) string {
	for range 8 {
		i := strings.Index(v, "var(")
		if i < 0 {
			return v
		}
		depth, j := 0, i+3
		for ; j < len(v); j++ {
			if v[j] == '(' {
				depth++
			} else if v[j] == ')' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		if j >= len(v) {
			return v
		}
		inner := v[i+4 : j]
		name, fallback, _ := strings.Cut(inner, ",")
		val, ok := vars[strings.TrimSpace(name)]
		if !ok {
			val = strings.TrimSpace(fallback)
		}
		v = v[:i] + val + v[j+1:]
	}
	return v
}

// side is the index of a -top/-right/-bottom/-left property's side.
func side(prop string) int {
	switch {
	case strings.HasSuffix(prop, "-top"):
		return 0
	case strings.HasSuffix(prop, "-right"):
		return 1
	case strings.HasSuffix(prop, "-bottom"):
		return 2
	}
	return 3
}

// boxSides reads a margin or padding shorthand of one to four values.
func boxSides(v string, out *[4]cssLen) {
	f := splitFields(v)
	var l []cssLen
	for _, s := range f {
		x, ok := parseLen(s)
		if !ok {
			return
		}
		l = append(l, x)
	}
	switch len(l) {
	case 1:
		*out = [4]cssLen{l[0], l[0], l[0], l[0]}
	case 2:
		*out = [4]cssLen{l[0], l[1], l[0], l[1]}
	case 3:
		*out = [4]cssLen{l[0], l[1], l[2], l[1]}
	case 4:
		*out = [4]cssLen{l[0], l[1], l[2], l[3]}
	}
}

// parseBorder reads a border shorthand: a width, a style (solid draws; none and hidden do not;
// any other is drawn solid), a colour.
func parseBorder(v string, cur color.NRGBA) cssBorder {
	b := cssBorder{w: cssLen{v: 3, unit: 'p'}} // medium
	styled := false
	for _, f := range splitFields(v) {
		switch f {
		case "none", "hidden":
			b.none = true
			styled = true
		case "solid", "dashed", "dotted", "double", "groove", "ridge", "inset", "outset":
			styled = true
		case "thin":
			b.w = cssLen{v: 1, unit: 'p'}
		case "medium":
			b.w = cssLen{v: 3, unit: 'p'}
		case "thick":
			b.w = cssLen{v: 5, unit: 'p'}
		default:
			if l, ok := parseLen(f); ok {
				b.w = l
			} else if c, ok := parseColor(f, cur); ok {
				b.color, b.set = c, true
			}
		}
	}
	if !styled {
		b.none = true // a border without a style draws nothing, as CSS's
	}
	return b
}

// splitFields splits on spaces outside parentheses (rgb(1, 2, 3) is one field).
func splitFields(v string) []string {
	v = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || r == '\f' {
			return ' '
		}
		return r
	}, v)
	var out []string
	for _, f := range css.SplitTop(v, ' ') {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// fontSize parses a CSS font-size string into concrete pixel dimensions.
func fontSize(v string, parent, root float32, view gui.Size) float32 {
	switch v {
	case "smaller":
		return parent * 0.83
	case "larger":
		return parent * 1.2
	case "small":
		return root * 0.89
	case "medium":
		return root
	case "large":
		return root * 1.2
	}
	if l, ok := parseLen(v); ok && !l.auto() {
		if l.unit == '%' {
			return l.v / 100 * parent
		}
		return l.px(parent, root, parent, view)
	}
	return parent
}

// lineHeight computes the line-height multiplier or length relative to the font size.
func lineHeight(v string, font float32) float32 {
	if v == "normal" {
		return 1.2
	}
	if n, err := strconv.ParseFloat(v, 32); err == nil {
		return float32(n)
	}
	if l, ok := parseLen(v); ok && font > 0 {
		if l.unit == '%' {
			return l.v / 100
		}
		return l.px(font, font, font, gui.Size{}) / font
	}
	return 1.2
}

// flex reads the flex shorthand: none, auto, initial, or grow [shrink] [basis].
func (c *computed) flex(v string) {
	switch v {
	case "none":
		c.grow, c.shrink, c.basis = 0, 0, cssLen{unit: 'a'}
		return
	case "auto":
		c.grow, c.shrink, c.basis = 1, 1, cssLen{unit: 'a'}
		return
	case "initial":
		c.grow, c.shrink, c.basis = 0, 1, cssLen{unit: 'a'}
		return
	}
	nums := 0
	c.basis = cssLen{unit: 'p'} // a bare number gives a basis of 0
	for _, f := range splitFields(v) {
		if n, err := strconv.ParseFloat(f, 32); err == nil && n >= 0 && nums < 2 {
			if nums == 0 {
				c.grow, c.shrink = float32(n), 1
			} else {
				c.shrink = float32(n)
			}
			nums++
			continue
		}
		if l, ok := parseLen(f); ok {
			c.basis = l
		}
	}
}

// boldWeight reports whether a font-weight is bold: bold, bolder, or a number from 600.
func boldWeight(v string) bool {
	if v == "bold" || v == "bolder" {
		return true
	}
	n, err := strconv.ParseFloat(v, 32)
	return err == nil && n >= 600
}

// familyList is a font-family list as the shaper takes it: its names, quotes dropped, and
// system-ui (which the shaper does not know) read as sans-serif.
func familyList(v string) string {
	var out []string
	for _, f := range strings.Split(v, ",") {
		f = strings.Trim(strings.TrimSpace(f), "\"'")
		switch strings.ToLower(f) {
		case "":
			continue
		case "system-ui", "-apple-system", "blinkmacsystemfont", "ui-sans-serif":
			f = "sans-serif"
		case "ui-serif":
			f = "serif"
		case "ui-monospace":
			f = "monospace"
		}
		out = append(out, f)
	}
	return strings.Join(out, ", ")
}

// monoFamily reports whether the CSS font-family includes common monospace font names.
func monoFamily(v string) bool {
	return strings.Contains(v, "mono") || strings.Contains(v, "courier") || strings.Contains(v, "consolas")
}

// font reads the font shorthand: [style] [weight] size[/line-height] family.
func (c *computed) font(v string, parent, root float32, view gui.Size) {
	f := splitFields(v)
	c.italic, c.bold = false, false
	for i, tok := range f {
		switch tok {
		case "italic", "oblique":
			c.italic = true
			continue
		case "bold", "bolder":
			c.bold = true
			continue
		case "normal", "lighter", "small-caps":
			continue
		}
		if n, err := strconv.Atoi(tok); err == nil && n >= 1 && n <= 1000 {
			c.bold = n >= 600
			continue
		}
		size, lh, _ := strings.Cut(tok, "/")
		c.fontSize = fontSize(size, parent, root, view)
		if lh != "" {
			c.lineHeight = lineHeight(lh, c.fontSize)
		}
		c.mono = monoFamily(strings.Join(f[i+1:], " "))
		c.family = familyList(strings.Join(f[i+1:], " "))
		return
	}
}
