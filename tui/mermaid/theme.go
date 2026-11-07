package mermaid

import "github.com/yongjohnlee80/golib/tui/style"

// Theme is the painter's styles: a host derives them from its own palette, so a diagram follows
// the user's theme. Series are the colours a kind's series take (a pie's slices, and each slice's
// legend swatch), in order, cycled: a host gives its charts' series (tui/vegalite's
// ThemeFromSyntax), so a pie and a chart on one page agree. None: a series fill takes the Box
// style, its densities alone telling the series apart.
type Theme struct {
	Line, Box, Label, Selected style.Style
	Series                     []style.ColorSpec
}

// TokenTheme is the diagram theme a cell page paints in: every style on the page's own
// background (style.PageText), the foregrounds the page's tokens name — edges as the text (a
// diagram's lines are its content, and the border's tone is under 2:1 on the light themes' pages),
// outlines in the document's accent, labels as the text. The page and the Mindmap pane both use it,
// so the page's background follows the theme by construction, never the terminal's own.
func TokenTheme() Theme {
	return Theme{
		Line:     style.PageText(),
		Box:      style.PageText().Foreground(style.TokenAccentDocument),
		Label:    style.PageText(),
		Selected: style.PageText().Reverse(true),
	}
}

// Element is what a cell draws. Each takes one of the Theme's styles (Style), in this one place:
// a kind draws elements, never a style of its own, so every diagram follows the host's theme.
type Element uint8

const (
	ElemLine     Element = iota // a line and its junctions
	ElemFrame                   // a frame's border: a subgraph's
	ElemMark                    // a mark on a line: an arrowhead, a dash, a grid column's ┊
	ElemFill                    // a filled cell: a bar, a slice
	ElemText                    // a label outside a box, a title, a legend row, a cap's …
	ElemBox                     // a box's outline
	ElemSelected                // the selected box's outline and label
)

// seriesStyle is series n's style (its index plus one) for a fill: the Box style in the series'
// colour, cycled; n 0, or a theme without series, the Box style.
func (th Theme) seriesStyle(n int) style.Style {
	st := th.Style(ElemFill)
	if n > 0 && len(th.Series) > 0 {
		st = st.Foreground(th.Series[(n-1)%len(th.Series)])
	}
	return st
}

// Style is e's style in th.
func (th Theme) Style(e Element) style.Style {
	switch e {
	case ElemLine, ElemFrame, ElemMark:
		return th.Line
	case ElemFill, ElemBox:
		return th.Box
	case ElemSelected:
		return th.Selected
	}
	return th.Label
}
