package tui_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// NormalizeNewlines makes every line break \n: a \r\n pair is one break, a lone \r is one, and
// text with neither is unchanged. NewPasteEvent's text is normalized the same way.
func TestPastedLineBreaksAreNormalized(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"a\r\nb", "a\nb"},
		{"a\rb", "a\nb"},
		{"a\nb", "a\nb"},
		{"a\r\r\nb", "a\n\nb"},
		{"a\n\rb", "a\n\nb"},
		{"\r\n\r\n", "\n\n"},
		{"no breaks", "no breaks"},
		{"", ""},
	} {
		if got := tui.NormalizeNewlines(c.in); got != c.want {
			t.Errorf("NormalizeNewlines(%q) = %q, want %q", c.in, got, c.want)
		}
		if got := tui.NewPasteEvent(c.in); got.Text != c.want {
			t.Errorf("NewPasteEvent(%q).Text = %q, want %q", c.in, got.Text, c.want)
		}
	}
}
