package widget

import (
	"strings"
)

// VIM OPERATORS — d, c and y over a motion or a text object, as Vim's operator-pending mode: the
// motion's target is COMPUTED (never moved to, never clamped to Normal mode's cluster), its kind
// (exclusive, inclusive, linewise) makes the range, and the range is made half-open before it is
// judged empty, so a one-cluster inclusive range or a one-line linewise one is still an edit.

// motionKind is how an operator reads a motion's range.
type motionKind int

const (
	exclusive motionKind = iota // up to the target, not including it
	inclusive                   // up to and including the target
	linewise                    // every line from the start's to the target's
)

// opTarget is where motion act lands from the cursor, for an operator, and the kind of its range.
// count is the operator's count times the motion's; hadCount says one was typed at all, for the
// motions that read a count as a number rather than a repeat (G, gg, N%). ok is false when the
// motion fails (an f with no match, a j off the last line), and nothing is to change.
func (c *EditorCore) opTarget(act Action, count int, hadCount bool) (to taPos, kind motionKind, ok bool) {
	b := &c.buf
	ln, col := b.ln, b.col
	last := len(b.lines) - 1
	lineLen := func(l int) int { return len(b.lineClusters(l)) }
	switch act {
	case ActLeft:
		return taPos{ln, max(0, col-count)}, exclusive, col > 0
	case ActRight:
		n := lineLen(ln)
		return taPos{ln, min(col+count, n)}, exclusive, col < n
	case ActLeftWrap:
		l, cc := c.leftWrapPos(count)
		return taPos{l, cc}, exclusive, l != ln || cc != col
	case ActDown, ActLineDownFirst, ActUp, ActLineUpFirst: // rows: a fold is one
		to, moved := c.rowsFrom(ln, count, act == ActDown || act == ActLineDownFirst)
		return taPos{to, 0}, linewise, moved
	case ActPageDown, ActPageUp, ActHalfPageDown, ActHalfPageUp:
		n := c.pageLines() * count
		if act == ActHalfPageDown || act == ActHalfPageUp {
			n = max(c.pageLines()/2, 1)
			if hadCount {
				n = count
			}
		}
		to, moved := c.rowsFrom(ln, n, act == ActPageDown || act == ActHalfPageDown)
		return taPos{to, 0}, linewise, moved
	case ActLineStart:
		return taPos{ln, 0}, exclusive, true
	case ActFirstNonBlank: // ^ ignores a count
		return taPos{ln, c.firstNonBlank(ln)}, exclusive, true
	case ActLineEnd: // N$: N-1 lines down, or as far as there are, to the end, inclusive (an empty
		// line's end is its start); from the last line a count past one has nowhere to go
		l := min(ln+count-1, last)
		return taPos{l, max(lineLen(l)-1, 0)}, inclusive, ln < last || count == 1
	case ActGoBottom: // [count]G: line count, else the last
		if hadCount {
			return taPos{min(count-1, last), 0}, linewise, true
		}
		return taPos{last, 0}, linewise, true
	case ActParaForward:
		p := c.paraForward(count)
		if p == last && strings.TrimSpace(b.lines[p]) != "" {
			return taPos{p, lineLen(p)}, exclusive, true // the last paragraph: to the text's end
		}
		return taPos{p, 0}, exclusive, p != ln
	case ActParaBack:
		return taPos{c.paraBack(count), 0}, exclusive, ln > 0 || col > 0
	case ActSentenceForward, ActSentenceBack:
		p := c.sentenceTarget(act == ActSentenceForward, count)
		return p, exclusive, p != taPos{ln, col}
	case ActWordForward, ActWORDForward:
		return c.wordsForwardTarget(act == ActWORDForward, count)
	case ActWordBack, ActWORDBack:
		p := c.repeatWalk(c.wordBack, act == ActWORDBack, count)
		return p, exclusive, p != taPos{ln, col}
	case ActWordEnd, ActWORDEnd:
		p := c.repeatWalk(c.wordEnd, act == ActWORDEnd, count)
		return p, inclusive, true
	case ActWordEndBack, ActWORDEndBack:
		p := c.repeatWalk(c.wordEndBack, act == ActWORDEndBack, count)
		return p, inclusive, p != taPos{ln, col}
	case ActFindCharRepeat, ActFindCharReverse:
		f := c.keys.lastFind
		if f == nil {
			return taPos{}, 0, false
		}
		g := *f
		if act == ActFindCharReverse {
			g.forward = !g.forward
		}
		return c.findTarget(g, count, true)
	case ActMatchPair:
		if hadCount { // N%
			return taPos{max(0, min((count*len(b.lines)+99)/100-1, last)), 0}, linewise, true
		}
		l, cc, found := c.matchPairPos()
		return taPos{l, cc}, inclusive, found
	case ActViewTop, ActViewMiddle, ActViewBottom:
		l, found := c.viewLinePos(act, count)
		return taPos{l, 0}, linewise, found
	}
	return taPos{}, 0, false
}

// gotoLineTarget is gg's target for an operator: line count when one was typed, else the first.
func (c *EditorCore) gotoLineTarget(count int, hadCount bool) taPos {
	if hadCount {
		return taPos{min(count-1, len(c.buf.lines)-1), 0}
	}
	return taPos{}
}

// findTarget is an f F t T (or ; ,) for an operator: f and t are inclusive, F and T exclusive.
func (c *EditorCore) findTarget(f charFind, count int, repeat bool) (taPos, motionKind, bool) {
	col, ok := c.findCharPos(f, count, repeat)
	kind := exclusive
	if f.forward {
		kind = inclusive
	}
	return taPos{c.buf.ln, col}, kind, ok
}

// repeatWalk runs one of the class walks count times from the cursor, without moving it, and
// returns where it lands, a line's end (one past its last cluster) included.
func (c *EditorCore) repeatWalk(step func(big bool) (int, int), big bool, count int) taPos {
	b := &c.buf
	ln, col := b.ln, b.col
	defer func() { b.ln, b.col = ln, col }()
	for range count {
		b.ln, b.col = step(big)
	}
	return taPos{b.ln, b.col}
}

// wordsForwardTarget is w (W) for an operator, with Vim's special case: when the last word moved
// over ends its line, the operated text ends there, not at the next line's first word.
func (c *EditorCore) wordsForwardTarget(big bool, count int) (taPos, motionKind, bool) {
	b := &c.buf
	ln, col := b.ln, b.col
	defer func() { b.ln, b.col = ln, col }()
	for i := range count {
		fromLn := b.ln
		b.ln, b.col = c.wordForward(big)
		if i == count-1 && b.ln > fromLn {
			end := taPos{fromLn, len(b.lineClusters(fromLn))}
			return end, exclusive, end != taPos{ln, col}
		}
	}
	return taPos{b.ln, b.col}, exclusive, taPos{b.ln, b.col} != taPos{ln, col}
}

// opRange is an operator's text: [lo, hiEx) by cluster, or the lines lo.ln..hiEx.ln when linewise.
type opRange struct {
	lo, hiEx taPos
	lines    bool
}

// rangeOf makes the half-open range between from and to of a motion of kind k, applying Vim's
// exclusive-linewise rule: an exclusive range ending in column 0 of a later line ends at the end
// of the line before it instead, and is whole lines when it also starts at or before its line's
// first non-blank. empty reports a range with nothing in it: the only one that is a no-op.
func (c *EditorCore) rangeOf(from, to taPos, k motionKind) (r opRange, empty bool) {
	lo, hi := from, to
	if hi.ln < lo.ln || (hi.ln == lo.ln && hi.col < lo.col) {
		lo, hi = hi, lo
	}
	switch k {
	case linewise:
		return opRange{lo: taPos{lo.ln, 0}, hiEx: taPos{hi.ln, 0}, lines: true}, false
	case inclusive:
		n := len(c.buf.lineClusters(hi.ln))
		hiEx := taPos{hi.ln, min(hi.col+1, n)}
		return opRange{lo: lo, hiEx: hiEx}, hiEx == lo
	}
	if hi.col == 0 && hi.ln > lo.ln {
		prev := hi.ln - 1
		if lo.col <= c.firstNonBlank(lo.ln) {
			return opRange{lo: taPos{lo.ln, 0}, hiEx: taPos{prev, 0}, lines: true}, false
		}
		hi = taPos{prev, len(c.buf.lineClusters(prev))}
	}
	return opRange{lo: lo, hiEx: hi}, hi == lo
}

// applyOp runs operator op over r: delete into the register, yank it, or delete and type in its
// place. One undo step; c's typing joins it.
func (c *EditorCore) applyOp(op Action, r opRange) {
	b := &c.buf
	switch op {
	case ActCaseToggleOp, ActLowerOp, ActUpperOp:
		c.applyCase(op, r)
	case ActIndentPrefix:
		c.applyShift(1, r)
	case ActOutdentPrefix:
		c.applyShift(-1, r)
	case ActYankPrefix:
		if !c.reg.yankAllowed() {
			return
		}
		text := b.textIn(r.lo, r.hiEx)
		if r.lines {
			text = strings.Join(b.lines[r.lo.ln:r.hiEx.ln+1], "\n")
		}
		c.regYank(text, r.lines)
		if r.lines {
			b.ln = r.lo.ln // the column stays, as Vim's yj and yk keep it
		} else {
			b.ln, b.col = r.lo.ln, r.lo.col
		}
		c.clampNormal()
		c.reveal()
		c.markDirty()
	case ActDeletePrefix:
		if r.lines {
			c.deleteLines(r.lo.ln, r.hiEx.ln)
			return
		}
		c.beginGroup()
		c.regDelete(b.textIn(r.lo, r.hiEx), false)
		b.deleteRegion(r.lo, r.hiEx)
		b.ln, b.col = r.lo.ln, r.lo.col
		c.clampNormal()
		c.edited()
	case ActChangePrefix:
		c.beginGroup()
		if r.lines {
			lines := b.lines[r.lo.ln : r.hiEx.ln+1]
			c.regDelete(strings.Join(lines, "\n"), true)
			indent := leadingBlanks(lines[0])
			rest := append(append(append([]string{}, b.lines[:r.lo.ln]...), indent), b.lines[r.hiEx.ln+1:]...)
			b.lines = rest
			b.touch(r.lo.ln)
			b.log.splice(r.lo.ln+1, r.hiEx.ln-r.lo.ln, 0) // the first line stays, emptied (Vim's cc)
			b.ln, b.col = r.lo.ln, len(clusters(indent))
		} else {
			c.regDelete(b.textIn(r.lo, r.hiEx), false)
			b.deleteRegion(r.lo, r.hiEx)
			b.ln, b.col = r.lo.ln, r.lo.col
		}
		b.anchor = nil
		c.enterInsert()
		c.hist.keepOpen() // the delete began this group; what is typed joins it
		c.edited()
	}
}

// leadingBlanks is s's indent: its leading spaces and tabs.
func leadingBlanks(s string) string { return s[:len(s)-len(strings.TrimLeft(s, " \t"))] }

// opMotion runs the pending operator over motion act, or reports false when act is no motion an
// operator takes (the operator is then cancelled by the caller).
func (c *EditorCore) opMotion(op Action, act Action, count int, hadCount bool) bool {
	b := &c.buf
	if op == ActChangePrefix && (act == ActWordForward || act == ActWORDForward) {
		if cs := b.lineClusters(b.ln); b.col < len(cs) && !isWS(cs[b.col]) {
			// cw and cW on a non-blank change to the word's end, as ce and cE
			if act == ActWORDForward {
				act = ActWORDEnd
			} else {
				act = ActWordEnd
			}
		}
	}
	to, kind, ok := c.opTarget(act, count, hadCount)
	if !ok {
		return true // a motion, but it fails: nothing changes
	}
	c.opRegOne = regOneMotion(act)
	c.runOp(op, to, kind)
	c.opRegOne = false
	return true
}

// regOneMotion reports a motion whose delete Vim keeps in register 1 even within a line (ops.c
// use_reg_one: %, (, ), `, /, ?, n, N, {, }); of those golib's operators take %, ( ), { and }.
func regOneMotion(act Action) bool {
	switch act {
	case ActMatchPair, ActParaForward, ActParaBack, ActSentenceForward, ActSentenceBack:
		return true
	}
	return false
}

// runOp makes the range from the cursor to to and runs op over it, unless it is empty.
func (c *EditorCore) runOp(op Action, to taPos, kind motionKind) {
	r, empty := c.rangeOf(taPos{c.buf.ln, c.buf.col}, to, kind)
	if empty {
		if op == ActChangePrefix {
			c.enterInsert() // c over nothing still types there: no edit, no undo step yet
		}
		return
	}
	c.applyOp(op, c.wholeFolds(r))
}

// opMotions are the actions an operator takes as its motion.
func opMotion(act Action) bool {
	switch act {
	case ActLeft, ActDown, ActUp, ActRight, ActLineStart, ActLineEnd,
		ActWordForward, ActWordBack, ActWordEnd, ActParaForward, ActParaBack,
		ActPageUp, ActPageDown, ActGoBottom,
		ActLineDownFirst, ActLineUpFirst, ActFirstNonBlank, ActLeftWrap,
		ActFindCharRepeat, ActFindCharReverse, ActMatchPair,
		ActViewTop, ActViewMiddle, ActViewBottom, ActHalfPageDown, ActHalfPageUp,
		ActWORDForward, ActWORDBack, ActWORDEnd, ActWordEndBack, ActWORDEndBack,
		ActSentenceForward, ActSentenceBack:
		return true
	}
	return false
}
