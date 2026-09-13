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
func (e *Editor) vimWordForward() (int, int) {
	ln, col := e.ln, e.col
	cs := e.lineClusters(ln)
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
		if ln >= len(e.lines)-1 {
			return ln, max(0, len(cs)-1)
		}
		ln, cs, i = ln+1, e.lineClusters(ln+1), 0
	}
}

// vimWordBack implements vim `b`: back over whitespace onto the start of
// the previous word run (crossing line ends).
func (e *Editor) vimWordBack() (int, int) {
	ln, col := e.ln, e.col
	cs := e.lineClusters(ln)
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
		cs = e.lineClusters(ln)
		i = len(cs)
	}
}

// wordEnd moves to the end of the current/next word (vim `e`, cluster form).
func (e *Editor) wordEnd() (int, int) {
	ln, col := e.ln, e.col
	for {
		cs := e.lineClusters(ln)
		i := col + 1
		for i < len(cs) && isWS(cs[i]) {
			i++
		}
		if i >= len(cs) {
			if ln < len(e.lines)-1 {
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
func (e *Editor) paraForward(count int) int {
	ln := e.ln
	for ; count > 0; count-- {
		i := ln + 1
		for i < len(e.lines) && strings.TrimSpace(e.lines[i]) != "" {
			i++
		}
		ln = min(i, len(e.lines)-1)
	}
	return ln
}

func (e *Editor) paraBack(count int) int {
	ln := e.ln
	for ; count > 0; count-- {
		i := ln - 1
		for i > 0 && strings.TrimSpace(e.lines[i]) != "" {
			i--
		}
		ln = max(i, 0)
	}
	return ln
}

// move applies a motion action count times, extending the selection in
// visual modes.
func (e *Editor) move(act Action, count int) {
	e.groupOpen = false
	// Visual highlights derive from vAnchor + cursor; the buffer's own
	// selection anchor stays nil so insertText never sees a stray region.
	const extend = false
	apply := func(ln, col int) {
		e.moveCursor(ln, col, extend)
		if e.mode != ModeInsert {
			e.clampNormal()
		}
		e.ensureVisible()
		e.MarkDirty()
	}
	switch act {
	case ActLeft:
		apply(e.ln, e.col-count)
		e.desired = -1
	case ActRight:
		apply(e.ln, min(e.col+count, e.normalMax(e.ln)))
		e.desired = -1
	case ActDown, ActUp:
		delta := count
		if act == ActUp {
			delta = -count
		}
		ln, col := e.verticalTarget(delta, e.measure)
		d := e.desired
		apply(ln, col)
		e.desired = d
	case ActPageDown, ActPageUp:
		delta := max(e.h, 1) * count
		if act == ActPageUp {
			delta = -delta
		}
		ln, col := e.verticalTarget(delta, e.measure)
		d := e.desired
		apply(ln, col)
		e.desired = d
	case ActLineStart:
		e.desired = -1
		apply(e.ln, 0)
	case ActLineEnd:
		e.desired = -1
		col := e.normalMax(e.ln)
		if e.mode == ModeInsert {
			col = len(e.lineClusters(e.ln))
		}
		apply(e.ln, col)
	case ActWordForward:
		e.desired = -1
		for i := 0; i < count; i++ {
			ln, col := e.vimWordForward()
			e.moveCursor(ln, col, false)
		}
		e.clampNormal()
		e.ensureVisible()
		e.MarkDirty()
	case ActWordBack:
		e.desired = -1
		for i := 0; i < count; i++ {
			ln, col := e.vimWordBack()
			e.moveCursor(ln, col, false)
		}
		e.clampNormal()
		e.ensureVisible()
		e.MarkDirty()
	case ActWordEnd:
		e.desired = -1
		for i := 0; i < count; i++ {
			ln, col := e.wordEnd()
			e.moveCursor(ln, col, false)
		}
		e.clampNormal()
		e.ensureVisible()
		e.MarkDirty()
	case ActParaForward:
		e.desired = -1
		apply(e.paraForward(count), 0)
	case ActParaBack:
		e.desired = -1
		apply(e.paraBack(count), 0)
	}
}

// goToLine is the shared gg/G target motion: an EXPLICIT count means
// "line count" (1-based, clamped); without one, gg goes to the top and G
// to the bottom.
func (e *Editor) goToLine(hadCount bool, count int, bottom bool) {
	e.desired = -1
	ln := 0
	switch {
	case hadCount:
		ln = min(count-1, len(e.lines)-1)
	case bottom:
		ln = len(e.lines) - 1
	}
	e.moveCursor(ln, 0, false)
	e.clampNormal()
	e.ensureVisible()
	e.MarkDirty()
}
