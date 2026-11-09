package gui

import (
	"strings"
	"testing"
)

// An edit across two pieces merges into the first and keeps the second's tail, at the tail's new
// position: a later edit of that tail must find it there. Typed "ab", "cd", "ef", then [1, 3)
// replaced by "XYZ" (buffer "aXYZdef"), then the "d" at 4 replaced by "Q" (buffer "aXYZQef"): the
// delivered pieces, concatenated, are the buffer, with the replaced "d" gone.
func TestIMEAnEditAcrossTwoPiecesKeepsTheTailWhereItIs(t *testing.T) {
	var s imeState
	s.edit(TextEdit{Start: 0, End: 0, Text: "ab"})
	s.edit(TextEdit{Start: 2, End: 2, Text: "cd"})
	s.edit(TextEdit{Start: 4, End: 4, Text: "ef"})
	s.edit(TextEdit{Start: 1, End: 3, Text: "XYZ"})
	if string(s.text) != "aXYZdef" {
		t.Fatalf("buffer = %q, want \"aXYZdef\"", string(s.text))
	}
	s.edit(TextEdit{Start: 4, End: 5, Text: "Q"})
	if string(s.text) != "aXYZQef" {
		t.Fatalf("buffer = %q, want \"aXYZQef\"", string(s.text))
	}
	got, ok := s.take()
	if !ok || strings.Join(got, "") != "aXYZQef" {
		t.Fatalf("delivered %q, %v; want the pieces of \"aXYZQef\"", got, ok)
	}
}

// The same, with the edit shrinking the text: [1, 3) of "abcdef" replaced by nothing leaves "adef",
// and the "d" (now at 1) replaced by "Q" gives "aQef".
func TestIMEADeletionAcrossTwoPiecesKeepsTheTailWhereItIs(t *testing.T) {
	var s imeState
	s.edit(TextEdit{Start: 0, End: 0, Text: "ab"})
	s.edit(TextEdit{Start: 2, End: 2, Text: "cd"})
	s.edit(TextEdit{Start: 4, End: 4, Text: "ef"})
	s.edit(TextEdit{Start: 1, End: 3, Text: ""})
	s.edit(TextEdit{Start: 1, End: 2, Text: "Q"})
	if string(s.text) != "aQef" {
		t.Fatalf("buffer = %q, want \"aQef\"", string(s.text))
	}
	got, ok := s.take()
	if !ok || strings.Join(got, "") != "aQef" {
		t.Fatalf("delivered %q, %v; want the pieces of \"aQef\"", got, ok)
	}
}
