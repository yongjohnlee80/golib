package mermaid

import (
	"image/color"
	"strconv"
	"strings"
)

// cssColor reads #rgb, #rgba, #rrggbb, #rrggbbaa, rgb(r, g, b) and rgba(r, g, b, a); anything
// else is not read.
func cssColor(s string) (color.NRGBA, bool) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "rgb") {
		return rgbFunc(s)
	}
	if !strings.HasPrefix(s, "#") {
		return color.NRGBA{}, false
	}
	h := s[1:]
	if len(h) == 3 || len(h) == 4 {
		var b strings.Builder
		for _, r := range h {
			b.WriteRune(r)
			b.WriteRune(r)
		}
		h = b.String()
	}
	if len(h) == 6 {
		h += "ff"
	}
	if len(h) != 8 {
		return color.NRGBA{}, false
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return color.NRGBA{}, false
	}
	return color.NRGBA{R: uint8(v >> 24), G: uint8(v >> 16), B: uint8(v >> 8), A: uint8(v)}, true
}

// rgbFunc reads rgb(r, g, b) and rgba(r, g, b, a): channels 0 to 255, alpha 0 to 1.
func rgbFunc(s string) (color.NRGBA, bool) {
	name, args, ok := strings.Cut(s, "(")
	if !ok || !strings.HasSuffix(args, ")") || (name != "rgb" && name != "rgba") {
		return color.NRGBA{}, false
	}
	parts := strings.Split(strings.TrimSuffix(args, ")"), ",")
	if len(parts) != 3 && len(parts) != 4 {
		return color.NRGBA{}, false
	}
	var v [4]float64
	v[3] = 1
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
		if err != nil || f < 0 || (i < 3 && f > 255) || (i == 3 && f > 1) {
			return color.NRGBA{}, false
		}
		v[i] = f
	}
	return color.NRGBA{R: uint8(v[0] + 0.5), G: uint8(v[1] + 0.5), B: uint8(v[2] + 0.5), A: uint8(v[3]*255 + 0.5)}, true
}
