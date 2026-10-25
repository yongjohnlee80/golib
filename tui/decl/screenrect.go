package decl

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// SCREEN RECT — where a window is drawn, for a Go program that compares windows by place (a
// keyboard move to the window that lies that way, say). A FloatWindow's and a Drawer's node, as
// Program.Find returns it, answers ScreenRect: its window's rectangle in the Window's frame, the
// screen's, whether it floats or a TileManager governs it, and false while it is not on screen
// (closed, minimized, or not yet laid out). A window core's own Bounds is its target's frame
// instead: a tile's is its manager's, a Float's the overlay's.

var (
	_ interface{ ScreenRect() (tui.Rect, bool) } = (*floatWindowNode)(nil)
	_ interface{ ScreenRect() (tui.Rect, bool) } = (*drawerNode)(nil)
)

// ScreenRect is where the window is drawn, in the Window's frame; false while it is not on screen.
func (n *floatWindowNode) ScreenRect() (tui.Rect, bool) { return screenRect(n.float, n.mod.Context()) }

// ScreenRect is where the drawer's panel is drawn, in the Window's frame; false while it is not on
// screen.
func (n *drawerNode) ScreenRect() (tui.Rect, bool) { return screenRect(n.float, n.frame.ctx) }

// screenRect is where float last laid its content out, whose Context is content, while the content
// is on screen. The Float fills its Window's overlay, which the Window lays out over its whole
// rectangle, so the Float's cells are the Window's.
func screenRect(float *widget.Float, content *tui.Context) (tui.Rect, bool) {
	r := float.ContentRect()
	if content == nil || !content.Visible() || r.Empty() {
		return tui.Rect{}, false
	}
	return r, true
}
