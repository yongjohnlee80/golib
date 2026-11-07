package widget

import (
	"fmt"
	"iter"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Box is the standard panel container: a bordered, single-child panel
// where title (embedded in the top border) and status line (embedded in the
// bottom border) are declarative configuration rather than custom drawing routines.
//
// Every widget that presents as an application "panel" (such as [List], [Table],
// [Tree], [BufferView], or [Editor]) is intended to be used wrapped inside a [Box].
// This provides a single, uniform chrome implementation and consistent focus
// visual treatment across the entire application.
//
// # In-Border Chrome Layout Model
//
// Box embeds its title and status lines directly into the border glyph rows:
//
//	 TopLeft          Title (truncated with ellipsis)          TopRight
//	    ┌─ [Explorer: /src/app] ────────────────────────────────────┐
//	    │                                                           │
//	    │                    Child Content Area                     │
//	    │                  (List, Table, Editor)                    │
//	    │                                                           │
//	    └──────────────────────────────────────────── [1/42 items] ─┘
//	BottomLeft                                    Status (truncated)  BottomRight
//
// Design Rationale:
// Traditional TUI frameworks frequently consume dedicated terminal lines above
// or below a widget for titles and keybinding hints. In terminal applications
// where vertical screen real estate is at a premium, dedicating separate rows
// per panel severely constrains data visibility. Box embeds text directly into
// the Top and Bottom border rows, delivering informative framing with ZERO
// extra vertical height consumption.
//
// # Focus Visuals and Style Inheritance
//
// When the Box itself or ANY component in its descendant subtree currently holds
// focus, FocusedStyle is merged ([style.Style.Inherit] semantics) over the base style.
// The default styles are semantic token-driven:
//   - Unfocused border: [style.TokenBorder]
//   - Focused border:   [style.TokenBorderFocused]
//
// This architecture gives every panel the familiar active-panel highlight (made
// famous by tools like lazygit) for free, completely re-themable through the
// application [style.Theme] without modifying widget code.
//
// # Architectural Invariants
//
//  1. Single-Child Encapsulation: Box hosts at most one direct child; geometry constraints
//     are calculated by subtracting border lines and padding margins before passing to the child.
//  2. Focus-Within Invariant: Box tracks both direct focus and focus across its entire descendant
//     subtree, dynamically applying [style.TokenBorderFocused] to highlight active panels.
//  3. Chrome Zero-Height Penalty: Title and status strings are drawn directly into the top and
//     bottom border rows (with ellipsis truncation), preserving maximum vertical space for content.
//
// # Concurrency Model
//
//   - Ownership: loop-goroutine-owned. Mutation of title, status, or children must occur on the loop.
//   - Layout/Render: Synchronous within the runtime frame cycle.
//
// # Usage Examples
//
// 1. Wrapping a list in a standard titled panel:
//
//	panel := widget.NewBox(myList,
//		widget.WithTitle("Git Branches"),
//		widget.WithStatus("enter: checkout | d: delete"),
//	)
//
// 2. Custom border styling with centered header:
//
//	modalBox := widget.NewBox(form,
//		widget.WithTitle("Create Pull Request"),
//		widget.WithTitleAlign(style.AlignCenter),
//		widget.WithBorder(style.BorderRounded),
//	)
//
// 3. Child-less focusable pane (e.g. an empty placeholder panel):
//
//	emptyPane := widget.NewBox(nil,
//		widget.WithTitle("Preview"),
//		widget.WithFocusable(true),
//	)
type Box struct {
	Base
	child tui.Component

	title       uiText
	titleAlign  style.Align
	titleElide  Elide // where a title too wide for its row loses text: the end (ElideRight) unless set
	status      string
	statusAlign style.Align

	base      style.Style // border + padding + colors; geometry source
	focusedSt style.Style // merged over base while focus is within
	// border and borderSet are WithBorder's, kept so a runtime restyle
	// derives the base as the constructor did.
	border    style.BorderStyle
	borderSet bool
	titleSt   style.Style
	statusSt  style.Style

	focusable   bool
	focusWithin bool

	// maxW caps the box's width, border included; 0 for none (SetMaximumWidth). inL and inR are
	// the columns the last layout left either side of the capped box, so it is centred.
	maxW, inL, inR int

	// slot is the control on the title row (SetTitleSlot), nil for none; slotW is the columns the
	// last layout gave it there, 0 while it is hidden (the box too narrow, or the slot empty).
	slot  tui.Component
	slotW int
	// foot is the control on the bottom border row (SetBottomSlot), nil for none; footW is the
	// columns the last layout gave it, 0 while it is hidden.
	foot  tui.Component
	footW int
}

// SetMaximumWidth caps the box at w columns, border included, centred in what it is given — CSS's
// max-width with auto margins: a page of text as wide as reading wants, however wide the screen.
// 0 removes the cap.
func (x *Box) SetMaximumWidth(w int) {
	x.maxW = max(w, 0)
	x.RequestLayout()
}

var (
	_ tui.Container     = (*Box)(nil)
	_ tui.Focusable     = (*Box)(nil)
	_ tui.FocusDesigner = (*Box)(nil)
)

// boxConfig is the option-set state of NewBox.
type boxConfig struct {
	title       string
	titleAlign  style.Align
	titleElide  Elide
	status      string
	statusAlign style.Align
	border      style.BorderStyle
	borderSet   bool
	st          style.Style
	focusedSt   style.Style
	focusedSet  bool
	focusable   bool
	slot        tui.Component
	foot        tui.Component
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

// WithTitleElide sets where a title too wide for its row loses text (SetTitleElide).
func WithTitleElide(e Elide) BoxOption { return func(c *boxConfig) { c.titleElide = e } }

// WithTitleSlot puts c on the box's title row (SetTitleSlot).
func WithTitleSlot(c tui.Component) BoxOption { return func(cfg *boxConfig) { cfg.slot = c } }

// WithBottomSlot puts c on the box's bottom border row (SetBottomSlot).
func WithBottomSlot(c tui.Component) BoxOption { return func(cfg *boxConfig) { cfg.foot = c } }

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
	return &Box{
		child:       child,
		title:       plainText(cfg.title),
		titleAlign:  cfg.titleAlign,
		titleElide:  cfg.titleElide,
		status:      cfg.status,
		statusAlign: cfg.statusAlign,
		base:        boxBase(cfg.st, cfg.border, cfg.borderSet),
		focusedSt:   boxFocused(cfg.focusedSt, cfg.focusedSet),
		border:      cfg.border,
		borderSet:   cfg.borderSet,
		titleSt:     style.New().Foreground(style.TokenForeground).Bold(true),
		statusSt:    style.New().Foreground(style.TokenTextMuted),
		focusable:   cfg.focusable,
		slot:        cfg.slot,
		foot:        cfg.foot,
	}
}

// boxBase is a Box's base style: the given look, WithBorder's border, and
// golib's normal border in the border token under both.
func boxBase(st style.Style, border style.BorderStyle, borderSet bool) style.Style {
	if borderSet {
		st = st.Border(border)
	}
	return st.Inherit(style.New().Border(style.BorderNormal).BorderForeground(style.TokenBorder))
}

// boxFocused is the style merged over the base while focus is inside: the
// given one, or the focused-border token.
func boxFocused(st style.Style, set bool) style.Style {
	if !set {
		return style.New().BorderForeground(style.TokenBorderFocused)
	}
	return st
}

// SetTitle replaces the title. Repaint only: the title lives in the border
// row, so geometry is unchanged.
//
// It ends a title set by [Box.SetTitleMessage].
func (x *Box) SetTitle(s string) {
	x.title = plainText(s)
	x.MarkDirty()
}

// SetTitleMessage shows a catalog message as the title, in the App's language from the next
// layout.
func (x *Box) SetTitleMessage(m tui.Message) {
	x.title = messageText(m)
	x.RequestLayout()
}

// SetTitleSlot puts c on the box's top border row: one control (an editor's Raw | Rendered
// switch, a close ×), one row tall at its own width, right-aligned inside the corners with a
// border cell after it (at the left end instead when the title is right-aligned), in the title
// row's look when it takes a style (SetStyle). The box owns its rect there: c is a second child, mounted, hit-tested and repainted as any child, and its rect
// never meets the content child's. The title truncates before it. A box too narrow for both
// (inner width under c's width + 4), or with no top border, hides c and keeps the title. nil
// removes the slot.
func (x *Box) SetTitleSlot(c tui.Component) {
	if c == x.slot {
		return
	}
	if old := x.slot; old != nil && x.ctx != nil {
		x.ctx.Unmount(old)
	}
	x.slot = c
	if c != nil && x.ctx != nil {
		x.ctx.Mount(c)
	}
	x.RequestLayout()
}

// TitleSlot is the control on the title row, nil for none.
func (x *Box) TitleSlot() tui.Component { return x.slot }

// SetBottomSlot puts c on the box's bottom border row, at its left, a border cell after the
// corner: a row of tabs, say, kept off the title row, which keeps the title and its slot whole. It
// is one row tall at its own width, given the row's room up to the status text (SetStatus) and a
// border cell before the corner; the status keeps its place after it. c is a child, mounted,
// hit-tested and repainted as any, and its rect never meets the content child's. One that takes
// no width (a strip with no tabs, or hidden) takes no room; a box with no bottom border hides it.
// nil removes it.
func (x *Box) SetBottomSlot(c tui.Component) {
	if c == x.foot {
		return
	}
	if old := x.foot; old != nil && x.ctx != nil {
		x.ctx.Unmount(old)
	}
	x.foot = c
	if c != nil && x.ctx != nil {
		x.ctx.Mount(c)
	}
	x.RequestLayout()
}

// BottomSlot is the control on the bottom border row, nil for none.
func (x *Box) BottomSlot() tui.Component { return x.foot }

// SetTitleElide sets where a title too wide for its row loses text, as Qt's Text.elide: the end
// (ElideRight, the default), the start (ElideLeft: a path's tail, with the badges a host writes
// after it, stays whole), the middle, or a path's start at a "/" (ElidePath). Measured in cells by
// grapheme, as every border text is: a wide grapheme is never split, and "…" marks the cut. A title
// that fits is drawn as it is. Repaint only.
func (x *Box) SetTitleElide(e Elide) {
	x.titleElide = e
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
	if x.slot != nil {
		ctx.Mount(x.slot)
	}
	if x.foot != nil {
		ctx.Mount(x.foot)
	}
}

// AcceptsFocus implements tui.Focusable per WithFocusable.
func (x *Box) AcceptsFocus() bool { return x.focusable }

// FocusableByDesign implements tui.FocusDesigner: a Box is focusable only when
// it was built WithFocusable(true) — a choice made at construction, not a
// state — so a pane Box is not focusable by design, whatever it holds.
func (x *Box) FocusableByDesign() bool { return x.focusable }

// Add sets the single child (Container contract). A Box wraps exactly one
// child; adding to an occupied Box panics.
func (x *Box) Add(children ...tui.Component) {
	for _, c := range children {
		if c == nil {
			panic("widget: Box.Add: nil child")
		}
		if x.child != nil {
			panic("widget: Box wraps exactly one child — nest a Flex/Split/Dock for more")
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
		panic(errs.Fatal{Op: "widget: Box.Move", Rule: "Box wraps exactly one child; only index 0 is valid"})
	}
}

// Remove unmounts the child (or a title-row control) and forgets it.
func (x *Box) Remove(child tui.Component) {
	if child != nil && child == x.slot {
		x.SetTitleSlot(nil)
		return
	}
	if child != nil && child == x.foot {
		x.SetBottomSlot(nil)
		return
	}
	if child == nil || x.child != child {
		return
	}
	x.child = nil
	if x.ctx != nil {
		x.ctx.Unmount(child)
	}
}

// Children enumerates the content child, then the title slot, then the bottom slot, each if any.
func (x *Box) Children() iter.Seq[tui.Component] {
	return func(yield func(tui.Component) bool) {
		if x.child != nil && !yield(x.child) {
			return
		}
		if x.slot != nil && !yield(x.slot) {
			return
		}
		if x.foot != nil {
			yield(x.foot)
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

// borderEdgeSizes mirrors the frame math per edge: 1 when the
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

// boxCommitKey is the Box's inset commit (tui.Context.AfterLayout).
const boxCommitKey tui.CommitKey = "widget.box"

// Layout subtracts the frame (border + padding + margin, frame math) from
// the constraints, lays out the child, and reports child size +
// frame. Title and status cost zero extra rows: they render inside the
// border rows.
func (x *Box) Layout(c tui.Constraints) tui.Size {
	sz, inL, inR := x.layoutFrame(c)
	x.placeSlot(sz.W, inL, inR)
	x.placeFoot(sz, inL, inR)
	return sz
}

// placeSlot lays the title slot out on the top border row of a box w columns wide and places it,
// or places it nowhere (a zero rect: never hit) while it doesn't fit; the width it took is
// committed after layout, for Render to truncate the title by.
func (x *Box) placeSlot(w, inL, inR int) {
	if x.slot == nil {
		return
	}
	mt, mr, _, ml := x.base.GetMargin()
	bt, br, _, bl := borderEdgeSizes(x.base)
	bx, bw := ml+inL, w-ml-mr-inL-inR
	inner := bw - bl - br
	sw := 0
	if bt == 1 && inner > 4 {
		// measured with the whole row, so one that doesn't fit is hidden below, never cut
		sw = x.ctx.LayoutChild(x.slot, tui.Constraints{MaxW: inner, MinH: 1, MaxH: 1}).W
	}
	if sw <= 0 || inner < sw+4 {
		x.ctx.PlaceChild(x.slot, tui.Rect{})
		x.ctx.AfterLayout(boxSlotKey, func() { x.slotW = 0 })
		return
	}
	at := bx + bl + inner - 1 - sw // a border cell between it and the corner
	if x.titleAlign == style.AlignRight {
		at = bx + bl + 1
	}
	x.ctx.PlaceChild(x.slot, tui.Rect{X: at, Y: mt, W: sw, H: 1})
	x.ctx.AfterLayout(boxSlotKey, func() { x.slotW = sw })
}

// placeFoot lays the bottom slot out on the bottom border row of a box sz big, at its left, in the
// room the status text leaves with a border cell either side, and places it, or places it nowhere
// while it takes no width or has no room; the width it took is committed after layout, for Render
// to move the status past.
func (x *Box) placeFoot(sz tui.Size, inL, inR int) {
	if x.foot == nil {
		return
	}
	_, mr, mb, ml := x.base.GetMargin()
	_, br, bb, bl := borderEdgeSizes(x.base)
	bx, bw := ml+inL, sz.W-ml-mr-inL-inR
	room := bw - bl - br - 2 // a border cell after the corner, and one before the other
	if x.status != "" {
		room -= tui.StringWidth(" "+x.status+" ") + 1 // the status keeps its words, a cell before them
	}
	fw := 0
	if bb == 1 && room > 0 && sz.H >= 2 {
		fw = x.ctx.LayoutChild(x.foot, tui.Constraints{MaxW: room, MinH: 1, MaxH: 1}).W
	}
	if fw <= 0 {
		x.ctx.PlaceChild(x.foot, tui.Rect{})
		x.ctx.AfterLayout(boxFootKey, func() { x.footW = 0 })
		return
	}
	x.ctx.PlaceChild(x.foot, tui.Rect{X: bx + bl + 1, Y: sz.H - mb - 1, W: fw, H: 1})
	x.ctx.AfterLayout(boxFootKey, func() { x.footW = fw })
}

// boxFootKey is the bottom slot's width commit (tui.Context.AfterLayout).
const boxFootKey tui.CommitKey = "widget.box.foot"

// slotStyler is a title slot that takes the title row's look (ModeSwitch's SetStyle).
type slotStyler interface{ SetStyle(style.Style) }

// boxSlotKey is the title slot's width commit (tui.Context.AfterLayout).
const boxSlotKey tui.CommitKey = "widget.box.slot"

// layoutFrame is Layout's frame and content child, and the capped box's centring.
func (x *Box) layoutFrame(c tui.Constraints) (sz tui.Size, inL, inR int) {
	fh := x.base.GetHorizontalFrameSize()
	fv := x.base.GetVerticalFrameSize()
	if x.child == nil {
		w := boundedMax(c.MaxW, max(c.MinW, fh))
		h := boundedMax(c.MaxH, max(c.MinH, fv))
		return c.Constrain(tui.Size{W: w, H: h}), 0, 0
	}
	// The insets Render centres a capped box by are committed after layout, so a measure moves
	// nothing on screen.
	defer func() { x.ctx.AfterLayout(boxCommitKey, func() { x.inL, x.inR = inL, inR }) }()
	if x.maxW > 0 && c.MaxW != tui.Unbounded && c.MaxW > x.maxW {
		// capped: the box is maxW wide, centred; it keeps what it was given, so nothing beside it moves
		extra := c.MaxW - x.maxW
		inL, inR = extra/2, extra-extra/2
		cc := tui.Constraints{MinW: x.maxW - fh, MaxW: x.maxW - fh, MinH: max(c.MinH-fv, 0), MaxH: subFrame(c.MaxH, fv)}
		csz := x.ctx.LayoutChild(x.child, cc)
		ft, _, _, fl := x.frame()
		x.ctx.PlaceChild(x.child, tui.Rect{X: inL + fl, Y: ft, W: csz.W, H: csz.H})
		return c.Constrain(tui.Size{W: c.MaxW, H: csz.H + fv}), inL, inR
	}
	cc := tui.Constraints{
		MinW: max(c.MinW-fh, 0), MaxW: subFrame(c.MaxW, fh),
		MinH: max(c.MinH-fv, 0), MaxH: subFrame(c.MaxH, fv),
	}
	csz := x.ctx.LayoutChild(x.child, cc)
	ft, _, _, fl := x.frame()
	x.ctx.PlaceChild(x.child, tui.Rect{X: fl, Y: ft, W: csz.W, H: csz.H})
	return c.Constrain(tui.Size{W: csz.W + fh, H: csz.H + fv}), inL, inR
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
// when focus is within (Inherit semantics —).
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
	ml, mr = ml+x.inL, mr+x.inR // a capped box's centring
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
	//
	// The title wears the box's own text colour when its style sets one — a
	// pane on a light surface titles in its dark text, as Qt's GroupBox takes
	// its title from the palette — and the terminal's foreground otherwise.
	// The status stays muted: it is a hint, not a heading.
	availW := bw - 2
	titleSt, statusSt := x.titleSt, x.statusSt
	if fg, ok := st.GetForeground(); ok {
		titleSt = titleSt.Foreground(fg)
	}
	// the title slot's columns, and a border cell beside them, are the slot's: the title truncates
	// before them (after them, when it is right-aligned)
	tx, tw := bx+1, availW
	if x.slot != nil && x.slotW > 0 {
		tw -= x.slotW + 1
		if x.titleAlign == style.AlignRight {
			tx += x.slotW + 1
		}
	}
	if sl, ok := x.slot.(slotStyler); ok && x.slotW > 0 {
		// the slot wears the title row's look: the title's colour on the row's, its own marks over
		// it (the switch's chosen segment bold); it paints after the box, as every child does
		sl.SetStyle(titleSt.Bold(false).Inherit(fill))
	}
	if title := x.title.inLanguage(&x.Base); bt == 1 && title != "" && tw > 0 {
		text := " " + title + " "
		if x.titleElide != ElideRight && s.StringWidth(text) > tw {
			// cut where the box was asked, inside the title's padding (the end keeps truncate's
			// look, unchanged)
			text = " " + elide(title, max(tw-2, 1), x.titleElide, s.StringWidth) + " "
		}
		x.renderBorderText(s, text, x.titleAlign, titleSt.Inherit(fill), tx, by, tw)
	}
	// the bottom slot's columns, and the border cell after them, are its: the status keeps to
	// the rest of the row
	sx, sw := bx+1, availW
	if x.foot != nil && x.footW > 0 {
		sx, sw = sx+x.footW+1, sw-x.footW-1
	}
	if bb == 1 && x.status != "" && sw > 0 {
		x.renderBorderText(s, " "+x.status+" ", x.statusAlign, statusSt.Inherit(fill), sx, by+bh-1, sw)
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

	paintFrame(s, tui.Rect{X: bx, Y: by, W: bw, H: bh}, bs,
		frameEdge{et, topSt}, frameEdge{er, rightSt}, frameEdge{eb, bottomSt}, frameEdge{el, leftSt})
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
