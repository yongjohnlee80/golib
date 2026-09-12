package widget

// editorSnap captures a restorable snapshot of the buffer text and cursor position
// for the bounded undo/redo history ring.
type editorSnap struct {
	lines   []string
	ln, col int
}

const editorUndoCap = 64

// snapshot produces an isolated copy of the editor's text lines and caret position.
func (e *Editor) snapshot() editorSnap {
	lines := make([]string, len(e.lines))
	copy(lines, e.lines)
	return editorSnap{lines: lines, ln: e.ln, col: e.col}
}

// beginGroup pushes an undo snapshot for a new edit group: every
// Normal-mode edit is one group; an Insert session is one group opened
// lazily at its first mutation. A paste during Insert stays inside the open
// group; focus loss closes it without leaving Insert.
func (e *Editor) beginGroup() {
	if !e.canUndo {
		return
	}
	if e.mode == ModeInsert && e.groupOpen {
		return
	}
	e.undo = append(e.undo, e.snapshot())
	if len(e.undo) > editorUndoCap {
		e.undo = e.undo[1:]
	}
	e.redo = nil
	if e.mode == ModeInsert {
		e.groupOpen = true
	}
}

// doUndo reverts the most recent edit group, shifting the current state to the redo stack.
func (e *Editor) doUndo() {
	if !e.canUndo || len(e.undo) == 0 {
		return
	}
	e.groupOpen = false
	snap := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.redo = append(e.redo, e.snapshot())
	e.restore(snap)
}

// doRedo reapplies the most recently reverted edit group from the redo stack.
func (e *Editor) doRedo() {
	if !e.canUndo || len(e.redo) == 0 {
		return
	}
	e.groupOpen = false
	snap := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.undo = append(e.undo, e.snapshot())
	e.restore(snap)
}

// restore resets the buffer content and cursor coordinates from a snapshot.
func (e *Editor) restore(s editorSnap) {
	e.lines = s.lines
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
	e.desired = -1
	e.ensureVisible()
	e.MarkDirty()
	e.publish(ChangeEvent{Owner: e.NodeID(), Value: e.Value()})
}

// WithUndo configures whether the editor maintains undo/redo history.
func WithUndo(enabled bool) EditorOption {
	return func(e *Editor) {
		e.canUndo = enabled
		if !enabled {
			e.undo = nil
			e.redo = nil
		}
	}
}
