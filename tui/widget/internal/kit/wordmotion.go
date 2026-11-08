package kit

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/tui/internal/grapheme"
)

// WORD MOTIONS — Vim's w, b, e and ge over lines of grapheme clusters (WordText), shared by the
// editor's buffer (bufferWords) and a Terminal's lines in Normal mode (terminalWords), so the
// motions mean the same in both.

// IsWS classifies a grapheme cluster as whitespace for the Editor's word
// motions: tabs and Unicode whitespace count, not just the literal space.
// The substrate's readline hops keep their own space-only rule.
func IsWS(cluster string) bool {
	return strings.TrimSpace(cluster) == ""
}

// WordClass is a cluster's class for the word motions, as Vim's cls() and utf_class give it: 0
// for a blank, 1 for punctuation, 2 for a keyword cluster (a letter, a digit or _), a class of
// its own for each CJK script (Hiragana, Katakana, ideographs, Hangul), and 3 for an emoji (the
// Emoji property past Latin-1, as Vim's emoji_all). A move stops where the class changes, so
// foo.bar is three words. big (W B E gE) folds every non-blank into one class: WORDs end only at
// blanks.
//
// The order matters: the CJK scripts are letters too, so they are tested before the keyword
// class, or 漢字かな would be one word. The keyword class is Vim's default 'iskeyword' (letters,
// digits, _) fixed: there is no option to change it.
func WordClass(cluster string, big bool) int {
	if cluster == "" || IsWS(cluster) {
		return 0
	}
	if big {
		return 1
	}
	r, _ := utf8.DecodeRuneInString(cluster)
	switch {
	case r > 0xFF && grapheme.IsEmoji(r):
		return 3
	case r >= 0x3001 && r <= 0x3020: // CJK punctuation, the iteration marks among it
		return 1
	case r >= 0x3040 && r <= 0x309F:
		return 0x3040 // Hiragana
	case r >= 0x30A0 && r <= 0x30FF:
		return 0x30A0 // Katakana, its prolonged sound mark included
	case r >= 0x3300 && r <= 0x9FFF, r >= 0xF900 && r <= 0xFAFF, r >= 0x20000 && r <= 0x2FA1F:
		return 0x4E00 // CJK ideographs
	case r >= 0xAC00 && r <= 0xD7A3:
		return 0xAC00 // Hangul syllables
	case r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
		return 2
	}
	return 1
}

// WordText is the text the word motions walk: lines of grapheme clusters. The editor's buffer is
// one (bufferWords); a Terminal's lines are another (terminalWords), so w, b, e and ge mean the
// same in both.
type WordText interface {
	LineCount() int
	LineClusters(i int) []string
}

// WordWalker steps through a WordText as Vim's inc and dec do: over each line's clusters and then
// its end, a blank position of its own (an empty line is that position alone).
type WordWalker struct {
	src   WordText
	big   bool
	ln, i int
	cs    []string
}

// NewWordWalker is a walker at (ln, col) of src, by WORD with big.
func NewWordWalker(src WordText, ln, col int, big bool) *WordWalker {
	return &WordWalker{src: src, big: big, ln: ln, i: col, cs: src.LineClusters(ln)}
}

// cls is the class at the walker; a line's end is blank.
func (w *WordWalker) cls() int {
	if w.i >= len(w.cs) {
		return 0
	}
	return WordClass(w.cs[w.i], w.big)
}

// empty reports an empty line: where w, b and ge stop.
func (w *WordWalker) empty() bool { return len(w.cs) == 0 }

// inc steps forward, false at the text's end.
func (w *WordWalker) inc() bool {
	if w.i < len(w.cs) {
		w.i++
		return true
	}
	if w.ln >= w.src.LineCount()-1 {
		return false
	}
	w.ln, w.i = w.ln+1, 0
	w.cs = w.src.LineClusters(w.ln)
	return true
}

// dec steps back, onto the previous line's end from a line's start; false at the text's start.
func (w *WordWalker) dec() bool {
	if w.i > 0 {
		w.i--
		return true
	}
	if w.ln == 0 {
		return false
	}
	w.ln--
	w.cs = w.src.LineClusters(w.ln)
	w.i = len(w.cs)
	return true
}

func (w *WordWalker) at() (int, int) { return w.ln, w.i }

// WordForwardFrom is wordForward from where w stands.
func WordForwardFrom(w *WordWalker) (int, int) {
	start := w.cls()
	if !w.inc() {
		return w.at()
	}
	if start != 0 {
		for w.cls() == start {
			if !w.inc() {
				return w.at()
			}
		}
	}
	for w.cls() == 0 && !w.empty() {
		if !w.inc() {
			return w.at()
		}
	}
	return w.at()
}

// WordBackFrom is wordBack from where w stands.
func WordBackFrom(w *WordWalker) (int, int) {
	if !w.dec() {
		return w.at()
	}
	for w.cls() == 0 {
		if w.empty() || !w.dec() {
			return w.at()
		}
	}
	for cl := w.cls(); w.cls() == cl; {
		if !w.dec() {
			return w.at()
		}
	}
	w.inc() // overshot: forward one
	return w.at()
}

// WordEndFrom is wordEnd from where w stands.
func WordEndFrom(w *WordWalker) (int, int) {
	if !w.inc() {
		return w.at()
	}
	for w.cls() == 0 {
		if !w.inc() {
			return w.at()
		}
	}
	for cl := w.cls(); w.cls() == cl; {
		if !w.inc() {
			return w.at()
		}
	}
	w.dec() // overshot: back one
	return w.at()
}

// WordEndBackFrom is wordEndBack from where w stands.
func WordEndBackFrom(w *WordWalker) (int, int) {
	start := w.cls()
	if !w.dec() {
		return w.at()
	}
	if start != 0 {
		for w.cls() == start {
			if !w.dec() {
				return w.at()
			}
		}
	}
	for w.cls() == 0 && !w.empty() {
		if !w.dec() {
			return w.at()
		}
	}
	return w.at()
}
