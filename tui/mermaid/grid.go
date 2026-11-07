// Package mermaid draws parse/mermaid's diagrams into a terminal's cells with box-drawing
// characters: the model is parse/mermaid's, the positions graph/layout's, only the painting is
// its own. It imports no gui package, so a terminal-only binary builds it.
//
// A diagram is laid out once into a Grid, in cells, then painted into any tui.Surface at an
// origin the host scrolls. Boxes are outlines around their labels; lines are per-cell direction
// masks, OR'd where they meet, so a junction (├ ┬ ┼) comes out right with no collision logic.
// Kinds reports the diagram kinds drawn; a host shows anything else its own way.
//
// THE SHARED LAYER: every kind draws through the Grid's layers and builders (the title, capped
// labels, the legend and footnotes, the axis, the forms), each written once; a kind's file holds
// only its own layout and registers itself (kind.go). A layer is visited in one place (each, shift),
// so settling, measuring and painting can't miss one.
package mermaid

import (
	"context"
	"errors"
	"math"

	"github.com/yongjohnlee80/golib/graph/layout"
	"github.com/yongjohnlee80/golib/tui"
)

// ErrUnsupported is Lay's answer for a diagram kind it doesn't draw.
var ErrUnsupported = errors.New("mermaid: this diagram kind is not drawn in cells")

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
	// frames are drawn under every other layer: a subgraph's outline (frames.go)
	frames []frame
	lines  map[cell]dirs
	// stroke is a line cell's stroke once finish has resolved it; strokes, the strokes the lines
	// through each cell were drawn with (addStroke), one bit each.
	stroke  map[cell]Stroke
	strokes map[cell]uint8
	marks   map[cell]string // drawn over a line's cell in the line's style: an arrowhead, a dash
	fills   map[cell]string // drawn over the marks in the box style: a bar, a slice
	series  map[cell]int    // a fill's series, its index plus one: drawn in its colour (Theme.Series)
	texts   []text          // labels outside any box, in the label's style
	notes   []legendRow     // the legend, placed under the drawing by finish
	foot    int             // footnotes given so far: the next one's number less one
	hits    []hit           // a capped label's cells and its full text (HitAt)
}

// text is a label at a cell, outside any box, w cells wide.
type text struct {
	x, y, w int
	s       string
}

// newGrid is an empty grid.
func newGrid() *Grid {
	return &Grid{lines: map[cell]dirs{}, stroke: map[cell]Stroke{}, strokes: map[cell]uint8{},
		marks: map[cell]string{}, fills: map[cell]string{}, series: map[cell]int{}}
}

// Box is a node's outline and label. ID is the model's node ID, so a host maps a selection back.
type Box struct {
	ID    string
	Label string // as the model has it, whole
	Rect  tui.Rect
	Form  Form
	// Ports are the sides a line may meet the outline at, chosen by the kind (a mind map's left and
	// right, a sequence participant's top and bottom); the form says which sides can (forms). A
	// side takes a junction (┤ ├ ┴ ┬) at any of its cells a line points into.
	Ports Port
	// Sections are a box's rows by section, drawn in place of its label: a class's name, attributes
	// and methods, a state's name and descriptions. The head section is centred, a rule between
	// sections; nil: the label alone.
	Sections      [][]string
	shown         string     // Label as drawn: capped (capLabel)
	sectionsShown [][]string // Sections as drawn: each row capped
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

// Stroke is how a line is drawn: light, dashed (a dotted edge), thick, or heavy (an activation's
// run, light lines meeting it from the side).
type Stroke uint8

const (
	StrokeLight Stroke = iota
	StrokeDashed
	StrokeThick
	StrokeHeavy
)

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

// addStroke records that a line drawn with s passes through c. finish resolves each cell: the one
// stroke when every line through it agrees, else light, so a dashed line never reads as cut and a
// junction where strokes meet is drawn light, never in mixed glyphs.
func (g *Grid) addStroke(c cell, s Stroke) { g.strokes[c] |= 1 << s }

// resolveStrokes is finish's: each cell's strokes to its one stroke.
func (g *Grid) resolveStrokes() {
	for c, bits := range g.strokes {
		g.stroke[c] = StrokeLight
		for s := StrokeDashed; s <= StrokeHeavy; s++ {
			if bits == 1<<s {
				g.stroke[c] = s
			}
		}
	}
}

// lineOf is the character a line cell is painted with.
func (g *Grid) lineOf(c cell, d dirs) string {
	switch g.stroke[c] {
	case StrokeHeavy:
		if r, ok := heavyRune[d]; ok {
			return r
		}
	case StrokeDashed:
		if r, ok := dashRune[d]; ok {
			return r
		}
	case StrokeThick:
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

// Paint draws g into s with g's (0, 0) at (x0, y0), clipped by s: a host scrolls by moving the
// origin. selected is a Boxes index, -1 for none: its outline and label take th.Selected.
func (g *Grid) Paint(s tui.Surface, x0, y0, selected int, th Theme) {
	// frames under all, then the lines (a line crossing a frame draws its own character there), their
	// marks, the fills, the labels and the legend, boxes over all: an outline owns its cells
	g.paintFrames(s, x0, y0, th)
	for c, d := range g.lines {
		s.SetCell(x0+c.x, y0+c.y, g.lineOf(c, d), th.Style(ElemLine))
	}
	for c, m := range g.marks {
		s.SetCell(x0+c.x, y0+c.y, m, th.Style(ElemMark))
	}
	for c, f := range g.fills {
		s.SetCell(x0+c.x, y0+c.y, f, th.seriesStyle(g.series[c]))
	}
	for _, t := range g.texts {
		tui.DrawText(s, x0+t.x, y0+t.y, t.s, th.Style(ElemText))
	}
	for i, b := range g.Boxes {
		box, label := th.Style(ElemBox), th.Style(ElemText)
		if i == selected {
			box, label = th.Style(ElemSelected), th.Style(ElemSelected)
		}
		paintBox(s, b, x0, y0, g.lines, box, label)
	}
}

// each visits a cell at each extreme of everything any layer holds: a box's and a text's two
// corners, every line, mark and fill cell. settle and measure read this one walk, so a layer is
// added in one place.
func (g *Grid) each(visit func(x, y int)) {
	for _, f := range g.frames {
		visit(f.rect.X, f.rect.Y)
		visit(f.rect.X+f.rect.W-1, f.rect.Y+f.rect.H-1)
	}
	for _, b := range g.Boxes {
		visit(b.Rect.X, b.Rect.Y)
		visit(b.Rect.X+b.Rect.W-1, b.Rect.Y+b.Rect.H-1)
	}
	for c := range g.lines {
		visit(c.x, c.y)
	}
	for c := range g.marks {
		visit(c.x, c.y)
	}
	for c := range g.fills {
		visit(c.x, c.y)
	}
	for _, t := range g.texts {
		visit(t.x, t.y)
		visit(t.x+max(t.w, 1)-1, t.y)
	}
}

// shift moves everything by (dx, dy): every layer, and the hover regions with their labels.
func (g *Grid) shift(dx, dy int) {
	if dx == 0 && dy == 0 {
		return
	}
	for i := range g.Boxes {
		g.Boxes[i].Rect.X += dx
		g.Boxes[i].Rect.Y += dy
	}
	move := func(c cell) cell { return cell{c.x + dx, c.y + dy} }
	for i := range g.frames {
		g.frames[i].rect.X += dx
		g.frames[i].rect.Y += dy
		g.frames[i].tx += dx
		g.frames[i].ty += dy
		if g.frames[i].meets != nil {
			g.frames[i].meets = shiftCells(g.frames[i].meets, move)
		}
	}
	g.lines = shiftCells(g.lines, move)
	g.stroke = shiftCells(g.stroke, move)
	g.strokes = shiftCells(g.strokes, move)
	g.marks = shiftCells(g.marks, move)
	g.fills = shiftCells(g.fills, move)
	g.series = shiftCells(g.series, move)
	for i := range g.texts {
		g.texts[i].x += dx
		g.texts[i].y += dy
	}
	for i := range g.hits {
		g.hits[i].r.X += dx
		g.hits[i].r.Y += dy
	}
}

// shiftCells is a cell layer moved by move.
func shiftCells[V any](layer map[cell]V, move func(cell) cell) map[cell]V {
	out := make(map[cell]V, len(layer))
	for c, v := range layer {
		out[move(c)] = v
	}
	return out
}

// settle moves everything so the leftmost and topmost cell is at 0, and sets Size: a layout may
// place a note left of the first lifeline, at a negative column.
func (g *Grid) settle() {
	minX, minY := math.MaxInt, math.MaxInt
	g.each(func(x, y int) { minX, minY = min(minX, x), min(minY, y) })
	if minX == math.MaxInt {
		return
	}
	g.shift(-minX, -minY)
	g.measure()
}

// measure sets Size to cover every cell of every layer.
func (g *Grid) measure() {
	g.each(func(x, y int) { g.Size.W, g.Size.H = max(g.Size.W, x+1), max(g.Size.H, y+1) })
}

// maxCells bounds a laid-out grid's area: Lay refuses a diagram larger, layout.ErrTooLarge, for
// the host's "too large" notice.
const maxCells = 1 << 22

// tooLarge reports whether g's area is past maxCells.
func (g *Grid) tooLarge() bool { return int64(g.Size.W)*int64(g.Size.H) > maxCells }

// guard is every Lay's first step: a model past its kind's bound (n of max) is layout.ErrTooLarge,
// for the host's "too large" notice, and a cancelled ctx its error.
func guard(ctx context.Context, n, max int) error {
	if n > max {
		return layout.ErrTooLarge
	}
	return ctx.Err()
}

// finish is every Lay's last step: the strokes resolved, the frames' titles placed where no line
// crosses them, everything settled at the origin, the legend placed under the drawing (measured by
// m), and the size checked (layout.ErrTooLarge past maxCells).
func (g *Grid) finish(m Measurer) (*Grid, error) {
	g.resolveStrokes()
	g.placeFrameTitles(m)
	g.settle()
	g.placeLegend(m)
	if g.tooLarge() {
		return nil, layout.ErrTooLarge
	}
	return g, nil
}
