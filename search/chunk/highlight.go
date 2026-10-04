package chunk

import (
	"strings"
	"unicode"

	"github.com/yongjohnlee80/golib/search"
)

// Highlight cuts a lexical snippet of body in Go: a window of about tokens
// words around the first word a term matches, with search.HighlightStart and
// search.HighlightEnd around every matched word in it. A word matches a term
// when, lowercased and stripped of punctuation at its edges, it equals the
// term's text, or starts with it for a prefix term: the literal terms
// search/query parses, where an engine's own snippet would apply its parser.
// With no match the window is body's first words, unmarked; tokens of zero or
// less keeps every word.
func Highlight(body string, terms []search.Term, tokens int) string {
	words := strings.Fields(body)
	if len(words) == 0 {
		return ""
	}
	matched := make([]bool, len(words))
	first := -1
	for i, w := range words {
		if matches(w, terms) {
			matched[i] = true
			if first < 0 {
				first = i
			}
		}
	}
	start, end := 0, len(words)
	if tokens > 0 && tokens < len(words) {
		if first > 0 {
			start = first - tokens/2
		}
		start = max(0, min(start, len(words)-tokens))
		end = start + tokens
	}
	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		w := words[i]
		if matched[i] {
			w = search.HighlightStart + w + search.HighlightEnd
		}
		out = append(out, w)
	}
	return strings.Join(out, " ")
}

func matches(word string, terms []search.Term) bool {
	w := strings.ToLower(strings.TrimFunc(word, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }))
	if w == "" {
		return false
	}
	for _, t := range terms {
		want := strings.ToLower(t.Text)
		if w == want || (t.Prefix && strings.HasPrefix(w, want)) {
			return true
		}
	}
	return false
}
