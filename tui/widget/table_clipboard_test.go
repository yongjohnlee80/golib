package widget

import (
	"slices"
	"testing"
)

// TestTableTextRoundTrips: what EncodeTableText writes, DecodeTableText reads back exactly.
func TestTableTextRoundTrips(t *testing.T) {
	for _, rows := range [][][]string{
		{{"plain", "cells"}, {"two", "rows"}},
		{{`a "quote"`, "tab\there", "new\nline", " leading space"}},
		{{"ragged"}, {"rows", "of", "three"}, {""}},
		{{"trailing", "", ""}, {"", "x"}},
		{{""}},
		{{"blank\n\nline inside"}, {""}, {""}, {"after"}},
	} {
		got, err := DecodeTableText(EncodeTableText(rows))
		if err != nil || !rowsEqual(got, rows) {
			t.Fatalf("%q: read back %q (%v)", rows, got, err)
		}
	}
}

// TestTableTextReadsBlankLinesAsRows: a line no record holds is a row of one empty cell, before,
// between and after the records; one final line ending is not a row; a CRLF text reads as its LF
// form.
func TestTableTextReadsBlankLinesAsRows(t *testing.T) {
	for _, tc := range []struct {
		text string
		want [][]string
	}{
		{"a\nb\n", [][]string{{"a"}, {"b"}}},
		{"a\n\nb", [][]string{{"a"}, {""}, {"b"}}},
		{"\na", [][]string{{""}, {"a"}}},
		{"a\n\n", [][]string{{"a"}, {""}}},
		{"a\n\n\n", [][]string{{"a"}, {""}, {""}}},
		{"\n", [][]string{{""}}},
		{"\"x\n\ny\"\tz\n\nw", [][]string{{"x\n\ny", "z"}, {""}, {"w"}}},
		{"a\tb\r\nc\td\r\n", [][]string{{"a", "b"}, {"c", "d"}}},
		{"", nil},
	} {
		got, err := DecodeTableText(tc.text)
		if err != nil || !rowsEqual(got, tc.want) {
			t.Fatalf("%q: %q (%v), want %q", tc.text, got, err, tc.want)
		}
	}
}

// TestPastedRowsInOrder: a quoted single cell gives its value; a bare quote on one line pastes as
// typed; a bare quote with a tab or a second line is refused.
func TestPastedRowsInOrder(t *testing.T) {
	for _, tc := range []struct {
		text string
		want [][]string
		bad  bool
	}{
		{`" a"`, [][]string{{" a"}}, false},
		{`"x""y"`, [][]string{{`x"y`}}, false},
		{"5\" pipe", [][]string{{`5" pipe`}}, false},
		{"5\" pipe\n", [][]string{{`5" pipe`}}, false},
		{"5\" pipe\tx", nil, true},
		{"5\" pipe\nx", nil, true},
	} {
		got, err := pastedRows(tc.text)
		if (err != nil) != tc.bad || !rowsEqual(got, tc.want) {
			t.Fatalf("%q: %q (%v), want %q refused=%v", tc.text, got, err, tc.want, tc.bad)
		}
	}
}

func rowsEqual(a, b [][]string) bool {
	return slices.EqualFunc(a, b, func(x, y []string) bool { return slices.Equal(x, y) })
}
