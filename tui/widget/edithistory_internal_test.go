package widget

import "testing"

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
