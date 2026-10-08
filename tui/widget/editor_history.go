package widget

// snapshot is the core's text and cursor, for its history (edithistory.go).
func (c *EditorCore) snapshot() textSnap {
	lines := make([]string, len(c.buf.lines))
	copy(lines, c.buf.lines)
	return textSnap{lines: lines, ln: c.buf.ln, col: c.buf.col}
}

// beginGroup starts an undo group before an edit: every Normal-mode edit is one group; an
// Insert session is one group, opened at its first mutation and kept open. A paste during Insert
// stays inside the open group; focus loss closes it without leaving Insert.
func (c *EditorCore) beginGroup() { c.hist.begin(c.snapshot, c.keys.mode == ModeInsert) }

// doUndo reverts the most recent edit group.
func (c *EditorCore) doUndo() {
	if s, ok := c.hist.stepBack(c.snapshot); ok {
		c.restore(s)
	}
}

// doRedo reapplies the most recently reverted edit group.
func (c *EditorCore) doRedo() {
	if s, ok := c.hist.stepForward(c.snapshot); ok {
		c.restore(s)
	}
}

// restore resets the buffer content and cursor coordinates from a snapshot.
func (c *EditorCore) restore(s textSnap) {
	b := &c.buf
	b.lines = s.lines
	b.touch(0)
	b.ln = max(0, min(s.ln, len(b.lines)-1))
	b.col = s.col
	b.anchor = nil
	if c.keys.modal {
		c.clampNormal()
	}
	c.edited()
}

// edited finalizes any buffer mutation: the layout hears which lines changed, the cursor is
// revealed, and the change is published.
func (c *EditorCore) edited() {
	c.buf.desired = -1
	c.changed()
	c.reveal()
	c.markDirty()
	c.publish(ChangeEvent{Owner: c.nodeID(), Value: c.buf.value()})
	if c.onChange != nil {
		c.onChange()
	}
}

// WithUndo configures whether the editor maintains undo/redo history.
func WithUndo(enabled bool) EditorOption { return WithCore(CoreUndo(enabled)) }
