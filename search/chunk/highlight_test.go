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
		{"every word", []search.Term{{Text: "alpha"}}, 0, "one two three four five [alpha] seven eight nine ten [Alpha], twelve"},
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

func TestHighlightWordEdges(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		body, term, want string
		prefix           bool
	}{
		{"a word.", "word", "a [word].", false},
		{"“fox!” and (grapes).", "fox", "“[fox]!” and (grapes).", false},
		{"“fox!” and (grapes).", "grapes", "“fox!” and ([grapes]).", false},
		{"don't!", "don't", "[don't]!", false},
		{"你好。", "你好", "[你好]。", false},
		{"(42),", "42", "([42]),", false},
		{"(grapes).", "gra", "([grapes]).", true},
		{"plain text.", "missing", "plain text.", false},
		{"!!!", "", "!!!", false},
		{"cafe\u0301!", "cafe\u0301", "[cafe\u0301]!", false},
		{"cafe\u0301!", "caf", "[cafe\u0301]!", true},
	} {
		if got := h(Highlight(c.body, []search.Term{{Text: c.term, Prefix: c.prefix}}, 0)); got != c.want {
			t.Errorf("%q, term %q: got %q, want %q", c.body, c.term, got, c.want)
		}
	}
}
