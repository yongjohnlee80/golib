package mermaid

import "github.com/yongjohnlee80/golib/tui"

// frame is a titled, rounded rectangle drawn under the lines: a flowchart's subgraph (a state's
// composite later). It is a layer of its own, never OR'd into the line masks, so a line crossing a
// frame draws its own character there, never a junction with it. A kind keeps its lines from
// running along a border; finish places the title where no line crosses it (placeFrameTitles).
type frame struct {
	rect   tui.Rect
	title  string // as the model has it, whole
	shown  string // the title as drawn on a border, capped; "" when it went to the legend
	tx, ty int    // where shown starts
	mark   string // the footnote mark after the top-left corner, when the title had no room
}

// frame adds a frame round r, titled title (none: ""); finish places the title.
func (g *Grid) frame(r tui.Rect, title string) {
	g.frames = append(g.frames, frame{rect: r, title: title})
}

// onFrame reports whether c is on any frame's border: a kind keeps its labels and marks off them.
func (g *Grid) onFrame(c cell) bool {
	for _, f := range g.frames {
		if onBorder(f.rect, c) {
			return true
		}
	}
	return false
}

// onBorder reports whether c is one of r's outermost cells.
func onBorder(r tui.Rect, c cell) bool {
	if c.x < r.X || c.x >= r.X+r.W || c.y < r.Y || c.y >= r.Y+r.H {
		return false
	}
	return c.x == r.X || c.x == r.X+r.W-1 || c.y == r.Y || c.y == r.Y+r.H-1
}

// placeFrameTitles is finish's, once every line is drawn: each frame's title, capped, on its top
// border at the first place no line crosses it or the space either side (╭─ Title ───╮), else on its
// bottom border, else a footnote: its mark in the cell after the top-left corner, which a kind keeps
// clear of lines, and the title in the legend. A capped title reveals itself whole (HitAt).
func (g *Grid) placeFrameTitles(m Measurer) {
	for i := range g.frames {
		f := &g.frames[i]
		if f.title == "" {
			continue
		}
		shown, w, capped := capLabel(f.title, RoleLabel, m)
		crossed := func(x, y int) bool {
			_, ok := g.lines[cell{x, y}]
			return ok
		}
		place := func(y int) bool {
			for x := f.rect.X + 3; x+w <= f.rect.X+f.rect.W-3; x++ {
				clear := true
				for c := x - 1; c <= x+w && clear; c++ {
					clear = !crossed(c, y)
				}
				if clear {
					f.shown, f.tx, f.ty = shown, x, y
					if capped {
						g.hits = append(g.hits, hit{r: tui.Rect{X: x, Y: y, W: w, H: 1}, full: f.title})
					}
					return true
				}
			}
			return false
		}
		if !place(f.rect.Y) && !place(f.rect.Y+f.rect.H-1) {
			f.mark = g.footnote(f.title)
		}
	}
}

// paintFrames draws the frames at the origin, under everything else: rounded corners, ─ and │
// borders in the frame's style, the title (or its footnote mark) in the text's.
func (g *Grid) paintFrames(s tui.Surface, x0, y0 int, th Theme) {
	line, text := th.Style(ElemFrame), th.Style(ElemText)
	for _, f := range g.frames {
		r := f.rect
		if r.W < 2 || r.H < 2 {
			continue
		}
		left, right, top, bottom := x0+r.X, x0+r.X+r.W-1, y0+r.Y, y0+r.Y+r.H-1
		for x := left + 1; x < right; x++ {
			s.SetCell(x, top, "─", line)
			s.SetCell(x, bottom, "─", line)
		}
		for y := top + 1; y < bottom; y++ {
			s.SetCell(left, y, "│", line)
			s.SetCell(right, y, "│", line)
		}
		s.SetCell(left, top, "╭", line)
		s.SetCell(right, top, "╮", line)
		s.SetCell(left, bottom, "╰", line)
		s.SetCell(right, bottom, "╯", line)
		if f.shown != "" {
			tui.DrawText(s, x0+f.tx-1, y0+f.ty, " "+f.shown+" ", text)
		}
		if f.mark != "" {
			tui.DrawText(s, left+1, top, f.mark, text)
		}
	}
}
