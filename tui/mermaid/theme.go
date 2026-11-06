package mermaid

import "github.com/yongjohnlee80/golib/tui/style"

// Theme is the painter's styles: a host derives them from its own palette, so a diagram follows
// the user's theme.
type Theme struct {
	Line, Box, Label, Selected style.Style
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
