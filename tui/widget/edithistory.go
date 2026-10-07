package widget

// EDIT HISTORY — undo and redo for a text widget: snapshots of the text and the cursor, taken at
// the start of each edit group, at most editHistoryCap of them. A group left open (an Insert
// session) takes no further snapshot until it is closed, so a session undoes as one step.
//
// It knows nothing of the widget: the widget hands it the state to keep (a func, so a keystroke
// inside an open group copies nothing) and restores what it hands back.

// textSnap is a text and its cursor, as they were before an edit group.
type textSnap struct {
	lines   []string
	ln, col int
}

// editHistoryCap is how many groups can be undone; older ones are dropped.
const editHistoryCap = 64

type editHistory struct {
	enabled    bool // false: nothing is recorded, and there is nothing to undo or redo
	undo, redo []textSnap
	open       bool // a group is open: edits join it without a snapshot of their own
}

// setEnabled turns the history on or off; off forgets it.
func (h *editHistory) setEnabled(on bool) {
	h.enabled = on
	if !on {
		h.undo, h.redo = nil, nil
	}
}

// begin starts an edit group with the state before it, unless keepOpen and a group is already
// open. keepOpen leaves the group open for the edits that follow. A new group clears redo.
func (h *editHistory) begin(before func() textSnap, keepOpen bool) {
	if !h.enabled || (keepOpen && h.open) {
		return
	}
	h.undo = append(h.undo, before())
	if len(h.undo) > editHistoryCap {
		h.undo = h.undo[1:]
	}
	h.redo = nil
	if keepOpen {
		h.open = true
	}
}

// keepOpen marks the group an edit just began as open: the edit that enters Insert (o, O) began
// it, and the session's typing joins it.
func (h *editHistory) keepOpen() { h.open = true }

// close ends the open group, if any: the next edit begins its own.
func (h *editHistory) close() { h.open = false }

// reset forgets every group, as a new text does.
func (h *editHistory) reset() { h.undo, h.redo, h.open = nil, nil, false }

func (h *editHistory) canUndo() bool { return h.enabled && len(h.undo) > 0 }
func (h *editHistory) canRedo() bool { return h.enabled && len(h.redo) > 0 }

// stepBack is the state to restore for an undo, keeping now for a redo; ok is false with nothing
// to undo.
func (h *editHistory) stepBack(now func() textSnap) (textSnap, bool) {
	if !h.canUndo() {
		return textSnap{}, false
	}
	h.open = false
	s := h.undo[len(h.undo)-1]
	h.undo = h.undo[:len(h.undo)-1]
	h.redo = append(h.redo, now())
	return s, true
}

// stepForward is the state to restore for a redo, keeping now for an undo; ok is false with
// nothing to redo.
func (h *editHistory) stepForward(now func() textSnap) (textSnap, bool) {
	if !h.canRedo() {
		return textSnap{}, false
	}
	h.open = false
	s := h.redo[len(h.redo)-1]
	h.redo = h.redo[:len(h.redo)-1]
	h.undo = append(h.undo, now())
	return s, true
}
