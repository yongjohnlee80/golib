package widget

import "strings"

// visualRange returns the INCLUSIVE charwise selection as an exclusive
// [lo, hiEx) buffer region.
func (e *Editor) visualRange() (lo, hiEx taPos) {
	a, b := e.vAnchor, taPos{ln: e.ln, col: e.col}
	if a.ln > b.ln || (a.ln == b.ln && a.col > b.col) {
		a, b = b, a
	}
	return a, taPos{ln: b.ln, col: min(b.col+1, len(e.lineClusters(b.ln)))}
}

// visualLines returns the inclusive line span of a line-wise selection.
func (e *Editor) visualLines() (lo, hi int) {
	lo, hi = e.vAnchor.ln, e.ln
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

// inVisual reports whether (ln, col) is inside the visual highlight.
func (e *Editor) inVisual(ln, col int) bool {
	if !e.canSelect {
		return false
	}
	switch e.mode {
	case ModeVisual:
		lo, hiEx := e.visualRange()
		p := taPos{ln: ln, col: col}
		return posLE(lo, p.ln, p.col) && !posLE(hiEx, p.ln, p.col)
	case ModeVisualLine:
		lo, hi := e.visualLines()
		return ln >= lo && ln <= hi
	}
	return false
}

// SelectedText returns the visual selection ("" outside visual modes or when selection is disabled).
func (e *Editor) SelectedText() string {
	if !e.canSelect {
		return ""
	}
	switch e.mode {
	case ModeVisual:
		lo, hiEx := e.visualRange()
		return e.textIn(lo, hiEx)
	case ModeVisualLine:
		lo, hi := e.visualLines()
		return strings.Join(e.lines[lo:hi+1], "\n")
	}
	return ""
}

// SetRegister imports text into the unnamed register (the application's
// value-inspect copy path).
func (e *Editor) SetRegister(text string, linewise bool) {
	e.regText, e.regLinewise = text, linewise
}

// Register returns the unnamed register's content.
func (e *Editor) Register() (text string, linewise bool) {
	return e.regText, e.regLinewise
}

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
func (e *Editor) exportYank(text string) {
	if !e.canYank {
		return
	}
	delivered := false
	if ctx := e.Context(); ctx != nil {
		delivered = ctx.CopyToClipboard(text)
	}
	e.publish(YankEvent{Owner: e.NodeID(), ClipboardDelivered: delivered})
}

func (e *Editor) yankSet(text string, linewise bool) {
	e.regText, e.regLinewise = text, linewise
}

// deleteLines removes [lo, hi] inclusive into the register (linewise).
func (e *Editor) deleteLines(lo, hi int) {
	e.beginGroup()
	e.yankSet(strings.Join(e.lines[lo:hi+1], "\n"), true)
	rest := append([]string{}, e.lines[:lo]...)
	rest = append(rest, e.lines[hi+1:]...)
	if len(rest) == 0 {
		rest = []string{""}
	}
	e.lines = rest
	e.ln = min(lo, len(e.lines)-1)
	e.col = 0
	e.anchor = nil
	e.clampNormal()
	e.edited()
}

func (e *Editor) pasteRegister(after bool) {
	if e.regText == "" && !e.regLinewise {
		return
	}
	e.beginGroup()
	if e.regLinewise {
		at := e.ln
		if after {
			at++
		}
		newLines := strings.Split(e.regText, "\n")
		e.lines = append(e.lines[:at], append(append([]string{}, newLines...), e.lines[at:]...)...)
		e.ln, e.col = at, 0
	} else {
		col := e.col
		if after && len(e.lineClusters(e.ln)) > 0 {
			col++
		}
		e.col = e.clampCol(e.ln, col)
		e.insertText(e.regText)
		// vim leaves the cursor ON the last pasted cluster.
		e.col = max(0, e.col-1)
		e.clampNormal()
	}
	e.edited()
}

// WithSelection configures whether visual selection is enabled on the editor.
func WithSelection(enabled bool) EditorOption {
	return func(e *Editor) {
		e.canSelect = enabled
	}
}

// WithYank configures whether yanking to system clipboard and registers is enabled.
func WithYank(enabled bool) EditorOption {
	return func(e *Editor) {
		e.canYank = enabled
	}
}
