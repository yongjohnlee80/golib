package term

import "github.com/yongjohnlee80/golib/tui"

// The frame emitter:
//
//   - R1 — the cursor is hidden (?25l) before any cell emission and re-shown
//     only per the latched state, within the same write.
//   - R2 — never clear-then-redraw: dirty cells are overwritten in place; no
//     ED/EL is ever emitted here.
//   - R3 — one frame, one Write: the entire frame (brackets, cursor ops,
//     cells) accumulates into one reusable buffer and hits the fd as a single
//     Write syscall. The latched cursor ops exist to serve
//     this rule.
//   - R4 — mode-2026 synchronized-update brackets around every frame when
//     Capabilities().SyncOutput; never latched on.
//
// CUP is emitted only on discontinuity, SGR only on attribute change; after a
// risky cluster on a terminal without mode 2027, the next emission is
// re-anchored absolutely.

// ShowCursor latches the cursor visible; the next Flush emits it.
func (b *Backend) ShowCursor() { b.latch(func() { b.cursor.Show() }) }

// HideCursor latches the cursor hidden; the next Flush emits it.
func (b *Backend) HideCursor() { b.latch(func() { b.cursor.Hide() }) }

// SetCursor latches the cursor position; the next Flush emits it.
func (b *Backend) SetCursor(x, y int) { b.latch(func() { b.cursor.SetPos(x, y) }) }

// SetCursorShape latches the cursor shape (DECSCUSR); the next Flush emits it.
func (b *Backend) SetCursorShape(s tui.CursorShape) { b.latch(func() { b.cursor.SetShape(s) }) }

// SetCursorColor latches the cursor's colour (OSC 12), or the terminal's own (OSC 112); the next
// Flush emits it. Only an RGB colour is sent; a palette index leaves the terminal's own, decided
// here, before it is latched.
func (b *Backend) SetCursorColor(c tui.CellColor, set bool) {
	b.latch(func() { b.cursor.SetColor(c, set && c.Kind == tui.CellColorRGB) })
}

// latch runs fn under cmu, the latches' lock.
func (b *Backend) latch(fn func()) {
	b.cmu.Lock()
	fn()
	b.cmu.Unlock()
}

// Flush applies one frame's cell diff plus any latched cursor-state changes
// as a SINGLE buffered write. An empty diff
// with unchanged cursor state writes zero bytes. The diff is ordered
// row-major by the caller.
func (b *Backend) Flush(diff []tui.CellUpdate) error {
	if b.stopped.Load() {
		return ErrClosed
	}

	b.cmu.Lock()
	want, dirty := b.cursor.Take()
	images := b.images.Take()
	b.cmu.Unlock()

	b.wmu.Lock()
	defer b.wmu.Unlock()
	frame := b.em.frame(b.caps, diff, want, dirty, images)
	if len(frame) == 0 {
		return nil // zero bytes
	}
	return b.writeAll(frame) // R3: one frame, one Write (short-write safe)
}
