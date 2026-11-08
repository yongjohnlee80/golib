package widget

import "github.com/yongjohnlee80/golib/tui"

// hostFor resolves the nearest enclosing ancestor satisfying T.
//
// ONE rule about which host a widget belongs to — nearest enclosing, never a
// sibling, never a broadcast — shared by every widget that owns a layer, with
// the interface saying what that widget actually needs from it.
func hostFor[T any](ctx *tui.Context) (T, bool) {
	var zero T
	if ctx == nil {
		return zero, false
	}
	found := ctx.Ancestor(func(c tui.Component) bool {
		_, ok := c.(T)
		return ok
	})
	h, ok := found.(T)
	return h, ok
}
