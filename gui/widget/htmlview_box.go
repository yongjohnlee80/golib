package widget

import (
	"image/color"
	"strconv"
	"strings"

	xhtml "github.com/yongjohnlee80/golib/extract/html"
	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	phtml "github.com/yongjohnlee80/golib/parse/html"
)

// THE BOX TREE — a page as HTMLView's pixel layout sees it: block boxes stacked down the page,
// each block's inline content one flow paragraph (an anonymous run), lists with hanging markers,
// tables in rows and cells, pre on a box of its own that scrolls sideways when wider than the
// page. Geometry is relative to the top-level block a box belongs to, so a block that keeps its
// key keeps its layout wherever it moves.

type boxKind uint8

const (
	kBlock boxKind = iota
	kRun           // an anonymous run of inline content: one flow paragraph
	kTable
	kRow
	kCell
)

type box struct {
	kind  boxKind
	st    *computed
	node  *phtml.Node
	kids  []*box
	spans []flow.Span // a run's
	para  *flow.Para
	src   [2]int
	// list items
	item   bool
	marker string
	// geometry, relative to the top-level block's border-box top-left
	x, y, w, h float32 // the border box
	px, py     float32 // a run's paragraph origin
	scrollX    float32 // a box with overflow: auto, scrolled sideways
	contentW   float32 // its content's width, when wider than the box
	// a run's place among the document's runs (what DocPos.Block counts), its paragraph's origin
	// in the document, and what is copied before it: "\n" between blocks, "\t" between cells
	run    int
	ax, ay float32
	sep    string
}

// styler computes elements' styles from the rules in force.
type styler struct {
	rules  []cssRule
	rootPx float32
}

// style computes element n's style; chain is n's ancestors outermost first, then n.
func (s *styler) style(n *phtml.Node, parent *computed, chain []*phtml.Node) *computed {
	c := parent.inherit()
	type hit struct {
		d     cssDecl
		spec  int
		order int
	}
	var hits []hit
	for _, r := range s.rules {
		best := -1
		for _, sel := range r.sels {
			if sel.spec > best && sel.matches(chain) {
				best = sel.spec
			}
		}
		if best < 0 {
			continue
		}
		for _, d := range r.decls {
			hits = append(hits, hit{d, best, r.order})
		}
	}
	if st, ok := n.Attr("style"); ok {
		for _, d := range parseDecls(st) {
			hits = append(hits, hit{d, 1000, 1 << 30})
		}
	}
	// the cascade: !important over normal, then specificity, then order; applied lowest first,
	// so the winner is applied last (a stable insertion sort: rules are few per element)
	rank := func(h hit) (int, int, int) {
		imp := 0
		if h.d.important {
			imp = 1
		}
		return imp, h.spec, h.order
	}
	for i := 1; i < len(hits); i++ {
		for j := i; j > 0; j-- {
			a1, a2, a3 := rank(hits[j-1])
			b1, b2, b3 := rank(hits[j])
			if a1 < b1 || a1 == b1 && (a2 < b2 || a2 == b2 && a3 <= b3) {
				break
			}
			hits[j-1], hits[j] = hits[j], hits[j-1]
		}
	}
	// custom properties first, so a var() in this element's own declarations sees them
	for _, h := range hits {
		if strings.HasPrefix(h.d.prop, "--") {
			c.apply(h.d, parent, s.rootPx)
		}
	}
	// the font size before the rest, so em lengths resolve against this element's own size
	for _, h := range hits {
		if h.d.prop == "font-size" || h.d.prop == "font" {
			c.apply(h.d, parent, s.rootPx)
		}
	}
	for _, h := range hits {
		if !strings.HasPrefix(h.d.prop, "--") && h.d.prop != "font-size" && h.d.prop != "font" {
			c.apply(h.d, parent, s.rootPx)
		}
	}
	if n.Name == "a" {
		if href, ok := n.Attr("href"); ok {
			c.link = href
		}
	}
	if _, ok := n.Attr("hidden"); ok {
		c.display = "none"
	}
	return c
}

// blockLevel reports whether a display starts a box of its own (not inline content).
func blockLevel(d string) bool {
	switch d {
	case "inline", "inline-block", "":
		return false
	}
	return true
}

// builder turns a node's children into boxes, grouping inline content into runs.
type builder struct {
	l     *htmlLayout
	s     *styler
	chain []*phtml.Node
	depth int
}

// maxBoxDepth bounds the box tree's nesting: deeper elements are read as inline content of the
// box at the limit, so a page nested past the parser's own limit cannot exhaust the stack.
const maxBoxDepth = 200

// children builds the boxes for n's children inside a block styled st.
func (b *builder) children(n *phtml.Node, st *computed, src [2]int) []*box {
	var out []*box
	var run *box
	flush := func() {
		if run == nil {
			return
		}
		if !blankRun(run) {
			out = append(out, run)
		}
		run = nil
	}
	cur := func() *box {
		if run == nil {
			run = &box{kind: kRun, st: st, src: src}
		}
		return run
	}
	var inline func(n *phtml.Node, st *computed)
	inline = func(n *phtml.Node, st *computed) {
		for _, c := range n.Children {
			switch c.Kind {
			case phtml.Text:
				text := c.Data
				if st.white == flow.Normal {
					text = strings.ReplaceAll(text, "\n", " ")
				}
				cur().spans = append(cur().spans, b.span(text, st, src))
				continue
			case phtml.StartTag, phtml.SelfClosing:
			default:
				continue
			}
			if xhtml.Dropped(c) {
				continue
			}
			b.chain = append(b.chain, c)
			cs := b.s.style(c, st, b.chain)
			switch {
			case cs.display == "none":
			case c.Name == "br":
				if st.white != flow.Normal {
					cur().spans = append(cur().spans, b.span("\n", st, src))
				} else {
					flush()
					run = &box{kind: kRun, st: st, src: src, sep: "\n"}
				}
			case c.Name == "img":
				cur().spans = append(cur().spans, b.l.imageSpan(c, cs, src))
			case blockLevel(cs.display) && b.depth < maxBoxDepth:
				flush()
				b.depth++
				out = append(out, b.box(c, cs, src))
				b.depth--
			default:
				inline(c, cs)
			}
			b.chain = b.chain[:len(b.chain)-1]
		}
	}
	inline(n, st)
	flush()
	return out
}

// blankRun reports whether a run holds nothing but collapsible white space.
func blankRun(r *box) bool {
	if r.st.white != flow.Normal {
		return len(r.spans) == 0
	}
	for _, s := range r.spans {
		if s.Atom != nil || strings.TrimSpace(s.Text) != "" {
			return false
		}
	}
	return true
}

// box builds element n's box, styled st.
func (b *builder) box(n *phtml.Node, st *computed, src [2]int) *box {
	if r, ok := blockSrc(n); ok {
		src = r
	}
	bx := &box{kind: kBlock, st: st, node: n, src: src}
	switch st.display {
	case "table":
		bx.kind = kTable
		bx.kids = b.rows(n, st, src)
		return bx
	case "table-row":
		bx.kind = kRow
	case "table-cell":
		bx.kind = kCell
	case "list-item":
		bx.item = true
	}
	if n.Name == "pre" {
		trimPre(n)
	}
	bx.kids = b.children(n, st, src)
	return bx
}

// rows collects a table's rows, through its row groups, each row's cells as boxes.
func (b *builder) rows(n *phtml.Node, st *computed, src [2]int) []*box {
	var out []*box
	for _, c := range n.Children {
		if c.Kind != phtml.StartTag && c.Kind != phtml.SelfClosing || xhtml.Dropped(c) {
			continue
		}
		b.chain = append(b.chain, c)
		cs := b.s.style(c, st, b.chain)
		switch cs.display {
		case "table-row-group":
			out = append(out, b.rows(c, cs, src)...)
		case "table-row":
			row := &box{kind: kRow, st: cs, node: c, src: src}
			for _, cell := range c.Children {
				if cell.Kind != phtml.StartTag || xhtml.Dropped(cell) {
					continue
				}
				b.chain = append(b.chain, cell)
				ccs := b.s.style(cell, cs, b.chain)
				if ccs.display != "none" {
					cb := b.box(cell, ccs, src)
					cb.kind = kCell
					row.kids = append(row.kids, cb)
				}
				b.chain = b.chain[:len(b.chain)-1]
			}
			out = append(out, row)
		}
		b.chain = b.chain[:len(b.chain)-1]
	}
	return out
}

// trimPre drops the newline right after <pre>, and the one before </pre>, as a browser does.
func trimPre(n *phtml.Node) {
	var first, last *phtml.Node
	var walk func(n *phtml.Node)
	walk = func(n *phtml.Node) {
		for _, c := range n.Children {
			if c.Kind == phtml.Text {
				if first == nil {
					first = c
				}
				last = c
			}
			walk(c)
		}
	}
	walk(n)
	if first != nil {
		first.Data = strings.TrimPrefix(first.Data, "\n")
	}
	if last != nil {
		last.Data = strings.TrimSuffix(last.Data, "\n")
	}
}

// blockSrc reads data-src="from-to".
func blockSrc(n *phtml.Node) ([2]int, bool) {
	v, ok := n.Attr("data-src")
	if !ok {
		return [2]int{}, false
	}
	a, c, ok := strings.Cut(v, "-")
	if !ok {
		return [2]int{}, false
	}
	from, err1 := strconv.Atoi(strings.TrimSpace(a))
	to, err2 := strconv.Atoi(strings.TrimSpace(c))
	if err1 != nil || err2 != nil || from < 0 || to < from {
		return [2]int{}, false
	}
	return [2]int{from, to}, true
}

// fontOf is a style's font.
func fontOf(st *computed) gui.Font {
	f := gui.Font{Size: st.fontSize, Bold: st.bold, Italic: st.italic}
	if st.mono {
		f.Family = gui.MonospaceFamily()
	}
	return f
}

// span is text in st's look; an inline element's own background shades it (code).
func (b *builder) span(text string, st *computed, src [2]int) flow.Span {
	return flow.Span{Text: text, Font: fontOf(st), Color: st.color, Background: st.background,
		Underline: st.underline, Strike: st.strike, Link: st.link, Line: -1, Src: src}
}

// ---- layout ----

// edges are a box's margin, border and padding widths, in px, at an available width.
type edges struct{ m, b, p [4]float32 }

func (l *htmlLayout) edgesOf(st *computed, avail float32) edges {
	var e edges
	for i := range 4 {
		e.m[i] = st.margin[i].px(st.fontSize, l.rootPx, avail)
		e.p[i] = st.padding[i].px(st.fontSize, l.rootPx, avail)
		if !st.border[i].none && st.border[i].w.set() {
			e.b[i] = st.border[i].w.px(st.fontSize, l.rootPx, avail)
		}
	}
	return e
}

func (e edges) left() float32  { return e.b[3] + e.p[3] }
func (e edges) right() float32 { return e.b[1] + e.p[1] }
func (e edges) top() float32   { return e.b[0] + e.p[0] }
func (e edges) bot() float32   { return e.b[2] + e.p[2] }

// layBlock lays box bx out at (x, y) in an available width avail: its border box's place and
// size. Its own vertical margins are its parent's to place (they collapse with its siblings').
func (l *htmlLayout) layBlock(bx *box, x, y, avail float32) {
	e := l.edgesOf(bx.st, avail)
	w := avail - e.m[1] - e.m[3]
	content := w - e.left() - e.right()
	if bx.st.width.set() && !bx.st.width.auto() {
		content = bx.st.width.px(bx.st.fontSize, l.rootPx, avail)
	}
	if bx.st.maxWidth.set() && !bx.st.maxWidth.auto() {
		content = min(content, bx.st.maxWidth.px(bx.st.fontSize, l.rootPx, avail))
	}
	content = max(content, 1)
	w = content + e.left() + e.right()
	left := x + e.m[3]
	if bx.st.margin[1].auto() && bx.st.margin[3].auto() {
		left = x + max((avail-w)/2, 0) // margin: auto centres a narrower box
	}
	bx.x, bx.y, bx.w = left, y, w
	inner := y + e.top()
	switch bx.kind {
	case kTable:
		inner += l.layTable(bx, left+e.left(), inner, content)
	default:
		inner += l.layKids(bx, left+e.left(), inner, content)
	}
	bx.h = inner + e.bot() - y
	if bx.item {
		bx.marker = l.markerOf(bx)
	}
}

// layKids stacks a block's children down from y and answers their height. A block's top margin
// collapses with the bottom margin of the block before it, to the larger; a run takes no margins.
// The last child's bottom margin stays inside its parent: margins collapse between siblings, not
// through parents.
func (l *htmlLayout) layKids(bx *box, x, y, width float32) float32 {
	cy, pending := y, float32(0)
	for _, k := range bx.kids {
		if k.kind == kRun {
			cy += pending
			pending = 0
			l.layRun(k, x, cy, width)
			cy += k.h
			continue
		}
		e := l.edgesOf(k.st, width)
		l.layBlock(k, x, cy+max(pending, e.m[0]), width)
		cy = k.y + k.h
		pending = e.m[2]
	}
	return cy + pending - y
}

// layRun lays an anonymous run's paragraph out at width.
func (l *htmlLayout) layRun(r *box, x, y, width float32) {
	st := r.st
	o := flow.Options{Width: width, Align: st.align, LineHeight: st.lineHeight, WhiteSpace: st.white, Color: st.color}
	if st.white == flow.Pre {
		o.Width = 0 // pre keeps its lines; a wider one scrolls sideways in its box
	}
	r.para = flow.Lay(r.spans, o, l.shaper)
	r.x, r.y, r.w, r.h = x, y, width, r.para.Height
	r.px, r.py = x, y
	r.contentW = r.para.Width
}

// markerOf is a list item's marker: a bullet by the list's depth, or its number.
func (l *htmlLayout) markerOf(bx *box) string {
	n := bx.node
	if n == nil {
		return "•"
	}
	list, depth := l.parents[n], 0
	for p := list; p != nil; p = l.parents[p] {
		if p.Name == "ul" || p.Name == "ol" || p.Name == "menu" {
			depth++
		}
	}
	if list != nil && list.Name == "ol" {
		num := 1
		if v, ok := list.Attr("start"); ok {
			if s, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				num = s
			}
		}
		for _, c := range list.Children {
			if c == n {
				break
			}
			if c.Kind == phtml.StartTag && c.Name == "li" {
				num++
			}
		}
		return strconv.Itoa(num) + "."
	}
	return []string{"•", "◦", "▪"}[max(depth-1, 0)%3]
}

// layTable lays a table's rows out in width: auto column widths from each column's minimum and
// maximum content, rows as tall as their tallest cell. It answers the rows' height.
func (l *htmlLayout) layTable(t *box, x, y, width float32) float32 {
	cols := 0
	for _, r := range t.kids {
		cols = max(cols, len(r.kids))
	}
	if cols == 0 {
		return 0
	}
	mins, maxs := make([]float32, cols), make([]float32, cols)
	for _, r := range t.kids {
		for i, c := range r.kids {
			lo, hi := l.minMax(c, width)
			mins[i], maxs[i] = max(mins[i], lo), max(maxs[i], hi)
		}
	}
	var sumMin, sumMax float32
	for i := range cols {
		sumMin += mins[i]
		sumMax += maxs[i]
	}
	widths := make([]float32, cols)
	switch {
	case sumMax <= width:
		copy(widths, maxs)
	case sumMin >= width:
		copy(widths, mins)
	default:
		extra := width - sumMin
		for i := range cols {
			share := float32(0)
			if sumMax > sumMin {
				share = (maxs[i] - mins[i]) / (sumMax - sumMin)
			}
			widths[i] = mins[i] + extra*share
		}
	}
	cy := y
	for _, r := range t.kids {
		cx := x
		rowH := float32(0)
		for i, c := range r.kids {
			l.layBlock(c, cx, cy, widths[i])
			c.w = widths[i]
			rowH = max(rowH, c.h)
			step := widths[i]
			if t.st.collapse {
				step -= l.edgesOf(c.st, widths[i]).b[1] // shared borders: drawn once
			}
			cx += step
		}
		for _, c := range r.kids {
			c.h = rowH // a row's cells are as tall as its tallest
		}
		r.x, r.y, r.w, r.h = x, cy, cx-x, rowH
		cy += rowH
		if t.st.collapse && len(r.kids) > 0 {
			cy -= l.edgesOf(r.kids[0].st, width).b[2]
		}
	}
	return cy - y
}

// minMax are a box's minimum content width (its widest unbreakable run) and its maximum (no
// wrapping at all), border box included.
func (l *htmlLayout) minMax(bx *box, avail float32) (float32, float32) {
	e := l.edgesOf(bx.st, avail)
	pad := e.left() + e.right()
	var lo, hi float32
	for _, k := range bx.kids {
		var a, b float32
		switch k.kind {
		case kRun:
			o := flow.Options{LineHeight: k.st.lineHeight, WhiteSpace: k.st.white}
			b = flow.Lay(k.spans, o, l.shaper).Width
			if k.st.white == flow.Pre {
				a = b
			} else {
				o.Width = 1
				a = flow.Lay(k.spans, o, l.shaper).Width
			}
		default:
			a, b = l.minMax(k, avail)
		}
		lo, hi = max(lo, a), max(hi, b)
	}
	return lo + pad, hi + pad
}

// ---- painting ----

// paintBox draws bx and what it holds with its top block's origin at (ox, oy).
func (l *htmlLayout) paintBox(c gui.Canvas, bx *box, ox, oy float32, sel selRange) {
	r := gui.Rect{X: ox + bx.x, Y: oy + bx.y, W: bx.w, H: bx.h}
	if bx.kind != kRun {
		radius := bx.st.radius.px(bx.st.fontSize, l.rootPx, bx.w)
		if bx.st.background.A > 0 {
			c.FillRRect(r, radius, gui.Solid(bx.st.background))
		}
		l.paintBorders(c, bx, r, radius)
		if bx.marker != "" {
			l.paintMarker(c, bx, ox, oy)
		}
	}
	if bx.kind == kRun {
		l.paintRun(c, bx, ox, oy, sel)
		return
	}
	if bx.st.overflowScroll && bx.kind == kBlock {
		// a box that scrolls sideways clips what it holds to its padding box
		e := l.edgesOf(bx.st, bx.w)
		in := gui.Rect{X: r.X + e.b[3], Y: r.Y + e.b[0], W: r.W - e.b[1] - e.b[3], H: r.H - e.b[0] - e.b[2]}
		sub := c.Sub(in)
		for _, k := range bx.kids {
			l.paintBox(sub, k, ox-in.X-bx.scrollX, oy-in.Y, sel)
		}
		return
	}
	for _, k := range bx.kids {
		l.paintBox(c, k, ox, oy, sel)
	}
}

func (l *htmlLayout) paintBorders(c gui.Canvas, bx *box, r gui.Rect, radius float32) {
	e := l.edgesOf(bx.st, bx.w)
	colorOf := func(i int) color.NRGBA {
		if bx.st.border[i].set {
			return bx.st.border[i].color
		}
		return bx.st.color
	}
	uniform := e.b[0] > 0 && e.b[0] == e.b[1] && e.b[1] == e.b[2] && e.b[2] == e.b[3] &&
		colorOf(0) == colorOf(1) && colorOf(1) == colorOf(2) && colorOf(2) == colorOf(3)
	if uniform && radius > 0 {
		c.StrokeRRect(r.Inset(e.b[0]/2), radius, e.b[0], gui.Solid(colorOf(0)))
		return
	}
	if e.b[0] > 0 {
		c.FillRect(gui.Rect{X: r.X, Y: r.Y, W: r.W, H: e.b[0]}, gui.Solid(colorOf(0)))
	}
	if e.b[2] > 0 {
		c.FillRect(gui.Rect{X: r.X, Y: r.Y + r.H - e.b[2], W: r.W, H: e.b[2]}, gui.Solid(colorOf(2)))
	}
	if e.b[3] > 0 {
		c.FillRect(gui.Rect{X: r.X, Y: r.Y, W: e.b[3], H: r.H}, gui.Solid(colorOf(3)))
	}
	if e.b[1] > 0 {
		c.FillRect(gui.Rect{X: r.X + r.W - e.b[1], Y: r.Y, W: e.b[1], H: r.H}, gui.Solid(colorOf(1)))
	}
}

// paintMarker draws a list item's marker hanging left of its content, on its first line.
func (l *htmlLayout) paintMarker(c gui.Canvas, bx *box, ox, oy float32) {
	f := fontOf(bx.st)
	t := l.shaper.Layout(bx.marker, f, 0)
	e := l.edgesOf(bx.st, bx.w)
	at := gui.Point{X: ox + bx.x + e.left() - t.Width - f.Size*0.5, Y: oy + bx.y + e.top()}
	if k := firstRun(bx); k != nil && k.para != nil && len(k.para.Lines) > 0 {
		ln := k.para.Lines[0]
		at.Y = oy + k.py + ln.Y + ln.Baseline - t.Ascent
	}
	c.DrawText(t, at, gui.Solid(bx.st.color))
}

// firstRun is the first run inside bx, depth first.
func firstRun(bx *box) *box {
	for _, k := range bx.kids {
		if k.kind == kRun {
			return k
		}
		if r := firstRun(k); r != nil {
			return r
		}
	}
	return nil
}

// paintRun draws a run's selection highlight, then its text.
func (l *htmlLayout) paintRun(c gui.Canvas, r *box, ox, oy float32, sel selRange) {
	at := gui.Point{X: ox + r.px, Y: oy + r.py}
	if from, to, ok := sel.within(r.run, r.spans); ok {
		for _, rr := range r.para.Rects(from, to) {
			c.FillRect(gui.Rect{X: at.X + rr.X, Y: at.Y + rr.Y, W: rr.W, H: rr.H}, gui.Solid(l.selColor))
		}
	}
	r.para.Paint(c, at, r.spans)
}
