package widget

import (
	"strings"
	"unicode/utf8"
)

// COMPLETION — a TextInput that offers what the word before its cursor may become. Tab takes it
// up as the command line's Tab does: one choice is put in whole; several put in what they all
// begin with when that is more than was typed, and otherwise each Tab puts in the next (Shift+Tab
// the previous), until an edit or a move of the cursor. While the field has the keys and its cursor is at the end,
// the choices are drawn after the text, muted: the rest of the first one, then the others. With
// nothing to offer, Tab is not taken and moves the focus as before.

// TextCompleter offers completions for text with the cursor at byte cursor: start is the byte
// where the word being completed begins (at most cursor), and each item replaces
// text[start:cursor]. No items is nothing to offer. It runs on the loop on each Tab and each paint
// while the field has the keys, so it must be cheap.
type TextCompleter func(text string, cursor int) (start int, items []string)

// WithCompleter installs a completer (SetCompleter).
func WithCompleter(fn TextCompleter) TextInputOption {
	return func(t *TextInput) { t.completer = fn }
}

// SetCompleter installs fn as the field's completer; nil removes it, and Tab is never taken.
func (t *TextInput) SetCompleter(fn TextCompleter) {
	t.completer, t.walk = fn, nil
	t.MarkDirty()
}

// Completions are the choices the field offers now, and the one Tab put in (-1 before a Tab
// chose one): what a view lists, as the field itself draws them after its text.
func (t *TextInput) Completions() (items []string, chosen int) {
	if t.walk != nil {
		return append([]string(nil), t.walk.items...), t.walk.at
	}
	if t.completer == nil {
		return nil, -1
	}
	_, items = t.offered()
	return items, -1
}

// textWalk is Tab's way through the choices: the word they replace begins at cluster start.
type textWalk struct {
	start int
	items []string
	at    int // the item in the field; -1 before the first
}

// offered asks the completer about the value and the cursor: the cluster where the word begins,
// and the choices.
func (t *TextInput) offered() (start int, items []string) {
	value := t.Value()
	at := len(strings.Join(t.clusters()[:t.cursor()], ""))
	b, items := t.completer(value, at)
	if len(items) == 0 || b < 0 || b > at {
		return 0, nil
	}
	return len(clusters(value[:b])), items
}

// walkStep is Tab's next place in a walk through n candidates from at (-1 before the first):
// forward from the first, or with back (Shift+Tab) from the last, wrapping round. CommandLine's
// completion and TextInput's walk by it.
func walkStep(at, n int, back bool) int {
	switch {
	case at < 0 && back:
		return n - 1
	case at < 0:
		return 0
	case back:
		return (at + n - 1) % n
	}
	return (at + 1) % n
}

// complete is Tab (back: Shift+Tab). It reports whether Tab was taken.
func (t *TextInput) complete(back bool) bool {
	if t.walk == nil {
		if back {
			return false
		}
		start, items := t.offered()
		if len(items) == 0 {
			return false
		}
		typed := strings.Join(t.clusters()[start:t.cursor()], "")
		if len(items) == 1 {
			t.replaceWord(start, items[0])
			return true
		}
		t.walk = &textWalk{start: start, items: items, at: -1}
		if p := commonPrefix(items, typed); len(p) > len(typed) {
			t.replaceWord(start, p)
			return true
		}
	}
	t.walk.at = walkStep(t.walk.at, len(t.walk.items), back)
	t.replaceWord(t.walk.start, t.walk.items[t.walk.at])
	return true
}

// replaceWord puts s in place of the clusters from start to the cursor, as one edit, keeping
// Tab's walk.
func (t *TextInput) replaceWord(start int, s string) {
	walk := t.walk
	t.buf.anchor = &taPos{col: start} // the word typed, whatever was selected
	t.buf.insertText(sanitizeLine(s))
	t.edited()
	t.walk = walk
}

// hint is what the field draws after its text while it has the keys: the rest of the first choice
// when it begins with the word typed, then the other choices; during Tab's walk, the others.
func (t *TextInput) hint() string {
	if t.completer == nil || !t.focused() || t.cursor() != t.length() || t.mask != 0 {
		return ""
	}
	if _, _, sel := t.selection(); sel {
		return ""
	}
	if t.walk != nil {
		var rest []string
		for i, it := range t.walk.items {
			if i != t.walk.at {
				rest = append(rest, it)
			}
		}
		if len(rest) == 0 {
			return ""
		}
		return "  " + strings.Join(rest, "  ")
	}
	start, items := t.offered()
	if len(items) == 0 {
		return ""
	}
	typed := strings.Join(t.clusters()[start:t.cursor()], "")
	head, rest := "", items
	if strings.HasPrefix(items[0], typed) {
		head, rest = items[0][len(typed):], items[1:]
	}
	if len(rest) > 0 {
		head += "  " + strings.Join(rest, "  ")
	}
	return head
}

// WordCompleter completes the word before the cursor from items: those the word begins,
// case-insensitively, a leading double quote of an item ignored, so `tot` offers `"total sum"`.
// A word runs back to a space, a parenthesis, a comma, an operator or a single quote; one opened
// with a double quote and not closed runs back to it. An empty word offers nothing.
func WordCompleter(items func() []string) TextCompleter {
	return func(text string, cursor int) (int, []string) {
		start, word := wordBefore(text, cursor)
		if word == "" {
			return 0, nil
		}
		return start, matching(items(), word)
	}
}

// wordBefore is the word ending at cursor and where it begins.
func wordBefore(text string, cursor int) (start int, word string) {
	cursor = min(max(cursor, 0), len(text))
	if open := openQuote(text[:cursor]); open >= 0 {
		return open, text[open:cursor]
	}
	start = cursor
	for start > 0 && !completeStop(text[start-1]) {
		start--
	}
	return start, text[start:cursor]
}

// openQuote is where an unclosed double quote opens in s, or -1; a quote inside a single-quoted
// string does not count, nor one doubled inside a quoted name.
func openQuote(s string) int {
	open, single := -1, false
	for i := 0; i < len(s); i++ {
		switch {
		case open >= 0 && s[i] == '"' && i+1 < len(s) && s[i+1] == '"':
			i++ // a doubled quote inside the name
		case open >= 0 && s[i] == '"':
			open = -1
		case open < 0 && s[i] == '\'':
			single = !single
		case open < 0 && !single && s[i] == '"':
			open = i
		}
	}
	return open
}

func completeStop(b byte) bool {
	return orderSpace(b) || strings.IndexByte(`()=!<>'",`, b) >= 0
}

// matching is the items word begins, case-insensitively and past an item's opening quote, in
// their order, each once.
func matching(items []string, word string) []string {
	var out []string
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it] {
			continue
		}
		if hasPrefixFold(it, word) || strings.HasPrefix(it, `"`) && hasPrefixFold(it[1:], strings.TrimPrefix(word, `"`)) {
			seen[it] = true
			out = append(out, it)
		}
	}
	return out
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// commonPrefix is the longest prefix every candidate's name shares, at least typed.
func commonPrefix(cs []string, typed string) string {
	p := cs[0]
	for _, c := range cs[1:] {
		for !strings.HasPrefix(c, p) {
			_, size := utf8.DecodeLastRuneInString(p) // a script's arguments need not be ASCII
			p = p[:len(p)-size]
		}
	}
	if len(p) < len(typed) {
		return typed
	}
	return p
}
