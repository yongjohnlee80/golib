package widget

import "github.com/yongjohnlee80/golib/tui"

// VIM LINE AND CHARACTER MOTIONS — Enter + - ^ Backspace, f F t T ; ,, %, H M L, Ctrl-d Ctrl-u,
// W B E, ge gE: Normal and Visual, with counts, as Vim moves. Each lands the cursor where Vim does,
// clamped to the Normal-mode invariant (on a cluster), and a Visual selection follows the cursor.

// EditorViewport is an EditorLayout that knows which buffer lines it shows and can scroll by
// lines: H, M and L move within the lines shown, and Ctrl-d and Ctrl-u scroll the view with the
// cursor, as Vim's do. A layout without it leaves H, M and L consumed with the cursor where it is,
// and makes Ctrl-d and Ctrl-u cursor motions the view follows.
type EditorViewport interface {
	// VisibleLines is the inclusive range of buffer lines whose top edge is in the view: a line
	// scrolled partly off the top is not in it, and one cut off at the bottom is. When no line's
	// top is in the view (one line taller than the view, scrolled into), it is that line alone;
	// before the layout has a size, the cursor's line.
	VisibleLines() (first, last int)
	// ScrollLines scrolls the view by delta buffer lines, down for a positive delta, without
	// moving the cursor; it stops where the text's end reaches the view's bottom, and at the top.
	ScrollLines(delta int)
}

// charFind is an f, F, t or T: the cluster sought, its direction, and whether it stops short.
type charFind struct {
	char    string
	forward bool
	till    bool
}

// firstNonBlank is line ln's first non-blank cluster, or its last when it has none (0 when empty).
func (c *EditorCore) firstNonBlank(ln int) int {
	cs := c.buf.lineClusters(ln)
	for i, cl := range cs {
		if !isWS(cl) {
			return i
		}
	}
	return max(0, len(cs)-1)
}

// toLine moves the cursor to line ln's first non-blank, keeping no sticky column.
func (c *EditorCore) toLine(ln int) {
	ln = max(0, min(ln, len(c.buf.lines)-1))
	c.buf.desired = -1
	c.buf.moveCursor(ln, c.firstNonBlank(ln), false)
	c.clampNormal()
	c.reveal()
	c.markDirty()
}

// leftWrap is Backspace: count clusters left, from a line's start onto the previous line's last.
func (c *EditorCore) leftWrap(count int) {
	ln, col := c.buf.ln, c.buf.col
	for range count {
		switch {
		case col > 0:
			col--
		case ln > 0:
			ln--
			col = c.normalMax(ln)
		}
	}
	c.buf.desired = -1
	c.buf.moveCursor(ln, col, false)
	c.clampNormal()
	c.reveal()
	c.markDirty()
}

// findChar is f F t T and ; ,: the countth cluster f.char on the cursor's line, forward or back,
// stopping one short for till. A repeat of a till (; after t) starts past the adjacent match, so it
// moves on rather than standing still. No match: the cursor stays.
func (c *EditorCore) findChar(f charFind, count int, repeat bool) {
	cs := c.buf.lineClusters(c.buf.ln)
	col, step := c.buf.col, 1
	if !f.forward {
		step = -1
	}
	i := col + step
	if f.till && repeat {
		i += step // the match next to the cursor is where the last till stopped
	}
	found := -1
	for ; i >= 0 && i < len(cs); i += step {
		if cs[i] == f.char {
			count--
			if count == 0 {
				found = i
				break
			}
		}
	}
	if found < 0 {
		return
	}
	if f.till {
		found -= step
	}
	c.buf.desired = -1
	c.buf.moveCursor(c.buf.ln, found, false)
	c.clampNormal()
	c.reveal()
	c.markDirty()
}

// brackets pairs each bracket % matches with its partner, and says which way the partner lies.
var brackets = map[string]struct {
	partner string
	forward bool
}{
	"(": {")", true}, "[": {"]", true}, "{": {"}", true},
	")": {"(", false}, "]": {"[", false}, "}": {"{", false},
}

// matchPair is % without a count: from the bracket under the cursor, or the first one after it on
// the line, to its partner, counting nesting across lines. No bracket, or no partner: no move.
func (c *EditorCore) matchPair() {
	ln, cs := c.buf.ln, c.buf.lineClusters(c.buf.ln)
	at := -1
	for i := c.buf.col; i < len(cs); i++ {
		if _, ok := brackets[cs[i]]; ok {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	open := cs[at]
	b := brackets[open]
	depth := 0
	l, i := ln, at
	for {
		cl := c.buf.lineClusters(l)
		if i >= 0 && i < len(cl) {
			switch cl[i] {
			case open:
				depth++
			case b.partner:
				depth--
				if depth == 0 {
					c.buf.desired = -1
					c.buf.moveCursor(l, i, false)
					c.clampNormal()
					c.reveal()
					c.markDirty()
					return
				}
			}
		}
		if b.forward {
			i++
			for i >= len(c.buf.lineClusters(l)) {
				if l == len(c.buf.lines)-1 {
					return
				}
				l, i = l+1, 0
			}
		} else {
			i--
			for i < 0 {
				if l == 0 {
					return
				}
				l--
				i = len(c.buf.lineClusters(l)) - 1
			}
		}
	}
}

// percentLine is N%: line N% of the text, as Vim rounds it ((N × lines + 99) / 100), N clamped
// to 100.
func (c *EditorCore) percentLine(n int) {
	n = min(n, 100)
	c.toLine((n*len(c.buf.lines)+99)/100 - 1)
}

// viewLine is H M L: the countth line from the view's top (H), its middle (M), or the countth
// from its bottom (L). Without a viewport the key is consumed and nothing moves.
func (c *EditorCore) viewLine(act Action, count int) {
	vp, ok := c.layout.(EditorViewport)
	if !ok {
		return
	}
	first, last := vp.VisibleLines()
	switch act {
	case ActViewTop:
		c.toLine(min(first+count-1, last))
	case ActViewMiddle:
		c.toLine((first + last) / 2)
	case ActViewBottom:
		c.toLine(max(last-count+1, first))
	}
}

// halfPage is Ctrl-d and Ctrl-u: the view scrolls half a page down or up (at least a line), or
// count lines when a count was typed, and the cursor moves as far, keeping the sticky column. At
// the text's end the view stops and the cursor still moves, as in Vim.
func (c *EditorCore) halfPage(down, hadCount bool, count int) {
	n := max(c.pageLines()/2, 1)
	if hadCount {
		n = count
	}
	if !down {
		n = -n
	}
	if vp, ok := c.layout.(EditorViewport); ok {
		vp.ScrollLines(n)
	}
	ln, col := c.buf.verticalTarget(n, c.measure)
	d := c.buf.desired
	c.buf.moveCursor(ln, col, false)
	c.clampNormal()
	c.buf.desired = d
	c.reveal()
	c.markDirty()
}

// wordEndBack is ge (and gE: the Editor's words are whitespace-delimited, WORDs): back to the end
// of the previous word, crossing lines.
func (c *EditorCore) wordEndBack() (int, int) {
	ln, i := c.buf.ln, c.buf.col
	cs := c.buf.lineClusters(ln)
	for i >= 0 && i < len(cs) && !isWS(cs[i]) {
		i-- // leave the cursor's own word
	}
	for {
		for i >= 0 && isWS(cs[i]) {
			i--
		}
		if i >= 0 {
			return ln, i
		}
		if ln == 0 {
			return 0, 0
		}
		ln--
		cs = c.buf.lineClusters(ln)
		i = len(cs) - 1
	}
}

// charFindOf is the find an f, F, t or T arms, for char.
func charFindOf(act Action, char string) charFind {
	return charFind{char: char, forward: act == ActFindCharForward || act == ActTillCharForward,
		till: act == ActTillCharForward || act == ActTillCharBack}
}

// firstCluster is s's first grapheme cluster: a find's target is the typed cluster, a composed
// character included, never the key's code. A key that carries more (an IME's commit) seeks its
// first cluster.
func firstCluster(s string) string {
	for g := range tui.Graphemes(s) {
		return g
	}
	return ""
}
