package widget

import "strings"

// VIM TEXT OBJECTS — iw aw iW aW, ip ap, i" a" i' a' i` a`, and the bracket pairs, for an operator
// (diw, ca() and for Visual (viw, va{). Each is a range around the cursor, or not found.

// textObject is the range object obj (the key after i or a) names around the cursor, count
// reaching that many words, paragraphs or bracket levels. ok is false when there is none.
func (c *EditorCore) textObject(inner bool, obj string, count int) (opRange, bool) {
	switch obj {
	case "w", "W":
		return c.wordObject(inner, obj == "W", count)
	case "p":
		return c.paraObject(inner, count)
	case `"`, "'", "`":
		return c.quoteObject(inner, obj)
	}
	if open, close, ok := bracketPair(obj); ok {
		return c.bracketObject(inner, open, close, count)
	}
	return opRange{}, false
}

// bracketPair is the pair a bracket object's key names: either bracket, or b and B.
func bracketPair(obj string) (open, close string, ok bool) {
	switch obj {
	case "(", ")", "b":
		return "(", ")", true
	case "{", "}", "B":
		return "{", "}", true
	case "[", "]":
		return "[", "]", true
	case "<", ">":
		return "<", ">", true
	}
	return "", "", false
}

// wordObject is iw aw (iW aW): runs of one class on the cursor's line. iw is count runs from the
// cursor's (a run of blanks counts as one); aw is count words with the blanks after each, or,
// with none after the last, the blanks before the first.
func (c *EditorCore) wordObject(inner, big bool, count int) (opRange, bool) {
	ln, col := c.buf.ln, c.buf.col
	cs := c.buf.lineClusters(ln)
	if len(cs) == 0 {
		return opRange{}, false
	}
	col = min(col, len(cs)-1)
	type run struct{ from, to, cls int } // [from, to)
	var runs []run
	for i := 0; i < len(cs); {
		k := wordClass(cs[i], big)
		j := i + 1
		for j < len(cs) && wordClass(cs[j], big) == k {
			j++
		}
		runs = append(runs, run{i, j, k})
		i = j
	}
	r := 0
	for r < len(runs) && runs[r].to <= col {
		r++
	}
	start, end := r, r
	if inner {
		end = min(r+count-1, len(runs)-1)
		return opRange{lo: taPos{ln, runs[start].from}, hiEx: taPos{ln, runs[end].to}}, true
	}
	if runs[r].cls == 0 { // on blanks: the blanks and the word after them, count times
		end = min(r+2*count-1, len(runs)-1)
		return opRange{lo: taPos{ln, runs[start].from}, hiEx: taPos{ln, runs[end].to}}, true
	}
	end = min(r+2*(count-1), len(runs)-1) // count words, the blanks between them included
	if end+1 < len(runs) && runs[end+1].cls == 0 {
		end++ // the blanks after
	} else if start > 0 && runs[start-1].cls == 0 {
		start-- // none after: the blanks before
	}
	return opRange{lo: taPos{ln, runs[start].from}, hiEx: taPos{ln, runs[end].to}}, true
}

// paraObject is ip ap: whole lines. ip is count runs of lines alike in being blank or not, from
// the cursor's; ap is a paragraph with the blank lines after it (count times).
func (c *EditorCore) paraObject(inner bool, count int) (opRange, bool) {
	lines := c.buf.lines
	blank := func(i int) bool { return strings.TrimSpace(lines[i]) == "" }
	runEnd := func(i int) int { // the last line of the run i is in
		for i+1 < len(lines) && blank(i+1) == blank(i) {
			i++
		}
		return i
	}
	start := c.buf.ln
	for start > 0 && blank(start-1) == blank(c.buf.ln) {
		start--
	}
	end := start - 1
	runs := count
	if !inner {
		runs = 2 * count
		if blank(c.buf.ln) {
			runs = 2 * count // blanks then a paragraph, count times
		}
	}
	for range runs {
		if end+1 >= len(lines) {
			break
		}
		end = runEnd(end + 1)
	}
	return opRange{lo: taPos{start, 0}, hiEx: taPos{end, 0}, lines: true}, true
}

// quoteObject is i" a" (and ' `): the pair of quotes on the cursor's line around it, or the first
// pair after it. Quotes pair up from the line's start, a backslash escaping one. a" takes the
// quotes, and the blanks after them, or, with none, the blanks before.
func (c *EditorCore) quoteObject(inner bool, q string) (opRange, bool) {
	ln, col := c.buf.ln, c.buf.col
	cs := c.buf.lineClusters(ln)
	var at []int
	for i, cl := range cs {
		if cl != q {
			continue
		}
		escapes := 0 // the backslashes right before it: an odd run escapes it, an even one escapes itself
		for j := i - 1; j >= 0 && cs[j] == `\`; j-- {
			escapes++
		}
		if escapes%2 == 0 {
			at = append(at, i)
		}
	}
	open, close := -1, -1
	for i := 0; i+1 < len(at); i += 2 {
		if at[i] <= col && col <= at[i+1] || at[i] > col {
			open, close = at[i], at[i+1]
			break
		}
	}
	if open < 0 {
		return opRange{}, false
	}
	if inner {
		return opRange{lo: taPos{ln, open + 1}, hiEx: taPos{ln, close}}, true
	}
	lo, hi := open, close+1
	if hi < len(cs) && isWS(cs[hi]) {
		for hi < len(cs) && isWS(cs[hi]) {
			hi++
		}
	} else {
		for lo > 0 && isWS(cs[lo-1]) {
			lo--
		}
	}
	return opRange{lo: taPos{ln, lo}, hiEx: taPos{ln, hi}}, true
}

// bracketObject is i( a( and the other pairs: the count-th pair enclosing the cursor (a bracket
// under the cursor encloses it), nesting counted across lines. a( takes the brackets; i( their
// inside, from the line after the open bracket when it ends its line, and to the end of the line
// before the close bracket when only blanks come before it.
func (c *EditorCore) bracketObject(inner bool, open, close string, count int) (opRange, bool) {
	b := &c.buf
	o, ok := c.enclosing(open, close, count)
	if !ok {
		return opRange{}, false
	}
	e, ok := c.matching(o, open, close)
	if !ok {
		return opRange{}, false
	}
	if !inner {
		return opRange{lo: o, hiEx: taPos{e.ln, e.col + 1}}, true
	}
	lo, hiEx := taPos{o.ln, o.col + 1}, e
	if lo.col >= len(b.lineClusters(o.ln)) && o.ln < e.ln {
		lo = taPos{o.ln + 1, 0}
	}
	if e.ln > lo.ln && e.col <= c.firstNonBlank(e.ln) && strings.TrimSpace(strings.Join(b.lineClusters(e.ln)[:e.col], "")) == "" {
		hiEx = taPos{e.ln - 1, len(b.lineClusters(e.ln - 1))}
	}
	if hiEx.ln < lo.ln || (hiEx.ln == lo.ln && hiEx.col < lo.col) {
		hiEx = lo
	}
	// brackets on lines of their own around whole lines: the inside is those lines, as Vim's
	if lo.ln > o.ln && lo.col == 0 && hiEx.ln < e.ln && hiEx.ln >= lo.ln && hiEx.col == len(b.lineClusters(hiEx.ln)) {
		return opRange{lo: taPos{lo.ln, 0}, hiEx: taPos{hiEx.ln, 0}, lines: true}, true
	}
	return opRange{lo: lo, hiEx: hiEx}, true
}

// enclosing is the count-th unmatched open bracket at or before the cursor, scanning back across
// lines: a close bracket passed on the way needs its open one first.
func (c *EditorCore) enclosing(open, close string, count int) (taPos, bool) {
	b := &c.buf
	ln, i := b.ln, b.col
	cs := b.lineClusters(ln)
	if i < len(cs) && cs[i] == close {
		i-- // on the close bracket: its own pair is the one around
		if p, ok := c.backTo(open, close, ln, i, 1); ok {
			if count == 1 {
				return p, true
			}
			return c.backTo(open, close, p.ln, p.col-1, count-1)
		}
		return taPos{}, false
	}
	if i < len(cs) && cs[i] == open {
		if count == 1 {
			return taPos{ln, i}, true
		}
		return c.backTo(open, close, ln, i-1, count-1)
	}
	return c.backTo(open, close, ln, i-1, count)
}

// backTo scans back from (ln, i) for the n-th unmatched open bracket.
func (c *EditorCore) backTo(open, close string, ln, i, n int) (taPos, bool) {
	b := &c.buf
	depth := 0
	for {
		cs := b.lineClusters(ln)
		for ; i >= 0; i-- {
			if i >= len(cs) {
				continue
			}
			switch cs[i] {
			case close:
				depth++
			case open:
				if depth > 0 {
					depth--
					continue
				}
				n--
				if n == 0 {
					return taPos{ln, i}, true
				}
			}
		}
		if ln == 0 {
			return taPos{}, false
		}
		ln--
		i = len(b.lineClusters(ln)) - 1
	}
}

// matching is the close bracket that pairs with the open one at o, nesting counted across lines.
func (c *EditorCore) matching(o taPos, open, close string) (taPos, bool) {
	b := &c.buf
	depth := 0
	ln, i := o.ln, o.col
	for ln < len(b.lines) {
		cs := b.lineClusters(ln)
		for ; i < len(cs); i++ {
			switch cs[i] {
			case open:
				depth++
			case close:
				depth--
				if depth == 0 {
					return taPos{ln, i}, true
				}
			}
		}
		ln, i = ln+1, 0
	}
	return taPos{}, false
}

// runObject runs operator op over a text object's range. An empty one (i" of "") is no edit; c
// over it still types there.
func (c *EditorCore) runObject(op Action, r opRange) {
	if !r.lines && r.lo == r.hiEx {
		if op == ActChangePrefix {
			c.buf.ln, c.buf.col = r.lo.ln, r.lo.col
			c.enterInsert()
		}
		return
	}
	c.applyOp(op, r)
}

// selectObject makes a text object the Visual selection: whole lines for ip and ap, else from its
// first cluster to its last.
func (c *EditorCore) selectObject(r opRange) {
	b := &c.buf
	if r.lines {
		c.vAnchor = taPos{r.lo.ln, 0}
		b.ln, b.col = r.hiEx.ln, 0
		c.setMode(ModeVisualLine)
		c.reveal()
		c.markDirty()
		return
	}
	if r.lo == r.hiEx {
		return
	}
	end := r.hiEx
	if end.col > 0 {
		end.col--
	} else if end.ln > r.lo.ln {
		end = taPos{end.ln - 1, max(0, len(b.lineClusters(end.ln-1))-1)}
	}
	c.vAnchor = r.lo
	b.ln, b.col = end.ln, end.col
	if c.keys.mode != ModeVisual {
		c.setMode(ModeVisual)
	}
	c.reveal()
	c.markDirty()
}
