package term

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/internal/xterm/sgr"
)

// emitter is the output side: the frame buffer and what the terminal was last given — the pen,
// the attributes, the cursor and the images on the screen. It writes bytes and never does I/O;
// the Backend holds it under wmu and writes what frame returns.
type emitter struct {
	buf        bytes.Buffer
	numScratch [20]byte
	tmux       bool // graphics commands pass through tmux (writeAPC)

	termVisible bool // terminal-side cursor visibility
	termShape   tui.CursorShape
	termColor   tui.CellColor // the cursor colour the terminal was last given
	termColored bool          // it was given one (OSC 12), to give back at teardown
	penX, penY  int
	penKnown    bool
	attrsKnown  bool
	lastAttrs   tui.CellAttrs
	forceAnchor bool

	// placed is the ids this program has on the screen, to delete at teardown. sent is the
	// version of each image whose bytes the terminal holds: placing that version again (a new
	// clip, new cells) sends no bytes.
	placed map[uint32]bool
	sent   map[uint32]uint64
}

func newEmitter(tmux bool) emitter {
	return emitter{tmux: tmux, termVisible: true} // terminals start with the cursor shown
}

// frame is one Flush's bytes: the cells, the images over them and the latched cursor, inside
// the synchronized-update brackets caps allows. Nothing to say is nil (zero bytes). The slice
// is the emitter's buffer, valid until the next frame.
func (e *emitter) frame(caps tui.Capabilities, diff []tui.CellUpdate, want tui.CursorState, dirty bool, images []tui.ImageOp) []byte {
	if len(diff) == 0 && !dirty && len(images) == 0 {
		return nil
	}
	e.buf.Reset()
	e.bracket(caps.SyncOutput, func() {
		if len(diff) > 0 && e.termVisible {
			e.buf.WriteString("\x1b[?25l") // R1: hide during paint
			e.termVisible = false
		}
		e.emitCells(diff, caps.UnicodeCore)
		e.writeImages(images) // over the cells just written, within the same Write (graphics.go)
		e.emitCursor(want)
	})
	return e.buf.Bytes()
}

// restore writes into buf what teardown gives back of the emitter's state: the terminal's own
// cursor colour, and the deletes of this program's images (only those, by id).
func (e *emitter) restore(buf *bytes.Buffer) {
	if e.termColored {
		buf.WriteString("\x1b]112\x07") // OSC 112
		e.termColored = false
	}
	for id := range e.placed {
		e.writeImageDelete(buf, id)
	}
	e.placed = nil
}

// bracket writes body's output inside mode-2026 synchronized-update brackets when the terminal has
// them (R4).
func (e *emitter) bracket(sync bool, body func()) {
	if sync {
		e.buf.WriteString("\x1b[?2026h") // R4: begin synchronized update
	}
	body()
	if sync {
		e.buf.WriteString("\x1b[?2026l") // R4: end synchronized update
	}
}

// emitCells writes the diff's cells: CUP only on a discontinuity, SGR only on an attribute change,
// and an absolute re-anchor after a risky cluster on a terminal without mode 2027.
func (e *emitter) emitCells(diff []tui.CellUpdate, unicodeCore bool) {
	buf := &e.buf
	for i := range diff {
		u := &diff[i]
		if u.Cell.Width == 0 {
			// Continuation cells ride on their head (W2 guarantees pairs
			// are dirtied together); emitting the head covers both columns.
			continue
		}
		if e.forceAnchor || !e.penKnown || u.Y != e.penY || u.X != e.penX {
			e.writeCUP(u.X, u.Y) // CUP only on discontinuity
			e.forceAnchor = false
		}
		if !e.attrsKnown || u.Cell.Attrs != e.lastAttrs {
			e.writeSGR(u.Cell.Attrs) // SGR only on attribute change
			e.lastAttrs = u.Cell.Attrs
			e.attrsKnown = true
		}
		content := u.Cell.Content
		if content == "" {
			content = " "
		}
		buf.WriteString(content)
		e.penX, e.penY = u.X+int(u.Cell.Width), u.Y
		e.penKnown = true
		if !unicodeCore && riskyCluster(content) {
			// Without mode 2027 the terminal's advance for this cluster is
			// untrusted — re-anchor absolutely next time.
			e.forceAnchor = true
		}
	}
}

// emitCursor writes the latched cursor state, within the same Write: its position, shape, colour
// and visibility, each only where it differs from what the terminal has.
func (e *emitter) emitCursor(want tui.CursorState) {
	buf := &e.buf
	if want.PosSet {
		e.writeCUP(want.X, want.Y)
		e.penX, e.penY, e.penKnown = want.X, want.Y, true
	}
	if want.Shape != e.termShape {
		e.writeShape(want.Shape)
		e.termShape = want.Shape
	}
	if want.Colored != e.termColored || (want.Colored && want.Color != e.termColor) {
		if want.Colored {
			fmt.Fprintf(buf, "\x1b]12;#%02x%02x%02x\x07", want.Color.R, want.Color.G, want.Color.B)
		} else {
			buf.WriteString("\x1b]112\x07")
		}
		e.termColor, e.termColored = want.Color, want.Colored
	}
	if want.Visible != e.termVisible {
		if want.Visible {
			buf.WriteString("\x1b[?25h")
		} else {
			buf.WriteString("\x1b[?25l")
		}
		e.termVisible = want.Visible
	}
}

func (e *emitter) writeInt(n int) {
	e.buf.Write(strconv.AppendInt(e.numScratch[:0], int64(n), 10))
}

// writeCUP emits an absolute cursor-position (1-based on the wire).
func (e *emitter) writeCUP(x, y int) {
	e.buf.WriteString("\x1b[")
	e.writeInt(y + 1)
	e.buf.WriteByte(';')
	e.writeInt(x + 1)
	e.buf.WriteByte('H')
}

// writeSGR emits the full resolved attribute state: a reset followed by every
// set attribute and both colors, so emission never depends on the previous
// cell's attributes beyond the change test.
func (e *emitter) writeSGR(a tui.CellAttrs) {
	buf := &e.buf
	buf.WriteString("\x1b[0")
	for _, at := range sgr.Attrs { // the codes vt reads them with
		if a.Mask&at.Attr != 0 {
			buf.WriteByte(';')
			e.writeInt(at.On)
		}
	}
	e.writeColor(a.FG, false)
	e.writeColor(a.BG, true)
	buf.WriteByte('m')
}

func (e *emitter) writeColor(c tui.CellColor, bg bool) {
	base := 30
	if bg {
		base = 40
	}
	switch c.Kind {
	case tui.CellColorDefault:
		// SGR 0 already selected the defaults; nothing to emit.
	case tui.CellColorANSI:
		e.buf.WriteByte(';')
		if c.Index < 8 {
			e.writeInt(base + int(c.Index))
		} else {
			e.writeInt(base + 60 + int(c.Index-8)) // bright: 90–97 / 100–107
		}
	case tui.CellColorANSI256:
		if bg {
			e.buf.WriteString(";48;5;")
		} else {
			e.buf.WriteString(";38;5;")
		}
		e.writeInt(int(c.Index))
	case tui.CellColorRGB:
		if bg {
			e.buf.WriteString(";48;2;")
		} else {
			e.buf.WriteString(";38;2;")
		}
		e.writeInt(int(c.R))
		e.buf.WriteByte(';')
		e.writeInt(int(c.G))
		e.buf.WriteByte(';')
		e.writeInt(int(c.B))
	}
}

// writeShape emits DECSCUSR (CSI Ps SP q), using the steady variants.
func (e *emitter) writeShape(s tui.CursorShape) {
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
	e.buf.WriteString("\x1b[")
	e.writeInt(n)
	e.buf.WriteString(" q")
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
