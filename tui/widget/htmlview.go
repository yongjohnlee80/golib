package widget

import (
	"time"

	"github.com/yongjohnlee80/golib/tui"
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
	src      []byte
	layout   HTMLLayout
	cells    *cellHTML // the default layout; nil once another is bound
	sel      [2]DocPos
	scrollY  float32 // in the layout's units
	w, h     int
	onLink   func(href string)
	onScroll func(srcByte int)
	lastSrc  int // the source byte last reported to onScroll
	resolve  ImageResolver
	css      string
	diagrams any

	pressing bool
	pressAt  [2]float32 // where the left button went down, in the layout's units
	moved    bool
	edge     int // -1/+1 while a drag holds the pointer past the top or bottom
	edgeStop func()
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

// Rect32 is a rectangle in a layout's units.
type Rect32 struct{ X, Y, W, H float32 }

// HTMLOption configures an HTMLView.
type HTMLOption func(*HTMLView)

// WithOnLink is called with a link's target when the link is clicked (a press and release on it
// without a drag).
func WithOnLink(fn func(href string)) HTMLOption { return func(v *HTMLView) { v.onLink = fn } }

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
func WithDiagrams(d any) HTMLOption { return func(v *HTMLView) { v.diagrams = d } }

// NewHTMLView builds an empty view, laid out in cells until a backend binds another layout.
func NewHTMLView(opts ...HTMLOption) *HTMLView {
	v := &HTMLView{lastSrc: -1}
	v.cells = newCellHTML(v.measure)
	v.layout = v.cells
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
	v.layout, v.cells = l, nil
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
func (v *HTMLView) SetImageResolver(r ImageResolver) { v.resolve = r }

// Diagrams is what WithDiagrams gave, for a pixel layout; nil for none.
func (v *HTMLView) Diagrams() any { return v.diagrams }

// SetHTML shows src, keeping the reader's place: the block that was at the top, found again by
// its source bytes, keeps its offset from the top; without source bytes the scroll keeps its y.
// The selection is cleared, as its positions belong to the old document.
func (v *HTMLView) SetHTML(src []byte) {
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
// copies the selection by Ctrl+C or y.
func (v *HTMLView) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.KeyEvent:
		return v.key(e)
	case tui.MouseEvent:
		return v.mouse(e)
	case tui.PointerCaptureLostEvent:
		v.endDrag()
		return true
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

func (v *HTMLView) key(e tui.KeyEvent) bool {
	if e.Kind == tui.KeyRelease {
		return false
	}
	_, uy := v.units()
	page := max(v.viewHeight()-uy, uy)
	switch {
	case e.Code == tui.KeyUp:
		v.scrollBy(-uy)
	case e.Code == tui.KeyDown:
		v.scrollBy(uy)
	case e.Code == tui.KeyPageUp:
		v.scrollBy(-page)
	case e.Code == tui.KeyPageDown || e.Code == ' ' && e.Mods.Chord() == 0:
		v.scrollBy(page)
	case e.Code == tui.KeyHome:
		v.scrollTo(0)
	case e.Code == tui.KeyEnd:
		v.scrollTo(v.maxScroll())
	case e.Code == 'c' && e.Mods.Chord() == tui.ModCtrl, e.Code == 'y' && e.Mods.Chord() == 0:
		return v.Copy()
	case e.Code == tui.KeyEscape && v.sel[0] != v.sel[1]:
		v.sel = [2]DocPos{}
		v.MarkDirty()
	default:
		return false
	}
	return true
}

func (v *HTMLView) mouse(m tui.MouseEvent) bool {
	_, uy := v.units()
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
	v.w = boundedMax(c.MaxW, max(c.MinW, 1))
	v.h = boundedMax(c.MaxH, max(c.MinH, 1))
	if v.cells != nil {
		v.cells.SetWidth(float32(v.w))
		if v.cells.Height() > float32(v.h) && v.w > 1 {
			v.cells.SetWidth(float32(v.w - 1))
		}
	} else if lw, ok := v.layout.(HTMLLayoutWidth); ok {
		ux, _ := v.units()
		lw.SetWidth(float32(v.w) * ux)
	}
	v.clampScroll()
	return c.Constrain(tui.Size{W: v.w, H: v.h})
}

// Render paints the cell layout; a bound layout is drawn by its backend.
func (v *HTMLView) Render(s tui.Surface) {
	if v.cells == nil {
		return
	}
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	top := int(v.scrollY)
	w := sz.W
	rows := int(v.cells.Height())
	if rows > sz.H {
		w--
		paintScrollIndicator(s, sz.W-1, sz.H, top, rows-sz.H)
	}
	v.cells.paint(s, top, w, v.sel[0], v.sel[1])
}
