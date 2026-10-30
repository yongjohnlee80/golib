package widget

// EDIT HISTORY — undo and redo for a text widget: at most editHistoryCap edit groups, each the
// cursor, marks and revision before it and the changes that take its text back. A group left open
// (an Insert session) takes in every edit until it is closed, so a session undoes as one step.
//
// No group copies the text. Each holds only what the line primitives handed it as they changed
// the lines (textbuffer.go's undoOp, through bufferText.undoRec): a group costs the lines its
// edits replaced, not the document. The texts the stacks stand for are as a whole-text snapshot
// would keep them:
//   - the undo stack's top takes the text now back to the text before the last group; each group
//     below it takes the text before the group above it back to the text before its own;
//   - the redo stack's top takes the text now forward to the text an undo left, and so on down.
//
// So every edit is recorded into the top of both, as it moves the text both are measured from;
// an undo or a redo, which moves it by applying a group, records instead into the group it gives
// the other stack (into). This is right only while the primitives are the lines' only writers,
// which internal/audit's line-primitives guard keeps.
//
// It knows nothing of the widget: the widget hands it the state to keep (a func, so a keystroke
// inside an open group copies nothing) and applies the groups it hands back.

// textSnap is an edit group: the cursor, marks and revision as they were before it, and the
// changes that take the text back there.
type textSnap struct {
	ops     []undoOp // in the order made: applied last first
	ln, col int
	rev     uint64  // the text's revision: undoing to the saved text reads as unmodified
	marks   markSet // the marks when the group began: undo and redo bring them back
}

// editHistoryCap is how many groups can be undone; older ones are dropped.
const editHistoryCap = 64

type editHistory struct {
	enabled    bool // false: nothing is recorded, and there is nothing to undo or redo
	undo, redo []textSnap
	open       bool // a group is open: edits join it without a snapshot of their own
	// held makes everything until release one group, whatever closes it meanwhile (a . replay,
	// whose Insert moves would close it); heldRedo is the redo the hold cleared, for a replay that
	// changed nothing.
	held     bool
	heldRedo []textSnap
	// into is the group an undo or a redo is applying its changes into, for the other stack: set
	// by stepBack and stepForward, cleared by settle
	into *textSnap
}

// recording reports whether an edit now has a group to go into (lineRecorder).
func (h *editHistory) recording() bool {
	return h.enabled && (h.into != nil || len(h.undo) > 0 || len(h.redo) > 0)
}

// record takes in what takes one edit back (lineRecorder): into the group an undo or a redo is
// applying, else into the top of both stacks. The primitives call it only once recording said
// yes, which a disabled history never does.
func (h *editHistory) record(op undoOp) {
	if h.into != nil {
		h.into.ops = addOp(h.into.ops, op)
		return
	}
	if n := len(h.undo); n > 0 {
		h.undo[n-1].ops = addOp(h.undo[n-1].ops, op)
	}
	if n := len(h.redo); n > 0 {
		h.redo[n-1].ops = addOp(h.redo[n-1].ops, op)
	}
}

// addOp adds op to ops, unless the last change already takes back every line op would: a change
// that keeps the line count within the lines the last one put there (a keystroke on the line the
// last keystroke made) is taken back with it, so a session of typing keeps one old line, not one
// per key.
func addOp(ops []undoOp, op undoOp) []undoOp {
	if n := len(ops); n > 0 {
		last := ops[n-1]
		if op.n == len(op.old) && op.lo >= last.lo && op.lo+op.n <= last.lo+last.n {
			return ops
		}
	}
	return append(ops, op)
}

// setEnabled turns the history on or off; off forgets it.
func (h *editHistory) setEnabled(on bool) {
	h.enabled = on
	if !on {
		h.undo, h.redo, h.into = nil, nil, nil
	}
}

// begin starts an edit group with the state before it, unless keepOpen and a group is already
// open. keepOpen leaves the group open for the edits that follow. A new group clears redo.
func (h *editHistory) begin(before func() textSnap, keepOpen bool) {
	if !h.enabled || h.held || (keepOpen && h.open) {
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

// close ends the open group, if any: the next edit begins its own. (A held group needs no guard
// here: begin takes no snapshot while held, so nothing can split it.)
func (h *editHistory) close() { h.open = false }

// hold begins one group for everything until release: one snapshot now, and none, nor any close,
// until release.
func (h *editHistory) hold(before func() textSnap) {
	if !h.enabled || h.held {
		return
	}
	// no trim to the cap yet: a replay that changes nothing gives its snapshot back on release, and
	// the oldest step must still be there
	h.undo = append(h.undo, before())
	h.heldRedo, h.redo = h.redo, nil
	h.held, h.open = true, true
}

// release ends the held group; one that changed nothing is no step at all, and the redo it cleared
// comes back.
func (h *editHistory) release(changed bool) {
	if !h.held {
		return
	}
	h.held, h.open = false, false
	switch {
	case !changed && len(h.undo) > 0:
		h.undo = h.undo[:len(h.undo)-1]
		h.redo = h.heldRedo
	case len(h.undo) > editHistoryCap:
		h.undo = h.undo[1:]
	}
	h.heldRedo = nil
}

// reset forgets every group, as a new text does.
func (h *editHistory) reset() { h.undo, h.redo, h.open, h.into = nil, nil, false, nil }

func (h *editHistory) canUndo() bool { return h.enabled && len(h.undo) > 0 }
func (h *editHistory) canRedo() bool { return h.enabled && len(h.redo) > 0 }

// stepBack is the group to apply for an undo; now, the state before it, becomes the redo group,
// which takes in the changes applying it makes until settle. ok is false with nothing to undo.
func (h *editHistory) stepBack(now func() textSnap) (textSnap, bool) {
	if !h.canUndo() {
		return textSnap{}, false
	}
	h.open = false
	s := h.undo[len(h.undo)-1]
	h.undo = h.undo[:len(h.undo)-1]
	h.redo = append(h.redo, now())
	h.into = &h.redo[len(h.redo)-1]
	return s, true
}

// stepForward is the group to apply for a redo; now becomes the undo group, which takes in the
// changes applying it makes until settle. ok is false with nothing to redo.
func (h *editHistory) stepForward(now func() textSnap) (textSnap, bool) {
	if !h.canRedo() {
		return textSnap{}, false
	}
	h.open = false
	s := h.redo[len(h.redo)-1]
	h.redo = h.redo[:len(h.redo)-1]
	h.undo = append(h.undo, now())
	h.into = &h.undo[len(h.undo)-1]
	return s, true
}

// settle ends an undo's or a redo's apply: edits go to the stacks' tops again.
func (h *editHistory) settle() { h.into = nil }
