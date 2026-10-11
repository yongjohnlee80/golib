package widget

import (
	"errors"
	"io"
)

// THE EDITOR CORE FUNCTIONS — what a host, a script or an agent edits a document through.
//
// Every function here works in buffer positions (TextPos: a line from 0, a column counting
// grapheme clusters from 0, the unit the cursor moves in), never in cells or pixels, so it means
// the same in every view of the core. Each edit is one undo group and one ChangeEvent; Batch makes
// several edits one of each. An edit on a read-only core is refused with ErrReadOnly.
//
//	Positions   Cursor, SetCursor, TextIn, Select, Selection
//	Edits       Insert, Delete, Replace, Batch
//	I/O         ReadFrom (io.ReaderFrom), WriteTo (io.WriterTo)
//	State       Modified, MarkSaved

// ErrReadOnly is what an edit returns on a read-only core.
var ErrReadOnly = errors.New("widget: the editor is read-only")

// TextPos is a place in an editor's text: Line counts lines from 0, Col counts grapheme clusters
// from 0. A position past a line's end is its end; a line past the last is the last.
type TextPos struct{ Line, Col int }

// TextRange is the text from From up to, not including, To. A range across lines includes the
// line breaks between them. Every function that takes one orders and clamps it first.
type TextRange struct{ From, To TextPos }

// Empty reports whether r covers no text once ordered.
func (r TextRange) Empty() bool { return r.From == r.To }

// newRevision is a revision no text has had: a counter, so undoing to an old text and editing
// again never lands back on a revision MarkSaved recorded.
func (c *EditorCore) newRevision() uint64 {
	c.revs++
	return c.revs
}

// clampPos keeps p inside the text.
func (c *EditorCore) clampPos(p TextPos) taPos {
	ln := max(0, min(p.Line, len(c.buf.lines)-1))
	return taPos{ln: ln, col: c.buf.clampCol(ln, p.Col)}
}

// clampRange orders and clamps r.
func (c *EditorCore) clampRange(r TextRange) (lo, hi taPos) {
	lo, hi = c.clampPos(r.From), c.clampPos(r.To)
	if hi.ln < lo.ln || (hi.ln == lo.ln && hi.col < lo.col) {
		lo, hi = hi, lo
	}
	return lo, hi
}

// Cursor is the cursor's position.
func (c *EditorCore) Cursor() TextPos { return TextPos{Line: c.buf.ln, Col: c.buf.col} }

// SetCursor moves the cursor to p, clamped, and reveals it, as SetLine does.
func (c *EditorCore) SetCursor(p TextPos) { c.SetLine(p.Line, p.Col) }

// TextIn is the text of r, line breaks as "\n".
func (c *EditorCore) TextIn(r TextRange) string {
	lo, hi := c.clampRange(r)
	return c.buf.textIn(lo, hi)
}

// Select shows r as the selection: the Visual selection, exactly [r.From, r.To). A range that ends
// at a line's start selects the line break before it, the cursor just past the previous line's
// last cluster. An empty r, or a core that does not select (WithSelection(false)), leaves the
// selection it has; Unselect clears it.
func (c *EditorCore) Select(r TextRange) {
	lo, hi := c.clampRange(r)
	if !c.canSelect || (lo == hi) {
		return
	}
	c.settlePendingRune()
	// the Visual selection includes the cluster under the cursor: end on the one before hi, or,
	// for a range ending at a line's start, on the previous line's break
	end, eol := hi, false
	if end.col > 0 {
		end.col--
	} else {
		end.ln--
		end.col, eol = len(c.buf.lineClusters(end.ln)), true
	}
	ln, col := c.buf.ln, c.buf.col
	defer c.cursorMoved(ln, col)
	c.vAnchor = lo
	c.buf.ln, c.buf.col = end.ln, end.col
	c.setMode(ModeVisual)
	c.vEOL, c.vPinned = eol, true
	c.reveal()
	c.markDirty()
}

// Unselect leaves the Visual modes, keeping the cursor where it is.
func (c *EditorCore) Unselect() {
	if c.keys.mode == ModeVisual || c.keys.mode == ModeVisualLine {
		c.exitVisual()
	}
}

// Selection is the selection as a range, ok false when there is none (SelectionRange).
func (c *EditorCore) Selection() (TextRange, bool) {
	row, col, endRow, endCol, ok := c.SelectionRange()
	return TextRange{From: TextPos{row, col}, To: TextPos{endRow, endCol}}, ok
}

// beginEdit prepares the core for an edit through these functions: input pending between keys
// settles, a selection is left (its anchor would not survive the edit), and an undo group begins
// unless a Batch holds one open.
func (c *EditorCore) beginEdit() error {
	if c.readOnly {
		return ErrReadOnly
	}
	c.settlePendingRune()
	c.Unselect()
	// inside a Batch, its first edit begins the group the rest join
	if (c.batch == 0 || !c.batchEdited) && !c.joinGroup {
		c.beginGroup()
	}
	return nil
}

// endEdit keeps a modal core's Normal-mode cursor on a cluster, and finishes the edit.
func (c *EditorCore) endEdit() {
	if c.keys.modal && c.keys.mode == ModeNormal {
		c.clampNormal()
	}
	c.edited()
}

// Insert puts text at p (line breaks split lines; "\r\n" and "\r" are breaks too) and returns the
// position just after it, where the cursor now is.
func (c *EditorCore) Insert(p TextPos, text string) (TextPos, error) {
	if err := c.beginEdit(); err != nil {
		return p, err
	}
	at := c.clampPos(p)
	c.buf.ln, c.buf.col, c.buf.anchor = at.ln, at.col, nil
	c.buf.insertText(text)
	end := c.Cursor()
	c.endEdit()
	return end, nil
}

// Delete removes the text of r; the cursor moves to where it began.
func (c *EditorCore) Delete(r TextRange) error {
	if err := c.beginEdit(); err != nil {
		return err
	}
	lo, hi := c.clampRange(r)
	c.buf.deleteRegion(lo, hi)
	c.endEdit()
	return nil
}

// Replace puts text in place of r's, in one undo group, and returns the position just after it.
func (c *EditorCore) Replace(r TextRange, text string) (TextPos, error) {
	if err := c.beginEdit(); err != nil {
		return r.From, err
	}
	lo, hi := c.clampRange(r)
	c.buf.deleteRegion(lo, hi)
	c.buf.insertText(text)
	end := c.Cursor()
	c.endEdit()
	return end, nil
}

// Batch runs fn with every edit it makes in ONE undo group and ONE ChangeEvent, sent when fn
// returns: a command's or a script's edits undo with one u. An error from fn is returned, and the
// edits it made stay (an undo reverts them). Batches nest; the outermost one ends the group.
func (c *EditorCore) Batch(fn func() error) error {
	c.batch++
	defer func() {
		if c.batch--; c.batch == 0 && c.batchEdited {
			c.batchEdited = false
			c.notifyEdited()
		}
	}()
	return fn()
}

// ReadFrom replaces the document with r's text, read to EOF, as SetValue does: history cleared,
// the cursor at the start, unmodified. On a read error the document is left as it was. It is
// io.ReaderFrom.
func (c *EditorCore) ReadFrom(r io.Reader) (int64, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return int64(len(b)), err
	}
	c.SetValue(string(b))
	return int64(len(b)), nil
}

// WriteTo writes the document: its lines joined by "\n", no newline added at the end. It does not
// mark the document saved, since a writer need not be its file: the host calls MarkSaved once the
// file is written. It is io.WriterTo.
func (c *EditorCore) WriteTo(w io.Writer) (int64, error) {
	var n int64
	for i, line := range c.buf.lines {
		if i > 0 {
			line = "\n" + line
		}
		k, err := io.WriteString(w, line)
		n += int64(k)
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// Modified reports whether the text differs from the one MarkSaved last recorded: an edit makes
// it so, and undoing back to the saved text makes it not. The text ReadFrom or SetValue gives is a
// new document and starts unmodified.
func (c *EditorCore) Modified() bool { return c.rev != c.savedRev }

// MarkSaved records the text as saved: the host calls it once its write succeeded.
func (c *EditorCore) MarkSaved() { c.savedRev = c.rev }
