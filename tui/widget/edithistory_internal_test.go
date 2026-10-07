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
