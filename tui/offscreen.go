package tui

// OFFSCREEN — a component drawn off the window at a size of its own, through the frame buffer's own
// code: a host that paints it elsewhere (a GUI's native view at another cell size) gets exactly the
// cells the window would have shown, styles resolved and faces stamped as the App's.

// Offscreen is an off-window grid a component owns and renders into with Context.RenderOffscreen. It
// is never shown, stamped for natives or diffed.
type Offscreen struct{ buf *buffer }

// RenderOffscreen clears o to size, then calls draw once with a Surface over it, as the frame
// buffer's root Surface: the App's style resolution (theme, adaptive tokens, capability
// downsampling, the width policy) and wide-cell rules, and face, the family the cells are drawn in
// (interned in the App's face table, so a cell's Face is the window's own index; "" is the face in
// effect now, as a node with no face of its own inherits it). It returns o's cells, row-major, valid
// until o is rendered again. Loop goroutine, render phase. Calls nest: each has its own Offscreen,
// and nothing of the App's frame is touched.
func (c *Context) RenderOffscreen(o *Offscreen, size Size, face string, draw func(Surface)) []Cell {
	a := c.app
	if o == nil || draw == nil || a.rctx == nil {
		return nil
	}
	w, h := max(size.W, 0), max(size.H, 0)
	if o.buf == nil || o.buf.w != w || o.buf.h != h {
		o.buf = &buffer{w: w, h: h, curr: make([]Cell, w*h)}
	}
	for i := range o.buf.curr {
		o.buf.curr[i] = blankCell
	}
	o.buf.face = 0
	if a.buf != nil {
		o.buf.face = a.buf.face
	}
	if face != "" {
		o.buf.face = a.faceIndex(face)
	}
	draw(newRootSurface(o.buf, a.rctx))
	return o.buf.curr
}
