package vegalite

import (
	"context"

	pv "github.com/yongjohnlee80/golib/parse/vegalite"
)

// The bars and Braille tiers register here, under Lay's registry (lay.go): the bars first (a lone
// bar layer over discrete categories), then Braille plots (lines, points, areas, rules), then the
// table (table.go).
func init() {
	registerTier(1, "bars", func(_ context.Context, ch *pv.Chart, m Measurer, room int) (*Drawing, error) {
		if d, ok := layBars(ch, room, m); ok {
			return d, nil
		}
		return nil, ErrUnsupported
	})
	registerTier(2, "braille", layBraille)
}
