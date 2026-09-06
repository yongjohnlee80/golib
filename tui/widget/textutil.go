package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Text measurement note. All width math is policy-aware (ADR-0003 §2.4
// normative). Render paths measure through Surface.StringWidth; layout,
// event, cursor, scroll, wrap, and hit-test paths measure through
// Base.measure (Context.StringWidth). Both resolve the App's single active
// width policy, so paint and geometry agree under WidthPolicyAmbiguousWide.
// The free helpers below take the caller's measure func so no path silently
// falls back to the package-level default.

const ellipsis = "…"

// clusters splits s into grapheme clusters (UAX #29 via tui.Graphemes).
func clusters(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for c := range tui.Graphemes(s) {
		out = append(out, c)
	}
	return out
}

// cellsBefore is the display width of cs[:i] under the caller's measure
// (Surface.StringWidth in Render, Base.measure elsewhere).
func cellsBefore(cs []string, i int, measure func(string) int) int {
	w := 0
	for j := 0; j < i && j < len(cs); j++ {
		w += measure(cs[j])
	}
	return w
}

// truncate fits s into w cells, replacing the overflow with an ellipsis.
// measure is the caller's width function (Surface.StringWidth in Render,
// Base.measure — the App width policy — elsewhere).
func truncate(s string, w int, measure func(string) int) string {
	if w <= 0 {
		return ""
	}
	if measure(s) <= w {
		return s
	}
	if w == 1 {
		return ellipsis
	}
	var sb strings.Builder
	used := 0
	for c := range tui.Graphemes(s) {
		cw := measure(c)
		if used+cw > w-1 {
			break
		}
		sb.WriteString(c)
		used += cw
	}
	sb.WriteString(ellipsis)
	return sb.String()
}

// wrapLine soft-wraps one line (no newlines) into rows of at most w cells,
// breaking at spaces when possible and mid-word otherwise. A "" line yields
// one empty row.
func wrapLine(s string, w int, measure func(string) int) []string {
	if w <= 0 {
		return []string{""}
	}
	if measure(s) <= w {
		return []string{s}
	}
	var rows []string
	var row strings.Builder
	rowW := 0
	lastSpace := -1 // byte offset in row of the last breakable space
	lastSpaceW := 0 // row width up to (excluding) that space
	for c := range tui.Graphemes(s) {
		cw := measure(c)
		if rowW+cw > w && rowW > 0 {
			if lastSpace >= 0 && lastSpace < row.Len() {
				full := row.String()
				rows = append(rows, full[:lastSpace])
				rest := strings.TrimPrefix(full[lastSpace:], " ")
				row.Reset()
				row.WriteString(rest)
				rowW = rowW - lastSpaceW - 1
				if rowW < 0 {
					rowW = measure(rest)
				}
			} else {
				rows = append(rows, row.String())
				row.Reset()
				rowW = 0
			}
			lastSpace = -1
			if c == " " && row.Len() == 0 {
				continue // never start a row with the break space
			}
		}
		if c == " " {
			lastSpace = row.Len()
			lastSpaceW = rowW
		}
		row.WriteString(c)
		rowW += cw
	}
	rows = append(rows, row.String())
	return rows
}

// wrapRanges wraps a cluster slice into rows of at most w cells, breaking
// at spaces when possible and mid-word otherwise. It returns [start, end)
// cluster index ranges per row; the single break space between two
// word-wrapped rows belongs to no row. Empty input yields one empty row.
func wrapRanges(cs []string, w int, measure func(string) int) [][2]int {
	if len(cs) == 0 || w <= 0 {
		return [][2]int{{0, len(cs)}}
	}
	var rows [][2]int
	start, rowW, lastSpace := 0, 0, -1
	i := 0
	for i < len(cs) {
		cw := measure(cs[i])
		if rowW+cw > w && rowW > 0 {
			if lastSpace >= start {
				rows = append(rows, [2]int{start, lastSpace})
				start = lastSpace + 1 // the break space belongs to no row
			} else {
				rows = append(rows, [2]int{start, i})
				start = i
			}
			lastSpace = -1
			rowW = 0
			for j := start; j < i; j++ {
				rowW += measure(cs[j])
			}
			continue
		}
		if cs[i] == " " {
			lastSpace = i
		}
		rowW += cw
		i++
	}
	rows = append(rows, [2]int{start, len(cs)})
	return rows
}

// drawText paints s cluster by cluster at (x, y), stopping at the surface's
// nominal width. It returns the x position after the last painted cell.
func drawText(sur tui.Surface, x, y int, s string, st style.Style) int {
	w := sur.Size().W
	for c := range tui.Graphemes(s) {
		cw := sur.StringWidth(c)
		if x+cw > w {
			break
		}
		sur.SetCell(x, y, c, st)
		x += cw
	}
	return x
}

// paintScrollIndicator paints the minimal per-widget scroll indicator
// column (chrome, not a widget): a proportional thumb at
// column x over h rows, for a viewport whose first visible unit is top out
// of total.
func paintScrollIndicator(s tui.Surface, x, h, top, total int) {
	if h <= 0 || total <= 0 {
		return
	}
	track := style.New().Foreground(style.TokenTextMuted).Faint(true)
	s.Fill(tui.Rect{X: x, Y: 0, W: 1, H: h}, "│", track)
	denom := max(total-1, 1)
	ty := min(top, denom) * (h - 1) / denom
	s.SetCell(x, ty, "█", style.New().Foreground(style.TokenBorder))
}
