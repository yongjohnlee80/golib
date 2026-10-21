package logger

import "testing"

func TestSafeBoundsAndDeControls(t *testing.T) {
	for _, c := range []struct {
		in   string
		max  int
		want string
	}{
		{"plain", 16, "plain"},
		{"a\nforged=line", 16, "a?forged=line"},
		{"esc\x1b[31m\x7f", 16, "esc?[31m?"},
		{"abcdefgh", 4, "abcd…"},
		{"abcd", 4, "abcd"},
	} {
		if got := Safe(c.in, c.max); got != c.want {
			t.Errorf("Safe(%q, %d) = %q, want %q", c.in, c.max, got, c.want)
		}
	}
}
