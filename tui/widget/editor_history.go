package widget

// snapshot is the core's text and cursor, for its history (edithistory.go).
func (c *EditorCore) snapshot() textSnap {
	lines := make([]string, len(c.buf.textLines))
	copy(lines, c.buf.textLines)
	return textSnap{lines: lines, ln: c.buf.ln, col: c.buf.col, rev: c.rev, marks: c.marks}
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
	blk := changedBlock(b.textLines, s.lines)
	c.marks.apply(blk)
	for _, o := range c.editors { // the other editors showing the buffer, and the places left in it, move as the marks
		if o != c {
			followEditor(o, blk)
		}
	}
	for ed, p := range c.places {
		c.places[ed] = followPlace(p, blk)
	}
	b.setLines(s.lines)
	b.log.take() // the snapshot is the text whole: what was logged since describes no edit of it
	c.marks.restore(s.marks)
	b.ln = max(0, min(s.ln, len(b.textLines)-1))
	b.col = s.col
	b.anchor = nil
	if c.keys.modal {
		c.clampNormal()
	}
	c.rev = s.rev // the snapshot's revision, not a new one: Modified compares it with the saved
	c.notifyEdited()
}

// edited finalizes any buffer mutation: the text takes a new revision, the layout hears which
// lines changed, the cursor is revealed, and the change is published. Inside a Batch the
// notification waits for the batch's end.
func (c *EditorCore) edited() {
	c.followEdits()
	c.rev = c.newRevision()
	if c.batch > 0 {
		c.batchEdited = true
		return
	}
	c.notifyEdited()
}

// notifyEdited tells the layout, the highlighter and the listeners that the text changed.
func (c *EditorCore) notifyEdited() {
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

// changedBlock is the splice an undo or a redo makes, as Vim's u_undoredo adjusts marks for it:
// the lines between the texts' common first and last lines, replaced. Vim moves marks only when the
// block's size changes, erasing those in it; a block of the same size keeps them, so then the
// splice is empty.
func changedBlock(from, to []string) lineEdit {
	pre := 0
	for pre < len(from) && pre < len(to) && from[pre] == to[pre] {
		pre++
	}
	suf := 0
	for suf < len(from)-pre && suf < len(to)-pre && from[len(from)-1-suf] == to[len(to)-1-suf] {
		suf++
	}
	del, ins := len(from)-pre-suf, len(to)-pre-suf
	if del == ins {
		return lineEdit{}
	}
	return lineEdit{ln: pre, del: del, ins: ins}
}
