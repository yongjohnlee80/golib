package decl

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// hostOne is a node hosting one widget of its own, inner: mounted in Init, laid out at the node's
// constraints and placed at its origin, drawing nothing and taking no events itself. A node embeds
// it, sets inner once it has made the widget, and overrides Layout when it lays the widget out
// another way.
type hostOne struct {
	widget.Base
	inner tui.Component
}

// Init mounts the hosted widget.
func (h *hostOne) Init(ctx *tui.Context) {
	h.Base.Init(ctx)
	ctx.Mount(h.inner)
}

// Layout lays the hosted widget out at c, at the node's origin.
func (h *hostOne) Layout(c tui.Constraints) tui.Size {
	sz := h.Context().LayoutChild(h.inner, c)
	h.Context().PlaceChild(h.inner, tui.Rect{W: sz.W, H: sz.H})
	return c.Constrain(sz)
}

func (*hostOne) Render(tui.Surface)         {}
func (*hostOne) HandleEvent(tui.Event) bool { return false }
