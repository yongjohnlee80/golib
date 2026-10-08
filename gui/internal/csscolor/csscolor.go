// Package csscolor reads CSS colour values, for gui's HTML layout and its SVG drawings alike: one
// grammar, one parser.
package csscolor

import (
	"image/color"
	"strconv"
	"strings"
)

var namedColors = map[string]color.NRGBA{
	"black": {0, 0, 0, 255}, "white": {255, 255, 255, 255}, "red": {255, 0, 0, 255},
	"green": {0, 128, 0, 255}, "blue": {0, 0, 255, 255}, "gray": {128, 128, 128, 255},
	"grey": {128, 128, 128, 255}, "silver": {192, 192, 192, 255}, "maroon": {128, 0, 0, 255},
	"purple": {128, 0, 128, 255}, "fuchsia": {255, 0, 255, 255}, "lime": {0, 255, 0, 255},
	"olive": {128, 128, 0, 255}, "yellow": {255, 255, 0, 255}, "navy": {0, 0, 128, 255},
	"teal": {0, 128, 128, 255}, "aqua": {0, 255, 255, 255}, "orange": {255, 165, 0, 255},
	"transparent": {},
}

// Parse reads a CSS colour value: currentColor (cur), a name, #rgb[a], #rrggbb[aa], rgb() or
// rgba(); false for anything else.
func Parse(s string, cur color.NRGBA) (color.NRGBA, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "currentcolor" {
		return cur, true
	}
	if c, ok := namedColors[s]; ok {
		return c, true
	}
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		hex := func(a string) (uint8, bool) {
			v, err := strconv.ParseUint(a, 16, 8)
			return uint8(v), err == nil
		}
		switch len(h) {
		case 3, 4:
			var c [4]uint8
			c[3] = 255
			for i := range len(h) {
				v, ok := hex(string([]byte{h[i], h[i]}))
				if !ok {
					return color.NRGBA{}, false
				}
				c[i] = v
			}
			return color.NRGBA{c[0], c[1], c[2], c[3]}, true
		case 6, 8:
			var c [4]uint8
			c[3] = 255
			for i := 0; i < len(h); i += 2 {
				v, ok := hex(h[i : i+2])
				if !ok {
					return color.NRGBA{}, false
				}
				c[i/2] = v
			}
			return color.NRGBA{c[0], c[1], c[2], c[3]}, true
		}
		return color.NRGBA{}, false
	}
	if strings.HasPrefix(s, "rgb") {
		open, end := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
		if open < 0 || end < open {
			return color.NRGBA{}, false
		}
		f := strings.FieldsFunc(s[open+1:end], func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
		if len(f) < 3 {
			return color.NRGBA{}, false
		}
		var c [4]uint8
		c[3] = 255
		for i := 0; i < len(f) && i < 4; i++ {
			v := strings.TrimSpace(f[i])
			pct := strings.HasSuffix(v, "%")
			n, err := strconv.ParseFloat(strings.TrimSuffix(v, "%"), 32)
			if err != nil {
				return color.NRGBA{}, false
			}
			switch {
			case i == 3 && pct:
				n = n / 100 * 255
			case i == 3:
				n *= 255
			case pct:
				n = n / 100 * 255
			}
			c[i] = uint8(min(max(n, 0), 255))
		}
		return color.NRGBA{c[0], c[1], c[2], c[3]}, true
	}
	return color.NRGBA{}, false
}
