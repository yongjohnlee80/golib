package widget

import (
	"fmt"
	"iter"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Box is the base container: a bordered single-child panel
// where title (top border) and status line (bottom border) are
// configuration, not custom drawing. Every widget that reads as a "panel"
// (List, BufferView, TextArea, …) is used wrapped in a Box — one chrome
// implementation, uniform focus visuals.
//
// Focus visuals: when the Box or any descendant holds focus, FocusedStyle is
// merged (Inherit semantics, ADR-0006) over the base style. Defaults are
// token-driven — border style.TokenBorder, focused border
// style.TokenBorderFocused — so every panel gets the lazygit "active panel
// highlight" for free, re-themable via the Theme alone.
type Box struct {
	Base
	child tui.Component

	title       string
	titleAlign  style.Align
	status      string
	statusAlign style.Align

	base      style.Style // border + padding + colors; geometry source
	focusedSt style.Style // merged over base while focus is within
	titleSt   style.Style
	statusSt  style.Style

	focusable   bool
	focusWithin bool
}

var (
	_ tui.Container = (*Box)(nil)
	_ tui.Focusable = (*Box)(nil)
)

// boxConfig is the option-set state of NewBox.
type boxConfig struct {
	title       string
	titleAlign  style.Align
	status      string
	statusAlign style.Align
	border      style.BorderStyle
	borderSet   bool
	st          style.Style
	focusedSt   style.Style
	focusedSet  bool
	focusable   bool
}

// BoxOption customizes a Box under construction.
type BoxOption func(*boxConfig)

// WithTitle sets the title rendered inside the top border row.
func WithTitle(s string) BoxOption { return func(c *boxConfig) { c.title = s } }

// WithTitleAlign sets the title alignment: style.AlignLeft (default),
// style.AlignCenter, or style.AlignRight. Anything else panics.
func WithTitleAlign(a style.Align) BoxOption {
	if a > style.AlignRight {
		panic(fmt.Sprintf("widget: WithTitleAlign: %d is not AlignLeft/AlignCenter/AlignRight", a))
	}
	return func(c *boxConfig) { c.titleAlign = a }
}

// WithStatus sets the status line rendered inside the bottom border row.
func WithStatus(s string) BoxOption { return func(c *boxConfig) { c.status = s } }

// WithStatusAlign sets the status alignment: style.AlignRight (default),
// style.AlignLeft, or style.AlignCenter. Anything else panics.
func WithStatusAlign(a style.Align) BoxOption {
	if a > style.AlignRight {
		panic(fmt.Sprintf("widget: WithStatusAlign: %d is not AlignLeft/AlignCenter/AlignRight", a))
	}
	return func(c *boxConfig) { c.statusAlign = a }
}

// WithBorder sets the border prefab (default style.BorderNormal).
func WithBorder(b style.BorderStyle) BoxOption {
	return func(c *boxConfig) { c.border = b; c.borderSet = true }
}

// WithStyle sets the base style (padding, colors). Properties it leaves
// unset inherit the Box defaults (BorderNormal border, TokenBorder border
// foreground).
func WithStyle(s style.Style) BoxOption { return func(c *boxConfig) { c.st = s } }

// WithFocusedStyle replaces the style merged over the base while the Box or
// a descendant holds focus (default: border foreground
// style.TokenBorderFocused).
func WithFocusedStyle(s style.Style) BoxOption {
	return func(c *boxConfig) { c.focusedSt = s; c.focusedSet = true }
}

// WithFocusable opts the Box itself into the focus chain (child-less
// panes). Default false: focus belongs to the child.
func WithFocusable(v bool) BoxOption { return func(c *boxConfig) { c.focusable = v } }

// NewBox builds a bordered container around child (which may be nil for a
// chrome-only pane).
func NewBox(child tui.Component, opts ...BoxOption) *Box {
	cfg := boxConfig{titleAlign: style.AlignLeft, statusAlign: style.AlignRight}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	base := cfg.st
	if cfg.borderSet {
		base = base.Border(cfg.border)
	}
	base = base.Inherit(style.New().
		Border(style.BorderNormal).
		BorderForeground(style.TokenBorder))
	focused := cfg.focusedSt
	if !cfg.focusedSet {
		focused = style.New().BorderForeground(style.TokenBorderFocused)
	}
	return &Box{
		child:       child,
		title:       cfg.title,
		titleAlign:  cfg.titleAlign,
		status:      cfg.status,
		statusAlign: cfg.statusAlign,
		base:        base,
		focusedSt:   focused,
		titleSt:     style.New().Foreground(style.TokenForeground).Bold(true),
		statusSt:    style.New().Foreground(style.TokenTextMuted),
		focusable:   cfg.focusable,
	}
}

// SetTitle replaces the title. Repaint only: the title lives in the border
// row, so geometry is unchanged.
func (x *Box) SetTitle(s string) {
	x.title = s
	x.MarkDirty()
}

// SetStatus replaces the bottom-border status line. Repaint only.
func (x *Box) SetStatus(s string) {
	x.status = s
	x.MarkDirty()
}

// Init mounts the child. Re-entrant across remounts.
func (x *Box) Init(ctx *tui.Context) {
	x.Base.Init(ctx)
	x.focusWithin = false
	if x.child != nil {
		ctx.Mount(x.child)
	}
}

// AcceptsFocus implements tui.Focusable per WithFocusable.
func (x *Box) AcceptsFocus() bool { return x.focusable }

// Add sets the single child (Container contract). A Box wraps exactly one
// child; adding to an occupied Box panics.
func (x *Box) Add(children ...tui.Component) {
	for _, c := range children {
		if c == nil {
			panic("widget: Box.Add: nil child")
		}
		if x.child != nil {
			panic("widget: Box wraps exactly one child (ADR-0007 Q1) — nest a Flex/Split/Dock for more")
		}
		x.child = c
		if x.ctx != nil {
			x.ctx.Mount(c)
		}
	}
}

// Move is degenerate for a single-child container: only index 0 is
// legal, and moving the child to 0 is a no-op (Container contract).
func (x *Box) Move(child tui.Component, to int) {
	if child == nil || x.child != child {
		return
	}
	if to != 0 {
		panic("widget: Box.Move: Box wraps exactly one child; only index 0 is valid")
	}
}

// Remove unmounts the child and forgets it.
func (x *Box) Remove(child tui.Component) {
	if child == nil || x.child != child {
		return
	}
	x.child = nil
	if x.ctx != nil {
		x.ctx.Unmount(child)
	}
}

// Children enumerates the single child, if any.
func (x *Box) Children() iter.Seq[tui.Component] {
	return func(yield func(tui.Component) bool) {
		if x.child != nil {
			yield(x.child)
		}
	}
}

// frame returns the per-edge frame offsets derived from the base style
// (geometry always comes from the base: Inherit never merges padding).
func (x *Box) frame() (top, right, bottom, left int) {
	mt, mr, mb, ml := x.base.GetMargin()
	pt, pr, pb, pl := x.base.GetPadding()
	bt, br, bb, bl := borderEdgeSizes(x.base)
	return mt + bt + pt, mr + br + pr, mb + bb + pb, ml + bl + pl
}

// borderEdgeSizes mirrors the ADR-0006 frame math per edge: 1 when the
// border is set, the edge enabled, and its glyph non-empty.
func borderEdgeSizes(st style.Style) (t, r, b, l int) {
	bs, ok := st.GetBorder()
	if !ok {
		return 0, 0, 0, 0
	}
	et, er, eb, el := st.GetBorderEdges()
	if et && bs.Top != "" {
		t = 1
	}
	if er && bs.Right != "" {
		r = 1
	}
	if eb && bs.Bottom != "" {
		b = 1
	}
	if el && bs.Left != "" {
		l = 1
	}
	return t, r, b, l
}

// Layout subtracts the frame (border + padding + margin, ADR-0006 frame
// math) from the constraints, lays out the child, and reports child size +
// frame. Title and status cost zero extra rows: they render inside the
// border rows.
func (x *Box) Layout(c tui.Constraints) tui.Size {
	fh := x.base.GetHorizontalFrameSize()
	fv := x.base.GetVerticalFrameSize()
	if x.child == nil {
		w := boundedMax(c.MaxW, max(c.MinW, fh))
		h := boundedMax(c.MaxH, max(c.MinH, fv))
		return c.Constrain(tui.Size{W: w, H: h})
	}
	cc := tui.Constraints{
		MinW: max(c.MinW-fh, 0), MaxW: subFrame(c.MaxW, fh),
		MinH: max(c.MinH-fv, 0), MaxH: subFrame(c.MaxH, fv),
	}
	sz := x.ctx.LayoutChild(x.child, cc)
	ft, _, _, fl := x.frame()
	x.ctx.PlaceChild(x.child, tui.Rect{X: fl, Y: ft, W: sz.W, H: sz.H})
	return c.Constrain(tui.Size{W: sz.W + fh, H: sz.H + fv})
}

// HandleEvent consumes nothing (bubbling continues); it only observes
// component FocusEvents bubbling past to track the focus-within state that
// drives the focused border merge.
func (x *Box) HandleEvent(ev tui.Event) bool {
	if fe, ok := ev.(tui.FocusEvent); ok && !fe.Terminal {
		if x.focusWithin != fe.Gained {
			x.focusWithin = fe.Gained
			x.MarkDirty()
		}
	}
	return false
}

// paintStyle is the effective style for this frame: focused merge over base
// when focus is within (Inherit semantics — ADR-0006).
func (x *Box) paintStyle() style.Style {
	if x.focusWithin || x.focused() {
		return x.focusedSt.Inherit(x.base)
	}
	return x.base
}

// Render paints border, title, status, and the interior background. The
// child paints itself afterwards on its own sub-Surface.
func (x *Box) Render(s tui.Surface) {
	sz := s.Size()
	st := x.paintStyle()
	mt, mr, mb, ml := x.base.GetMargin()
	bx, by := ml, mt
	bw, bh := sz.W-ml-mr, sz.H-mt-mb
	if bw <= 0 || bh <= 0 {
		return
	}

	// Interior fill (inside the border), carrying the background.
	fill := style.New()
	if bg, ok := st.GetBackground(); ok {
		fill = fill.Background(bg)
	}
	bt, brr, bb, bl := borderEdgeSizes(x.base)
	inner := tui.Rect{X: bx + bl, Y: by + bt, W: bw - bl - brr, H: bh - bt - bb}
	if !inner.Empty() {
		s.Fill(inner, " ", fill)
	}

	bs, hasBorder := st.GetBorder()
	if hasBorder {
		x.renderBorder(s, st, bs, bx, by, bw, bh)
	}

	// Title (top border row) and status (bottom border row) truncate with
	// ellipsis when narrow; they sit between the corner cells.
	availW := bw - 2
	if bt == 1 && x.title != "" && availW > 0 {
		x.renderBorderText(s, " "+x.title+" ", x.titleAlign, x.titleSt.Inherit(fill), bx+1, by, availW)
	}
	if bb == 1 && x.status != "" && availW > 0 {
		x.renderBorderText(s, " "+x.status+" ", x.statusAlign, x.statusSt.Inherit(fill), bx+1, by+bh-1, availW)
	}
}

// renderBorder paints the enabled border edges with their per-edge
// foregrounds.
func (x *Box) renderBorder(s tui.Surface, st style.Style, bs style.BorderStyle, bx, by, bw, bh int) {
	et, er, eb, el := st.GetBorderEdges()
	bg, hasBG := st.GetBackground()
	edge := func(c style.Color, ok bool) style.Style {
		es := style.New()
		if ok {
			es = es.Foreground(c)
		}
		if hasBG {
			es = es.Background(bg)
		}
		return es
	}
	topSt := edge(st.GetBorderTopForeground())
	rightSt := edge(st.GetBorderRightForeground())
	bottomSt := edge(st.GetBorderBottomForeground())
	leftSt := edge(st.GetBorderLeftForeground())

	x2, y2 := bx+bw-1, by+bh-1
	if et && bs.Top != "" && bh > 0 {
		s.Fill(tui.Rect{X: bx, Y: by, W: bw, H: 1}, bs.Top, topSt)
	}
	if eb && bs.Bottom != "" && bh > 1 {
		s.Fill(tui.Rect{X: bx, Y: y2, W: bw, H: 1}, bs.Bottom, bottomSt)
	}
	if el && bs.Left != "" && bw > 0 {
		s.Fill(tui.Rect{X: bx, Y: by, W: 1, H: bh}, bs.Left, leftSt)
	}
	if er && bs.Right != "" && bw > 1 {
		s.Fill(tui.Rect{X: x2, Y: by, W: 1, H: bh}, bs.Right, rightSt)
	}
	// Corners paint where both adjoining edges exist.
	if et && el && bs.TopLeft != "" {
		s.SetCell(bx, by, bs.TopLeft, topSt)
	}
	if et && er && bs.TopRight != "" {
		s.SetCell(x2, by, bs.TopRight, topSt)
	}
	if eb && el && bs.BottomLeft != "" {
		s.SetCell(bx, y2, bs.BottomLeft, bottomSt)
	}
	if eb && er && bs.BottomRight != "" {
		s.SetCell(x2, y2, bs.BottomRight, bottomSt)
	}
}

// renderBorderText paints one aligned, ellipsis-truncated text run into a
// border row.
func (x *Box) renderBorderText(s tui.Surface, text string, align style.Align, st style.Style, xOff, y, availW int) {
	t := truncate(text, availW, s.StringWidth)
	tw := s.StringWidth(t)
	switch align {
	case style.AlignCenter:
		xOff += (availW - tw) / 2
	case style.AlignRight:
		xOff += availW - tw
	}
	cx := xOff
	for c := range tui.Graphemes(t) {
		cw := s.StringWidth(c)
		s.SetCell(cx, y, c, st)
		cx += cw
	}
}
