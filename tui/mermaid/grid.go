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
	"math"
	"slices"

	pm "github.com/yongjohnlee80/golib/parse/mermaid"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// ErrUnsupported is Lay's answer for a diagram kind it doesn't draw.
var ErrUnsupported = errors.New("mermaid: this diagram kind is not drawn in cells")

// Kinds reports the diagram kinds this package draws; a host shows any other its own way.
func Kinds() []pm.Kind {
	return []pm.Kind{pm.Mindmap, pm.Timeline, pm.Journey, pm.Sequence, pm.Flowchart}
}

// Supported reports whether k is one of Kinds.
func Supported(k pm.Kind) bool { return slices.Contains(Kinds(), k) }

// Measurer is the one thing a layout needs from a surface: text width in cells. tui.Surface
// satisfies it; a host measures through its surface, never a package default, so the width
// policy the frame paints with is the one the layout used.
type Measurer interface{ StringWidth(s string) int }

// Grid is a laid-out diagram in cells, painted later into any surface of at least Size.
type Grid struct {
	Size tui.Size
	// Boxes are the diagram's outlines. A mind map's are its nodes, in the model's order: Boxes[i]
	// is node i. A sequence's start with its participants (Boxes[i] is participant i).
	Boxes []Box
	lines map[cell]dirs
	heavy map[cell]bool // line cells drawn heavy where they run north–south: a sequence's activations
	// stroke is how a line cell is drawn when every line through it is drawn that way: a flowchart's
	// dotted and thick edges. A cell where strokes meet (or a light line) is drawn light.
	stroke map[cell]stroke
	marks  map[cell]string // drawn over a line's cell in the line's style: an arrowhead, a dash
	texts  []text          // labels outside any box, in the label's style
}

// text is a label at a cell, outside any box, w cells wide.
type text struct {
	x, y, w int
	s       string
}

// label puts s at (x, y), measured by m.
func (g *Grid) label(x, y int, s string, m Measurer) {
	if s != "" {
		g.texts = append(g.texts, text{x: x, y: y, w: m.StringWidth(s), s: s})
	}
}

// newGrid is an empty grid.
func newGrid() *Grid {
	return &Grid{lines: map[cell]dirs{}, heavy: map[cell]bool{}, marks: map[cell]string{}, stroke: map[cell]stroke{}}
}

// Box is a node's outline and label. ID is the model's node ID, so a host maps a selection back.
type Box struct {
	ID    string
	Label string
	Rect  tui.Rect
	Form  Form
	// Stem: a line may meet the outline's top or bottom at its middle column (a sequence's
	// lifeline), and the outline takes a junction there (┴ ┬).
	Stem bool
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

// heavyRune is the character of a heavy cell that runs north–south (an activation), light lines
// meeting it from the side; a heavy cell of any other dirs is drawn light.
var heavyRune = map[dirs]string{
	north:                       "╹",
	south:                       "╻",
	north | south:               "┃",
	north | south | east:        "┠",
	north | south | west:        "┨",
	north | south | east | west: "╂",
}

// stroke is a line's drawing: light, dashed (a dotted edge) or thick.
type stroke uint8

const (
	strokeLight stroke = iota
	strokeDashed
	strokeThick
)

// dashRune is a dashed line's character where it runs straight; its corners, junctions and ends are
// drawn light, so a turn still reads.
var dashRune = map[dirs]string{north | south: "┆", east | west: "┄"}

// thickRune is every dirs value's character in the heavy set.
var thickRune = [16]string{
	0:                           " ",
	north:                       "╹",
	east:                        "╺",
	south:                       "╻",
	west:                        "╸",
	north | south:               "┃",
	east | west:                 "━",
	south | east:                "┏",
	south | west:                "┓",
	north | east:                "┗",
	north | west:                "┛",
	north | south | east:        "┣",
	north | south | west:        "┫",
	south | east | west:         "┳",
	north | east | west:         "┻",
	north | south | east | west: "╋",
}

// lineOf is the character a line cell is painted with.
func (g *Grid) lineOf(c cell, d dirs) string {
	if g.heavy[c] {
		if r, ok := heavyRune[d]; ok {
			return r
		}
	}
	switch g.stroke[c] {
	case strokeDashed:
		if r, ok := dashRune[d]; ok {
			return r
		}
	case strokeThick:
		return thickRune[d]
	}
	return lineRune[d]
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
	// lines first, then their marks and the labels, boxes over all: an outline owns its cells
	for c, d := range g.lines {
		s.SetCell(x0+c.x, y0+c.y, g.lineOf(c, d), th.Line)
	}
	for c, m := range g.marks {
		s.SetCell(x0+c.x, y0+c.y, m, th.Line)
	}
	for _, t := range g.texts {
		tui.DrawText(s, x0+t.x, y0+t.y, t.s, th.Label)
	}
	for i, b := range g.Boxes {
		box, label := th.Box, th.Label
		if i == selected {
			box, label = th.Selected, th.Selected
		}
		paintBox(s, b, x0, y0, g.lines, box, label)
	}
}

// settle moves everything so the leftmost and topmost cell is at 0, and sets Size: a layout may
// place a note left of the first lifeline, at a negative column.
func (g *Grid) settle() {
	minX, minY := math.MaxInt, math.MaxInt
	for _, b := range g.Boxes {
		minX, minY = min(minX, b.Rect.X), min(minY, b.Rect.Y)
	}
	for c := range g.lines {
		minX, minY = min(minX, c.x), min(minY, c.y)
	}
	for c := range g.marks {
		minX, minY = min(minX, c.x), min(minY, c.y)
	}
	for _, t := range g.texts {
		minX, minY = min(minX, t.x), min(minY, t.y)
	}
	if minX == math.MaxInt {
		return
	}
	for i := range g.Boxes {
		g.Boxes[i].Rect.X -= minX
		g.Boxes[i].Rect.Y -= minY
	}
	shift := func(c cell) cell { return cell{c.x - minX, c.y - minY} }
	lines, heavy, marks, strokes := map[cell]dirs{}, map[cell]bool{}, map[cell]string{}, map[cell]stroke{}
	for c, d := range g.lines {
		lines[shift(c)] = d
	}
	for c := range g.heavy {
		heavy[shift(c)] = true
	}
	for c, st := range g.stroke {
		strokes[shift(c)] = st
	}
	for c, m := range g.marks {
		marks[shift(c)] = m
	}
	g.lines, g.heavy, g.marks, g.stroke = lines, heavy, marks, strokes
	for i := range g.texts {
		g.texts[i].x -= minX
		g.texts[i].y -= minY
	}
	g.measure()
}

// maxCells bounds a laid-out grid's area: Lay refuses a diagram larger, layout.ErrTooLarge, for
// the host's "too large" notice.
const maxCells = 1 << 22

// tooLarge reports whether g's area is past maxCells.
func (g *Grid) tooLarge() bool { return int64(g.Size.W)*int64(g.Size.H) > maxCells }
