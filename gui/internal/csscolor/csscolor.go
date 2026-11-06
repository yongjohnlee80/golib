// Package csscolor is parse/csscolor in Go's image/color, for gui's HTML layout, its SVG and its
// Mermaid drawings: one grammar, one parser, in the root module.
package csscolor

import (
	"image/color"

	"github.com/yongjohnlee80/golib/parse/csscolor"
)

// Parse reads a CSS colour value: currentColor (cur), a name, #rgb[a], #rrggbb[aa], rgb() or
// rgba(); false for anything else.
func Parse(s string, cur color.NRGBA) (color.NRGBA, bool) {
	c, ok := csscolor.Parse(s, csscolor.Color(cur))
	return color.NRGBA(c), ok
}
