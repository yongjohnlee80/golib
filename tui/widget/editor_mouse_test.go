package widget_test

// Editor pointer behaviour (ADR-0010 §2.3, criteria 11-17).
//
// A press is a COMMAND BOUNDARY, not just a cursor move: Editor carries modal
// state (counts, pending operators, an insert escape-chord rune, a visual anchor,
// an open undo group) and a click has to resolve every one of them deliberately.
// The rule that carries the most weight is the pending RUNE: it is input the user
// physically typed, and accepted ADR-0008 binds every non-chord input to settle
// it first, so a click settles rather than discards.

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// Criterion 11 — a press in Normal mode with a pending count AND pending
// operator discards both, moves the caret, and modifies no text.
func TestEditorPressDiscardsPendingOperator(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6)
	h.onLoop(func() { ed.SetValue("alpha\nbravo\ncharlie") })
	h.barrier(sh)

	before, _, _, _ := edState(h, ed)

	// Arm `2d`, click, and press the COMPLETING `d` — all in ONE batch. Every
	// step has to be in the same batch: the loop going idle at a barrier expires
	// the pending prefix on its own, so a barrier anywhere in this sequence makes
	// the test pass while proving nothing. With the click clearing the prefix this
	// trailing `d` merely arms a fresh one; with the prefix surviving, `2dd` fires
	// and deletes two lines.
	h.inject(key('2'), key('d'), click(2, 1), key('d')) // click: row 1, column 2
	h.barrier(sh)

	val, mode, ln, col := edState(h, ed)
	if val != before {
		t.Errorf("a press completed the pending operator: text changed\n before: %q\n after:  %q",
			before, val)
	}
	if mode != widget.ModeNormal {
		t.Errorf("mode = %v, want Normal", mode)
	}
	if ln != 1 || col != 2 {
		t.Errorf("caret = (%d,%d), want (1,2)", ln, col)
	}

}

// Criterion 12 — a press mid insert-chord SETTLES the held rune at the OLD caret.
// Discarding it would delete a character the user typed and would silently
// reverse accepted ADR-0008.
func TestEditorPressSettlesPendingChordRune(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6)
	h.inject(key('i'))
	h.inject(typeString("abc")...)
	h.barrier(sh)

	// The held rune and the click must be injected in ONE batch. With a barrier
	// between them the loop goes idle, the chord tick fires, and the rune is
	// already settled before the click arrives — the test would pass while
	// exercising nothing. (Same reason TestEditorInsertAndChordEscape sends "jk"
	// as one batch.)
	h.inject(key('j'), click(0, 0))
	h.barrier(sh)

	val, mode, _, _ := edState(h, ed)
	if !strings.Contains(val, "abcj") {
		t.Errorf("the pending chord rune was LOST: value = %q, want it settled as \"abcj\" "+
			"at the caret where it was typed (ADR-0008)", val)
	}
	if mode != widget.ModeInsert {
		t.Errorf("mode = %v, want Insert retained across the click", mode)
	}
}

// Criterion 13 — a press closes the Insert undo group, so text typed before and
// after the click undo separately. A click is a deliberate discontinuity.
func TestEditorPressClosesUndoGroup(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6)
	h.inject(key('i'))
	h.inject(typeString("first")...)
	h.barrier(sh)

	h.inject(click(0, 0)) // boundary
	h.barrier(sh)
	h.inject(typeString("X")...)
	h.barrier(sh)

	withBoth, _, _, _ := edState(h, ed)
	h.inject(key(tui.KeyEscape), key('u')) // one undo
	h.barrier(sh)
	afterUndo, _, _, _ := edState(h, ed)

	if afterUndo == withBoth {
		t.Fatalf("undo did nothing (value %q)", withBoth)
	}
	if !strings.Contains(afterUndo, "first") {
		t.Errorf("one undo removed text from BEFORE the click too: %q — the press must "+
			"close the group so the two edits undo separately", afterUndo)
	}
}

// Criterion 14 — a press leaves Visual and Visual-line, clearing the anchor.
func TestEditorPressExitsVisual(t *testing.T) {
	for _, tc := range []struct {
		name  string
		enter rune
	}{{"visual", 'v'}, {"visual-line", 'V'}} {
		t.Run(tc.name, func(t *testing.T) {
			h, ed, sh := focusedEditor(t, 30, 6)
			h.onLoop(func() { ed.SetValue("alpha\nbravo") })
			h.barrier(sh)

			h.inject(key(tc.enter), key('l')) // select something
			h.barrier(sh)

			h.inject(click(1, 1))
			h.barrier(sh)

			_, mode, _, _ := edState(h, ed)
			if mode != widget.ModeNormal {
				t.Errorf("mode = %v after a press, want Normal (the press must exit %s)",
					mode, tc.name)
			}
			// The anchor must be cleared, not retained: a following motion must not
			// extend a selection the user believes they dismissed.
			before, _, _, _ := edState(h, ed)
			h.inject(key('l'), key('d'))
			h.barrier(sh)
			if after, _, _, _ := edState(h, ed); after != before {
				t.Errorf("a stale visual anchor survived the press: `d` after the click "+
					"deleted a selection\n before: %q\n after:  %q", before, after)
			}
		})
	}
}

// Criterion 15 — caret placement across a WIDE grapheme resolves to the cell the
// grapheme STARTS at, and is correct at a non-zero vertical scroll.
func TestEditorPressWideGraphemeAndScroll(t *testing.T) {
	h, ed, sh := focusedEditor(t, 20, 3) // 3 visible rows
	// "a" then a double-width CJK cluster then "b": cells a=1, 漢=2, b=1.
	h.onLoop(func() { ed.SetValue("a漢b\nsecond\nthird\nfourth\nfifth") })
	h.barrier(sh)

	// Click the SECOND cell of the wide grapheme (x=2): it must resolve to the
	// grapheme itself (col 1), not to "b".
	h.inject(click(2, 0))
	h.barrier(sh)
	if _, _, ln, col := edState(h, ed); ln != 0 || col != 1 {
		t.Errorf("click on the trailing half of a wide grapheme -> (%d,%d), want (0,1)", ln, col)
	}

	// Scroll down two logical lines with the wheel, then click the top row: it
	// must be line 2, proving the click inverts `top` rather than assuming 0.
	h.inject(
		tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 1, Y: 1},
		tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 1, Y: 1},
	)
	h.barrier(sh)
	h.inject(click(0, 0))
	h.barrier(sh)
	if _, _, ln, _ := edState(h, ed); ln != 2 {
		t.Errorf("after scrolling 2 lines, a click on the top row -> line %d, want 2", ln)
	}
}

// Criterion 16 — clamping: past end-of-line lands on the last column, and below
// the last line lands on the last buffer line.
func TestEditorPressClamps(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 8)
	h.onLoop(func() { ed.SetValue("ab\nlonger line") })
	h.barrier(sh)

	h.inject(click(25, 0)) // far past the end of "ab"
	h.barrier(sh)
	if _, _, ln, col := edState(h, ed); ln != 0 || col != 1 {
		t.Errorf("click past EOL -> (%d,%d), want (0,1) — the last column of \"ab\"", ln, col)
	}

	h.inject(click(0, 6)) // below the last line
	h.barrier(sh)
	if _, _, ln, _ := edState(h, ed); ln != 1 {
		t.Errorf("click below the last line -> line %d, want 1 (the last buffer line)", ln)
	}
}

// Criterion 17 — the wheel scrolls and never moves the caret.
func TestEditorWheelScrollsWithoutMovingCaret(t *testing.T) {
	h, ed, sh := focusedEditor(t, 20, 3)
	h.onLoop(func() { ed.SetValue("one\ntwo\nthree\nfour\nfive") })
	h.barrier(sh)

	_, _, ln0, col0 := edState(h, ed)

	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 1, Y: 1})
	h.barrier(sh)

	_, _, ln1, col1 := edState(h, ed)
	if ln1 != ln0 || col1 != col0 {
		t.Errorf("the wheel moved the caret (%d,%d) -> (%d,%d); it must only scroll",
			ln0, col0, ln1, col1)
	}

	// It really scrolled: a click on the top row is now a later line.
	h.inject(click(0, 0))
	h.barrier(sh)
	if _, _, ln, _ := edState(h, ed); ln == 0 {
		t.Error("the wheel did not scroll: the top row is still line 0")
	}
}
