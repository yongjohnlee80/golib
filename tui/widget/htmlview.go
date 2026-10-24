package widget

import (
	"bytes"
	"math"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
	"github.com/yongjohnlee80/golib/tui/style"
)

// HTMLView shows an HTML document: read-only, scrollable, its text selectable and copied by
// Ctrl+C, its links reported by WithOnLink. It is not a browser: no script runs, and only the
// page's text and structure are drawn.
//
// Its layout is the backend's. In a terminal it lays the document out in cells (headings bold,
// links underlined, quotes barred, lists hanging, tables in columns when they fit, images as
// their alt text). A native backend binds a pixel layout with BindLayout before the first paint
// and draws the widget itself; the selection, the scroll and the signals stay this widget's, in
// document positions, whichever layout draws it, so input is the same code on both.
//
// SCROLL SYNC. Elements may carry data-src="from-to", the source bytes they show (golib's
// Markdown renderer writes them with its SourceSpans option). WithOnScroll reports the source
// byte of the block at the top as the view scrolls, and ScrollToSource brings a source byte's
// block to the top. SetHTML keeps the reader's place: the block that was at the top, found again
// by its source bytes, stays at the top.
type HTMLView struct {
	Base
	src       []byte
	layout    HTMLLayout
	sel       [2]DocPos
	scrollY   float32 // in the layout's units
	w, h      int
	linkRects []Rect32 // PointerShapes' scratch
	onLink    func(href string)
	onScroll  func(srcByte int)
	lastSrc   int // the source byte last reported to onScroll
	resolve   ImageResolver
	onRefused func(src string) // told of a resource the resolver refused (WithOnRefused)
	refused   map[string]bool  // what onRefused was told, until the resolver changes
	css       string
	diagrams  any
	diagramsN uint64 // how many times WithDiagrams set diagrams: a new one's answers are its own

	pressing bool
	pressAt  [2]float32 // where the left button went down, in the layout's units
	moved    bool
	edge     int // -1/+1 while a drag holds the pointer past the top or bottom
	edgeStop func()
	theme    *style.Theme // the surface's at the last paint, for a pixel layout's colours
}

// HTMLLayout is what an HTMLView needs from whoever lays it out: the cell layout here, a pixel
// layout from a native backend. Its coordinates are the document's, in its own units (cells, or
// pixels): y grows down from the document's top, whatever is scrolled.
type HTMLLayout interface {
	// SetSource reads a new document.
	SetSource(src []byte)
	// Height is the laid-out document's height.
	Height() float32
	// At is the document position nearest (x, y).
	At(x, y float32) DocPos
	// Rects are a selection's highlight.
	Rects(from, to DocPos) []Rect32
	// Text is the text a selection copies.
	Text(from, to DocPos) string
	// LinkAt is the link target under (x, y), "" when there is none.
	LinkAt(x, y float32) string
	// BlockAt is the block at y: the start of its source bytes (-1 when it has none) and its top.
	BlockAt(y float32) (srcFrom int, top float32)
	// BlockTop is the top of the block showing source byte b.
	BlockTop(srcByte int) (float32, bool)
}

// HTMLLayoutWidth is a layout the view tells its width, in the layout's units, as it is laid out.
type HTMLLayoutWidth interface {
	SetWidth(w float32)
}

// HTMLLayoutTasks is a layout that runs work off the loop (decoding an image) through the view's
// Context.Go: the view hands it each TaskResult, and lays out and paints again when it took one.
type HTMLLayoutTasks interface {
	HandleTask(r tui.TaskResult) bool
}

// HTMLLayoutHScroll is a layout with parts that scroll sideways on their own (a pre wider than
// the page): the view hands it a sideways wheel (Shift+wheel, or a horizontal wheel) at (x, y).
type HTMLLayoutHScroll interface {
	ScrollX(x, y, dx float32) bool
}

// HTMLLayoutLinks is a layout that tells where its links are: LinkRects appends to dst each link's
// rects, in the layout's units, that fall between top and bottom. For a cell layout
// (HTMLLayoutCells) the view shows the pointer's hand over them; a layout a backend paints gives
// its hands itself (gui's PointerRegionsView).
type HTMLLayoutLinks interface {
	LinkRects(top, bottom float32, dst []Rect32) []Rect32
}

// HTMLLayoutActivate is a layout with parts a click acts on (a <details>' summary, which folds
// and opens it): the view asks it on a click with no drag, before following a link; true when it
// acted.
type HTMLLayoutActivate interface {
	Activate(x, y float32) bool
}

// HTMLLayoutImages is a layout that keeps the images it loaded: SetImageResolver drops them and
// their pending loads, since a page's relative src means another file under another resolver.
type HTMLLayoutImages interface {
	ResetImages()
}

// HTMLLayoutCells is a layout drawn in the view's own cells, as the built-in one is. The view lays
// it out a column narrower when it is taller than the view, for the scroll indicator, and paints it
// from row top, w cells wide, with the selection from..to marked. A layout without it is drawn by
// its backend.
type HTMLLayoutCells interface {
	HTMLLayout
	HTMLLayoutWidth
	PaintCells(s tui.Surface, top, w int, from, to DocPos)
}

// HTMLLayoutPixels is a layout whose units are the backend's pixels rather than cells. The view
// turns a pointer's cell (and the point inside it) into pixels with the App's CellPixels, and
// scrolls by a cell's height a line.
type HTMLLayoutPixels interface {
	Pixels() bool
}

// DocPos is a position in an HTMLView's document, between two clusters of text: the block it is
// in, the span of that block, and the byte offset in the span's text. Its numbering is the
// layout's own; positions compare in document order.
type DocPos struct{ Block, Span, Offset int }

// Before reports whether p comes before o in the document: by block, then span, then offset.
func (p DocPos) Before(o DocPos) bool {
	if p.Block != o.Block {
		return p.Block < o.Block
	}
	if p.Span != o.Span {
		return p.Span < o.Span
	}
	return p.Offset < o.Offset
}

// OrderDocPos is a and b in document order, the earlier first: a selection's ends, however it was
// dragged.
func OrderDocPos(a, b DocPos) (DocPos, DocPos) {
	if b.Before(a) {
		return b, a
	}
	return a, b
}

// SourceIndex is which of n blocks shows source byte b, by the one rule an HTML layout answers it
// with: the block whose source holds b, else the first that starts after it, else (a byte after
// every block, the blank lines that end a note) the last before it. src(i) is block i's source
// bytes, from-to, with from -1 when it has none. -1 when no block has any.
func SourceIndex(n int, src func(i int) [2]int, b int) int {
	after, last := -1, -1
	for i := range n {
		s := src(i)
		if s[0] < 0 {
			continue
		}
		if s[0] <= b && b < s[1] {
			return i
		}
		if s[0] >= b {
			if after < 0 {
				after = i
			}
			continue
		}
		last = i
	}
	if after < 0 {
		return last
	}
	return after
}

// Rect32 is a rectangle in a layout's units.
type Rect32 struct{ X, Y, W, H float32 }

// HTMLOption configures an HTMLView.
type HTMLOption func(*HTMLView)

// WithOnLink is called with a link's target when the link is clicked (a press and release on it
// without a drag).
func WithOnLink(fn func(href string)) HTMLOption { return func(v *HTMLView) { v.onLink = fn } }

// SetOnLink replaces what a clicked link calls, as WithOnLink gives it.
func (v *HTMLView) SetOnLink(fn func(href string)) { v.onLink = fn }

// WithOnRefused is told, once per source for each page shown (until a different page, or another
// resolver), of a resource the page names that the resolver refused (ErrImageRefused): a
// stylesheet or an image outside its root, or a URL. A host says why a page draws without it.
func WithOnRefused(fn func(src string)) HTMLOption { return func(v *HTMLView) { v.onRefused = fn } }

// SetOnRefused replaces what WithOnRefused gives.
func (v *HTMLView) SetOnRefused(fn func(src string)) { v.onRefused = fn }

// Refused is a layout's report of a resource its resolver refused; the view tells the host once.
func (v *HTMLView) Refused(src string) {
	if v.refused[src] {
		return
	}
	if v.refused == nil {
		v.refused = map[string]bool{}
	}
	v.refused[src] = true
	if v.onRefused != nil {
		v.onRefused(src)
	}
}

// WithOnScroll is called with the source byte of the block at the top as the view scrolls to
// another block (data-src); blocks without source bytes report nothing.
func WithOnScroll(fn func(srcByte int)) HTMLOption { return func(v *HTMLView) { v.onScroll = fn } }

// WithImageResolver gives a pixel layout its images (DirImages). The cell layout shows an image's
// alt text whatever it is; with none, a pixel layout loads data: URIs alone.
func WithImageResolver(r ImageResolver) HTMLOption { return func(v *HTMLView) { v.resolve = r } }

// WithStylesheet adds a stylesheet a pixel layout applies after the page's own. The cell layout
// draws by structure, not by style.
func WithStylesheet(css string) HTMLOption { return func(v *HTMLView) { v.css = css } }

// WithDiagrams gives a pixel layout what draws a page's diagrams (pre class="mermaid"), held
// opaque here; the cell layout shows a diagram's source.
func WithDiagrams(d any) HTMLOption {
	return func(v *HTMLView) { v.diagrams, v.diagramsN = d, v.diagramsN+1 }
}

// NewHTMLView builds an empty view, laid out in cells until a backend binds another layout.
func NewHTMLView(opts ...HTMLOption) *HTMLView {
	v := &HTMLView{lastSrc: -1}
	v.layout = newCellHTML(v.measure)
	for _, o := range opts {
		if o != nil {
			o(v)
		}
	}
	return v
}

var _ tui.Focusable = (*HTMLView)(nil)

// AcceptsFocus implements tui.Focusable: a scrollable, selectable viewer.
func (v *HTMLView) AcceptsFocus() bool { return true }

// BindLayout replaces the cell layout with l (a native backend's pixel layout), given the
// document the view holds. A native style calls it once, before the first paint.
func (v *HTMLView) BindLayout(l HTMLLayout) {
	if l == nil {
		return
	}
	v.layout = l
	v.sel = [2]DocPos{}
	v.scrollY = 0
	v.layout.SetSource(v.src)
	v.RequestLayout()
}

// BoundLayout returns the layout the view draws with: the cell layout, or the one BindLayout gave.
func (v *HTMLView) BoundLayout() HTMLLayout { return v.layout }

// Source is the document the view shows.
func (v *HTMLView) Source() []byte { return v.src }

// Stylesheet is the one WithStylesheet or SetStylesheet gave, for a pixel layout.
func (v *HTMLView) Stylesheet() string { return v.css }

// SetStylesheet replaces the extra stylesheet; a pixel layout reads it again.
func (v *HTMLView) SetStylesheet(css string) {
	v.css = css
	v.SetHTML(v.src)
}

// Images is the resolver WithImageResolver gave, for a pixel layout; nil for none.
func (v *HTMLView) Images() ImageResolver { return v.resolve }

// SetImageResolver replaces the resolver a pixel layout loads images through.
// The layout's images and their pending loads are dropped: a host sets it before the page it
// serves, and only when the folder its paths are read from changes.
func (v *HTMLView) SetImageResolver(r ImageResolver) {
	v.resolve = r
	v.refused = nil
	if l, ok := v.layout.(HTMLLayoutImages); ok {
		l.ResetImages()
	}
}

// Diagrams is what WithDiagrams gave, for a pixel layout; nil for none.
func (v *HTMLView) Diagrams() any { return v.diagrams }

// DiagramsSet counts the times WithDiagrams set the view's diagrams: a pixel layout asks again,
// once, when it changes, whatever the Diagrammer is (any value, comparable or not).
func (v *HTMLView) DiagramsSet() uint64 { return v.diagramsN }

// SetHTML shows src, keeping the reader's place: the block that was at the top, found again by
// its source bytes, keeps its offset from the top; without source bytes the scroll keeps its y.
// The selection is cleared, as its positions belong to the old document.
// ShowDocument shows text as the page: SetHTML, as a gui Editor's document view calls it.
func (v *HTMLView) ShowDocument(text []byte) { v.SetHTML(text) }

func (v *HTMLView) SetHTML(src []byte) {
	if !bytes.Equal(src, v.src) {
		v.refused = nil // another page: what it refuses is told again
	}
	anchor, top := v.layout.BlockAt(v.scrollY)
	off := v.scrollY - top
	v.src = src
	v.layout.SetSource(src)
	v.sel = [2]DocPos{}
	if anchor >= 0 {
		if t, ok := v.layout.BlockTop(anchor); ok {
			v.scrollY = t + max(off, 0)
		}
	}
	v.clampScroll()
	v.RequestLayout()
	v.MarkDirty()
}

// ScrollToSource brings the block showing source byte b to the top.
func (v *HTMLView) ScrollToSource(b int) {
	t, ok := v.layout.BlockTop(b)
	if !ok {
		return
	}
	v.scrollTo(t)
}

// ScrollY is the scroll position, in the layout's units: the document y at the view's top.
func (v *HTMLView) ScrollY() float32 { return v.scrollY }

// Selection is the selection's two ends, in the order they were made.
func (v *HTMLView) Selection() (anchor, head DocPos) { return v.sel[0], v.sel[1] }

// SelectedText is the selection's text, "" when nothing is selected.
func (v *HTMLView) SelectedText() string {
	if v.sel[0] == v.sel[1] {
		return ""
	}
	return v.layout.Text(v.sel[0], v.sel[1])
}

// Copy puts the selection's text on the clipboard; it reports whether there was any.
func (v *HTMLView) Copy() bool {
	t := v.SelectedText()
	if t == "" || v.Context() == nil {
		return false
	}
	return v.Context().CopyToClipboard(t)
}

// units are the layout's units per cell: 1 for cells; a cell's pixels for a pixel layout.
func (v *HTMLView) units() (float32, float32) {
	if p, ok := v.layout.(HTMLLayoutPixels); ok && p.Pixels() && v.Context() != nil {
		if cp := v.Context().CellPixels(); cp.W > 0 && cp.H > 0 {
			return cp.W, cp.H
		}
	}
	return 1, 1
}

// docPoint is the document point under a pointer at cell (x, y), inside it at (subX, subY).
func (v *HTMLView) docPoint(m tui.MouseEvent) (float32, float32) {
	ux, uy := v.units()
	if ux == 1 && uy == 1 {
		return float32(m.X), float32(m.Y) + v.scrollY
	}
	return (float32(m.X) + m.SubX) * ux, (float32(m.Y)+m.SubY)*uy + v.scrollY
}

func (v *HTMLView) viewHeight() float32 {
	_, uy := v.units()
	return float32(v.h) * uy
}

func (v *HTMLView) maxScroll() float32 { return max(v.layout.Height()-v.viewHeight(), 0) }

func (v *HTMLView) clampScroll() { v.scrollY = min(max(v.scrollY, 0), v.maxScroll()) }

// scrollTo scrolls to y and reports the block at the top when it is another.
func (v *HTMLView) scrollTo(y float32) {
	v.scrollY = y
	v.clampScroll()
	v.MarkDirty()
	v.reportScroll()
}

func (v *HTMLView) scrollBy(dy float32) { v.scrollTo(v.scrollY + dy) }

func (v *HTMLView) reportScroll() {
	if v.onScroll == nil {
		return
	}
	src, _ := v.layout.BlockAt(v.scrollY)
	if src < 0 || src == v.lastSrc {
		return
	}
	v.lastSrc = src
	v.onScroll(src)
}

// HandleEvent scrolls by the keys and the wheel, selects by a drag, follows a clicked link, and
// copies the selection by Ctrl+C or y. The keys: Up/Down or k/j a line, PgUp/PgDn or Space a view,
// Ctrl+U/Ctrl+D half a view, Home/End or g/G the ends.
func (v *HTMLView) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.KeyEvent:
		return v.key(e)
	case tui.MouseEvent:
		return v.mouse(e)
	case tui.PointerCaptureLostEvent:
		v.endDrag()
		return true
	case tui.TaskResult:
		if l, ok := v.layout.(HTMLLayoutTasks); ok && l.HandleTask(e) {
			v.RequestLayout()
			v.MarkDirty()
			return true
		}
		return false
	case tui.TickEvent:
		if v.pressing && v.edge != 0 {
			_, uy := v.units()
			v.scrollBy(float32(v.edge) * uy)
			x, y := v.pressAt[0], v.scrollY
			if v.edge > 0 {
				y += v.viewHeight() - 1
			}
			v.sel[1] = v.layout.At(x, y)
		}
		return false
	}
	return false
}

// keyOf is the HTMLView command e is, if any; Esc is one only with a selection to clear, else it is
// the parent's.
func (v *HTMLView) keyOf(e tui.KeyEvent, hasSelection bool) (keymap.Command, bool) {
	cmd, ok := v.Context().KeyCommand(e, htmlViewCommands...)
	if ok && cmd.ID == HTMLViewClear.ID && !hasSelection {
		return keymap.Command{}, false
	}
	return cmd, ok
}

func (v *HTMLView) key(e tui.KeyEvent) bool {
	cmd, ok := v.keyOf(e, v.sel[0] != v.sel[1])
	if !ok {
		return false
	}
	_, uy := v.units()
	page := max(v.viewHeight()-uy, uy)
	half := max(v.viewHeight()/2, uy)
	switch cmd.ID {
	case HTMLViewUp.ID:
		v.scrollBy(-uy)
	case HTMLViewDown.ID:
		v.scrollBy(uy)
	case HTMLViewPageUp.ID:
		v.scrollBy(-page)
	case HTMLViewPageDown.ID:
		v.scrollBy(page)
	case HTMLViewHalfUp.ID:
		v.scrollBy(-half)
	case HTMLViewHalfDown.ID:
		v.scrollBy(half)
	case HTMLViewTop.ID:
		v.scrollTo(0)
	case HTMLViewBottom.ID:
		v.scrollTo(v.maxScroll())
	case HTMLViewCopy.ID:
		return v.Copy()
	case HTMLViewClear.ID:
		v.sel = [2]DocPos{}
		v.MarkDirty()
	}
	return true
}

// KeyScope is an HTMLView's key-map scope.
func (v *HTMLView) KeyScope() keymap.Scope { return HTMLViewScope }

// KeyBindings is the HTMLView's commands as m binds them (tui.KeyBinder).
func (v *HTMLView) KeyBindings(m *keymap.Map) []keymap.Binding {
	return tui.BindingsOf(m, htmlViewCommands...)
}

func (v *HTMLView) mouse(m tui.MouseEvent) bool {
	_, uy := v.units()
	if h, ok := v.layout.(HTMLLayoutHScroll); ok && m.Kind == tui.MouseWheel {
		dx := float32(0)
		switch {
		case m.Button == tui.WheelLeft, m.Button == tui.WheelUp && m.Mods&tui.ModShift != 0:
			dx = -3
		case m.Button == tui.WheelRight, m.Button == tui.WheelDown && m.Mods&tui.ModShift != 0:
			dx = 3
		}
		if dx != 0 {
			ux, _ := v.units()
			x, y := v.docPoint(m)
			if h.ScrollX(x, y, dx*ux) {
				v.MarkDirty()
			}
			return true
		}
	}
	switch {
	case m.Kind == tui.MouseWheel && m.Button == tui.WheelUp:
		v.scrollBy(-3 * uy)
	case m.Kind == tui.MouseWheel && m.Button == tui.WheelDown:
		v.scrollBy(3 * uy)
	case m.Kind == tui.MousePress && m.Button == tui.MouseLeft:
		x, y := v.docPoint(m)
		v.pressing, v.moved, v.pressAt = true, false, [2]float32{x, y}
		p := v.layout.At(x, y)
		v.sel = [2]DocPos{p, p}
		if ctx := v.Context(); ctx != nil {
			ctx.CapturePointer()
			ctx.RequestFocus()
		}
		v.MarkDirty()
	case m.Kind == tui.MouseMotion && v.pressing:
		x, y := v.docPoint(m)
		v.moved = v.moved || x != v.pressAt[0] || y != v.pressAt[1]
		v.pressAt[0] = x
		v.setEdge(m.Y)
		v.sel[1] = v.layout.At(x, y)
		v.MarkDirty()
	case m.Kind == tui.MouseRelease && v.pressing:
		x, y := v.docPoint(m)
		v.endDrag()
		if !v.moved && v.sel[0] == v.sel[1] {
			if a, ok := v.layout.(HTMLLayoutActivate); ok && a.Activate(x, y) {
				v.RequestLayout()
				v.MarkDirty()
				return true
			}
			if href := v.layout.LinkAt(x, y); href != "" && v.onLink != nil {
				v.onLink(href)
			}
		}
	default:
		return false
	}
	return true
}

// setEdge starts or stops scrolling by a drag held past the top or bottom row.
func (v *HTMLView) setEdge(y int) {
	edge := 0
	switch {
	case y < 0:
		edge = -1
	case v.h > 0 && y >= v.h:
		edge = 1
	}
	if edge == v.edge {
		return
	}
	v.edge = edge
	if edge != 0 && v.edgeStop == nil {
		if ctx := v.Context(); ctx != nil {
			v.edgeStop = ctx.Every(htmlDragScrollInterval)
		}
	}
	if edge == 0 && v.edgeStop != nil {
		v.edgeStop()
		v.edgeStop = nil
	}
}

// htmlDragScrollInterval is a drag's scroll pace while the pointer is past an edge.
const htmlDragScrollInterval = 50 * time.Millisecond

// endDrag ends a press: the selection stays, for Ctrl+C.
func (v *HTMLView) endDrag() {
	v.pressing = false
	v.setEdge(0)
	if ctx := v.Context(); ctx != nil && ctx.HasPointerCapture() {
		ctx.ReleasePointer()
	}
	v.MarkDirty()
}

// Layout is greedy on both axes. The cell layout is laid out at the width, a column narrower when
// the document is taller than the view, for the scroll indicator.
func (v *HTMLView) Layout(c tui.Constraints) tui.Size {
	v.w = fillW(c)
	v.h = fillH(c)
	if cells, ok := v.layout.(HTMLLayoutCells); ok {
		cells.SetWidth(float32(v.w))
		if cells.Height() > float32(v.h) && v.w > 1 {
			cells.SetWidth(float32(v.w - 1))
		}
	} else if lw, ok := v.layout.(HTMLLayoutWidth); ok {
		ux, _ := v.units()
		lw.SetWidth(float32(v.w) * ux)
	}
	v.clampScroll()
	return c.Constrain(tui.Size{W: v.w, H: v.h})
}

// Theme is the tui theme of the surface the view last painted on: what a pixel layout takes its
// accent and muted colours from. nil before the first paint.
func (v *HTMLView) Theme() *style.Theme { return v.theme }

// PointerShapes is the hand over the links that show, when a cell layout tells where they are
// (HTMLLayoutLinks): a cell any part of a link falls in. A bound layout the backend paints gives
// its own, after it paints (gui.PointerRegionsView): its links are where that paint laid them out.
func (v *HTMLView) PointerShapes(dst []tui.PointerRegion) []tui.PointerRegion {
	links, ok := v.layout.(HTMLLayoutLinks)
	if _, cells := v.layout.(HTMLLayoutCells); !ok || !cells || v.w <= 0 || v.h <= 0 {
		return dst
	}
	ux, uy := v.units()
	v.linkRects = links.LinkRects(v.scrollY, v.scrollY+v.viewHeight(), v.linkRects[:0])
	for _, r := range v.linkRects {
		x0, x1 := int(math.Floor(float64(r.X/ux))), int(math.Ceil(float64((r.X+r.W)/ux)))
		y0, y1 := int(math.Floor(float64((r.Y-v.scrollY)/uy))), int(math.Ceil(float64((r.Y+r.H-v.scrollY)/uy)))
		if x1 > x0 && y1 > y0 {
			dst = append(dst, tui.PointerRegion{Rect: tui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, Shape: tui.PointerHand})
		}
	}
	return dst
}

// Render paints the cell layout. A bound layout is drawn by its backend: the cells under it are
// filled with the page's colours, which the native view reads as its own.
func (v *HTMLView) Render(s tui.Surface) {
	v.theme = s.Theme()
	cells, ok := v.layout.(HTMLLayoutCells)
	if !ok {
		sz := s.Size()
		s.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", style.New().Foreground(style.TokenForeground).Background(style.TokenBackground))
		return
	}
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	top := int(v.scrollY)
	w := sz.W
	rows := int(cells.Height())
	if rows > sz.H {
		w--
		paintScrollIndicator(s, sz.W-1, sz.H, top, rows-sz.H)
	}
	cells.PaintCells(s, top, w, v.sel[0], v.sel[1])
}
