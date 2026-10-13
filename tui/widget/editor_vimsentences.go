package widget

import "strings"

// VIM SENTENCES — ( and ) move by sentence, and is and as select sentences, as Vim's findsent and
// current_sent (src/textobject.c) have them. A sentence ends at '.', '!' or '?', followed by any
// number of ')', ']', '"' or '\'', and then the line's end, a space or a tab; the next starts at
// the first non-blank after. An empty line is a sentence of its own, and a paragraph's first line
// starts one.
//
// One difference from Vim, on purpose: a line of only blanks counts as empty here, as it does for
// golib's { } (M1), where Vim needs a line with nothing on it. So ( ) and { } agree.
//
// The port walks positions as Vim's do: a line's clusters, then the end-of-line position after the
// last (Vim's NUL), which incl and decl step over except on an empty line.

// sentWalk is the text as the sentence rules see it: a line of only blanks reads empty.
type sentWalk struct {
	lines [][]string
}

func (c *EditorCore) sentWalk() *sentWalk {
	w := &sentWalk{lines: make([][]string, len(c.buf.lines))}
	for i, l := range c.buf.lines {
		if strings.TrimSpace(l) != "" {
			w.lines[i] = clusters(l)
		}
	}
	return w
}

// at is the cluster at p; "" at a line's end (Vim's NUL).
func (w *sentWalk) at(p taPos) string {
	if p.col < len(w.lines[p.ln]) {
		return w.lines[p.ln][p.col]
	}
	return ""
}

func (w *sentWalk) empty(ln int) bool { return len(w.lines[ln]) == 0 }

// inc is Vim's inc: 0 moved within the line, 2 moved onto its end, 1 to the next line, -1 at the
// text's end.
func (w *sentWalk) inc(p *taPos) int {
	if p.col < len(w.lines[p.ln]) {
		p.col++
		if p.col < len(w.lines[p.ln]) {
			return 0
		}
		return 2
	}
	if p.ln < len(w.lines)-1 {
		p.ln, p.col = p.ln+1, 0
		return 1
	}
	return -1
}

// dec is Vim's dec: 0 within the line, 1 to the previous line's end, -1 at the text's start.
func (w *sentWalk) dec(p *taPos) int {
	if p.col > 0 {
		p.col--
		return 0
	}
	if p.ln > 0 {
		p.ln--
		p.col = len(w.lines[p.ln])
		return 1
	}
	return -1
}

// incl and decl step over a line's end position, except an empty line's.
func (w *sentWalk) incl(p *taPos) int {
	r := w.inc(p)
	if r >= 1 && p.col > 0 {
		r = w.inc(p)
	}
	return r
}

func (w *sentWalk) decl(p *taPos) int {
	r := w.dec(p)
	if r == 1 && p.col > 0 {
		r = w.dec(p)
	}
	return r
}

func isBlankChar(s string) bool { return s == " " || s == "\t" }

// findSent is Vim's findsent: from p, count sentences forward or back. ok is false when the
// count ran out of text (Vim's FAIL); p is then where the walk stopped.
func (w *sentWalk) findSent(p taPos, forward bool, count int) (taPos, bool) {
	step := w.decl
	if forward {
		step = w.incl
	}
	for ; count > 0; count-- {
		prev := p
		noskip := false
		switch {
		case w.at(p) == "": // on an empty line: skip up to a non-empty one
			for {
				if step(&p) == -1 || w.at(p) != "" {
					break
				}
			}
			if forward {
				goto found
			}
		case !forward:
			w.decl(&p)
		}
		// back to the previous non-blank, non-punctuation character
		{
			foundDot := false
			for {
				ch := w.at(p)
				if !isBlankChar(ch) && !strings.Contains(".!?)]\"'", ch) || ch == "" {
					break
				}
				t := p
				if w.decl(&t) == -1 || (w.empty(t.ln) && forward) {
					break
				}
				if foundDot {
					break
				}
				if strings.Contains(".!?", ch) {
					foundDot = true
				}
				if strings.Contains(")]\"'", ch) {
					if tc := w.at(t); tc == "" || !strings.Contains(".!?)]\"'", tc) {
						break
					}
				}
				w.decl(&p)
			}
		}
		{
			start := p.ln
			for { // find the end of the sentence
				ch := w.at(p)
				if ch == "" { // an empty line (a paragraph's boundary), or a line's end
					if !forward && p.ln != start {
						p.ln, p.col = p.ln+1, 0
					}
					break
				}
				if ch == "." || ch == "!" || ch == "?" {
					t := p
					r := 0
					for {
						if r = w.inc(&t); r == -1 {
							break
						}
						if nc := w.at(t); nc == "" || !strings.Contains(")]\"'", nc) {
							break
						}
					}
					if nc := w.at(t); r == -1 || isBlankChar(nc) || nc == "" {
						p = t
						if w.at(p) == "" { // skip the line's end
							w.inc(&p)
						}
						break
					}
				}
				if step(&p) == -1 {
					if count > 1 {
						return p, false
					}
					noskip = true
					break
				}
			}
		}
	found:
		for !noskip && isBlankChar(w.at(p)) { // skip blanks
			if w.incl(&p) == -1 {
				break
			}
		}
		if p == prev { // didn't move: advance one and try again
			if step(&p) == -1 {
				if count > 1 {
					return p, false
				}
				break
			}
			count++
		}
	}
	return p, true
}

// firstBlank is Vim's find_first_blank: back over the blanks before p, to the first of them.
func (w *sentWalk) firstBlank(p *taPos) {
	for w.decl(p) != -1 {
		if !isBlankChar(w.at(*p)) {
			w.incl(p)
			break
		}
	}
}

// sentForward is Vim's findsent_forward: count sentences on, the cursor at the last one's end.
func (w *sentWalk) sentForward(p taPos, count int, atStart bool) taPos {
	for ; count > 0; count-- {
		p, _ = w.findSent(p, true, 1)
		if atStart {
			w.firstBlank(&p)
		}
		if count == 1 || atStart {
			w.decl(&p)
		}
		atStart = !atStart
	}
	return p
}

// sentenceTarget is ( or ): the count'th sentence start back or on. Past the last sentence ) stops
// at the text's last character; a count that runs out of text before its last sentence moves
// nothing, as Vim's findsent FAIL leaves the cursor (and nv_brace beeps).
func (c *EditorCore) sentenceTarget(forward bool, count int) taPos {
	w := c.sentWalk()
	at := taPos{c.buf.ln, c.buf.col}
	p, ok := w.findSent(at, forward, max(count, 1))
	if !ok {
		return at
	}
	return c.clampSentPos(p)
}

// clampSentPos puts a walk's position on the text: past a line's last cluster is its last.
func (c *EditorCore) clampSentPos(p taPos) taPos {
	p.ln = max(0, min(p.ln, len(c.buf.lines)-1))
	p.col = max(0, min(p.col, len(c.buf.lineClusters(p.ln))))
	return p
}

// sentObject is is (inner) and as, as Vim's current_sent makes them for an operator or a fresh
// Visual selection: from start to last (inclusive, Visual's cursor), or to end, exclusive, for an
// operator; incl says the sentence ends the text, so end couldn't step past last. In Visual, a
// selection that came out empty extends instead (stuck reports it: Vim's goto extend).
func (w *sentWalk) sentObject(cursor taPos, inner bool, count int) (start, last, end taPos, incl, stuck bool, pos taPos) {
	start, pos = cursor, cursor
	cur, _ := w.findSent(cursor, true, 1) // the next sentence's start
	for isBlankChar(w.at(pos)) {
		w.incl(&pos)
	}
	startBlank := pos == cur // on the blanks just before the next sentence
	if startBlank {
		w.firstBlank(&start)
	} else {
		cur, _ = w.findSent(cur, false, 1)
		start = cur
	}
	n := count
	switch {
	case !inner:
		n = count * 2
	case startBlank:
		n--
	}
	if n > 0 {
		cur = w.sentForward(cur, n, true)
	} else {
		w.decl(&cur)
	}
	if !inner {
		// with the blanks before the sentence, none after it; with none after, take those before
		if startBlank {
			w.firstBlank(&cur)
			if isBlankChar(w.at(cur)) {
				w.decl(&cur)
			}
		} else if !isBlankChar(w.at(cur)) {
			w.firstBlank(&start)
		}
	}
	last, end = cur, cur
	incl = w.incl(&end) == -1
	return start, last, end, incl, start == cur, pos
}

// sentExtend is is and as on a Visual selection already more than one character: the cursor moves
// on (or back, when it is at the selection's start) by count sentences, as Vim's current_sent's
// extend does. start is where the cursor was, cur the next sentence's start from it, pos the walk
// position carried from the caller.
func (w *sentWalk) sentExtend(start, cur, pos, anchor taPos, inner bool, count int) taPos {
	if sentLess(start, anchor) { // the cursor at the selection's start: extend back
		atStart := true
		w.decl(&pos)
		for sentLess(pos, cur) {
			if !isBlankChar(w.at(pos)) {
				atStart = false
				break
			}
			w.incl(&pos)
		}
		if !atStart {
			cur, _ = w.findSent(cur, false, 1)
			if cur == start {
				atStart = true // exactly at a sentence's start
			} else {
				cur, _ = w.findSent(cur, true, 1) // inside one: to its end (the next's start)
			}
		}
		if !inner {
			count *= 2
		}
		for ; count > 0; count-- {
			if atStart {
				w.firstBlank(&cur)
			}
			if !atStart || (inner && !isBlankChar(w.at(cur))) {
				cur, _ = w.findSent(cur, false, 1)
			}
			atStart = !atStart
		}
		return cur
	}
	w.incl(&pos) // the cursor at the selection's end: extend on
	atStart := true
	if pos != cur { // not just before a sentence
		atStart = false
		for sentLess(pos, cur) {
			if !isBlankChar(w.at(pos)) {
				atStart = true
				break
			}
			w.incl(&pos)
		}
		if atStart { // in the sentence
			cur, _ = w.findSent(cur, false, 1)
		} else { // in or before the blanks before one
			cur = start
		}
	}
	if !inner {
		count *= 2
	}
	return w.sentForward(cur, count, atStart)
}

func sentLess(a, b taPos) bool { return a.ln < b.ln || a.ln == b.ln && a.col < b.col }

// sentenceObject is is or as: for an operator, the range; in Visual, the selection made or
// extended. ok is false when there is nothing to take.
func (c *EditorCore) sentenceObject(inner bool, count int) (opRange, bool) {
	w := c.sentWalk()
	cursor := taPos{c.buf.ln, c.buf.col}
	count = max(count, 1)
	visual := c.keys.mode == ModeVisual || c.keys.mode == ModeVisualLine || c.keys.mode == ModeVisualBlock
	if visual && c.vAnchor != cursor {
		cur, _ := w.findSent(cursor, true, 1)
		c.setVisualCursor(c.clampSentPos(w.sentExtend(cursor, cur, cursor, c.vAnchor, inner, count)))
		return opRange{}, false // the selection moved; nothing more to do
	}
	start, last, end, incl, stuck, pos := w.sentObject(cursor, inner, count)
	if visual {
		if stuck { // a single blank before a sentence: extend from there (Vim's goto extend)
			c.setVisualCursor(c.clampSentPos(w.sentExtend(start, last, pos, c.vAnchor, inner, count)))
			return opRange{}, false
		}
		c.vAnchor = c.clampSentPos(start)
		if c.keys.mode != ModeVisual {
			c.setMode(ModeVisual)
		}
		c.setVisualCursor(c.clampSentPos(last))
		return opRange{}, false
	}
	// exclusive to end, or inclusive of last when the sentence ends the text; rangeOf applies Vim's
	// exclusive rules (an end at a later line's start ends the line before, and from a line's
	// first non-blank the range is whole lines)
	k, to := exclusive, end
	if incl {
		k, to = inclusive, last
	}
	r, empty := c.rangeOf(c.clampSentPos(start), c.clampSentPos(to), k)
	return r, !empty
}

// setVisualCursor puts the Visual cursor at p.
func (c *EditorCore) setVisualCursor(p taPos) {
	c.buf.ln, c.buf.col = p.ln, p.col
	c.buf.desired = -1
	c.reveal()
	c.markDirty()
}
