package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
)

// visualRange returns the INCLUSIVE charwise selection as an exclusive
// [lo, hiEx) buffer region.
func (c *EditorCore) visualRange() (lo, hiEx taPos) {
	a, b := ordered(c.vAnchor, taPos{ln: c.buf.ln, col: c.buf.col})
	n := len(c.buf.lineClusters(b.ln))
	if (c.vEOL && b.col >= n || n == 0) && b.ln+1 < len(c.buf.textLines) {
		// the line's break is selected: Select's, or an empty line's, as Vim's inclusive Visual
		// takes the break of an empty line it ends on
		return a, taPos{ln: b.ln + 1}
	}
	return a, taPos{ln: b.ln, col: min(b.col+1, n)}
}

// visualLines returns the inclusive line span of a line-wise selection.
func (c *EditorCore) visualLines() (lo, hi int) {
	lo, hi = c.vAnchor.ln, c.buf.ln
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

// inVisual reports whether (ln, col) is inside the visual highlight.
func (c *EditorCore) inVisual(ln, col int) bool {
	if !c.canSelect {
		return false
	}
	if s, ok := c.visualSelection(); ok {
		return s.contains(ln, col)
	}
	// a table cell Tab selected, which typing replaces
	return c.keys.mode == ModeInsert && c.inCellSelection(ln, col)
}

// SelectedText returns the visual selection ("" outside visual modes or when selection is disabled):
// a block's pieces, one per line, joined with line breaks.
func (c *EditorCore) SelectedText() string {
	if !c.canSelect {
		return ""
	}
	if s, ok := c.visualSelection(); ok {
		return s.text()
	}
	return ""
}

// visual reports one of the Visual modes.
func (c *EditorCore) visual() bool {
	switch c.keys.mode {
	case ModeVisual, ModeVisualLine, ModeVisualBlock:
		return true
	}
	return false
}

// SelectionRange returns the visual selection as a region from (row, col) up to, not including,
// (endRow, endCol): rows and columns from 0, a column counting grapheme clusters, as Line reports
// the cursor. A line-wise selection runs from the start of its first line to the end of its last.
// ok is false outside the visual modes, or when selection is disabled. It is the region
// SelectedText returns the text of, except for a block, which is no single region: then it runs
// from the block's top left to its bottom right.
func (c *EditorCore) SelectionRange() (row, col, endRow, endCol int, ok bool) {
	if !c.canSelect {
		return 0, 0, 0, 0, false
	}
	if s, ok := c.visualSelection(); ok {
		row, col, endRow, endCol = s.bounds()
		return row, col, endRow, endCol, true
	}
	return 0, 0, 0, 0, false
}

// SetRegister imports text into the unnamed register (the application's
// value-inspect copy path).
func (c *EditorCore) SetRegister(text string, linewise bool) { c.reg.set(text, linewise) }

// Register returns the unnamed register's content.
func (c *EditorCore) Register() (text string, linewise bool) { return c.reg.content() }

// NamedRegister returns register name's content: a–z, 0–9, -, + or * ('+' and '*' hold what this
// editor last wrote to the system clipboard), or '"' for the unnamed register, as Register returns
// it. ok is false for _ (which holds nothing) and for a name that is no register.
func (c *EditorCore) NamedRegister(name rune) (text string, linewise, ok bool) {
	e, ok := c.reg.get(name)
	return e.text, e.linewise, ok
}

// SetNamedRegister sets register name as a yank into it would, without 0's or the numbered
// registers' shifts: A–Z append, and the unnamed register then holds it. Setting '+' or '*' sets
// what this editor last wrote there; it does not write the system clipboard. It reports false for
// '"' (SetRegister sets that one), _ and a name that is no register.
func (c *EditorCore) SetNamedRegister(name rune, text string, linewise bool) bool {
	return c.reg.put(name, regEntry{text: text, linewise: linewise})
}

// RegisterKind is what a register holds: characters, whole lines, or a block (Visual block's).
type RegisterKind uint8

const (
	RegisterChars RegisterKind = iota
	RegisterLines
	RegisterBlock
)

// RegisterKind reports what register name holds, for the names NamedRegister reads.
func (c *EditorCore) RegisterKind(name rune) (kind RegisterKind, ok bool) {
	e, ok := c.reg.get(name)
	switch {
	case e.block:
		kind = RegisterBlock
	case e.linewise:
		kind = RegisterLines
	}
	return kind, ok
}

// NamedBlock returns register name's block: its pieces, one per line, and its width in display
// cells. ok is false when the register holds no block.
func (c *EditorCore) NamedBlock(name rune) (pieces []string, width int, ok bool) {
	e, ok := c.reg.get(name)
	if !ok || !e.block {
		return nil, 0, false
	}
	return strings.Split(e.text, "\n"), e.width, true
}

// SetNamedBlock sets register name to a block, as SetNamedRegister sets text. width is the
// block's width in display cells (a wide character two, a tab tabCells): 0 is the widest piece's,
// and a width less than that is raised to it, so no piece is cut. A block of empty pieces with a
// width puts that many spaces; one of no pieces holds nothing. It reports false as
// SetNamedRegister does.
func (c *EditorCore) SetNamedBlock(name rune, pieces []string, width int) bool {
	if len(pieces) == 0 {
		width = 0
	}
	for _, p := range pieces {
		width = max(width, blockCells(p))
	}
	return c.reg.put(name, regEntry{text: strings.Join(pieces, "\n"), block: true, width: width})
}

// exportYank puts a yanked selection on the SYSTEM clipboard and reports the
// outcome, and it is called only from the explicit yank actions.
//
// NOT from regDelete, which is the REGISTER seam: a vim delete populates the
// register too -- correctly -- so exporting there would push deleted text out
// over OSC 52. Delete a line holding a secret and it would land in the
// clipboard of whoever ran the app. Two seams, because they mean two different
// things.
//
// A raw-mode TUI blocks the terminal's own selection, so the widget owes its
// user a way to copy out; bufferview already does this on `y` for the same
// reason. Backends without a ClipboardWriter make CopyToClipboard report
// false, which is surfaced rather than treated as an error.
func (c *EditorCore) exportYank(text string) {
	if !c.reg.yankAllowed() {
		return
	}
	delivered := false
	if c.ctx != nil {
		delivered = c.ctx.CopyToClipboard(text)
	}
	c.publish(YankEvent{Owner: c.nodeID(), ClipboardDelivered: delivered})
}

// regYank records a yank into the register the command named ("{reg}; none: the unnamed and 0),
// and exports it to the system clipboard, as every explicit yank is. A yank into the black hole
// keeps nothing and exports nothing.
func (c *EditorCore) regYank(text string, linewise bool) {
	c.yankEntry(regEntry{text: text, linewise: linewise})
}

// yankEntry is regYank of an entry of any kind; a block exports its pieces joined with line breaks.
func (c *EditorCore) yankEntry(e regEntry) {
	c.reg.yanked(c.keys.reg, e)
	if c.keys.reg != '_' {
		c.exportYank(e.text)
	}
}

// regDelete records a delete into the register the command named (editregister.go's rules): a line
// or more, or a motion Vim keeps in 1 (opRegOne), shifts the numbered registers. A delete named
// into + or * is exported, since the clipboard was asked for by name; an unnamed delete never is.
func (c *EditorCore) regDelete(text string, linewise bool) {
	c.deleteEntry(regEntry{text: text, linewise: linewise}, linewise || strings.Contains(text, "\n"))
}

// deleteEntry is regDelete of an entry of any kind; lines is a delete over more than one line.
func (c *EditorCore) deleteEntry(e regEntry, lines bool) {
	name := c.keys.reg
	c.reg.deleted(name, e, lines || c.opRegOne)
	if name == '+' || name == '*' {
		c.exportYank(e.text)
	}
}

// deleteLines removes [lo, hi] inclusive into the register (linewise).
func (c *EditorCore) deleteLines(lo, hi int) {
	c.beginGroup()
	c.regDelete(strings.Join(c.buf.textLines[lo:hi+1], "\n"), true)
	c.buf.removeLines(lo, hi+1) // every line gone leaves one empty line
	c.buf.ln = min(lo, len(c.buf.textLines)-1)
	c.buf.col = 0
	c.buf.anchor = nil
	c.clampNormal()
	c.edited()
}

// pasteRegister is p (after) and P: the register the command named (none: the unnamed), count
// times. Whole lines go below (or above) the cursor's line, the cursor on the first; text goes after
// (or before) the cursor, the cursor on its last cluster.
func (c *EditorCore) pasteRegister(after bool, count int) {
	name := c.keys.reg
	if (name == '+' || name == '*') && c.readClipboardPut(name, after, count) {
		return // put when the clipboard answers
	}
	e, _ := c.reg.get(name)
	c.putEntry(e, after, count)
}

// putEntry puts e as p (after) or P does, count times.
func (c *EditorCore) putEntry(e regEntry, after bool, count int) {
	if !e.holds() {
		return
	}
	if e.block {
		c.blockPut(e, after, count) // editor_vimblock.go
		return
	}
	text, linewise := e.text, e.linewise
	count = max(count, 1)
	c.beginGroup()
	if linewise {
		at := c.buf.ln
		if from, to, _, ok := c.foldLayout().FoldOf(at); ok { // around a fold, as Vim puts
			at = from
			if after {
				at = to
			}
		} else if after {
			at++
		}
		newLines := strings.Split(strings.Repeat(text+"\n", count-1)+text, "\n")
		c.buf.insertLines(at, newLines...)
		c.buf.ln, c.buf.col = at, 0
	} else {
		col := c.buf.col
		if after && len(c.buf.lineClusters(c.buf.ln)) > 0 {
			col++
		}
		c.buf.col = c.buf.clampCol(c.buf.ln, col)
		c.buf.insertText(strings.Repeat(text, count))
		// vim leaves the cursor ON the last pasted cluster.
		c.buf.col = max(0, c.buf.col-1)
		c.clampNormal()
	}
	c.edited()
}

// WithSelection configures whether visual selection is enabled on the editor.
func WithSelection(enabled bool) EditorOption { return WithCore(CoreSelection(enabled)) }

// WithYank configures whether yanking to system clipboard and registers is enabled.
func WithYank(enabled bool) EditorOption { return WithCore(CoreYank(enabled)) }

// clipPut is a "+p or "*p waiting for the system clipboard: the read it waits on, and the put as
// it was asked for, at the text's revision and the cursor it was asked at.
type clipPut struct {
	read    tui.ClipboardRead
	reg     rune
	after   bool
	count   int
	rev     uint64
	ln, col int
	// keys is the command as . recorded it ("+p): the last change once the put lands, since the
	// command itself ended before it changed anything.
	keys []dotAtom
}

// readClipboardPut starts "+p or "*p as a read of the system clipboard, and reports whether it
// did: the put runs when the read answers. It doesn't when there is no clipboard to read (no
// Context, or a backend that can't: the register's last write is put now), or inside a replay (.
// or a macro), which puts what was last read or written there without waiting, so the replay stays
// one synchronous step.
func (c *EditorCore) readClipboardPut(reg rune, after bool, count int) bool {
	if c.ctx == nil || c.dot.replaying || c.macro.playing > 0 {
		return false
	}
	p := clipPut{reg: reg, after: after, count: count, rev: c.rev, ln: c.buf.ln, col: c.buf.col}
	if c.dot.on && !c.dot.inInsert {
		p.keys = append([]dotAtom(nil), c.dot.atoms...)
	}
	read, err := c.ctx.ReadClipboard(0, func(read tui.ClipboardRead, text string, err error) {
		c.clipboardAnswered(read, text, err)
	})
	if err != nil {
		return false
	}
	p.read = read
	c.clipPut = &p // a later "+p replaces it: this read's answer is then dropped
	return true
}

// clipboardAnswered puts what the clipboard answered, if the put still waits on that read and the
// text and cursor are as they were (a put landing where the user has since typed or moved would be
// a surprise). Text ending in a line break is whole lines, else characters (Vim's MAUTO). A failed
// read puts what this editor last wrote there. What was read is kept as the register's, for a
// replay to put.
func (c *EditorCore) clipboardAnswered(read tui.ClipboardRead, text string, err error) {
	p := c.clipPut
	if p == nil || p.read != read {
		return
	}
	c.clipPut = nil
	if c.rev != p.rev || c.buf.ln != p.ln || c.buf.col != p.col || c.readOnly {
		return
	}
	ln, col := c.buf.ln, c.buf.col
	defer c.cursorMoved(ln, col)
	e, _ := c.reg.get(p.reg)
	if err == nil {
		e = regEntry{text: text}
		if strings.HasSuffix(text, "\n") {
			e = regEntry{text: strings.TrimSuffix(text, "\n"), linewise: true}
		}
		c.reg.setClip(p.reg, e)
	}
	rev := c.rev
	c.putEntry(e, p.after, p.count)
	if c.rev != rev && p.keys != nil {
		c.dot.last = p.keys // . repeats the "+p
	}
}
