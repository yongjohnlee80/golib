package widget

import (
	"testing"
)

// snapAt is a history-test state: one line of text, the cursor at its end.
func snapAt(text string) func() textSnap {
	return func() textSnap { return textSnap{lines: []string{text}, col: len(text)} }
}

func TestEditHistoryUndoesAndRedoesGroups(t *testing.T) {
	h := editHistory{enabled: true}
	h.begin(snapAt("a"), false)
	h.begin(snapAt("ab"), false)
	if !h.canUndo() || h.canRedo() {
		t.Fatalf("after two groups: canUndo %v canRedo %v", h.canUndo(), h.canRedo())
	}
	s, ok := h.stepBack(snapAt("abc"))
	if !ok || s.lines[0] != "ab" {
		t.Fatalf("undo restored %v %v; want ab", s.lines, ok)
	}
	s, ok = h.stepForward(snapAt("ab"))
	if !ok || s.lines[0] != "abc" {
		t.Fatalf("redo restored %v %v; want abc", s.lines, ok)
	}
	h.stepBack(snapAt("abc"))
	h.begin(snapAt("ab"), false) // a new edit after an undo
	if h.canRedo() {
		t.Fatal("a new group kept the redo stack")
	}
}

// An open group takes one snapshot for all its edits, and never calls for another until closed.
func TestEditHistoryOpenGroupIsOneStep(t *testing.T) {
	h := editHistory{enabled: true}
	calls := 0
	before := func() textSnap { calls++; return textSnap{lines: []string{""}} }
	for range 5 {
		h.begin(before, true)
	}
	if calls != 1 || len(h.undo) != 1 || !h.open {
		t.Fatalf("five edits in an open group: %d snapshots, %d groups, open %v", calls, len(h.undo), h.open)
	}
	h.close()
	h.begin(before, true)
	if calls != 2 || len(h.undo) != 2 {
		t.Fatalf("after close: %d snapshots, %d groups; want a new group", calls, len(h.undo))
	}
	h.stepBack(snapAt("x"))
	if h.open {
		t.Fatal("undo left the group open")
	}
}

func TestEditHistoryCapAndOff(t *testing.T) {
	h := editHistory{enabled: true}
	for i := range editHistoryCap + 6 {
		h.begin(snapAt(string(rune('a'+i%26))), false)
	}
	if len(h.undo) != editHistoryCap {
		t.Fatalf("%d groups kept; want the cap, %d", len(h.undo), editHistoryCap)
	}
	h.setEnabled(false)
	if h.canUndo() || len(h.undo) != 0 {
		t.Fatal("turning the history off kept it")
	}
	h.begin(func() textSnap { t.Fatal("a disabled history took a snapshot"); return textSnap{} }, false)
	if _, ok := h.stepBack(snapAt("")); ok {
		t.Fatal("a disabled history undid")
	}
	h.setEnabled(true)
	h.begin(snapAt("a"), true)
	h.reset()
	if h.canUndo() || h.open || len(h.redo) != 0 {
		t.Fatal("reset kept a group or the open flag")
	}
}

// snapN is a snapshot told apart by its rev.
func snapN(n uint64) func() textSnap { return func() textSnap { return textSnap{rev: n} } }

// A held group is one step: begin takes no snapshot while held, a second hold is no second step,
// and release without a hold does nothing.
func TestEditHistoryHoldIsOneStep(t *testing.T) {
	var h editHistory
	h.setEnabled(true)
	h.begin(snapN(1), false)
	h.redo = []textSnap{{rev: 9}}
	h.hold(snapN(2))
	h.hold(snapN(3)) // held already: no second snapshot
	h.begin(snapN(4), false)
	h.close()
	h.begin(snapN(5), true)
	if len(h.undo) != 2 || h.undo[1].rev != 2 || len(h.redo) != 0 {
		t.Fatalf("while held: undo %v redo %v, want [1 2] and no redo", h.undo, h.redo)
	}
	h.release(true)
	if h.held || h.open || len(h.undo) != 2 {
		t.Fatalf("after release: held %v open %v undo %v", h.held, h.open, h.undo)
	}
	h.release(true) // not held: nothing
	if len(h.undo) != 2 {
		t.Fatalf("a release with no hold changed the history: %v", h.undo)
	}
}

// A held group that changed nothing gives its snapshot and the redo it cleared back; one that
// changed something at the cap drops the oldest step then, not before.
func TestEditHistoryReleaseAtTheCap(t *testing.T) {
	var h editHistory
	h.setEnabled(true)
	for i := range editHistoryCap {
		h.begin(snapN(uint64(i)), false)
	}
	h.redo = []textSnap{{rev: 99}}
	h.hold(snapN(100))
	h.release(false)
	if len(h.undo) != editHistoryCap || h.undo[0].rev != 0 || len(h.redo) != 1 {
		t.Fatalf("a no-op release: %d steps from %d, redo %v; want %d from 0 and the redo back",
			len(h.undo), h.undo[0].rev, h.redo, editHistoryCap)
	}
	h.hold(snapN(100))
	h.release(true)
	if len(h.undo) != editHistoryCap || h.undo[0].rev != 1 || h.undo[len(h.undo)-1].rev != 100 {
		t.Fatalf("a changing release at the cap: %d steps from %d to %d; want %d from 1 to 100",
			len(h.undo), h.undo[0].rev, h.undo[len(h.undo)-1].rev, editHistoryCap)
	}
}

// A disabled history holds nothing.
func TestEditHistoryHoldWhenDisabled(t *testing.T) {
	var h editHistory
	h.hold(snapN(1))
	h.release(true)
	if h.held || len(h.undo) != 0 {
		t.Fatalf("a disabled history held: held %v undo %v", h.held, h.undo)
	}
}
