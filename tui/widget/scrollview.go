package widget

import (
	"iter"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// ScrollView shows a vertical window onto one child taller than itself. The child
// is laid out at the ScrollView's width less the indicator's column and at its own height, then
// placed offset rows up; tui's clip shows the window, and hit-testing, the cursor, native views and
// images all keep to it. The wheel scrolls it, and PageUp/PageDown/Home/End do when the focused
// child lets them through. A field that takes focus is brought into view by the runtime, which asks
// the ScrollView through Reveal.
//
// The indicator's column is always reserved, blank when nothing overflows: the child is laid out
// once per pass at one width, so no second measurement leaves a child's own commit stale.
//
// The child's own layout is unchanged: a widget that fills whatever height it is given (BufferView,
// HTMLView) answers about a row under the unbounded height a ScrollView offers, so a ScrollView
// holds sized content: forms, frames, lists of fields.
type ScrollView struct {
	Base
	child   tui.Component
	offset  int // the first child row shown
	content int // the child's height at the last layout
	view    int // the rows shown at the last layout
}

// ScrollScope is a ScrollView's keys: they reach it only when the focused child declines them.
const ScrollScope keymap.Scope = "tui.scroll"

var (
	ScrollPageUp   = cmd("tui.scroll.pageUp", ScrollScope, "PgUp")
	ScrollPageDown = cmd("tui.scroll.pageDown", ScrollScope, "PgDown")
	ScrollHome     = cmd("tui.scroll.home", ScrollScope, "Home")
	ScrollEnd      = cmd("tui.scroll.end", ScrollScope, "End")

	scrollCommands = []keymap.Command{ScrollPageUp, ScrollPageDown, ScrollHome, ScrollEnd}
)

// NewScrollView makes a ScrollView over child.
func NewScrollView(child tui.Component) *ScrollView { return &ScrollView{child: child} }

// Init mounts the child.
func (v *ScrollView) Init(ctx *tui.Context) {
	v.Base.Init(ctx)
	if v.child != nil {
		ctx.Mount(v.child)
	}
}

// Children implements tui.Container's walk.
func (v *ScrollView) Children() iter.Seq[tui.Component] {
	return func(yield func(tui.Component) bool) {
		if v.child != nil {
			yield(v.child)
		}
	}
}

// KeyScope is the ScrollView's key-map scope.
func (v *ScrollView) KeyScope() keymap.Scope { return ScrollScope }

// Layout lays the child out once, at its own height, and places it offset rows up. An unbounded
// offer is answered with the child's own size, and nothing scrolls.
func (v *ScrollView) Layout(c tui.Constraints) tui.Size {
	if v.child == nil {
		v.content, v.view, v.offset = 0, 0, 0
		return c.Constrain(tui.Size{})
	}
	if c.MaxW == tui.Unbounded || c.MaxH == tui.Unbounded {
		sz := v.Context().LayoutChild(v.child, tui.Constraints{MaxW: c.MaxW, MaxH: tui.Unbounded})
		v.content, v.view, v.offset = sz.H, sz.H, 0
		v.Context().PlaceChild(v.child, tui.Rect{W: sz.W, H: sz.H})
		return c.Constrain(tui.Size{W: sz.W + 1, H: sz.H})
	}
	w := max(c.MaxW-1, 0) // the indicator's column
	sz := v.Context().LayoutChild(v.child, tui.Constraints{MinW: w, MaxW: w, MaxH: tui.Unbounded})
	v.content, v.view = sz.H, max(c.MaxH, 0)
	v.offset = clampOffset(v.offset, v.content, v.view)
	v.Context().PlaceChild(v.child, tui.Rect{Y: -v.offset, W: w, H: sz.H})
	return tui.Size{W: c.MaxW, H: c.MaxH}
}

func clampOffset(off, content, view int) int {
	return min(max(off, 0), max(content-view, 0))
}

// Reveal implements tui.Revealer: the least move of the offset that shows r, its top when r is
// taller than the view.
func (v *ScrollView) Reveal(r tui.Rect) bool {
	if v.view <= 0 {
		return false
	}
	off := v.offset
	switch {
	case r.Y < 0 || r.H > v.view:
		off += r.Y
	case r.Y+r.H > v.view:
		off += r.Y + r.H - v.view
	}
	return v.scrollTo(off)
}

// scrollTo moves the offset, clamped, and reports whether it moved.
func (v *ScrollView) scrollTo(off int) bool {
	off = clampOffset(off, v.content, v.view)
	if off == v.offset {
		return false
	}
	v.offset = off
	v.RequestLayout()
	return true
}

// HandleEvent scrolls on a wheel no child consumed, and on the page keys when they bubble.
func (v *ScrollView) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.MouseEvent:
		if e.Kind != tui.MouseWheel || (e.Button != tui.WheelUp && e.Button != tui.WheelDown) {
			return false
		}
		if v.content <= v.view {
			return false // nothing to scroll: an outer one may
		}
		step := scrollWheelRows
		if e.Button == tui.WheelUp {
			step = -step
		}
		v.scrollTo(v.offset + step)
		return true
	case tui.KeyEvent:
		cmd, ok := v.Context().KeyCommand(e, scrollCommands...)
		if !ok || v.content <= v.view {
			return false
		}
		switch cmd.ID {
		case ScrollPageUp.ID:
			v.scrollTo(v.offset - max(v.view-1, 1))
		case ScrollPageDown.ID:
			v.scrollTo(v.offset + max(v.view-1, 1))
		case ScrollHome.ID:
			v.scrollTo(0)
		case ScrollEnd.ID:
			v.scrollTo(v.content - v.view)
		}
		return true
	}
	return false
}

// ScrollState implements tui.Scroller, in rows.
func (v *ScrollView) ScrollState() (offset, viewport, content int) {
	return v.offset, v.view, v.content
}

// Render paints the indicator's column when the content overflows; the child paints itself over
// the rest, clipped.
func (v *ScrollView) Render(s tui.Surface) {
	sz := s.Size()
	if v.content > v.view && sz.W > 0 {
		paintScrollIndicator(s, sz.W-1, sz.H, v.offset, v.content-v.view)
	}
}

var (
	_ tui.Revealer = (*ScrollView)(nil)
	_ tui.Scroller = (*ScrollView)(nil)
)
