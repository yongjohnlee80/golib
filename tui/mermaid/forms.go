package mermaid

import (
	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Form is a box's outline. Mermaid's shapes have no exact cell equivalent; each maps to the
// closest corner set.
type Form uint8

const (
	FormText    Form = iota // MindDefault: no outline, the label alone
	FormSquare              // MindSquare: ┌ ┐ └ ┘
	FormRounded             // MindRounded, MindCircle, MindCloud: ╭ ╮ ╰ ╯
	FormHexagon             // MindHexagon: ╱ ╲ corners, the label between < >
	FormBang                // MindBang: heavy ┏ ┓ ┗ ┛
)

// FormOf is the outline a mind map's shape is drawn with.
func FormOf(s pm.MindShape) Form {
	switch s {
	case pm.MindSquare:
		return FormSquare
	case pm.MindRounded, pm.MindCircle, pm.MindCloud:
		return FormRounded
	case pm.MindHexagon:
		return FormHexagon
	case pm.MindBang:
		return FormBang
	}
	return FormText
}

// Port is a set of a box's sides a line may meet.
type Port uint8

const (
	PortLeft Port = 1 << iota
	PortRight
	PortTop
	PortBottom

	PortSides = PortLeft | PortRight             // a mind map's, a spine's: lines meet a box's sides
	PortAll   = PortSides | PortTop | PortBottom // a participant's: its lifeline at its top and bottom too
)

// formSpec is one outline: its corners and sides, its size around a label, and the sides that can
// take a junction where a line points in (the heavy and the slanted outlines can't).
type formSpec struct {
	tl, tr, bl, br, h, left, right string
	ports                          Port
}

// forms is every outline the kinds draw, one row each.
var forms = [...]formSpec{
	FormText:    {},
	FormSquare:  {"┌", "┐", "└", "┘", "─", "│", "│", PortAll},
	FormRounded: {"╭", "╮", "╰", "╯", "─", "│", "│", PortAll},
	FormHexagon: {"╱", "╲", "╲", "╱", "─", "<", ">", 0},
	FormBang:    {"┏", "┓", "┗", "┛", "━", "┃", "┃", 0},
}

// formSize is a box's size around a label lw cells wide: an outline adds a border and a space
// each side, and a row above and below.
func formSize(f Form, lw int) (w, h int) {
	if f == FormText {
		return max(lw, 1), 1
	}
	return lw + 4, 3
}

// drawn is b's label as painted: capped when the layout capped it, else as given.
func (b Box) drawn() string {
	if b.shown != "" {
		return b.shown
	}
	return b.Label
}

// paintBox draws b's outline and label at the origin. A side among its ports (and its form's) takes
// a junction at each cell a line points into, where the outline is light: ┤ ├ on the sides, ┴ ┬
// on the top and bottom.
func paintBox(s tui.Surface, b Box, x0, y0 int, lines map[cell]dirs, box, label style.Style) {
	r := tui.Rect{X: x0 + b.Rect.X, Y: y0 + b.Rect.Y, W: b.Rect.W, H: b.Rect.H}
	if b.Form == FormText {
		tui.DrawText(s, r.X, r.Y, b.drawn(), label)
		return
	}
	f := forms[b.Form]
	ports := b.Ports & f.ports
	right, bottom, mid := r.X+r.W-1, r.Y+r.H-1, r.Y+r.H/2
	s.SetCell(r.X, r.Y, f.tl, box)
	s.SetCell(right, r.Y, f.tr, box)
	s.SetCell(r.X, bottom, f.bl, box)
	s.SetCell(right, bottom, f.br, box)
	for x := r.X + 1; x < right; x++ {
		top, bot := f.h, f.h
		gx := x - x0
		if ports&PortTop != 0 && lines[cell{gx, b.Rect.Y - 1}]&south != 0 {
			top = "┴"
		}
		if ports&PortBottom != 0 && lines[cell{gx, b.Rect.Y + b.Rect.H}]&north != 0 {
			bot = "┬"
		}
		s.SetCell(x, r.Y, top, box)
		s.SetCell(x, bottom, bot, box)
	}
	for y := r.Y + 1; y < bottom; y++ {
		left, rightSide := f.left, f.right
		gy := y - y0
		if ports&PortLeft != 0 && lines[cell{b.Rect.X - 1, gy}]&east != 0 {
			left = "┤"
		}
		if ports&PortRight != 0 && lines[cell{b.Rect.X + b.Rect.W, gy}]&west != 0 {
			rightSide = "├"
		}
		s.SetCell(r.X, y, left, box)
		s.SetCell(right, y, rightSide, box)
		s.Fill(tui.Rect{X: r.X + 1, Y: y, W: r.W - 2, H: 1}, " ", label)
	}
	tui.DrawText(s, r.X+2, mid, b.drawn(), label)
}
