package widget

import "slices"

// snapshot is the core's cursor, revision and marks, for a new group of its history
// (edithistory.go): its changes come in from the line primitives as the group's edits are made.
func (c *EditorCore) snapshot() textSnap {
	return textSnap{ln: c.buf.ln, col: c.buf.col, rev: c.rev, marks: c.marks}
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

// restore applies a group: its changes take the text back (or forward), and the cursor, marks and
// revision become the group's. The changes are made to the lines they span, as one replace, which
// the history takes in as the group for the other way, and that recording ends (settle) as soon as
// the replace is made: what follows notifies listeners, and a listener's own edit (CoreOnChange
// inserting, say) begins a group of its own, which must take that edit in.
func (c *EditorCore) restore(s textSnap) {
	b := &c.buf
	lo, hi, span := replayed(b.textLines, s.ops)
	blk := changedBlock(b.textLines, lo, hi, span)
	c.marks.apply(blk)
	for _, o := range c.editors { // the other editors showing the buffer, and the places left in it, move as the marks
		if o != c {
			followEditor(o, blk)
		}
	}
	for ed, p := range c.places {
		c.places[ed] = followPlace(p, blk)
	}
	if hi > lo || len(span) > 0 {
		b.replaceLines(lo, hi, span...)
	}
	c.hist.settle()
	b.log.take() // the marks and cursors moved by blk: what the replace logged describes no edit
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
	if publishChange(c.ctx, c.nodeID(), c.buf.value) {
		c.changeValues++
	}
	if c.onChange != nil {
		c.onChange()
	}
}

// replayed is what applying ops last first makes of lines, as a replace: lines [lo, hi) become
// span. Only the lines the ops reach are copied and changed; lines itself is not.
func replayed(lines []string, ops []undoOp) (lo, hi int, span []string) {
	if len(ops) == 0 {
		return 0, 0, nil
	}
	// the lines the ops reach, as they are applied: [lo, end) in the text as it then is, and grow,
	// the lines they have added so far
	lo, end, grow := -1, 0, 0
	for i := len(ops) - 1; i >= 0; i-- {
		op := ops[i]
		at, del, ins := op.lo, op.n, len(op.old)
		if lo < 0 {
			lo, end = at, at+ins
		} else {
			switch {
			case end >= at+del:
				end += ins - del
			case end > at:
				end = at + ins
			}
			lo, end = min(lo, at), max(end, at+ins)
		}
		grow += ins - del
	}
	hi = end - grow
	span = slices.Clone(lines[lo:hi])
	for i := len(ops) - 1; i >= 0; i-- {
		op := ops[i]
		span = slices.Replace(span, op.lo-lo, op.lo-lo+op.n, op.old...)
	}
	return lo, hi, span
}

// changedBlock is the splice an undo or a redo makes, as Vim's u_undoredo adjusts marks for it:
// the lines between the texts' common first and last lines, replaced. Vim moves marks only when the
// block's size changes, erasing those in it; a block of the same size keeps them, so then the
// splice is empty.
//
// The texts are from and from with lines [lo, hi) replaced by span. Lines outside [lo, hi) are the
// same in both, so the common first lines run at least to lo and the common last ones at least
// over from[hi:]: the scan starts there, and the block is what comparing the two texts whole would
// give, though only the lines between are compared.
func changedBlock(from []string, lo, hi int, span []string) lineEdit {
	toLen := len(from) - (hi - lo) + len(span)
	// to is line i of the second text, for i at lo or later: both scans stay there, the first
	// starting at lo and the second above the first's end
	to := func(i int) string {
		if i < lo+len(span) {
			return span[i-lo]
		}
		return from[i-len(span)+(hi-lo)]
	}
	pre := lo
	for pre < len(from) && pre < toLen && from[pre] == to(pre) {
		pre++
	}
	suf := min(len(from)-hi, len(from)-pre, toLen-pre)
	for suf < len(from)-pre && suf < toLen-pre && from[len(from)-1-suf] == to(toLen-1-suf) {
		suf++
	}
	del, ins := len(from)-pre-suf, toLen-pre-suf
	if del == ins {
		return lineEdit{}
	}
	return lineEdit{ln: pre, del: del, ins: ins}
}
