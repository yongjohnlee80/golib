package widget

import (
	"strings"
	"unicode"

	"github.com/yongjohnlee80/golib/indent"
)

// VIM EDITS — r{char}, J and gJ, ~, and the case and indent operators (g~ gu gU, > <) that M2's
// operators run over a motion or a text object.

// replaceChars is r{char}: count clusters from the cursor become char, the cursor on the last; a
// line break replaces them all with one break, the cursor at the new line's start. Fewer than
// count left: nothing changes.
func (c *EditorCore) replaceChars(char string, count int) {
	b := &c.buf
	cs := b.lineClusters(b.ln)
	if b.col+count > len(cs) {
		return
	}
	c.beginGroup()
	lo, hi := taPos{b.ln, b.col}, taPos{b.ln, b.col + count}
	b.deleteRegion(lo, hi)
	b.ln, b.col = lo.ln, lo.col
	b.anchor = nil
	if char == "\n" {
		b.insertText("\n")
	} else {
		b.insertText(strings.Repeat(char, count))
		b.col--
	}
	c.clampNormal()
	c.edited()
}

// joinLines is J (gJ with raw): count lines, at least two, become one. J drops each joined line's
// leading blanks and puts one blank between, none when the line so far ends in a blank, the next
// is empty, or it starts with ')' (Neovim's nojoinspaces); gJ joins them as they are. A count past
// the text's end joins as many as there are; on the last line nothing changes. The cursor lands at
// the last join.
func (c *EditorCore) joinLines(count int, raw bool) {
	b := &c.buf
	n := min(max(count, 2)-1, len(b.lines)-1-b.ln)
	if n <= 0 {
		return
	}
	c.beginGroup()
	cur := b.lines[b.ln]
	at := 0
	for i := 1; i <= n; i++ {
		next := b.lines[b.ln+i]
		cut := 0
		sep := ""
		if !raw {
			trimmed := strings.TrimLeft(next, " \t")
			cut = len(clusters(next)) - len(clusters(trimmed))
			next = trimmed
			if cur != "" && !strings.HasSuffix(cur, " ") && !strings.HasSuffix(cur, "\t") && next != "" && !strings.HasPrefix(next, ")") {
				sep = " "
			}
		}
		at = len(clusters(cur))
		// the joined line's marks move onto this one, where its text now starts (Vim's do_join)
		b.logMove(b.ln+i, cut, b.ln, len(clusters(cur+sep)), true)
		cur += sep + next
	}
	b.lines = append(append(b.lines[:b.ln:b.ln], cur), b.lines[b.ln+n+1:]...)
	b.touch(b.ln)
	b.logSplice(b.ln+1, n, 0)
	b.col = at
	b.anchor = nil
	c.clampNormal()
	c.edited()
}

// toggleCaseChars is ~: the case of count clusters from the cursor toggled, the cursor past them
// (on the line's last when they reach its end).
func (c *EditorCore) toggleCaseChars(count int) {
	b := &c.buf
	cs := b.lineClusters(b.ln)
	if b.col >= len(cs) {
		return
	}
	end := min(b.col+count, len(cs))
	c.beginGroup()
	c.replaceRegion(taPos{b.ln, b.col}, taPos{b.ln, end}, toggleCase(strings.Join(cs[b.col:end], "")))
	b.col = min(end, len(cs)-1)
	c.clampNormal()
	c.edited()
}

// replaceRegion puts text in place of [lo, hiEx), the cursor at lo.
func (c *EditorCore) replaceRegion(lo, hiEx taPos, text string) {
	b := &c.buf
	b.deleteRegion(lo, hiEx)
	b.ln, b.col = lo.ln, lo.col
	b.anchor = nil
	b.insertText(text)
	b.ln, b.col = lo.ln, lo.col
}

// The case transforms, rune by rune: a cluster's combining marks are runes that have no case,
// and stay as they are.
func toggleCase(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsUpper(r):
			return unicode.ToLower(r)
		case unicode.IsLower(r):
			return unicode.ToUpper(r)
		}
		return r
	}, s)
}

// caseOf is what a case operator does to text.
func caseOf(op Action) func(string) string {
	switch op {
	case ActLowerOp:
		return strings.ToLower
	case ActUpperOp:
		return strings.ToUpper
	}
	return toggleCase
}

// caseOpOf is the case operator the g prefix's second key names, or ActUnbound.
func caseOpOf(code rune) Action {
	switch code {
	case '~':
		return ActCaseToggleOp
	case 'u':
		return ActLowerOp
	case 'U':
		return ActUpperOp
	}
	return ActUnbound
}

// applyCase runs case operator op over r, the cursor at its start.
func (c *EditorCore) applyCase(op Action, r opRange) {
	b := &c.buf
	f := caseOf(op)
	c.beginGroup()
	if r.lines {
		for i := r.lo.ln; i <= r.hiEx.ln; i++ {
			b.lines[i] = f(b.lines[i])
		}
		b.touch(r.lo.ln)
		b.ln = r.lo.ln
	} else {
		c.replaceRegion(r.lo, r.hiEx, f(b.textIn(r.lo, r.hiEx)))
	}
	c.clampNormal()
	c.edited()
}

// applyShift is > (dir 1) and < (dir -1) over every line r touches, linewise whatever the motion,
// the cursor on the first line's first non-blank. (An exclusive range never ends at a later
// line's start here: rangeOf has already moved such an end back, or made the range whole lines.)
func (c *EditorCore) applyShift(dir int, r opRange) {
	b := &c.buf
	last := r.hiEx.ln
	unit := c.shiftUnit()
	c.beginGroup()
	for i := r.lo.ln; i <= last; i++ {
		b.lines[i] = shiftLine(b.lines[i], unit, dir)
	}
	b.touch(r.lo.ln)
	b.ln, b.col = r.lo.ln, c.firstNonBlank(r.lo.ln)
	b.anchor = nil
	c.clampNormal()
	c.edited()
}

// shiftUnit is one level of indent for > and <: the editor's unit when the host set one, else the
// buffer's own (its first indented line's leading blanks), else a tab's width as spaces.
func (c *EditorCore) shiftUnit() string {
	if c.indentUnit != "" {
		return c.indentUnit
	}
	for _, l := range c.buf.lines {
		if lead := indent.Leading(l); lead != "" && strings.TrimSpace(l) != "" {
			return lead
		}
	}
	return strings.Repeat(" ", tabCells)
}

// indentWidth is leading blanks' width in columns, a tab counting tabCells.
func indentWidth(lead string) int {
	w := 0
	for _, r := range lead {
		if r == '\t' {
			w += tabCells
		} else {
			w++
		}
	}
	return w
}

// shiftLine moves line's indent one unit's width in (dir 1) or out (dir -1), never below zero, and
// writes it again in the unit's style: tabs for each whole tab and spaces for the rest when the
// unit has a tab, else spaces. A blank line, or an unindented one going out, is left as it is.
func shiftLine(line, unit string, dir int) string {
	lead := indent.Leading(line)
	body := line[len(lead):]
	if body == "" {
		return line
	}
	w := indentWidth(lead)
	if dir < 0 && w == 0 {
		return line
	}
	w = max(0, w+dir*indentWidth(unit))
	if strings.Contains(unit, "\t") {
		return strings.Repeat("\t", w/tabCells) + strings.Repeat(" ", w%tabCells) + body
	}
	return strings.Repeat(" ", w) + body
}
