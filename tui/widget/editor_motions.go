package widget

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/tui/internal/grapheme"
)

// isWS classifies a grapheme cluster as whitespace for the Editor's word
// motions: tabs and Unicode whitespace count, not just the literal space.
// The substrate's readline hops keep their own space-only rule.
func isWS(cluster string) bool {
	return strings.TrimSpace(cluster) == ""
}

// wordClass is a cluster's class for the word motions, as Vim's cls() and utf_class give it: 0
// for a blank, 1 for punctuation, 2 for a keyword cluster (a letter, a digit or _), a class of
// its own for each CJK script (Hiragana, Katakana, ideographs, Hangul), and 3 for an emoji (the
// Emoji property past Latin-1, as Vim's emoji_all). A move stops where the class changes, so
// foo.bar is three words. big (W B E gE) folds every non-blank into one class: WORDs end only at
// blanks.
//
// The order matters: the CJK scripts are letters too, so they are tested before the keyword
// class, or 漢字かな would be one word. The keyword class is Vim's default 'iskeyword' (letters,
// digits, _) fixed: there is no option to change it.
func wordClass(cluster string, big bool) int {
	if cluster == "" || isWS(cluster) {
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

// wordWalker steps through the buffer as Vim's inc and dec do: over each line's clusters and
// then its end, a blank position of its own (an empty line is that position alone).
type wordWalker struct {
	b     *textBuffer
	big   bool
	ln, i int
	cs    []string
}

func (c *EditorCore) walker(big bool) *wordWalker {
	return &wordWalker{b: &c.buf, big: big, ln: c.buf.ln, i: c.buf.col, cs: c.buf.lineClusters(c.buf.ln)}
}

// cls is the class at the walker; a line's end is blank.
func (w *wordWalker) cls() int {
	if w.i >= len(w.cs) {
		return 0
	}
	return wordClass(w.cs[w.i], w.big)
}

// empty reports an empty line: where w, b and ge stop.
func (w *wordWalker) empty() bool { return len(w.cs) == 0 }

// inc steps forward, false at the text's end.
func (w *wordWalker) inc() bool {
	if w.i < len(w.cs) {
		w.i++
		return true
	}
	if w.ln >= len(w.b.lines)-1 {
		return false
	}
	w.ln, w.i = w.ln+1, 0
	w.cs = w.b.lineClusters(w.ln)
	return true
}

// dec steps back, onto the previous line's end from a line's start; false at the text's start.
func (w *wordWalker) dec() bool {
	if w.i > 0 {
		w.i--
		return true
	}
	if w.ln == 0 {
		return false
	}
	w.ln--
	w.cs = w.b.lineClusters(w.ln)
	w.i = len(w.cs)
	return true
}

func (w *wordWalker) at() (int, int) { return w.ln, w.i }

// wordForward is w (W with big), as Vim's fwd_word: past the cursor's run, then past blanks to
// the next run's start, stopping on an empty line. At the text's end it rests there.
func (c *EditorCore) wordForward(big bool) (int, int) {
	w := c.walker(big)
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

// wordBack is b (B with big), as Vim's bck_word: back past blanks, stopping on an empty line, then
// to the start of the run there.
func (c *EditorCore) wordBack(big bool) (int, int) {
	w := c.walker(big)
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

// wordEnd is e (E with big), as Vim's end_word: one step, then past blanks (empty lines too), then
// to the end of the run there, which is the cursor's own when it goes on.
func (c *EditorCore) wordEnd(big bool) (int, int) {
	w := c.walker(big)
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

// wordEndBack is ge (gE with big), as Vim's bckend_word: back past the cursor's run, then past
// blanks to the end of the previous run, stopping on an empty line.
func (c *EditorCore) wordEndBack(big bool) (int, int) {
	w := c.walker(big)
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

// paraForward/paraBack: next/previous blank-line boundary.
func (c *EditorCore) paraForward(count int) int {
	ln := c.buf.ln
	for ; count > 0; count-- {
		i := ln + 1
		for i < len(c.buf.lines) && strings.TrimSpace(c.buf.lines[i]) != "" {
			i++
		}
		ln = min(i, len(c.buf.lines)-1)
	}
	return ln
}

func (c *EditorCore) paraBack(count int) int {
	ln := c.buf.ln
	for ; count > 0; count-- {
		i := ln - 1
		for i > 0 && strings.TrimSpace(c.buf.lines[i]) != "" {
			i--
		}
		ln = max(i, 0)
	}
	return ln
}

// move applies a motion action count times, extending the selection in
// visual modes.
func (c *EditorCore) move(act Action, count int) {
	c.hist.close()
	// Visual highlights derive from vAnchor + cursor; the buffer's own
	// selection anchor stays nil so insertText never sees a stray region.
	const extend = false
	apply := func(ln, col int) {
		c.buf.moveCursor(ln, col, extend)
		if c.keys.mode != ModeInsert {
			c.clampNormal()
		}
		c.reveal()
		c.markDirty()
	}
	switch act {
	case ActLeft:
		apply(c.buf.ln, c.buf.col-count)
		c.buf.desired = -1
	case ActRight:
		apply(c.buf.ln, min(c.buf.col+count, c.normalMax(c.buf.ln)))
		c.buf.desired = -1
	case ActDown, ActUp:
		delta := count
		if act == ActUp {
			delta = -count
		}
		ln, col := c.buf.verticalTarget(delta, c.measure)
		d := c.buf.desired
		apply(ln, col)
		c.buf.desired = d
	case ActPageDown, ActPageUp:
		delta := c.pageLines() * count
		if act == ActPageUp {
			delta = -delta
		}
		ln, col := c.buf.verticalTarget(delta, c.measure)
		d := c.buf.desired
		apply(ln, col)
		c.buf.desired = d
	case ActLineStart:
		c.buf.desired = -1
		apply(c.buf.ln, 0)
	case ActLineEnd:
		c.buf.desired = -1
		c.oneShot.end = c.oneShot.on // Ctrl-o $: Insert resumes past the end
		col := c.normalMax(c.buf.ln)
		if c.keys.mode == ModeInsert {
			col = len(c.buf.lineClusters(c.buf.ln))
		}
		apply(c.buf.ln, col)
		if c.keys.mode == ModeVisualBlock {
			c.buf.desired = blockToEnd // the block runs to every line's end, through j and k
		}
	case ActWordForward, ActWORDForward, ActWordBack, ActWORDBack, ActWordEnd, ActWORDEnd,
		ActWordEndBack, ActWORDEndBack:
		c.buf.desired = -1
		step, big := c.wordForward, act == ActWORDForward
		switch act {
		case ActWordBack, ActWORDBack:
			step, big = c.wordBack, act == ActWORDBack
		case ActWordEnd, ActWORDEnd:
			step, big = c.wordEnd, act == ActWORDEnd
		case ActWordEndBack, ActWORDEndBack:
			step, big = c.wordEndBack, act == ActWORDEndBack
		}
		for range count {
			ln, col := step(big)
			c.buf.moveCursor(ln, col, false)
		}
		c.clampNormal()
		c.reveal()
		c.markDirty()
	case ActParaForward:
		c.buf.desired = -1
		apply(c.paraForward(count), 0)
	case ActParaBack:
		c.buf.desired = -1
		apply(c.paraBack(count), 0)
	case ActLineDownFirst:
		c.toLine(c.buf.ln + count)
	case ActLineUpFirst:
		c.toLine(c.buf.ln - count)
	case ActFirstNonBlank:
		c.toLine(c.buf.ln)
	case ActLeftWrap:
		c.leftWrap(count)
	case ActFindCharRepeat, ActFindCharReverse:
		if f := c.keys.lastFind; f != nil {
			g := *f
			if act == ActFindCharReverse {
				g.forward = !g.forward
			}
			c.findChar(g, count, true)
		}
	case ActViewTop, ActViewMiddle, ActViewBottom:
		c.viewLine(act, count)
	}
}

// goToLine is the shared gg/G target motion: an EXPLICIT count means
// "line count" (1-based, clamped); without one, gg goes to the top and G
// to the bottom.
func (c *EditorCore) goToLine(hadCount bool, count int, bottom bool) {
	c.setJumpMark()
	c.buf.desired = -1
	ln := 0
	switch {
	case hadCount:
		ln = min(count-1, len(c.buf.lines)-1)
	case bottom:
		ln = len(c.buf.lines) - 1
	}
	c.buf.moveCursor(ln, 0, false)
	c.clampNormal()
	c.reveal()
	c.markDirty()
}
