package widget

import (
	phtml "github.com/yongjohnlee80/golib/parse/html"
)

// THE BLOCKS — top-level blocks placed down the page and laid out lazily, as far as the view
// needs them, each at an estimated height until then.

// contentWidth is the width the body's content is laid out in.
func (l *htmlLayout) contentWidth() float32 {
	w := l.width - l.bodyE.m[1] - l.bodyE.m[3] - l.bodyE.left() - l.bodyE.right()
	if l.body.maxWidth.set() && !l.body.maxWidth.auto() {
		w = min(w, l.body.maxWidth.px(l.body.fontSize, l.rootPx, l.width, l.view()))
	}
	return max(w, 1)
}

// bodyLeft is the x the body's content starts at: its margins (auto centres it), border and
// padding.
func (l *htmlLayout) bodyLeft() float32 {
	cw := l.contentWidth()
	full := cw + l.bodyE.left() + l.bodyE.right()
	left := l.bodyE.m[3]
	if l.body.margin[1].auto() && l.body.margin[3].auto() {
		left = max((l.width-full)/2, 0)
	}
	return left + l.bodyE.left()
}

// place sets each top-level block's y from the heights above it (laid out or estimated),
// collapsing adjacent margins, and renumbers the laid-out runs.
func (l *htmlLayout) place() {
	y := l.bodyE.m[0] + l.bodyE.top()
	pending := float32(0)
	for i, b := range l.blocks {
		if !b.laid {
			b.h = l.estimate(b)
			b.mTop, b.mBot = l.rootPx*0.5, l.rootPx*0.5
		}
		if i == 0 {
			y += b.mTop
		} else {
			y += max(pending, b.mTop)
		}
		b.y = y
		y += b.h
		pending = b.mBot
	}
	l.topY = y + pending + l.bodyE.bot() + l.bodyE.m[2]
	for _, b := range l.abs {
		b.laid = false // its place with no top may follow the flow's
	}
	l.renumber()
}

// estimate is an unlaid block's height: its source length over a line's worth of characters.
func (l *htmlLayout) estimate(b *topBlock) float32 {
	perLine := max(l.contentWidth()/(l.rootPx*0.5), 1)
	lines := float32(b.textLen)/perLine + 1
	return lines * l.rootPx * 1.5
}

// renumber gives each laid-out run its place in document order, its origin in the document, and
// the separator copied before it: a tab before a row's second cell on, a newline otherwise.
func (l *htmlLayout) renumber() {
	l.runs = l.runs[:0]
	for _, b := range l.blocks {
		if !b.laid {
			break // runs are counted through the laid-out blocks, which come first
		}
		tab := false
		var walk func(bx *box, scroll float32)
		walk = func(bx *box, scroll float32) {
			if bx.kind == kRun {
				bx.sep = "\n"
				if tab {
					bx.sep, tab = "\t", false
				}
				bx.run = len(l.runs)
				bx.ax, bx.ay = bx.px-scroll, b.y+bx.py
				l.runs = append(l.runs, bx)
				return
			}
			for i, k := range bx.kids {
				if bx.kind == kRow {
					tab = i > 0
				}
				walk(k, scroll+bx.scrollX)
			}
		}
		walk(b.box, 0)
	}
}

// layOutTo lays out the top-level blocks down to document y: the view's bottom and a screen more.
// A laid-out block replaces its estimate, and the blocks below move up or down: it goes on until
// every block starting above y is laid out.
func (l *htmlLayout) layOutTo(y float32) {
	for {
		changed := false
		for _, b := range l.blocks {
			if b.y > y {
				break
			}
			if !b.laid {
				l.layTop(b)
				changed = true
			}
		}
		if !changed {
			return
		}
		l.place()
	}
}

// layOutThrough lays out every block up to and including block i.
func (l *htmlLayout) layOutThrough(i int) {
	changed := false
	for _, b := range l.blocks[:min(i+1, len(l.blocks))] {
		if !b.laid {
			l.layTop(b)
			changed = true
		}
	}
	if changed {
		l.place()
	}
}

// layTop builds and lays out one top-level block, relative to its own top-left.
func (l *htmlLayout) layTop(b *topBlock) {
	l.relaid++
	bd := &builder{l: l, s: l.styler}
	chain := l.chainTo(b)
	bd.chain = chain
	width := l.contentWidth()
	var bx *box
	if b.node != nil {
		st := l.styler.style(b.node, l.body, chain)
		bx = bd.box(b.node, st, b.src)
	} else {
		holder := &phtml.Node{Kind: phtml.StartTag, Children: b.inline}
		bx = &box{kind: kBlock, st: l.body.inherit(), src: b.src}
		bx.st.display = "block"
		bx.kids = bd.children(holder, l.body, b.src)
	}
	e := l.edgesOf(bx.st, width)
	l.layBlock(bx, l.bodyLeft(), 0, width)
	if bx.st.position == "relative" {
		l.shiftRelative(bx, width)
	}
	if b.node == nil {
		e = edges{}
	}
	b.box, b.h, b.mTop, b.mBot, b.laid = bx, bx.h, e.m[0], e.m[2], true
}

// chainTo is a top-level block's ancestors, outermost first: what its selectors match against.
func (l *htmlLayout) chainTo(b *topBlock) []*phtml.Node {
	var chain []*phtml.Node
	if h := findElement(l.doc, "html"); h != nil {
		chain = append(chain, h)
	}
	if bd := findElement(l.doc, "body"); bd != nil {
		chain = append(chain, bd)
	}
	if b.node != nil {
		chain = append(chain, b.node)
	}
	return chain
}

// Height is the page's height: laid out where it has been, estimated below.
func (l *htmlLayout) Height() float32 {
	if !l.ready() {
		return 0
	}
	return l.topY
}

// blockAtY is the top-level block holding document y (the last one above it in a gap).
func (l *htmlLayout) blockAtY(y float32) int {
	i := 0
	for j, b := range l.blocks {
		if b.y > y {
			break
		}
		i = j
	}
	return i
}
