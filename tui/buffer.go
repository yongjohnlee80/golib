package tui

// buffer is the double-buffered cell grid (ADR-0003 §2.2; unexported — owned
// by the runtime). The tcell model, adopted verbatim: the last-frame copy IS
// the dirty tracking — dirty(i) ≡ curr[i] != last[i]. No separate bitmap or
// dirty list exists to fall out of sync with reality.
type buffer struct {
	w, h    int
	curr    []Cell       // row-major, len w*h — what components painted this frame
	last    []Cell       // what the terminal is currently showing
	scratch []CellUpdate // reusable diff slice (steady state allocates nothing)
}

// invalidCell is the last-buffer sentinel after (re)allocation: it compares
// unequal to every cell the write path can produce (SetCell never writes a
// NUL cluster), so a fresh or resized buffer diffs as the full grid — the
// "never diff across a size change" rule.
var invalidCell = Cell{Content: "\x00", Width: 1}

// blankCell is the cleared-cell value: a plain space with default attrs.
var blankCell = Cell{Content: " ", Width: 1}

// newBuffer allocates a w×h double buffer with curr cleared to blanks and
// last fully invalidated (the first diff emits every cell).
func newBuffer(w, h int) *buffer {
	b := &buffer{}
	b.resize(w, h)
	return b
}

// resize reallocates both grids at the new size, clears curr, and
// invalidates last entirely: post-resize rendering is a full repaint, never
// a diff across sizes (ADR-0003 §2.6; the ED 2 clear is the term emitter's
// job).
func (b *buffer) resize(w, h int) {
	w, h = max(w, 0), max(h, 0)
	b.w, b.h = w, h
	b.curr = make([]Cell, w*h)
	b.last = make([]Cell, w*h)
	for i := range b.curr {
		b.curr[i] = blankCell
		b.last[i] = invalidCell
	}
}

// size returns the grid size.
func (b *buffer) size() Size { return Size{W: b.w, H: b.h} }

// setCell writes c at grid coordinates (x, y), enforcing the wide-cell
// boundary invariants (ADR-0003 §2.3) so they hold by construction, not by
// caller discipline:
//
//   - W1 — no orphan halves: writing over either half of an existing wide
//     pair clears both — the overwritten position takes the new content, the
//     freed sibling becomes a space cell in the old head's style.
//   - W3 — no wide write into the last column: a width-2 cell whose
//     continuation would fall off the grid is dropped whole, never
//     half-painted.
//
// Out-of-range writes are dropped silently (clipping is a rendering fact,
// not an error — ADR-0003 §2.4). c.Width must be 1 or 2; continuation cells
// are written by setCell itself, never by callers.
func (b *buffer) setCell(x, y int, c Cell) {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return
	}
	if c.Width == 2 && x+1 >= b.w {
		return // W3
	}
	b.clearOverlap(x, y)
	i := y*b.w + x
	if c.Width == 2 {
		b.clearOverlap(x+1, y)
		b.curr[i] = c
		b.curr[i+1] = Cell{Content: "", Width: 0, Attrs: c.Attrs}
		return
	}
	b.curr[i] = c
}

// clearOverlap dissolves any wide pair overlapping (x, y) per W1: the
// sibling of the overwritten half becomes a space cell in the old head's
// style. The position (x, y) itself is left for the caller to overwrite.
func (b *buffer) clearOverlap(x, y int) {
	i := y*b.w + x
	switch {
	case b.curr[i].Width == 2 && x+1 < b.w && b.curr[i+1].Continuation():
		// (x, y) is a head: free the continuation to its right.
		b.curr[i+1] = Cell{Content: " ", Width: 1, Attrs: b.curr[i].Attrs}
	case b.curr[i].Continuation() && x > 0 && b.curr[i-1].Width == 2:
		// (x, y) is a continuation: free the head to its left.
		b.curr[i-1] = Cell{Content: " ", Width: 1, Attrs: b.curr[i-1].Attrs}
	}
}

// diff walks curr vs last row-major and returns the frame's dirty cells as
// CellUpdates (ADR-0003 §2.2), updating last as it emits (emission = the
// backend will apply it). The returned slice is the buffer's reusable
// scratch — valid until the next diff call.
//
// Rules:
//  1. Clean cells (curr[i] == last[i]) are skipped.
//  2. A continuation cell dirty alongside its head is covered by the head's
//     emission (a width-2 update covers both columns) and is not emitted.
//  3. A damage region never splits a wide pair (W2): a dirty continuation
//     with a clean head re-emits the head so the pair travels whole.
func (b *buffer) diff() []CellUpdate {
	out := b.scratch[:0]
	for y := 0; y < b.h; y++ {
		row := y * b.w
		for x := 0; x < b.w; x++ {
			i := row + x
			if b.curr[i] == b.last[i] {
				continue
			}
			c := b.curr[i]
			if c.Continuation() && x > 0 && b.curr[i-1].Width == 2 {
				// Dirty continuation. If the head was clean it was not
				// emitted this pass — widen the run to the full pair (W2).
				if b.curr[i-1] == b.last[i-1] {
					out = append(out, CellUpdate{X: x - 1, Y: y, Cell: b.curr[i-1]})
				}
				b.last[i] = c
				continue
			}
			out = append(out, CellUpdate{X: x, Y: y, Cell: c})
			b.last[i] = c
			if c.Width == 2 {
				// The head's emission covers its continuation (rule 2).
				b.last[i+1] = b.curr[i+1]
				x++
			}
		}
	}
	b.scratch = out
	return out
}
