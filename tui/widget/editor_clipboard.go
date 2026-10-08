package widget

import "strings"

// visualRange returns the INCLUSIVE charwise selection as an exclusive
// [lo, hiEx) buffer region.
func (c *EditorCore) visualRange() (lo, hiEx taPos) {
	a, b := c.vAnchor, taPos{ln: c.buf.ln, col: c.buf.col}
	if a.ln > b.ln || (a.ln == b.ln && a.col > b.col) {
		a, b = b, a
	}
	return a, taPos{ln: b.ln, col: min(b.col+1, len(c.buf.lineClusters(b.ln)))}
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
	switch c.keys.mode {
	case ModeVisual:
		lo, hiEx := c.visualRange()
		p := taPos{ln: ln, col: col}
		return posLE(lo, p.ln, p.col) && !posLE(hiEx, p.ln, p.col)
	case ModeVisualLine:
		lo, hi := c.visualLines()
		return ln >= lo && ln <= hi
	}
	return false
}

// SelectedText returns the visual selection ("" outside visual modes or when selection is disabled).
func (c *EditorCore) SelectedText() string {
	if !c.canSelect {
		return ""
	}
	switch c.keys.mode {
	case ModeVisual:
		lo, hiEx := c.visualRange()
		return c.buf.textIn(lo, hiEx)
	case ModeVisualLine:
		lo, hi := c.visualLines()
		return strings.Join(c.buf.lines[lo:hi+1], "\n")
	}
	return ""
}

// SelectionRange returns the visual selection as a region from (row, col) up to, not including,
// (endRow, endCol): rows and columns from 0, a column counting grapheme clusters, as Line reports
// the cursor. A line-wise selection runs from the start of its first line to the end of its last.
// ok is false outside the visual modes, or when selection is disabled. It is the region
// SelectedText returns the text of.
func (c *EditorCore) SelectionRange() (row, col, endRow, endCol int, ok bool) {
	if !c.canSelect {
		return 0, 0, 0, 0, false
	}
	switch c.keys.mode {
	case ModeVisual:
		lo, hiEx := c.visualRange()
		return lo.ln, lo.col, hiEx.ln, hiEx.col, true
	case ModeVisualLine:
		lo, hi := c.visualLines()
		return lo, 0, hi, len(c.buf.lineClusters(hi)), true
	}
	return 0, 0, 0, 0, false
}

// SetRegister imports text into the unnamed register (the application's
// value-inspect copy path).
func (c *EditorCore) SetRegister(text string, linewise bool) { c.reg.set(text, linewise) }

// Register returns the unnamed register's content.
func (c *EditorCore) Register() (text string, linewise bool) { return c.reg.content() }

// exportYank puts a yanked selection on the SYSTEM clipboard and reports the
// outcome, and it is called only from the explicit yank actions.
//
// NOT from yankSet, which is the REGISTER seam: a vim delete populates the
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

func (c *EditorCore) yankSet(text string, linewise bool) { c.reg.set(text, linewise) }

// deleteLines removes [lo, hi] inclusive into the register (linewise).
func (c *EditorCore) deleteLines(lo, hi int) {
	c.beginGroup()
	c.yankSet(strings.Join(c.buf.lines[lo:hi+1], "\n"), true)
	rest := append([]string{}, c.buf.lines[:lo]...)
	rest = append(rest, c.buf.lines[hi+1:]...)
	if len(rest) == 0 {
		rest = []string{""}
	}
	c.buf.lines = rest
	c.buf.touch(lo)
	c.buf.ln = min(lo, len(c.buf.lines)-1)
	c.buf.col = 0
	c.buf.anchor = nil
	c.clampNormal()
	c.edited()
}

func (c *EditorCore) pasteRegister(after bool) {
	if !c.reg.holds() {
		return
	}
	text, linewise := c.reg.content()
	c.beginGroup()
	if linewise {
		at := c.buf.ln
		if after {
			at++
		}
		newLines := strings.Split(text, "\n")
		c.buf.lines = append(c.buf.lines[:at], append(append([]string{}, newLines...), c.buf.lines[at:]...)...)
		c.buf.touch(at)
		c.buf.ln, c.buf.col = at, 0
	} else {
		col := c.buf.col
		if after && len(c.buf.lineClusters(c.buf.ln)) > 0 {
			col++
		}
		c.buf.col = c.buf.clampCol(c.buf.ln, col)
		c.buf.insertText(text)
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
