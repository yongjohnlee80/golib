package flow

import (
	"strings"

	"github.com/yongjohnlee80/golib/gui"
)

// Paint draws the paragraph with its top-left at at. spans are the ones it was laid out from;
// passing them again lets a caller recolour without laying out again.
func (p *Para) Paint(c gui.Canvas, at gui.Point, spans []Span) {
	for _, l := range p.Lines {
		for _, f := range l.Frags {
			sp := spans[f.Span]
			if sp.Hidden {
				continue
			}
			x, top := at.X+f.X, at.Y+l.Y
			if sp.Background.A > 0 && f.W > 0 {
				// a chip around the glyphs, not the line: a tall line's chip is not a column. A
				// plain rectangle: a rounded one is a path, and a page of code spans as paths
				// made every frame slow
				pad := sp.Font.Size * 0.18
				chip := gui.Rect{X: x - pad/2, Y: at.Y + l.Baseline - f.ascent - pad/2, W: f.W + pad, H: sp.Font.Size*1.15 + pad}
				if f.ascent == 0 {
					chip = gui.Rect{X: x, Y: top, W: f.W, H: l.H}
				}
				c.FillRect(chip, gui.Solid(sp.Background))
			}
			if sp.Atom != nil {
				if sp.Atom.Paint != nil {
					sp.Atom.Paint(c.Sub(gui.Rect{X: x, Y: at.Y + l.Baseline - sp.Atom.Baseline, W: sp.Atom.W, H: sp.Atom.H}))
				}
				continue
			}
			col := sp.Color
			if col.A == 0 {
				col = p.opts.Color
			}
			base := at.Y + l.Baseline
			if f.Layout != nil {
				c.DrawText(f.Layout, gui.Pt(x, base-f.Layout.Ascent), gui.Solid(col))
			}
			thick := max(1, sp.Font.Size/14)
			if sp.Underline {
				c.FillRect(gui.Rect{X: x, Y: base + thick, W: f.W, H: thick}, gui.Solid(col))
			}
			if sp.Strike {
				c.FillRect(gui.Rect{X: x, Y: base - f.ascent*0.35, W: f.W, H: thick}, gui.Solid(col))
			}
		}
	}
}

// less orders boundaries by span, then offset: spans are in reading order.
func less(a, b Pos) bool { return a.Span < b.Span || a.Span == b.Span && a.Offset < b.Offset }

// edgeOf is the x, from the frag's X, of the boundary at off in f: the last cluster edge at or
// before it.
func (f *Frag) edgeOf(off int) float32 {
	if off >= f.To {
		return f.xs[len(f.xs)-1]
	}
	x := f.xs[0]
	for i, o := range f.offsets {
		if o > off {
			break
		}
		x = f.xs[i]
	}
	return x
}

// find is the line and frag holding pos. A boundary at the end of a frag that the next line
// continues belongs to the next line (downstream, as at a soft wrap); a boundary inside a hidden
// span's bytes is found in that span's frag.
func (p *Para) find(pos Pos) (li, fi int, ok bool) {
	li, fi = -1, -1
	for i, l := range p.Lines {
		for j, f := range l.Frags {
			if f.Span != pos.Span {
				continue
			}
			if pos.Offset >= f.From && pos.Offset < f.To {
				return i, j, true
			}
			if pos.Offset == f.To {
				li, fi = i, j // a later frag starting here wins
			}
		}
	}
	return li, fi, li >= 0
}

// Caret is a 1-px-wide rect at pos, the height of its line.
func (p *Para) Caret(pos Pos) gui.Rect {
	li, fi, ok := p.find(pos)
	if !ok {
		if len(p.Lines) == 0 {
			return gui.Rect{W: 1}
		}
		l := p.Lines[len(p.Lines)-1]
		return gui.Rect{Y: l.Y, W: 1, H: l.H}
	}
	l := p.Lines[li]
	f := l.Frags[fi]
	return gui.Rect{X: f.X + f.edgeOf(pos.Offset), Y: l.Y, W: 1, H: l.H}
}

// At is the boundary nearest pt: on the line at pt's y, in its visible frags. A point where
// hidden clusters sit maps past them, to the first visible cluster after them.
func (p *Para) At(pt gui.Point) Pos {
	if len(p.Lines) == 0 {
		return Pos{}
	}
	li := len(p.Lines) - 1
	for i, l := range p.Lines {
		if pt.Y < l.Y+l.H {
			li = i
			break
		}
	}
	l := p.Lines[li]
	best, bestD := Pos{}, float32(-1)
	consider := func(pos Pos, x float32) {
		d := pt.X - x
		if d < 0 {
			d = -d
		}
		if bestD < 0 || d < bestD {
			best, bestD = pos, d
		}
	}
	visible := false
	for _, f := range l.Frags {
		if f.W > 0 || len(f.offsets) > 0 && f.xs[len(f.xs)-1] > 0 {
			visible = true
		}
	}
	for _, f := range l.Frags {
		if visible && f.W == 0 {
			continue // hidden marks: their boundaries share the next visible one's x
		}
		for i, o := range f.offsets {
			consider(Pos{f.Span, o}, f.X+f.xs[i])
		}
		consider(Pos{f.Span, f.To}, f.X+f.xs[len(f.xs)-1])
	}
	if bestD < 0 && len(l.Frags) > 0 {
		f := l.Frags[0]
		return Pos{f.Span, f.From}
	}
	return best
}

// Rects are the highlight of the boundaries from..to, one rect per line it touches.
func (p *Para) Rects(from, to Pos) []gui.Rect {
	if less(to, from) {
		from, to = to, from
	}
	var out []gui.Rect
	for _, l := range p.Lines {
		x0, x1, any := float32(0), float32(0), false
		for _, f := range l.Frags {
			start, end := Pos{f.Span, f.From}, Pos{f.Span, f.To}
			if !less(start, to) || !less(from, end) {
				continue
			}
			a, b := f.xs[0], f.xs[len(f.xs)-1]
			if from.Span == f.Span && from.Offset > f.From {
				a = f.edgeOf(from.Offset)
			}
			if to.Span == f.Span && to.Offset < f.To {
				b = f.edgeOf(to.Offset)
			}
			if !any {
				x0, x1, any = f.X+a, f.X+b, true
			} else {
				x0, x1 = min(x0, f.X+a), max(x1, f.X+b)
			}
		}
		if any && x1 > x0 {
			out = append(out, gui.Rect{X: x0, Y: l.Y, W: x1 - x0, H: l.H})
		}
	}
	return out
}

// Text is what the boundaries from..to show: collapsed spaces as one, a '\n' where the text had
// one, and nothing at a soft wrap but the space that hung there.
func (p *Para) Text(from, to Pos) string {
	if less(to, from) {
		from, to = to, from
	}
	var b strings.Builder
	for _, pc := range p.pieces {
		start, end := Pos{pc.span, pc.from}, Pos{pc.span, pc.to}
		if !less(start, to) || !less(from, end) {
			continue
		}
		switch pc.kind {
		case newline:
			b.WriteByte('\n')
			continue
		case atom:
			continue
		}
		d := pc.display
		if len(d) == pc.to-pc.from { // shown as written: clip to the range
			lo, hi := 0, len(d)
			if from.Span == pc.span && from.Offset > pc.from {
				lo = from.Offset - pc.from
			}
			if to.Span == pc.span && to.Offset < pc.to {
				hi = to.Offset - pc.from
			}
			d = d[lo:hi]
		}
		b.WriteString(d)
	}
	return b.String()
}
