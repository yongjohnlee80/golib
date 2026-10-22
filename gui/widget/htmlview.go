package widget

import (
	"bytes"
	"image/color"

	"github.com/yongjohnlee80/golib/gui"
	phtml "github.com/yongjohnlee80/golib/parse/html"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// HTML View Incremental Pixel Layout Pipeline:
//
//	[ Raw HTML Source & Inline Stylesheets ]
//	                   |
//	                   v
//	       [ parse/html HTML5 Parser ] ----------> DOM Element Tree
//	                                                  |
//	                                                  v
//	[ CSS Style Cascade & Rule Engine ] ---------> Computed Styles
//	                                                  |
//	                                                  v
//	[ Incremental Top-Level Block Partitioning ]
//	(Blocks keyed by source bytes, stylesheets, text size, and images)
//	      /                                    \
//	[ Cache Hit ]                         [ Cache Miss ]
//	Reuse cached boxes & text             Compute box geometry, margins,
//	Update Y offsets only                 and flow paragraphs
//	      \                                    /
//	       +-----------------+----------------+
//	                         |
//	                         v
//	       [ Viewport-Clipped Vector Painting ]
//	       (Renders visible blocks + 1 screen lookahead)
//
// BindHTML binds a standard [tuiwidget.HTMLView] component to hardware-accelerated
// pixel-level layout and vector graphics rendering.
//
// When bound, the widget parses HTML documents into a structured DOM tree, resolves CSS
// styling rules, computes CSS box models (padding, border, margin, flexbox), and renders
// proportional typography, embedded images, and vector diagrams with sub-pixel precision.
func BindHTML(v *tuiwidget.HTMLView) {
	v.BindLayout(newHTMLLayout(v))
}

// topBlock is one of the page's top-level blocks.
type topBlock struct {
	node    *phtml.Node // nil: an anonymous run of the body's inline content
	inline  []*phtml.Node
	box     *box
	key     uint64
	src     [2]int
	y       float32 // its border box's top, in the document
	h       float32 // its border box's height: laid out, or estimated
	mTop    float32
	mBot    float32
	laid    bool
	textLen int
	after   int // an absolutely positioned block's: the index of the flow block it comes before
}

type htmlLayout struct {
	v      *tuiwidget.HTMLView
	shaper *gui.TextShaper
	src    []byte
	doc    *phtml.Node

	textPx   float32
	rootPx   float32
	fg, bg   color.NRGBA
	accent   color.NRGBA
	muted    color.NRGBA
	surface  color.NRGBA
	selColor color.NRGBA
	look     uint64 // a hash of the colours, text size and font generation the boxes were styled with

	width  float32
	viewH  float32 // the view's height at the last paint: vh, and a media query's height
	styler *styler
	css    uint64 // a hash of the stylesheets in force
	body   *computed
	bodyE  edges
	blocks []*topBlock
	// abs are the body's absolutely positioned children: out of the flow, laid against the page
	// and painted over it. Their text is drawn, not selectable, and their links do not follow.
	abs      []*topBlock
	runs     []*box // the laid-out runs in document order: DocPos.Block
	parents  map[*phtml.Node]*phtml.Node
	images   *htmlImages
	sheets   *htmlSheets
	diagrams *htmlDiagrams
	// toggled is the <details> a click on their summary folded or opened, by their source offset:
	// kept across builds of the same page, dropped for a new one
	toggled map[int]bool

	built  bool // the source has been read since it last changed
	relaid int  // top-level blocks laid out, for the tests that count them
	topY   float32
}

// newHTMLLayout initializes the HTML layout engine and subsystems for an HTMLView.
func newHTMLLayout(v *tuiwidget.HTMLView) *htmlLayout {
	l := &htmlLayout{v: v, width: 600, textPx: 16, rootPx: 16}
	l.images = newHTMLImages(v)
	l.sheets = newHTMLSheets(v)
	l.diagrams = newHTMLDiagrams(l)
	return l
}

var (
	_ tuiwidget.HTMLLayout         = (*htmlLayout)(nil)
	_ tuiwidget.HTMLLayoutWidth    = (*htmlLayout)(nil)
	_ tuiwidget.HTMLLayoutPixels   = (*htmlLayout)(nil)
	_ tuiwidget.HTMLLayoutTasks    = (*htmlLayout)(nil)
	_ tuiwidget.HTMLLayoutHScroll  = (*htmlLayout)(nil)
	_ tuiwidget.HTMLLayoutImages   = (*htmlLayout)(nil)
	_ tuiwidget.HTMLLayoutActivate = (*htmlLayout)(nil)
	_ gui.View                     = (*htmlLayout)(nil)
)

// Pixels reports that the layout's units are pixels.
func (l *htmlLayout) Pixels() bool { return true }

// SetSource reads a new page; it is laid out at the next paint or question.
func (l *htmlLayout) SetSource(src []byte) {
	if !bytes.Equal(src, l.src) {
		l.toggled = nil
	}
	l.src = src
	l.built = false
}

// detailsOpen reports whether a <details> shows all of it: its open attribute, unless a click on
// its summary flipped it.
func (l *htmlLayout) detailsOpen(n *phtml.Node) bool {
	_, open := n.Attr("open")
	return open != l.toggled[n.Span[0]]
}

// Activate folds or opens the <details> whose summary is under document point (x, y).
func (l *htmlLayout) Activate(x, y float32) bool {
	if !l.ready() {
		return false
	}
	var hit *phtml.Node
	for _, b := range l.blocks {
		if !b.laid || y < b.y || y >= b.y+b.h {
			continue
		}
		var walk func(bx *box)
		walk = func(bx *box) {
			if bx.details != nil && x >= bx.x && x < bx.x+bx.w && y >= b.y+bx.y && y < b.y+bx.y+bx.h {
				hit = bx.details
			}
			for _, k := range bx.kids {
				walk(k)
			}
		}
		walk(b.box)
	}
	if hit == nil {
		return false
	}
	if l.toggled == nil {
		l.toggled = map[int]bool{}
	}
	l.toggled[hit.Span[0]] = !l.toggled[hit.Span[0]]
	l.built = false
	return true
}

// ResetImages drops the page's images and their loads on the way, and lays it out again: the view
// has a new resolver (tuiwidget.HTMLLayoutImages).
func (l *htmlLayout) ResetImages() {
	l.images.reset()
	l.sheets.reset()
	l.built = false
}

// view is the view's size, what vw and vh are hundredths of.
func (l *htmlLayout) view() gui.Size { return gui.Size{W: l.width, H: l.viewH} }

// SetWidth lays the page out at w pixels from the next paint on.
func (l *htmlLayout) SetWidth(w float32) {
	if w > 0 && w != l.width {
		l.width = w
		l.built = false
	}
}

// ready builds the page when it changed, once a shaper is known (the first paint).
func (l *htmlLayout) ready() bool {
	if l.shaper == nil {
		return false
	}
	if !l.built {
		l.build()
	}
	return true
}

// HandleTask takes an image decoded off the loop, a stylesheet, or a diagram's ready; the blocks
// holding it are laid out again.
func (l *htmlLayout) HandleTask(r tui.TaskResult) bool {
	if !l.images.done(r) && !l.sheets.done(r) && !l.diagrams.done(r) {
		return false
	}
	l.built = false
	return true
}

// Paint draws the page from the view's scroll position: the visible blocks, laid out as far as
// one screen below, with the selection under the text.
func (l *htmlLayout) Paint(c gui.Canvas) {
	l.shaper = c.Text()
	l.adoptLook(c)
	if h := c.Size().H; h > 0 && h != l.viewH {
		l.viewH = h
		l.built = false // vh and a media query's height read it
	}
	if !l.built {
		l.build()
	}
	size := c.Size()
	if l.width != size.W && size.W > 0 {
		l.width = size.W
		l.build()
	}
	scroll := l.v.ScrollY()
	l.layOutTo(scroll + 2*size.H)
	c.FillRect(gui.Rect{W: size.W, H: size.H}, gui.Solid(l.bg))
	if l.body.background.A > 0 {
		c.FillRect(gui.Rect{W: size.W, H: size.H}, gui.Solid(l.body.background))
	}
	a, b := l.v.Selection()
	sel := makeSel(a, b)
	for _, blk := range l.blocks {
		if blk.y > scroll+size.H {
			break
		}
		if !blk.laid || blk.y+blk.h < scroll {
			continue
		}
		l.paintBox(c, blk.box, 0, blk.y-scroll, sel)
	}
	for _, b := range l.abs {
		if !b.laid {
			l.layAbsTop(b)
		}
		// laid in document coordinates; one wholly off the page (a skip link at top: -100px) is
		// never drawn
		if bx := b.box; bx.y+bx.h > scroll && bx.y < scroll+size.H && bx.y+bx.h > 0 {
			l.paintBox(c, bx, 0, -scroll, sel)
		}
	}
}

// layAbsTop lays an absolutely positioned child of the body against the page's content box, in
// document coordinates: at its insets, or where it would have been in the flow.
func (l *htmlLayout) layAbsTop(b *topBlock) {
	bd := &builder{l: l, s: l.styler, chain: l.chainTo(b)}
	st := l.styler.style(b.node, l.body, bd.chain)
	bx := bd.box(b.node, st, b.src)
	staticY := l.topY
	if b.after < len(l.blocks) {
		staticY = l.blocks[b.after].y
	}
	top := l.bodyE.m[0] + l.bodyE.top()
	l.layAbsolute(bx, l.bodyLeft(), top, l.contentWidth(), staticY)
	b.box, b.y, b.h, b.laid = bx, 0, bx.y+bx.h, true
}

// adoptLook takes the window's colours, text size and fonts: the page's text and ground from the cells
// under the view that nothing covers, the accent and muted text from the tui theme. A change restyles
// every block.
func (l *htmlLayout) adoptLook(c gui.Canvas) {
	lk := readLook(c, l.v.Theme(), l.fg, l.bg)
	if lk.key == l.look {
		return
	}
	l.look = lk.key
	l.fg, l.bg, l.accent, l.muted = lk.fg, lk.bg, lk.accent, lk.muted
	l.surface = mix(lk.fg, lk.bg, 0.07)
	l.selColor = mix(lk.accent, lk.bg, 0.35)
	l.textPx, l.rootPx = lk.px, lk.px
	l.built = false
}

// findElement is the first element named name, depth first.
func findElement(n *phtml.Node, name string) *phtml.Node {
	var found *phtml.Node
	walkNodes(n, nil, func(c, _ *phtml.Node) {
		if found == nil && c.Kind == phtml.StartTag && c.Name == name {
			found = c
		}
	})
	return found
}

// walkNodes visits n's descendants in document order, each with its parent, without recursion
// on the tree's depth.
func walkNodes(n, parent *phtml.Node, visit func(n, parent *phtml.Node)) {
	type item struct{ n, p *phtml.Node }
	stack := []item{{n, parent}}
	first := true
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if !first {
			visit(it.n, it.p)
		}
		first = false
		for i := len(it.n.Children) - 1; i >= 0; i-- {
			stack = append(stack, item{it.n.Children[i], it.n})
		}
	}
}
