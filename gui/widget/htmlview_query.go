package widget

import (
	"math"
	"strings"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	phtml "github.com/yongjohnlee80/golib/parse/html"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// THE POINTER-QUERY SURFACE — what tui's HTMLView asks of the laid-out page: the position at a
// point, a selection's rectangles and text, the link under the pointer, the block showing a
// source byte, and sideways scrolling.

// runOrigin is a laid-out run's paragraph origin in the document.
func runOrigin(r *box) (float32, float32) { return r.ax, r.ay }

// At is the document position nearest (x, y): in the run under y (the nearest by x where runs sit
// side by side, a table's cells), else the start of the next run below, else the page's end.
func (l *htmlLayout) At(x, y float32) tuiwidget.DocPos {
	if !l.ready() {
		return tuiwidget.DocPos{}
	}
	l.layOutTo(y + 1)
	if len(l.runs) == 0 {
		return tuiwidget.DocPos{}
	}
	best, bestD := -1, float32(math.MaxFloat32)
	next := -1
	for i, r := range l.runs {
		ox, oy := runOrigin(r)
		top, bot := oy, oy+r.para.Height
		if y >= top && y < bot {
			d := float32(0)
			switch {
			case x < ox:
				d = ox - x
			case x > ox+max(r.w, r.para.Width):
				d = x - ox - max(r.w, r.para.Width)
			}
			if d < bestD {
				best, bestD = i, d
			}
		} else if y < top && next < 0 {
			next = i
		}
	}
	if best >= 0 {
		r := l.runs[best]
		ox, oy := runOrigin(r)
		p := r.para.At(gui.Point{X: x - ox, Y: y - oy})
		return tuiwidget.DocPos{Block: best, Span: p.Span, Offset: p.Offset}
	}
	if next >= 0 {
		return tuiwidget.DocPos{Block: next}
	}
	last := len(l.runs) - 1
	return l.endOf(last)
}

// endOf is the position after run i's last text.
func (l *htmlLayout) endOf(i int) tuiwidget.DocPos {
	r := l.runs[i]
	if len(r.spans) == 0 {
		return tuiwidget.DocPos{Block: i}
	}
	n := len(r.spans) - 1
	return tuiwidget.DocPos{Block: i, Span: n, Offset: len(r.spans[n].Text)}
}

// selRange is a selection, ordered, as the paint reads it.
type selRange struct {
	from, to tuiwidget.DocPos
	ok       bool
}

// makeSel constructs an ordered selection range between document positions a and b.
func makeSel(a, b tuiwidget.DocPos) selRange {
	a, b = tuiwidget.OrderDocPos(a, b)
	return selRange{from: a, to: b, ok: a != b}
}

// within is the part of the selection in run i, as flow positions.
func (s selRange) within(i int, spans []flow.Span) (flow.Pos, flow.Pos, bool) {
	if !s.ok || i < s.from.Block || i > s.to.Block {
		return flow.Pos{}, flow.Pos{}, false
	}
	from, to := flow.Pos{}, flow.Pos{Span: max(len(spans)-1, 0)}
	if len(spans) > 0 {
		to.Offset = len(spans[len(spans)-1].Text)
	}
	if i == s.from.Block {
		from = flow.Pos{Span: s.from.Span, Offset: s.from.Offset}
	}
	if i == s.to.Block {
		to = flow.Pos{Span: s.to.Span, Offset: s.to.Offset}
	}
	return from, to, from != to
}

// Rects are a selection's highlight, in document pixels.
func (l *htmlLayout) Rects(from, to tuiwidget.DocPos) []tuiwidget.Rect32 {
	if !l.ready() {
		return nil
	}
	s := makeSel(from, to)
	var out []tuiwidget.Rect32
	for i := s.from.Block; i <= s.to.Block && i < len(l.runs); i++ {
		r := l.runs[i]
		f, t, ok := s.within(i, r.spans)
		if !ok {
			continue
		}
		ox, oy := runOrigin(r)
		for _, rr := range r.para.Rects(f, t) {
			out = append(out, tuiwidget.Rect32{X: ox + rr.X, Y: oy + rr.Y, W: rr.W, H: rr.H})
		}
	}
	return out
}

// Text is a selection's text: each run's own (flow's Text), a newline between blocks and a tab
// between a row's cells.
func (l *htmlLayout) Text(from, to tuiwidget.DocPos) string {
	if !l.ready() {
		return ""
	}
	s := makeSel(from, to)
	var sb strings.Builder
	for i := s.from.Block; i <= s.to.Block && i < len(l.runs); i++ {
		r := l.runs[i]
		if i > s.from.Block {
			sb.WriteString(r.sep)
		}
		f, t, ok := s.within(i, r.spans)
		if ok {
			sb.WriteString(r.para.Text(f, t))
		}
	}
	return sb.String()
}

// LinkRects are the link fragments' boxes on the lines between top and bottom.
func (l *htmlLayout) LinkRects(top, bottom float32, dst []tuiwidget.Rect32) []tuiwidget.Rect32 {
	if !l.ready() {
		return dst
	}
	for _, r := range l.runs {
		ox, oy := runOrigin(r)
		if oy >= bottom || oy+r.para.Height <= top {
			continue
		}
		for _, ln := range r.para.Lines {
			if oy+ln.Y >= bottom || oy+ln.Y+ln.H <= top {
				continue
			}
			for _, f := range ln.Frags {
				if r.spans[f.Span].Link != "" {
					dst = append(dst, tuiwidget.Rect32{X: ox + f.X, Y: oy + ln.Y, W: f.W, H: ln.H})
				}
			}
		}
	}
	return dst
}

// LinkAt is the target of the link under (x, y): the fragment the point is inside, not the
// nearest.
func (l *htmlLayout) LinkAt(x, y float32) string {
	if !l.ready() {
		return ""
	}
	for _, r := range l.runs {
		ox, oy := runOrigin(r)
		if y < oy || y >= oy+r.para.Height {
			continue
		}
		for _, ln := range r.para.Lines {
			if y < oy+ln.Y || y >= oy+ln.Y+ln.H {
				continue
			}
			for _, f := range ln.Frags {
				if x >= ox+f.X && x < ox+f.X+f.W {
					return r.spans[f.Span].Link
				}
			}
		}
	}
	return ""
}

// spanOf is the source bytes a top-level element shows: its own data-src, else the span of the
// data-src its descendants carry (a list whose items carry theirs); {-1, -1} when there are none.
func spanOf(n *phtml.Node) [2]int {
	if r, ok := blockSrc(n); ok {
		return r
	}
	out := [2]int{-1, -1}
	walkNodes(n, nil, func(c, _ *phtml.Node) {
		if c.Kind != phtml.StartTag {
			return
		}
		if r, ok := blockSrc(c); ok {
			if out[0] < 0 || r[0] < out[0] {
				out[0] = r[0]
			}
			out[1] = max(out[1], r[1])
		}
	})
	return out
}

// deepest is the innermost box of a laid-out top block holding document y that has source bytes:
// a list's item, not the list.
func deepest(blk *topBlock, y float32) *box {
	var found *box
	var walk func(bx *box)
	walk = func(bx *box) {
		if bx.src[0] < 0 || y < blk.y+bx.y || y >= blk.y+bx.y+bx.h {
			return
		}
		found = bx
		for _, k := range bx.kids {
			walk(k)
		}
	}
	walk(blk.box)
	return found
}

// ownAt is the innermost box of a top block whose own data-src holds source byte b; else, b in a
// gap between them (the blank line between a list's items), the first after it, else the last
// before it; nil when no box has its own.
func ownAt(blk *topBlock, b int) *box {
	var in, after, before *box
	var walk func(bx *box)
	walk = func(bx *box) {
		if bx.own {
			switch {
			case bx.src[0] <= b && b < bx.src[1]:
				in = bx
			case bx.src[0] >= b:
				if after == nil {
					after = bx
				}
			default:
				before = bx
			}
		}
		for _, k := range bx.kids {
			walk(k)
		}
	}
	walk(blk.box)
	switch {
	case in != nil:
		return in
	case after != nil:
		return after
	}
	return before
}

// BlockAt is the block at document y with source bytes of its own (a list's item rather than the
// list): the start of its source bytes, -1 when there are none, and its top.
func (l *htmlLayout) BlockAt(y float32) (int, float32) {
	if !l.ready() || len(l.blocks) == 0 {
		return -1, 0
	}
	blk := l.blocks[l.blockAtY(y)]
	if blk.laid {
		if bx := deepest(blk, y); bx != nil {
			return bx.src[0], blk.y + bx.y
		}
	}
	return blk.src[0], blk.y
}

// BlockTop is the top of the block showing source byte b, the innermost with source bytes of its
// own: the block whose source holds it, else the first after it, else (a byte after every block,
// the blank lines that end a note) the last.
func (l *htmlLayout) BlockTop(b int) (float32, bool) {
	if !l.ready() {
		return 0, false
	}
	at := tuiwidget.SourceIndex(len(l.blocks), func(i int) [2]int { return l.blocks[i].src }, b)
	if at < 0 {
		return 0, false
	}
	l.layOutThrough(at)
	blk := l.blocks[at]
	if bx := ownAt(blk, b); bx != nil {
		return blk.y + bx.y, true
	}
	return blk.y, true
}

// ScrollX scrolls the box under (x, y) that scrolls sideways (a pre wider than the page).
func (l *htmlLayout) ScrollX(x, y, dx float32) bool {
	if !l.ready() {
		return false
	}
	b := l.blocks[l.blockAtY(y)]
	if !b.laid {
		return false
	}
	var hit *box
	var walk func(bx *box)
	walk = func(bx *box) {
		if y-b.y >= bx.y && y-b.y < bx.y+bx.h && x >= bx.x && x < bx.x+bx.w && bx.st != nil && bx.st.overflowScroll {
			hit = bx
		}
		for _, k := range bx.kids {
			walk(k)
		}
	}
	walk(b.box)
	if hit == nil {
		return false
	}
	over := float32(0)
	for _, k := range hit.kids {
		over = max(over, k.contentW-(k.w))
	}
	next := min(max(hit.scrollX+dx, 0), max(over, 0))
	if next == hit.scrollX {
		return false
	}
	hit.scrollX = next
	l.renumber() // the runs inside moved
	return true
}
