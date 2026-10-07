package widget

// snapshot is the editor's text and cursor, for its history (edithistory.go).
func (e *Editor) snapshot() textSnap {
	lines := make([]string, len(e.lines))
	copy(lines, e.lines)
	return textSnap{lines: lines, ln: e.ln, col: e.col}
}

// beginGroup starts an undo group before an edit: every Normal-mode edit is one group; an
// Insert session is one group, opened at its first mutation and kept open. A paste during Insert
// stays inside the open group; focus loss closes it without leaving Insert.
func (e *Editor) beginGroup() { e.hist.begin(e.snapshot, e.mode == ModeInsert) }

// doUndo reverts the most recent edit group.
func (e *Editor) doUndo() {
	if s, ok := e.hist.stepBack(e.snapshot); ok {
		e.restore(s)
	}
}

// doRedo reapplies the most recently reverted edit group.
func (e *Editor) doRedo() {
	if s, ok := e.hist.stepForward(e.snapshot); ok {
		e.restore(s)
	}
}

// restore resets the buffer content and cursor coordinates from a snapshot.
func (e *Editor) restore(s textSnap) {
	e.lines = s.lines
	e.touch(0)
	e.ln = max(0, min(s.ln, len(e.lines)-1))
	e.col = s.col
	e.anchor = nil
	if e.modal {
		e.clampNormal()
	}
	e.edited()
}

// edited finalizes any buffer mutation: viewport, dirt, change event.
func (e *Editor) edited() {
	if e.numbers && e.gutterWidth() != e.gutter {
		e.RequestLayout() // the lines' count has another number of digits: the gutter's width moves
	}
	e.desired = -1
	e.ensureVisible()
	e.MarkDirty()
	e.publish(ChangeEvent{Owner: e.NodeID(), Value: e.Value()})
	if e.onChange != nil {
		e.onChange()
	}
}

// WithUndo configures whether the editor maintains undo/redo history.
func WithUndo(enabled bool) EditorOption {
	return func(e *Editor) { e.hist.setEnabled(enabled) }
}
