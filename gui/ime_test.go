package gui

import (
	"testing"

	"gioui.org/io/key"
)

func TestIMEPlainTyping(t *testing.T) {
	var s imeState
	s.edit(key.EditEvent{Range: key.Range{Start: 0, End: 0}, Text: "a"})
	if got, ok := s.take(); !ok || got != "a" {
		t.Fatalf("take = %q, %v; want \"a\"", got, ok)
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
	if got, ok := s.take(); !ok || got != "한" {
		t.Fatalf("take = %q, %v; want 한", got, ok)
	}
	if s.preedit() != "" {
		t.Fatal("a preedit survived the commit")
	}
}

func TestIMEClampsRanges(t *testing.T) {
	var s imeState
	s.edit(key.EditEvent{Range: key.Range{Start: 7, End: 9}, Text: "x"}) // past the end
	s.edit(key.EditEvent{Range: key.Range{Start: 1, End: 0}, Text: "y"}) // reversed
	if got, ok := s.take(); !ok || got != "y" {
		t.Fatalf("take = %q, %v; want \"y\" (x replaced by y)", got, ok)
	}
}
