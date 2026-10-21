package term

import (
	"fmt"
	"strconv"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/internal/xterm/sgr"
)

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

	if len(diff) == 0 && !dirty && len(images) == 0 {
		return nil // zero bytes
	}

	b.buf.Reset()
	b.bracket(func() {
		if len(diff) > 0 && b.termVisible {
			b.buf.WriteString("\x1b[?25l") // R1: hide during paint
			b.termVisible = false
		}
		b.emitCells(diff)
		b.writeImages(images) // over the cells just written, within the same Write (graphics.go)
		b.emitCursor(want)
	})
	if b.buf.Len() == 0 {
		return nil
	}
	return b.writeAll(b.buf.Bytes()) // R3: one frame, one Write (short-write safe)
}

// bracket writes body's output inside mode-2026 synchronized-update brackets when the terminal has
// them (R4).
func (b *Backend) bracket(body func()) {
	sync := b.caps.SyncOutput
	if sync {
		b.buf.WriteString("\x1b[?2026h") // R4: begin synchronized update
	}
	body()
	if sync {
		b.buf.WriteString("\x1b[?2026l") // R4: end synchronized update
	}
}

// emitCells writes the diff's cells: CUP only on a discontinuity, SGR only on an attribute change,
// and an absolute re-anchor after a risky cluster on a terminal without mode 2027.
func (b *Backend) emitCells(diff []tui.CellUpdate) {
	buf := &b.buf
	for i := range diff {
		u := &diff[i]
		if u.Cell.Width == 0 {
			// Continuation cells ride on their head (W2 guarantees pairs
			// are dirtied together); emitting the head covers both columns.
			continue
		}
		if b.forceAnchor || !b.penKnown || u.Y != b.penY || u.X != b.penX {
			b.writeCUP(u.X, u.Y) // CUP only on discontinuity
			b.forceAnchor = false
		}
		if !b.attrsKnown || u.Cell.Attrs != b.lastAttrs {
			b.writeSGR(u.Cell.Attrs) // SGR only on attribute change
			b.lastAttrs = u.Cell.Attrs
			b.attrsKnown = true
		}
		content := u.Cell.Content
		if content == "" {
			content = " "
		}
		buf.WriteString(content)
		b.penX, b.penY = u.X+int(u.Cell.Width), u.Y
		b.penKnown = true
		if !b.caps.UnicodeCore && riskyCluster(content) {
			// Without mode 2027 the terminal's advance for this cluster is
			// untrusted — re-anchor absolutely next time.
			b.forceAnchor = true
		}
	}
}

// emitCursor writes the latched cursor state, within the same Write: its position, shape, colour
// and visibility, each only where it differs from what the terminal has.
func (b *Backend) emitCursor(want tui.CursorState) {
	buf := &b.buf
	if want.PosSet {
		b.writeCUP(want.X, want.Y)
		b.penX, b.penY, b.penKnown = want.X, want.Y, true
	}
	if want.Shape != b.termShape {
		b.writeShape(want.Shape)
		b.termShape = want.Shape
	}
	if want.Colored != b.termColored || (want.Colored && want.Color != b.termColor) {
		if want.Colored {
			fmt.Fprintf(buf, "\x1b]12;#%02x%02x%02x\x07", want.Color.R, want.Color.G, want.Color.B)
		} else {
			buf.WriteString("\x1b]112\x07")
		}
		b.termColor, b.termColored = want.Color, want.Colored
	}
	if want.Visible != b.termVisible {
		if want.Visible {
			buf.WriteString("\x1b[?25h")
		} else {
			buf.WriteString("\x1b[?25l")
		}
		b.termVisible = want.Visible
	}
}

func (b *Backend) writeInt(n int) {
	b.buf.Write(strconv.AppendInt(b.numScratch[:0], int64(n), 10))
}

// writeCUP emits an absolute cursor-position (1-based on the wire).
func (b *Backend) writeCUP(x, y int) {
	b.buf.WriteString("\x1b[")
	b.writeInt(y + 1)
	b.buf.WriteByte(';')
	b.writeInt(x + 1)
	b.buf.WriteByte('H')
}

// writeSGR emits the full resolved attribute state: a reset followed by every
// set attribute and both colors, so emission never depends on the previous
// cell's attributes beyond the change test.
func (b *Backend) writeSGR(a tui.CellAttrs) {
	buf := &b.buf
	buf.WriteString("\x1b[0")
	for _, at := range sgr.Attrs { // the codes vt reads them with
		if a.Mask&at.Attr != 0 {
			buf.WriteByte(';')
			b.writeInt(at.On)
		}
	}
	b.writeColor(a.FG, false)
	b.writeColor(a.BG, true)
	buf.WriteByte('m')
}

func (b *Backend) writeColor(c tui.CellColor, bg bool) {
	base := 30
	if bg {
		base = 40
	}
	switch c.Kind {
	case tui.CellColorDefault:
		// SGR 0 already selected the defaults; nothing to emit.
	case tui.CellColorANSI:
		b.buf.WriteByte(';')
		if c.Index < 8 {
			b.writeInt(base + int(c.Index))
		} else {
			b.writeInt(base + 60 + int(c.Index-8)) // bright: 90–97 / 100–107
		}
	case tui.CellColorANSI256:
		if bg {
			b.buf.WriteString(";48;5;")
		} else {
			b.buf.WriteString(";38;5;")
		}
		b.writeInt(int(c.Index))
	case tui.CellColorRGB:
		if bg {
			b.buf.WriteString(";48;2;")
		} else {
			b.buf.WriteString(";38;2;")
		}
		b.writeInt(int(c.R))
		b.buf.WriteByte(';')
		b.writeInt(int(c.G))
		b.buf.WriteByte(';')
		b.writeInt(int(c.B))
	}
}

// writeShape emits DECSCUSR (CSI Ps SP q), using the steady variants.
func (b *Backend) writeShape(s tui.CursorShape) {
	var n int
	switch s {
	case tui.CursorShapeBlock:
		n = 2
	case tui.CursorShapeUnderline:
		n = 4
	case tui.CursorShapeBar:
		n = 6
	default:
		n = 0 // the terminal's configured default
	}
	b.buf.WriteString("\x1b[")
	b.writeInt(n)
	b.buf.WriteString(" q")
}

// riskyCluster reports whether the terminal's width opinion for the cluster
// may disagree with ours: multi-rune clusters containing ZWJ,
// VS15/VS16, or a regional-indicator pair. Single-rune wide CJK is never
// risky — every terminal agrees on it.
func riskyCluster(s string) bool {
	if len(s) < 2 {
		return false
	}
	runes, ri := 0, 0
	risky := false
	for _, r := range s {
		runes++
		switch {
		case r == 0x200D || r == 0xFE0E || r == 0xFE0F: // ZWJ, VS15, VS16
			risky = true
		case r >= 0x1F1E6 && r <= 0x1F1FF: // regional indicators
			ri++
		}
	}
	return runes > 1 && (risky || ri >= 2)
}
