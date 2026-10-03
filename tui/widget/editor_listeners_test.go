package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// editor_listeners_test.go covers the Editor's constructor-time listeners.
//
// They exist for a caller that builds the editor before it is mounted and so
// has no Context to subscribe to the bus with. The property they must hold is
// that they hear EXACTLY what the bus hears: a listener that fired on a
// non-change, or missed one, would make a status line lie.

func TestOnModeChangeHearsEveryTransitionAndNothingElse(t *testing.T) {
	var got []widget.EditorMode
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithOnModeChange(func(m widget.EditorMode) {
		got = append(got, m)
	}))
	bus := record[widget.ModeChangedEvent](h)

	h.inject(key('i'))
	h.inject(typeString("abc")...) // typing in Insert is not a mode change
	h.inject(key(tui.KeyEscape))
	h.barrier(sh)

	var modes []widget.EditorMode
	h.onLoop(func() { modes = append(modes, got...) })
	if len(modes) != 2 || modes[0] != widget.ModeInsert || modes[1] != widget.ModeNormal {
		t.Fatalf("listener heard %v, want [INSERT NORMAL]", modes)
	}
	if bus.count() != len(modes) {
		t.Errorf("listener heard %d transitions and the bus %d; they must agree", len(modes), bus.count())
	}
	_ = ed
}

// TestOnChangeHearsEditsButNotSetValue.
//
// SetValue is a program replacing the buffer, not an edit. Reporting it would
// mark a freshly loaded file dirty the moment it opened.
func TestOnChangeHearsEditsButNotSetValue(t *testing.T) {
	edits := 0
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithOnChange(func() { edits++ }))

	h.onLoop(func() { ed.SetValue("loaded from disk") })
	h.barrier(sh)
	var afterLoad int
	h.onLoop(func() { afterLoad = edits })
	if afterLoad != 0 {
		t.Fatalf("SetValue reported %d edits; loading a file is not an edit", afterLoad)
	}

	h.inject(key('i'))
	h.inject(typeString("xy")...)
	h.barrier(sh)
	var afterTyping int
	h.onLoop(func() { afterTyping = edits })
	if afterTyping != 2 {
		t.Errorf("two typed characters reported %d edits, want 2", afterTyping)
	}
}

// TestOnCursorPositionChangeHearsMovesNotStillKeys: the listener fires when the cursor lands on
// another line or column — a motion, typing, or the program moving it — and not for a key that
// leaves it where it was (a refused motion at the buffer's edge).
func TestOnCursorPositionChangeHearsMovesNotStillKeys(t *testing.T) {
	moves := 0
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithOnCursorPositionChange(func() { moves++ }))
	count := func() int {
		h.barrier(sh)
		var n int
		h.onLoop(func() { n = moves })
		return n
	}

	h.onLoop(func() { ed.SetValue("one\ntwo\nthree") })
	if n := count(); n != 0 {
		t.Fatalf("SetValue on an editor already at 1:1 reported %d moves", n)
	}
	h.inject(key('j')) // down a line
	if n := count(); n != 1 {
		t.Fatalf("j reported %d moves, want 1", n)
	}
	h.inject(key('k'), key('k')) // up, then a refused up at the first line
	if n := count(); n != 2 {
		t.Fatalf("k k (the second at the top) reported %d moves in all, want 2", n)
	}
	h.onLoop(func() { ed.SetLine(2, 1) })
	if n := count(); n != 3 {
		t.Fatalf("SetLine reported %d moves in all, want 3", n)
	}
	h.onLoop(func() { ed.SetLine(2, 1) }) // where it already is
	if n := count(); n != 3 {
		t.Fatalf("SetLine to the same place reported a move (%d)", n)
	}
	h.onLoop(func() { ed.SetValue("fresh") }) // the cursor returns to 1:1
	if n := count(); n != 4 {
		t.Fatalf("SetValue moving the cursor home reported %d moves in all, want 4", n)
	}
	h.inject(key('i'))
	h.inject(typeString("ab")...)
	if n := count(); n != 6 {
		t.Fatalf("typing two characters reported %d moves in all, want 6", n)
	}
}
