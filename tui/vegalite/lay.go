package vegalite

import (
	"context"
	"errors"
	"fmt"
	"sort"

	pv "github.com/yongjohnlee80/golib/parse/vegalite"
)

// THE TIERS: a chart is drawn by the first tier that takes it, each refusing with ErrUnsupported
// one not of its kind: bars in cells, then Braille plots (lines, points, areas, rules), then the
// table, which takes anything the subset plots. Each tier registers itself with its order.

// ErrUnsupported and ErrTooLarge are parse/vegalite's: a spec outside the subset, or over a limit.
// A tier refuses a chart not of its kind with ErrUnsupported too.
var (
	ErrUnsupported = pv.ErrUnsupported
	ErrTooLarge    = pv.ErrTooLarge
)

// tierFunc draws a plotted chart for room cells, or refuses it (ErrUnsupported).
type tierFunc func(ctx context.Context, ch *pv.Chart, m Measurer, room int) (*Drawing, error)

type tier struct {
	order int
	name  string
	lay   tierFunc
}

// tiers are the registered tiers, in order.
var tiers []tier

// registerTier adds a tier; Lay tries them by order (bars 1, Braille 2, the table 3).
func registerTier(order int, name string, lay tierFunc) {
	tiers = append(tiers, tier{order, name, lay})
	sort.SliceStable(tiers, func(i, j int) bool { return tiers[i].order < tiers[j].order })
}

// Lay draws src, a Vega-Lite spec, for room cells: plotted (parse/vegalite's Plot), then drawn by
// the first tier that takes it. A spec outside the subset is ErrUnsupported (wrapped with what),
// one over a limit ErrTooLarge, malformed JSON the decoder's error: the page's notice, never the
// source.
func Lay(ctx context.Context, src string, m Measurer, room int) (*Drawing, error) {
	ch, err := pv.Plot(ctx, src)
	if err != nil {
		return nil, err
	}
	for _, t := range tiers {
		d, err := t.lay(ctx, ch, m, room)
		switch {
		case err == nil:
			return d, nil
		case !isUnsupported(err):
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w: no tier draws this chart", ErrUnsupported)
}

// isUnsupported reports whether err is a tier's refusal: the chart isn't its kind.
func isUnsupported(err error) bool { return errors.Is(err, ErrUnsupported) }
