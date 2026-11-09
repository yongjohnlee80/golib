package core

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget/internal/kit"
)

// Elide is where a text too wide for its cell loses what does not fit, as Qt's
// Qt::TextElideMode: the end (ElideRight, the zero value), the start
// (ElideLeft), the middle, or nowhere (ElideNone: cut at the edge, no
// ellipsis). ElidePath is golib's, for a path: the start, at a separator
// ("/", or Windows' "\") so the cut never falls inside a name
// (…/dao/name.go, …\dao\name.go), and as ElideLeft when even the last name
// does not fit.
type Elide int

const (
	ElideRight Elide = iota
	ElideLeft
	ElideMiddle
	ElideNone
	ElidePath
)

// ElideText fits s into w cells, dropping what does not fit where mode says (Elide): an ellipsis
// (…) takes the place of the end, the start, the middle, or a path's start up to a "/"; ElideNone
// cuts at the edge with none. A widget of your own fits a label into its cells with it.
func ElideText(s string, w int, mode Elide, measure func(string) int) string {
	if w <= 0 {
		return ""
	}
	if measure(s) <= w {
		return s
	}
	var gs []string
	for c := range tui.Graphemes(s) {
		gs = append(gs, c)
	}
	// head takes graphemes from the start while they fit in room; tail, from the end
	head := func(room int) string {
		var sb strings.Builder
		used := 0
		for _, c := range gs {
			if used+measure(c) > room {
				break
			}
			sb.WriteString(c)
			used += measure(c)
		}
		return sb.String()
	}
	tail := func(room int) string {
		used, i := 0, len(gs)
		for i > 0 && used+measure(gs[i-1]) <= room {
			i--
			used += measure(gs[i])
		}
		return strings.Join(gs[i:], "")
	}
	switch mode {
	case ElidePath:
		// the longest tail that starts at a separator and fits after the ellipsis
		for i := 1; i < len(gs); i++ {
			if gs[i] == "/" || gs[i] == `\` {
				if t := strings.Join(gs[i:], ""); measure(t) <= w-1 {
					return kit.Ellipsis + t
				}
			}
		}
		return kit.Ellipsis + tail(w-1)
	case ElideNone:
		return head(w)
	case ElideLeft:
		return kit.Ellipsis + tail(w-1)
	case ElideMiddle:
		back := (w - 1) / 2
		return head(w-1-back) + kit.Ellipsis + tail(back)
	default:
		return kit.Truncate(s, w, measure)
	}
}
