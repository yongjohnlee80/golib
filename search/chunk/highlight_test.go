package chunk

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/search"
)

// h shows the marks as [ and ].
func h(s string) string {
	return strings.NewReplacer(search.HighlightStart, "[", search.HighlightEnd, "]").Replace(s)
}

func TestHighlight(t *testing.T) {
	t.Parallel()
	body := "one two three four five alpha seven eight nine ten Alpha, twelve"
	for _, tc := range []struct {
		name   string
		terms  []search.Term
		tokens int
		want   string
	}{
		{"a window around the first match", []search.Term{{Text: "alpha"}}, 4, "four five [alpha] seven"},
		{"every word", []search.Term{{Text: "alpha"}}, 0, "one two three four five [alpha] seven eight nine ten [Alpha,] twelve"},
		{"a window at the end", []search.Term{{Text: "twelve"}}, 3, "ten Alpha, [twelve]"},
		{"a window at the start", []search.Term{{Text: "one"}}, 3, "[one] two three"},
		{"a prefix term", []search.Term{{Text: "thr", Prefix: true}}, 2, "two [three]"},
		{"not a prefix without the flag", []search.Term{{Text: "thr"}}, 2, "one two"},
		{"no match", []search.Term{{Text: "zeta"}}, 3, "one two three"},
		{"more tokens than words", []search.Term{{Text: "ten"}}, 50, "one two three four five alpha seven eight nine [ten] Alpha, twelve"},
	} {
		if got := h(Highlight(body, tc.terms, tc.tokens)); got != tc.want {
			t.Errorf("%s:\n got: %s\nwant: %s", tc.name, got, tc.want)
		}
	}
	if Highlight("   ", []search.Term{{Text: "a"}}, 3) != "" {
		t.Error("an empty body")
	}
	if got := h(Highlight("--- a", []search.Term{{Text: ""}}, 0)); got != "--- a" {
		t.Errorf("punctuation alone or an empty term matched: %s", got)
	}
}
