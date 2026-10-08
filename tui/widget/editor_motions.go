package widget

import "strings"

// isWS classifies a grapheme cluster as whitespace for the Editor's word
// motions: tabs and Unicode whitespace count, not just the literal space.
// The substrate's readline hops keep their own space-only rule.
func isWS(cluster string) bool {
	return strings.TrimSpace(cluster) == ""
}

// vimWordForward implements vim `w`: past the current word run, over
// whitespace, onto the start of the next word (crossing line ends).
func (c *EditorCore) vimWordForward() (int, int) {
	ln, col := c.buf.ln, c.buf.col
	cs := c.buf.lineClusters(ln)
	i := col
	for i < len(cs) && !isWS(cs[i]) {
		i++ // leave the current run
	}
	for {
		for i < len(cs) && isWS(cs[i]) {
			i++
		}
		if i < len(cs) {
			return ln, i
		}
		if ln >= len(c.buf.lines)-1 {
			return ln, max(0, len(cs)-1)
		}
		ln, cs, i = ln+1, c.buf.lineClusters(ln+1), 0
	}
}

// vimWordBack implements vim `b`: back over whitespace onto the start of
// the previous word run (crossing line ends).
func (c *EditorCore) vimWordBack() (int, int) {
	ln, col := c.buf.ln, c.buf.col
	cs := c.buf.lineClusters(ln)
	i := col
	for {
		for i > 0 && isWS(cs[i-1]) {
			i--
		}
		if i > 0 {
			for i > 0 && !isWS(cs[i-1]) {
				i--
			}
			return ln, i
		}
		if ln == 0 {
			return 0, 0
		}
		ln--
		cs = c.buf.lineClusters(ln)
		i = len(cs)
	}
}

// wordEnd moves to the end of the current/next word (vim `e`, cluster form).
func (c *EditorCore) wordEnd() (int, int) {
	ln, col := c.buf.ln, c.buf.col
	for {
		cs := c.buf.lineClusters(ln)
		i := col + 1
		for i < len(cs) && isWS(cs[i]) {
			i++
		}
		if i >= len(cs) {
			if ln < len(c.buf.lines)-1 {
				ln, col = ln+1, -1
				continue
			}
			return ln, max(0, len(cs)-1)
		}
		for i+1 < len(cs) && !isWS(cs[i+1]) {
			i++
		}
		return ln, i
	}
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
		col := c.normalMax(c.buf.ln)
		if c.keys.mode == ModeInsert {
			col = len(c.buf.lineClusters(c.buf.ln))
		}
		apply(c.buf.ln, col)
	case ActWordForward:
		c.buf.desired = -1
		for i := 0; i < count; i++ {
			ln, col := c.vimWordForward()
			c.buf.moveCursor(ln, col, false)
		}
		c.clampNormal()
		c.reveal()
		c.markDirty()
	case ActWordBack:
		c.buf.desired = -1
		for i := 0; i < count; i++ {
			ln, col := c.vimWordBack()
			c.buf.moveCursor(ln, col, false)
		}
		c.clampNormal()
		c.reveal()
		c.markDirty()
	case ActWordEnd:
		c.buf.desired = -1
		for i := 0; i < count; i++ {
			ln, col := c.wordEnd()
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
	}
}

// goToLine is the shared gg/G target motion: an EXPLICIT count means
// "line count" (1-based, clamped); without one, gg goes to the top and G
// to the bottom.
func (c *EditorCore) goToLine(hadCount bool, count int, bottom bool) {
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
