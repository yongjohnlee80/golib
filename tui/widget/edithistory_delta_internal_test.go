package widget

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// changedBlockWhole is changedBlock over two texts whole, as undo computed it when it kept a copy
// of the text: the reference changedBlock must give from only the lines between.
func changedBlockWhole(from, to []string) lineEdit {
	pre := 0
	for pre < len(from) && pre < len(to) && from[pre] == to[pre] {
		pre++
	}
	suf := 0
	for suf < len(from)-pre && suf < len(to)-pre && from[len(from)-1-suf] == to[len(to)-1-suf] {
		suf++
	}
	del, ins := len(from)-pre-suf, len(to)-pre-suf
	if del == ins {
		return lineEdit{}
	}
	return lineEdit{ln: pre, del: del, ins: ins}
}

// TestChangedBlockOverASpanIsTheWholeTextsBlock: for random texts of three letters' lines (so
// lines repeat, and the common first and last lines run past the replaced ones), the block from
// the span alone is the block from the two texts whole.
func TestChangedBlockOverASpanIsTheWholeTextsBlock(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	lines := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = string(rune('a' + r.IntN(3)))
		}
		return out
	}
	for i := range 50000 {
		from := lines(r.IntN(9))
		lo := r.IntN(len(from) + 1)
		hi := lo + r.IntN(len(from)-lo+1)
		span := lines(r.IntN(5))
		to := slices.Concat(from[:lo], span, from[hi:])
		if got, want := changedBlock(from, lo, hi, span), changedBlockWhole(from, to); got != want {
			t.Fatalf("case %d: from %v, [%d,%d) -> %v: block %+v, want %+v", i, from, lo, hi, span, got, want)
		}
	}
}

// recordingBuffer is a text whose primitives hand their changes to a history with one group open.
func recordingBuffer(text string) (*textBuffer, *editHistory) {
	b := newTextBuffer()
	b.setValue(text)
	h := &editHistory{enabled: true}
	h.begin(func() textSnap { return textSnap{} }, true)
	b.undoRec = h
	return &b, h
}

// TestReplayedTakesEveryPrimitiveBack: after random calls of every line primitive (the whole-text
// remove and setValue among them), the group's changes, replayed over the lines they reach, give
// back the text the group began with.
func TestReplayedTakesEveryPrimitiveBack(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	word := func() string { return []string{"a", "b", "", "cc"}[r.IntN(4)] }
	for i := range 3000 {
		start := strings.Join([]string{word(), word(), word(), word()}, "\n")
		b, h := recordingBuffer(start)
		for range 1 + r.IntN(12) {
			n := len(b.textLines)
			switch r.IntN(6) {
			case 0:
				b.setLine(r.IntN(n), word())
			case 1:
				b.insertLines(r.IntN(n+1), word(), word())
			case 2:
				lo := r.IntN(n)
				b.removeLines(lo, lo+1+r.IntN(n-lo))
			case 3:
				lo := r.IntN(n + 1)
				b.replaceLines(lo, lo+r.IntN(n-lo+1), word())
			case 4:
				b.removeLines(0, n) // the whole text: one empty line left
			case 5:
				if r.IntN(4) == 0 {
					b.setValue(word() + "\n" + word())
				}
			}
		}
		lo, hi, span := replayed(b.textLines, h.undo[0].ops)
		if got := strings.Join(slices.Concat(b.textLines[:lo], span, b.textLines[hi:]), "\n"); got != start {
			t.Fatalf("case %d: the changes took %q back to %q, want %q (ops %+v)",
				i, b.value(), got, start, h.undo[0].ops)
		}
	}
}

// TestTypingOnALineKeepsOneOldLine: a session of keystrokes on the lines an earlier change put
// there keeps that change's old lines alone, not one copy of the line per key.
func TestTypingOnALineKeepsOneOldLine(t *testing.T) {
	b, h := recordingBuffer("first\nsecond")
	b.replaceLines(1, 2, "", "") // Enter at the end of a line: the line and a new one
	for i := range 500 {
		b.setLine(2, strings.Repeat("x", i+1))
	}
	if ops := h.undo[0].ops; len(ops) != 1 {
		t.Fatalf("500 keystrokes kept %d changes, want the one that made the lines: %+v", len(ops), ops)
	}
	b.setLine(0, "elsewhere") // a line the first change did not put there
	if ops := h.undo[0].ops; len(ops) != 2 {
		t.Fatalf("a change outside the lines made was folded in: %d changes", len(ops))
	}
}

// BenchmarkUndoGroupsOnALargeText: 64 one-line edit groups on a 1M-line text. It reports the bytes
// of old lines the history keeps (old-line-B), the number a byte budget would start from, and the
// allocation per group, which a whole-text copy made O(lines).
func BenchmarkUndoGroupsOnALargeText(b *testing.B) {
	lines := make([]string, 1<<20)
	for i := range lines {
		lines[i] = "a line of an ordinary length, about forty bytes"
	}
	c := NewEditorCore(CoreModal(true), CoreInitialText(strings.Join(lines, "\n")))
	b.ResetTimer()
	for b.Loop() {
		for i := range editHistoryCap {
			c.SetLine(i*1000, 0)
			revFeed(c, "x")
		}
	}
	b.StopTimer()
	kept := 0
	for _, g := range c.hist.undo {
		for _, op := range g.ops {
			for _, l := range op.old {
				kept += len(l)
			}
		}
	}
	b.ReportMetric(float64(kept), "old-line-B")
}

// TestAnEditOutsideAGroupIsUndoneAndRedoneAsWithSnapshots: an edit made in no group (no key path
// makes one after an undo today; the contract is still the whole-text one) after an undo is
// dropped by the redo, which brings back the text before the undo whole, and an undo after the
// redo brings back the text with that edit, as whole-text snapshots would.
func TestAnEditOutsideAGroupIsUndoneAndRedoneAsWithSnapshots(t *testing.T) {
	c := NewEditorCore(CoreModal(true), CoreInitialText("one\ntwo\nthree"))
	revFeed(c, "ddjx") // two groups: "two\nhree" before any undo
	beforeUndo := c.Value()
	c.doUndo() // "two\nthree"
	c.buf.setLine(0, "stray")
	c.edited()
	withStray := c.Value()
	c.doRedo()
	if got := c.Value(); got != beforeUndo {
		t.Fatalf("the redo gave %q, want the text before the undo, %q", got, beforeUndo)
	}
	c.doUndo()
	if got := c.Value(); got != withStray {
		t.Fatalf("the undo after the redo gave %q, want the text the redo left, %q", got, withStray)
	}
}

// TestAnEditInAChangeCallbackDuringUndoIsItsOwnStep: a CoreOnChange listener that edits while an
// undo notifies it begins a group of its own, and the next undo takes that edit back. The undo's
// recording ends before listeners hear of it; ended after, the listener's edit went into the
// undo's group and the next undo left it in place. Zen's probe on golib #530, as a cell.
func TestAnEditInAChangeCallbackDuringUndoIsItsOwnStep(t *testing.T) {
	var c *EditorCore
	armed := false
	c = NewEditorCore(CoreModal(true), CoreInitialText("abc"), CoreOnChange(func() {
		if armed {
			armed = false
			if _, err := c.Insert(TextPos{}, "Z"); err != nil {
				t.Errorf("the listener's insert: %v", err)
			}
		}
	}))
	revFeed(c, "x") // "bc"
	armed = true
	revFeed(c, "u") // "abc", then the listener makes it "Zabc"
	if got := c.Value(); got != "Zabc" {
		t.Fatalf("after the undo and the listener's insert: %q, want Zabc", got)
	}
	revFeed(c, "u")
	if got := c.Value(); got != "abc" {
		t.Fatalf("an undo after the listener's insert: %q, want abc (the insert taken back)", got)
	}
}
