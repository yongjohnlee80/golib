package mermaid

import "github.com/yongjohnlee80/golib/tui/style"

// Theme is the painter's styles: a host derives them from its own palette, so a diagram follows
// the user's theme.
type Theme struct {
	Line, Box, Label, Selected style.Style
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
