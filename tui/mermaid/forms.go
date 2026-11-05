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
	FormDot                 // a state diagram's start: ● alone, no outline, no label
	FormEndDot              // its end: ◉ alone
	FormBar                 // a fork or a join: a bar ━━━━━, no label
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
// take a junction where a line points in: none on the heavy outline, and on the hexagon only its
// light top and bottom (a flowchart's decision takes its edges there), never its < > sides.
type formSpec struct {
	tl, tr, bl, br, h, left, right string
	ports                          Port
	glyph                          string // a form drawn as one glyph in its middle, no outline (a dot); "" an outline
}

// acrossRanks reports whether a form's size runs across the ranks whichever way they run (a dot, a
// bar), not as it reads: a fork's bar lies across its edges, and a dot keeps a port of its own, when
// the ranks run across (LR, RL) as when they run down.
func (f formSpec) acrossRanks() bool { return f.glyph != "" || f.tl == "" && f.h != "" }

// forms is every outline the kinds draw, one row each.
var forms = [...]formSpec{
	FormText:    {},
	FormSquare:  {"┌", "┐", "└", "┘", "─", "│", "│", PortAll, ""},
	FormRounded: {"╭", "╮", "╰", "╯", "─", "│", "│", PortAll, ""},
	FormHexagon: {"╱", "╲", "╲", "╱", "─", "<", ">", PortTop | PortBottom, ""},
	FormBang:    {"┏", "┓", "┗", "┛", "━", "┃", "┃", 0, ""},
	FormDot:     {glyph: "●"},
	FormEndDot:  {glyph: "◉"},
	FormBar:     {h: "━"},
}

// formSize is a box's size around a label lw cells wide: an outline adds a border and a space
// each side, and a row above and below. A dot is three cells, its glyph in the middle (so a line
// meets it at a column of its own, as at a box's port; the router widens one that a cycle runs
// through), and a bar five: neither has a label.
func formSize(f Form, lw int) (w, h int) {
	switch f {
	case FormText:
		return max(lw, 1), 1
	case FormDot, FormEndDot:
		return 3, 1
	case FormBar:
		return 5, 1
	}
	return lw + 4, 3
}

// sectionsSize is a sectioned box's size (Box.Sections): its widest row, capped, with a border and
// a space each side; a row per row, a rule between sections, and the border above and below.
func sectionsSize(sections [][]string, m Measurer) (w, h int) {
	for _, sec := range sections {
		for _, row := range sec {
			_, rw, _ := capLabel(row, RoleLabel, m)
			w = max(w, rw)
		}
		h += len(sec)
	}
	return w + 4, h + max(len(sections)-1, 0) + 2
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
	switch {
	case f.glyph != "": // a dot: its glyph in the middle
		s.SetCell(r.X+r.W/2, r.Y+r.H/2, f.glyph, box)
		return
	case f.tl == "": // a bar: its rule, across the ranks (━ when they run down, ┃ when across)
		if r.H > r.W {
			for y := r.Y; y < r.Y+r.H; y++ {
				s.SetCell(r.X+r.W/2, y, "┃", box)
			}
			return
		}
		for x := r.X; x < r.X+r.W; x++ {
			s.SetCell(x, r.Y+r.H/2, f.h, box)
		}
		return
	}
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
	if len(b.Sections) == 0 {
		tui.DrawText(s, r.X+2, mid, b.drawn(), label)
		return
	}
	// a sectioned box: its head section's rows centred, each other's left-aligned, a rule (├──┤)
	// between sections; a row as the layout capped it
	y := r.Y + 1
	for si, sec := range b.shownSections() {
		if si > 0 {
			s.SetCell(r.X, y, "├", box)
			for x := r.X + 1; x < right; x++ {
				s.SetCell(x, y, "─", box)
			}
			s.SetCell(right, y, "┤", box)
			y++
		}
		for _, row := range sec {
			x := r.X + 2
			if si == 0 {
				x = r.X + (r.W-tui.StringWidth(row))/2
			}
			tui.DrawText(s, x, y, row, label)
			y++
		}
	}
}

// shownSections is b's sections as painted: capped when the layout capped them, else as given.
func (b Box) shownSections() [][]string {
	if b.sectionsShown != nil {
		return b.sectionsShown
	}
	return b.Sections
}
