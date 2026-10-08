package widget

import (
	"image/color"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/gui/flow"
	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// The text's inset from the body's edges, in logical pixels.
const (
	padX = 8
	padY = 4
)

// pixelLayout is an Editor's geometry in logical pixels: the buffer's blocks, laid out as they
// are needed, the scroll, and the mapping between buffer positions and points. It is the
// EditorLayout the core reaches its widget through. It runs on the loop, as painting does: the
// shaper it measures with is the last paint's.
type pixelLayout struct {
	e *Editor

	sh   *gui.TextShaper // nil before the first paint
	cell gui.Size
	th   Theme

	width, height float32 // the text's area
	scroll        float32 // how far the text's top is above the view's
	left          float32 // how far the text's left is left of the view's: the Raw view with WrapNone
	gutter        float32 // the line numbers' width, their gap included; 0 without them

	lines  []string // the buffer, read once per change
	blocks []Block
	laid   []*laidBlock // parallel to blocks: nil until laid out
	stale  bool         // lines and blocks are to be read again
	fonts  uint64       // the window's font generation they were shaped in (Canvas.Fonts)
	keep   int          // after a change, blocks wholly before this line keep their layout

	diagrams map[int]*diagramSlot // the diagram requests of blocks, by the block's first line
	gen      uint64               // the last generation handed out
}

// laidBlock is a block as laid out, and what it was laid out for.
type laidBlock struct {
	bl     BlockLayout
	inside bool
	width  float32
	colors []color.NRGBA // Raw: the line's cluster colours
	text   string        // Raw: the line's text
}

func newPixelLayout(e *Editor) *pixelLayout {
	return &pixelLayout{e: e, stale: true, diagrams: map[int]*diagramSlot{}}
}

// --- tuiwidget.EditorLayout ---------------------------------------------------------------

// Reveal scrolls the caret at (line, col) into view: down or up, and sideways when long lines
// are not wrapped.
func (l *pixelLayout) Reveal(line, col int) {
	if l.height <= 0 {
		return
	}
	r, ok := l.caretAt(line, col)
	if !ok {
		return
	}
	switch {
	case r.Y < l.scroll:
		l.scroll = r.Y
	case r.Y+r.H > l.scroll+l.height-2*padY:
		l.scroll = r.Y + r.H - (l.height - 2*padY)
	}
	l.scroll = max(l.scroll, 0)
	if l.sideways() {
		cw := l.monoCell()
		switch {
		case r.X < l.left:
			l.left = r.X
		case r.X+cw > l.left+l.width:
			l.left = r.X + cw - l.width
		}
		l.left = max(l.left, 0)
	}
	l.e.body.MarkDirty()
}

// sideways reports whether the view scrolls horizontally: the Raw view, with WrapNone.
func (l *pixelLayout) sideways() bool {
	return l.e.wrap == tuiwidget.WrapNone && !(l.e.mode == Rendered && l.e.render != nil)
}

// monoCell is one monospace cluster's width: the unit the gutter and the ruler are counted in.
func (l *pixelLayout) monoCell() float32 {
	if l.sh != nil {
		if m := l.sh.Measure("0", l.monoFont()); len(m.X) > 1 && m.X[1] > 0 {
			return m.X[1]
		}
	}
	return max(l.cell.W, 1)
}

// textX is where the text's x = 0 is in the view: past the padding and the gutter, less the
// sideways scroll. Painting, hit-tests and the caret all place the text from here.
func (l *pixelLayout) textX() float32 { return padX + l.gutter - l.left }

// scrollX moves the view sideways by dx pixels, within the widest line laid out.
func (l *pixelLayout) scrollX(dx float32) bool {
	if !l.sideways() {
		return false
	}
	var widest float32
	for _, lb := range l.laid {
		if lb == nil {
			continue
		}
		for _, ll := range lb.bl.Lines {
			widest = max(widest, ll.Para.Width)
		}
	}
	was := l.left
	l.left = max(min(l.left+dx, widest+l.monoCell()-l.width), 0)
	return l.left != was
}

// PageLines is how many lines of the text font fit the view.
func (l *pixelLayout) PageLines() int {
	return max(int(l.height/l.lineHeight(l.monoFont())), 1)
}

// Measure is the width of s in the monospace font, in pixels: the cursor's line is always laid
// out Raw, in that font, so j and k keep their column in the units it is drawn in.
func (l *pixelLayout) Measure(s string) int {
	if l.sh == nil {
		n := 0
		for range tui.Graphemes(s) {
			n++
		}
		return n * max(int(l.cell.W), 1)
	}
	m := l.sh.Measure(s, l.monoFont())
	return int(m.X[len(m.X)-1] + 0.5)
}

// Changed lays out again from fromLine on.
func (l *pixelLayout) Changed(fromLine int) {
	if !l.stale || fromLine < l.keep {
		l.keep = fromLine
	}
	l.dropDiagrams(fromLine)
	l.stale = true
	l.e.body.MarkDirty()
}

// --- blocks ---------------------------------------------------------------------------------

// invalidate drops every layout: the width, the mode or the fonts changed.
func (l *pixelLayout) invalidate() {
	l.stale, l.keep = true, 0
	l.laid = nil
}

// ensure reads the buffer and splits it into blocks again when it changed, keeping the layouts
// of the blocks wholly before the first changed line.
func (l *pixelLayout) ensure() {
	if !l.stale {
		return
	}
	c := l.e.core
	n := c.LineCount()
	l.lines = l.lines[:0]
	for i := range n {
		l.lines = append(l.lines, c.LineAt(i))
	}
	var blocks []Block
	if l.e.mode == Rendered && l.e.render != nil {
		blocks = l.e.render.Blocks(l.lines, 0, n)
	} else {
		blocks = make([]Block, n)
		for i := range n {
			blocks[i] = Block{From: i, To: i + 1}
		}
	}
	laid := make([]*laidBlock, len(blocks))
	for i := range min(len(blocks), len(l.blocks), len(l.laid)) {
		if blocks[i] != l.blocks[i] || blocks[i].To > l.keep {
			break
		}
		laid[i] = l.laid[i]
	}
	l.blocks, l.laid, l.stale, l.keep = blocks, laid, false, n
}

// blockOf is the index of the block holding line.
func (l *pixelLayout) blockOf(line int) int {
	l.ensure()
	lo, hi := 0, len(l.blocks)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if l.blocks[mid].To <= line {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return max(lo, 0)
}

func (l *pixelLayout) monoFont() gui.Font {
	if l.th.Mono.Size > 0 {
		return l.th.Mono
	}
	return gui.Font{Family: gui.CellFamily, Size: max(l.cell.H/1.2, 10)} // before the first paint: about the cells' font
}

func (l *pixelLayout) lineHeight(f gui.Font) float32 { return max(f.Size*1.2, 1) }

// heightOf is block i's height: as laid out, else an estimate of a row per line.
func (l *pixelLayout) heightOf(i int) float32 {
	if lb := l.laid[i]; lb != nil && lb.width == l.width {
		return lb.bl.Height
	}
	f := l.monoFont()
	if l.e.mode == Rendered {
		f = l.th.Prose
	}
	return float32(l.blocks[i].To-l.blocks[i].From) * l.lineHeight(f)
}

// topOf is block i's top, in the text's coordinates.
func (l *pixelLayout) topOf(i int) float32 {
	var y float32
	for j := range i {
		y += l.heightOf(j)
	}
	return y
}

// total is the text's height.
func (l *pixelLayout) total() float32 { return l.topOf(len(l.blocks)) }

// cursorLine is the core's cursor line.
func (l *pixelLayout) cursorLine() int {
	ln, _ := l.e.core.Line()
	return ln
}

// lay is block i laid out for the current width, mode and cursor. frame gives Raw's colours
// while painting; nil keeps a Raw block's last colours (geometry does not depend on them).
func (l *pixelLayout) lay(i int, frame *tuiwidget.HighlightFrame) *laidBlock {
	l.ensure()
	b := l.blocks[i]
	cl := l.cursorLine()
	inside := cl >= b.From && cl < b.To
	if l.e.mode == Rendered && l.e.render != nil {
		if lb := l.laid[i]; lb != nil && lb.width == l.width && lb.inside == inside {
			return lb
		}
		lb := &laidBlock{inside: inside, width: l.width}
		lb.bl = l.e.render.LayOut(b, l.lines, l.width, inside, l.sh, l.th, l)
		l.laid[i] = lb
		return lb
	}
	text := l.lines[b.From]
	var colors []color.NRGBA
	if frame != nil {
		colors = l.rawColors(b.From, text, frame)
	} else if lb := l.laid[i]; lb != nil {
		colors = lb.colors
	}
	if lb := l.laid[i]; lb != nil && lb.width == l.width && lb.text == text && sameColors(lb.colors, colors) {
		return lb
	}
	var colorAt func(int) color.NRGBA
	if colors != nil {
		colorAt = func(col int) color.NRGBA {
			if col < len(colors) {
				return colors[col]
			}
			return l.th.Text
		}
	}
	spans := rawSpans(b.From, text, l.monoFont(), colorAt)
	ws := flow.PreWrap
	if l.e.wrap == tuiwidget.WrapNone {
		ws = flow.Pre // one row per line; the view scrolls sideways instead
	}
	p := flow.Lay(spans, flow.Options{Width: l.width, WhiteSpace: ws, Color: l.th.Text, LineHeight: rawLineHeight}, l.sh)
	lb := &laidBlock{inside: inside, width: l.width, text: text, colors: colors,
		bl: BlockLayout{Lines: []LineLayout{{Para: p, Spans: spans}}, Height: p.Height}}
	l.laid[i] = lb
	return lb
}

// rawColors are line ln's cluster colours from the highlighter, or nil without one.
func (l *pixelLayout) rawColors(ln int, text string, frame *tuiwidget.HighlightFrame) []color.NRGBA {
	c := l.e.core
	if !c.Highlighting() {
		return nil
	}
	styles := frame.Styles(ln)
	dark := isDark(l.th.Background)
	var out []color.NRGBA
	col := 0
	for range tui.Graphemes(text) {
		k := highlight.Normal
		if col < len(styles) {
			k = styles[col]
		}
		cc := l.th.Text
		if st, ok := c.SyntaxStyle(k); ok {
			if fg, set := st.GetForeground(); set {
				if v, ok := colorOf(fg, l.e.body.styleTheme, dark); ok {
					cc = v
				}
			}
		}
		out = append(out, cc)
		col++
	}
	return out
}

func sameColors(a, b []color.NRGBA) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- positions ------------------------------------------------------------------------------

// posIn is (line, col)'s place in a laid block: the paragraph and its boundary.
func posIn(lb *laidBlock, line, col int) (li int, pos flow.Pos, ok bool) {
	for i, ll := range lb.bl.Lines {
		last := -1
		for si, sp := range ll.Spans {
			if sp.Line != line {
				continue
			}
			n := clusters(sp.Text)
			if col >= sp.Col && col < sp.Col+n {
				return i, flow.Pos{Span: si, Offset: byteOfCluster(sp.Text, col-sp.Col)}, true
			}
			last = si
		}
		if last >= 0 {
			return i, flow.Pos{Span: last, Offset: len(ll.Spans[last].Text)}, true
		}
	}
	return 0, flow.Pos{}, false
}

// caretAt is the caret at (line, col), in the text's coordinates.
func (l *pixelLayout) caretAt(line, col int) (gui.Rect, bool) {
	if l.sh == nil {
		h := l.lineHeight(l.monoFont())
		return gui.Rect{X: float32(col) * l.cell.W, Y: float32(line) * h, W: 1, H: h}, true
	}
	i := l.blockOf(line)
	if i >= len(l.blocks) {
		return gui.Rect{}, false
	}
	lb := l.lay(i, nil)
	li, pos, ok := posIn(lb, line, col)
	if !ok {
		return gui.Rect{}, false
	}
	// text coordinates: x from the text's left edge, y from the text's top
	ll := lb.bl.Lines[li]
	r := ll.Para.Caret(pos)
	r.Y += l.topOf(i) + ll.Y
	return r, true
}

// at is the buffer position under a point in the view (logical pixels from the body's top-left).
func (l *pixelLayout) at(x, y float32) (line, col int) {
	l.ensure()
	if len(l.blocks) == 0 || l.sh == nil {
		return 0, 0
	}
	dy := y - padY + l.scroll
	var top float32
	i := 0
	for ; i < len(l.blocks)-1; i++ {
		h := l.heightOf(i)
		if dy < top+h {
			break
		}
		top += h
	}
	lb := l.lay(i, nil)
	in := dy - top
	if len(lb.bl.Lines) == 0 {
		return l.blocks[i].From, 0
	}
	li := len(lb.bl.Lines) - 1
	for k, ll := range lb.bl.Lines {
		if in < ll.Y+ll.Para.Height {
			li = k
			break
		}
	}
	ll := lb.bl.Lines[li]
	pos := ll.Para.At(gui.Pt(x-l.textX(), in-ll.Y))
	if pos.Span >= len(ll.Spans) {
		return l.blocks[i].From, 0
	}
	sp := ll.Spans[pos.Span]
	return sp.Line, sp.Col + clusters(sp.Text[:min(pos.Offset, len(sp.Text))])
}

// scrollBy moves the view by dy pixels, within the text.
func (l *pixelLayout) scrollBy(dy float32) bool {
	was := l.scroll
	l.scroll = max(min(l.scroll+dy, l.total()-(l.height-2*padY)), 0)
	return l.scroll != was
}

func clusters(s string) int {
	n := 0
	for range tui.Graphemes(s) {
		n++
	}
	return n
}

// byteOfCluster is the byte offset of cluster i of s.
func byteOfCluster(s string, i int) int {
	off := 0
	for c := range tui.Graphemes(s) {
		if i == 0 {
			return off
		}
		off += len(c)
		i--
	}
	return off
}
