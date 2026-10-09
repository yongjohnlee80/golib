package gui

import (
	"strings"
	"testing"

	"gioui.org/io/key"
)

func TestIMEPlainTyping(t *testing.T) {
	var s imeState
	s.edit(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "a"})
	if got, ok := s.take(); !ok || len(got) != 1 || got[0] != "a" {
		t.Fatalf("take = %q, %v; want [\"a\"]", got, ok)
	}
	if !s.empty() {
		t.Fatal("the buffer was not emptied after commit")
	}
	if _, ok := s.take(); ok {
		t.Fatal("an empty buffer committed text")
	}
}

// A Korean composition: the jamo assemble in place while composing, and nothing is committed
// until the input method ends the composition.
func TestIMEComposition(t *testing.T) {
	var s imeState
	s.edit(key.EditEvent{Range: key.Range{}, Text: "ㅎ"})
	s.compose(key.CompositionEvent{Start: 0, End: 1})
	if got := s.preedit(); got != "ㅎ" {
		t.Fatalf("preedit = %q; want ㅎ", got)
	}
	if _, ok := s.take(); ok {
		t.Fatal("text was committed in the middle of a composition")
	}
	s.edit(key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "하"})
	s.edit(key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: "한"})
	if got := s.preedit(); got != "한" {
		t.Fatalf("preedit = %q; want 한", got)
	}
	s.compose(key.CompositionEvent{Start: -1, End: -1})
	if got, ok := s.take(); !ok || len(got) != 1 || got[0] != "한" {
		t.Fatalf("take = %q, %v; want [한]", got, ok)
	}
	if s.preedit() != "" {
		t.Fatal("a preedit survived the commit")
	}
}

func TestIMEClampsRanges(t *testing.T) {
	var s imeState
	s.edit(key.EditEvent{Range: key.Range{Start: 7, End: 9}, Text: "x"}) // past the end
	s.edit(key.EditEvent{Range: key.Range{Start: 1, End: 0}, Text: "y"}) // reversed
	if got, ok := s.take(); !ok || len(got) != 1 || got[0] != "y" {
		t.Fatalf("take = %q, %v; want [\"y\"] (x replaced by y)", got, ok)
	}
}

// Typed text in one frame — a letter, a space, a letter, a held letter's
// repeat — delivers one piece per edit in the order typed, exactly the
// buffer. Each edit's range is the selection the previous edit left, as
// both macOS's insertText and Wayland's xkb deliver them.
func TestIMECommitsInArrivalOrder(t *testing.T) {
	var s imeState
	s.edit(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "a"})
	s.edit(key.EditEvent{Range: key.Range{Start: 1, End: 1}, Text: " "})
	s.edit(key.EditEvent{Range: key.Range{Start: 2, End: 2}, Text: "b"})
	s.edit(key.EditEvent{Range: key.Range{Start: 3, End: 3}, Text: "b"})
	got, ok := s.take()
	if !ok || strings.Join(got, "") != "a bb" {
		t.Fatalf("take = %q, %v; want the pieces of \"a bb\"", got, ok)
	}
	if len(got) != 4 {
		t.Fatalf("take = %q; want 4 pieces, one per typed key", got)
	}
}

// A front insertion followed by a deletion of the inserted bytes: the buffer
// holds the first piece's text and the delivered text matches it. The deleted
// character must not arrive; the surviving one must.
func TestIMEFrontInsertionThenDeletion(t *testing.T) {
	var s imeState
	s.edit(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "a"})
	s.edit(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "b"})
	if string(s.text) != "ba" {
		t.Fatalf("buffer = %q, want \"ba\"", string(s.text))
	}
	s.edit(key.EditEvent{Range: key.Range{Start: 0, End: 1}, Text: ""}) // delete "b"
	if string(s.text) != "a" {
		t.Fatalf("buffer = %q after the deletion, want \"a\"", string(s.text))
	}
	got, ok := s.take()
	if !ok || strings.Join(got, "") != "a" {
		t.Fatalf("delivered %q, %v; want the pieces of \"a\" — the deleted character must not arrive, the surviving one must", got, ok)
	}
}

// A replacement inside a front-inserted piece revises that piece, and a
// later piece's bytes survive it. The delivered pieces, concatenated, are
// the buffer.
func TestIMEReplacementInsideFrontInsertedPiece(t *testing.T) {
	var s imeState
	s.edit(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "a"})
	s.edit(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "b"}) // inserted before "a": buffer "ba"
	s.edit(key.EditEvent{Range: key.Range{Start: 1, End: 2}, Text: "c"}) // "a" -> "c": buffer "bc"
	if string(s.text) != "bc" {
		t.Fatalf("buffer = %q, want \"bc\"", string(s.text))
	}
	got, ok := s.take()
	if !ok || strings.Join(got, "") != "bc" {
		t.Fatalf("delivered %q, %v; want the pieces of \"bc\"", got, ok)
	}
}
