package widget

import (
	"bytes"
	"hash/fnv"
	"image/color"
	"math"
	"strings"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	phtml "github.com/yongjohnlee80/golib/parse/html"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// HTMLVIEW'S PIXEL LAYOUT — tui/widget's HTMLView drawn natively: the page parsed by parse/html,
// styled by the CSS subset (htmlcss.go), laid out as boxes (htmlview_box.go) with its text in
// flow paragraphs, painted on the window's canvas. The selection, the scroll and the signals stay
// the tui widget's; this answers its questions in document pixels.
//
// RE-RENDERING IS INCREMENTAL. The page's top-level blocks are keyed by their source bytes, the
// width, the stylesheets, the text size and the images they hold; a block whose key is unchanged
// keeps its layout, moved and not laid out again. Blocks are laid out only as far as the view's
// bottom and one screen more; below, a block carries an estimated height until it is reached.

// BindHTML binds an HTMLView to a pixel layout drawn in the window: gui/decl's Native style does
// it for a document's HTMLView, and a Go program building one does it before showing it.
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
	look     uint64 // a hash of the colours and text size the boxes were styled with

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

func newHTMLLayout(v *tuiwidget.HTMLView) *htmlLayout {
	l := &htmlLayout{v: v, width: 600, textPx: 16, rootPx: 16}
	l.images = newHTMLImages(l)
	l.sheets = newHTMLSheets(l)
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

// build reads the page into top-level blocks, keeping the layout of each block whose key is
// unchanged.
func (l *htmlLayout) build() {
	l.built = true
	// the laid-out blocks by key, each kept once: identical blocks share a key, and each takes a
	// layout of its own, never another's (their boxes hold their places)
	old := map[uint64][]*topBlock{}
	for _, b := range l.blocks {
		if b.laid {
			old[b.key] = append(old[b.key], b)
		}
	}
	doc, err := phtml.Parse(l.src)
	if err != nil {
		doc, _ = phtml.Parse([]byte("<p><i>this page could not be shown: " + escapeText(err.Error()) + "</i></p>"))
	}
	l.doc = doc
	l.parents = map[*phtml.Node]*phtml.Node{}
	// the window's theme colours a page with no stylesheet of its own; a page that styles itself
	// is drawn as its author wrote it (a light page in a dark window stays light, its code unchipped)
	var author strings.Builder
	authored := false
	linked := 0
	walkNodes(doc, nil, func(n, parent *phtml.Node) {
		l.parents[n] = parent
		if n.Kind != phtml.StartTag && n.Kind != phtml.SelfClosing {
			return
		}
		// a refused image is told for each page that names it: a block reused from an earlier
		// page does not look its images up again, so the page's build does
		if n.Name == "img" {
			if src, _ := n.Attr("src"); src != "" {
				if e, ok := l.images.entries[strings.TrimSpace(src)]; ok && e.refused {
					l.v.Refused(strings.TrimSpace(src))
				}
			}
		}
		if n.Name == "style" {
			for _, c := range n.Children {
				author.WriteString("\n" + c.Data)
				authored = authored || strings.TrimSpace(c.Data) != ""
			}
			return
		}
		// a linked sheet in its place in the cascade, once it has loaded
		if href, ok := sheetLink(n); ok && linked < maxSheets {
			linked++
			authored = true // a sheet still loading counts: the page styles itself
			if e := l.sheets.entry(href); e.state == imgReady {
				media, _ := n.Attr("media")
				author.WriteString("\n" + mediaWrapped(e.text, media))
			}
		}
	})
	var sheets strings.Builder
	sheets.WriteString(uaCSS)
	if !authored {
		sheets.WriteString(l.themeCSS())
	}
	sheets.WriteString(author.String())
	sheets.WriteString("\n" + l.v.Stylesheet())
	css := sheets.String()
	order := 0
	l.styler = &styler{rules: parseCSS(css, &order, mediaEnv{w: l.width, h: l.viewH, dark: isDark(l.bg)}), rootPx: l.rootPx, view: l.view()}
	l.css = hashText(css)

	// rem is the root element's size, which starts from the window's text: never the last build's,
	// so html{font-size:2rem} is twice the text however often the page is built
	l.rootPx = l.textPx
	l.styler.rootPx = l.textPx
	root := &computed{color: l.fg, fontSize: l.textPx, lineHeight: 1.2, display: "block"}
	htmlNode, bodyNode := findElement(doc, "html"), findElement(doc, "body")
	chain := []*phtml.Node{}
	st := root
	if htmlNode != nil {
		chain = append(chain, htmlNode)
		st = l.styler.style(htmlNode, root, chain)
		l.rootPx = st.fontSize
		l.styler.rootPx = st.fontSize
	}
	content := doc
	if bodyNode != nil {
		chain = append(chain, bodyNode)
		st = l.styler.style(bodyNode, st, chain)
		content = bodyNode
	} else if htmlNode != nil {
		content = htmlNode
	}
	l.body = st
	l.bodyE = l.edgesOf(st, l.width)

	// the top-level blocks: each block-level child of the body, and each run of inline content
	// between them
	l.blocks = l.blocks[:0]
	l.abs = l.abs[:0]
	var pending []*phtml.Node
	flushInline := func() {
		text, shows := 0, false
		for _, n := range pending {
			text += n.Span[1] - n.Span[0]
			shows = shows || n.Kind != phtml.Text || strings.TrimSpace(n.Data) != ""
		}
		if shows {
			l.blocks = append(l.blocks, &topBlock{inline: pending, src: [2]int{-1, -1}, textLen: text})
		}
		pending = nil
	}
	for _, c := range content.Children {
		if c.Kind == phtml.Text {
			pending = append(pending, c)
			continue
		}
		if c.Kind != phtml.StartTag && c.Kind != phtml.SelfClosing {
			continue
		}
		cs := l.styler.style(c, st, append(chain, c))
		if cs.display == "none" {
			continue
		}
		if cs.position == "absolute" {
			flushInline()
			l.abs = append(l.abs, &topBlock{node: c, src: spanOf(c), after: len(l.blocks)})
			continue
		}
		if !blockLevel(cs.display) {
			pending = append(pending, c)
			continue
		}
		flushInline()
		l.blocks = append(l.blocks, &topBlock{node: c, src: spanOf(c), textLen: c.Span[1] - c.Span[0]})
	}
	flushInline()

	contentW := l.contentWidth()
	for _, b := range l.blocks {
		b.key = l.keyOf(b, contentW)
		if olds := old[b.key]; len(olds) > 0 {
			o := olds[0]
			old[b.key] = olds[1:]
			b.box, b.h, b.mTop, b.mBot, b.laid = o.box, o.h, o.mTop, o.mBot, true
		}
	}
	l.place()
}

// themeCSS gives a page with no colours of its own the window's: links in the accent, code and
// pre on the surface, quotes and rules muted.
func (l *htmlLayout) themeCSS() string {
	hex := func(c color.NRGBA) string {
		return "#" + hex2(c.R) + hex2(c.G) + hex2(c.B)
	}
	return "\na{color:" + hex(l.accent) + "}code,pre{background:" + hex(l.surface) + "}" +
		"pre{padding:.5em .75em}blockquote,.callout{border-left:3px solid " + hex(l.muted) + "}" +
		"blockquote{color:" + hex(l.muted) + "}hr{border-top:1px solid " + hex(l.muted) + "}" +
		"td,th{border:1px solid " + hex(l.muted) + "}table{border-collapse:collapse}\n"
}

func hex2(b uint8) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[b>>4], digits[b&15]})
}

func escapeText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func hashText(s string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}

// keyOf is a top-level block's key: its source bytes, the width, the stylesheets, the look and
// the images it holds, as they stand.
func (l *htmlLayout) keyOf(b *topBlock, width float32) uint64 {
	h := fnv.New64a()
	write := func(n *phtml.Node) {
		if n.Span[1] <= len(l.src) && n.Span[0] <= n.Span[1] {
			_, _ = h.Write(l.src[n.Span[0]:n.Span[1]])
		}
	}
	if b.node != nil {
		write(b.node)
	}
	for _, n := range b.inline {
		write(n)
	}
	var buf [8]byte
	for _, v := range []uint64{uint64(math.Float32bits(width)), l.css, l.look} {
		for i := range 8 {
			buf[i] = byte(v >> (8 * i))
		}
		_, _ = h.Write(buf[:])
	}
	nodes := b.inline
	if b.node != nil {
		nodes = []*phtml.Node{b.node}
	}
	diagram := func(n *phtml.Node) {
		if text, ok := mermaidSource(n); ok {
			g := l.diagrams.genOf(text)
			for i := range 8 {
				buf[i] = byte(g >> (8 * i))
			}
			_, _ = h.Write(buf[:])
		}
	}
	for _, n := range nodes {
		if n.Kind == phtml.StartTag && n.Name == "details" && l.detailsOpen(n) {
			_, _ = h.Write([]byte{'o'}) // the block itself is a <details>: walkNodes visits below it
		}
		diagram(n) // the block itself may be the <pre class="mermaid">
		walkNodes(n, nil, func(c, _ *phtml.Node) {
			if c.Kind == phtml.StartTag || c.Kind == phtml.SelfClosing {
				if c.Name == "img" {
					src, _ := c.Attr("src")
					_, _ = h.Write([]byte{byte(l.images.gen(src))})
				}
				diagram(c)
				if c.Name == "details" && l.detailsOpen(c) {
					_, _ = h.Write([]byte{'o'})
				}
			}
		})
	}
	return h.Sum64()
}

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

func makeSel(a, b tuiwidget.DocPos) selRange {
	if posBefore(b, a) {
		a, b = b, a
	}
	return selRange{from: a, to: b, ok: a != b}
}

func posBefore(a, b tuiwidget.DocPos) bool {
	if a.Block != b.Block {
		return a.Block < b.Block
	}
	if a.Span != b.Span {
		return a.Span < b.Span
	}
	return a.Offset < b.Offset
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
	at, last := -1, -1
	for i, blk := range l.blocks {
		if blk.src[0] < 0 {
			continue
		}
		if blk.src[0] <= b && b < blk.src[1] {
			at = i
			break
		}
		if blk.src[0] >= b {
			if at < 0 {
				at = i
			}
			continue
		}
		last = i
	}
	if at < 0 {
		at = last
	}
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

// adoptLook takes the window's colours and text size: the page's text and ground from the cells
// under the view, the accent and muted text from the tui theme. A change restyles every block.
func (l *htmlLayout) adoptLook(c gui.Canvas) {
	fg, bg := c.CellColors(0, 0)
	if fg.A == 0 && bg.A == 0 {
		fg, bg = color.NRGBA{A: 255}, color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	}
	px := max(c.TextSize(), 10)
	accent := color.NRGBA{R: 0x5c, G: 0x9c, B: 0xf5, A: 0xff}
	muted := mix(fg, bg, 0.55)
	if th := l.v.Theme(); th != nil {
		dark := isDark(bg)
		if col, ok := colorOf(th.Color(style.TokenAccent), th, dark); ok {
			accent = col
		}
		if col, ok := colorOf(th.Color(style.TokenTextMuted), th, dark); ok {
			muted = col
		}
	}
	h := fnv.New64a()
	for _, col := range []color.NRGBA{fg, bg, accent, muted} {
		_, _ = h.Write([]byte{col.R, col.G, col.B, col.A})
	}
	_, _ = h.Write([]byte{byte(math.Float32bits(px)), byte(math.Float32bits(px) >> 8), byte(math.Float32bits(px) >> 16)})
	look := h.Sum64()
	if look == l.look {
		return
	}
	l.look = look
	l.fg, l.bg, l.accent, l.muted = fg, bg, accent, muted
	l.surface = mix(fg, bg, 0.07)
	l.selColor = mix(accent, bg, 0.35)
	l.textPx, l.rootPx = px, px
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
