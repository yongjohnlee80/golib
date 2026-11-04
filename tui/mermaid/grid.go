// Package mermaid draws parse/mermaid's diagrams into a terminal's cells with box-drawing
// characters: the model is parse/mermaid's, the positions graph/layout's, only the painting is
// its own. It imports no gui package, so a terminal-only binary builds it.
//
// A diagram is laid out once into a Grid, in cells, then painted into any tui.Surface at an
// origin the host scrolls. Boxes are outlines around their labels; lines are per-cell direction
// masks, OR'd where they meet, so a junction (├ ┬ ┼) comes out right with no collision logic.
// Kinds reports the diagram kinds drawn; a host shows anything else its own way.
package mermaid

import (
	"errors"
	"slices"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// ErrUnsupported is Lay's answer for a diagram kind it doesn't draw.
var ErrUnsupported = errors.New("mermaid: this diagram kind is not drawn in cells")

// Kinds reports the diagram kinds this package draws; a host shows any other its own way.
func Kinds() []pm.Kind { return []pm.Kind{pm.Mindmap} }

// Supported reports whether k is one of Kinds.
func Supported(k pm.Kind) bool { return slices.Contains(Kinds(), k) }

// Measurer is the one thing a layout needs from a surface: text width in cells. tui.Surface
// satisfies it; a host measures through its surface, never a package default, so the width
// policy the frame paints with is the one the layout used.
type Measurer interface{ StringWidth(s string) int }

// Grid is a laid-out diagram in cells, painted later into any surface of at least Size.
type Grid struct {
	Size  tui.Size
	Boxes []Box // one per node, in the model's order: Boxes[i] is the diagram's node i
	lines map[cell]dirs
}

// Box is a node's outline and label. ID is the model's node ID, so a host maps a selection back.
type Box struct {
	ID    string
	Label string
	Rect  tui.Rect
	Form  Form
}

// Centre is b's middle cell.
func (b Box) Centre() (x, y int) { return b.Rect.X + b.Rect.W/2, b.Rect.Y + b.Rect.H/2 }

type cell struct{ x, y int }

// dirs is the set of directions a line leaves a cell by. Lines OR their dirs into a cell, so two
// lines meeting make a junction with no special case: N|S|E is ├, N|S|E|W is ┼.
type dirs uint8

const (
	north dirs = 1 << iota
	east
	south
	west
)

// lineRune is every dirs value's character: the light box-drawing set, and ╴╵╶╷ for a line's
// dead end. Zero is never painted.
var lineRune = [16]string{
	0:                           " ",
	north:                       "╵",
	east:                        "╶",
	south:                       "╷",
	west:                        "╴",
	north | south:               "│",
	east | west:                 "─",
	south | east:                "┌",
	south | west:                "┐",
	north | east:                "└",
	north | west:                "┘",
	north | south | east:        "├",
	north | south | west:        "┤",
	south | east | west:         "┬",
	north | east | west:         "┴",
	north | south | east | west: "┼",
}

// link joins two neighbouring cells with a line: a's side toward b and b's side toward a.
func (g *Grid) link(a, b cell) {
	switch {
	case b.x == a.x+1 && b.y == a.y:
		g.lines[a] |= east
		g.lines[b] |= west
	case b.x == a.x-1 && b.y == a.y:
		g.lines[a] |= west
		g.lines[b] |= east
	case b.y == a.y+1 && b.x == a.x:
		g.lines[a] |= south
		g.lines[b] |= north
	case b.y == a.y-1 && b.x == a.x:
		g.lines[a] |= north
		g.lines[b] |= south
	}
}

// run lines the straight path from a to b, both included: one of x or y is the same in both.
func (g *Grid) run(a, b cell) {
	step := func(v, to int) int {
		switch {
		case v < to:
			return v + 1
		case v > to:
			return v - 1
		}
		return v
	}
	for c := a; c != b; {
		n := cell{step(c.x, b.x), step(c.y, b.y)}
		if c.x != b.x && c.y != b.y {
			n.y = c.y // a run is straight: x first if both differ (never asked)
		}
		g.link(c, n)
		c = n
	}
}

// Theme is the painter's styles: a host derives them from its own palette, so a diagram follows
// the user's theme.
type Theme struct {
	Line, Box, Label, Selected style.Style
}

// Paint draws g into s with g's (0, 0) at (x0, y0), clipped by s: a host scrolls by moving the
// origin. selected is a Boxes index, -1 for none: its outline and label take th.Selected.
func (g *Grid) Paint(s tui.Surface, x0, y0, selected int, th Theme) {
	for c, d := range g.lines { // lines first, boxes over them: an outline owns its cells
		s.SetCell(x0+c.x, y0+c.y, lineRune[d], th.Line)
	}
	for i, b := range g.Boxes {
		box, label := th.Box, th.Label
		if i == selected {
			box, label = th.Selected, th.Selected
		}
		paintBox(s, b, x0, y0, g.lines, box, label)
	}
}
