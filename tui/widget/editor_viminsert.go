package widget

import (
	txt "github.com/yongjohnlee80/golib/tui/widget/internal/text"
)

// VIM INSERT KEYS — Ctrl-w and Ctrl-u in Insert mode, as Vim's ins_bs (src/edit.c) has them with
// 'backspace' indent,eol,start: Ctrl-w deletes any blanks before the cursor, then one run of a
// single class (M1b's word classes: a keyword, punctuation, a script); Ctrl-u deletes back to the
// line's first non-blank, keeping the indent, or to the line's start when the cursor is at or
// before it. At a line's start either joins the line to the one before, and does nothing more.
// Deleting stops once where this Insert began, as Vim's Insstart does without 'backspace' nostop:
// the next press goes on past it.

// insertDeleteBack is Ctrl-w (line false) and Ctrl-u (line true). In Replace they are Backspace
// over the same clusters, so what they overwrote comes back (editor_vimreplace.go); at a line's
// start they do nothing there, as Vim's (Backspace joins a break typed, they don't).
func (c *EditorCore) insertDeleteBack(line bool) {
	b := &c.buf
	if c.replace != nil {
		if b.Col > 0 {
			for n := b.Col - c.deleteBackFrom(line); n > 0; n-- {
				c.replaceBackspace()
			}
		}
		return
	}
	if b.Col == 0 {
		if b.Ln == 0 {
			return
		}
		c.beginGroup()
		b.DeleteRegion(txt.Pos{Ln: b.Ln - 1, Col: len(b.LineClusters(b.Ln - 1))}, txt.Pos{Ln: b.Ln, Col: 0})
		c.edited()
		return
	}
	from := c.deleteBackFrom(line) // left of the cursor: the stop always is, so a cluster at least goes
	c.beginGroup()
	b.DeleteRegion(txt.Pos{Ln: b.Ln, Col: from}, txt.Pos{Ln: b.Ln, Col: b.Col})
	c.edited()
	if s := &c.insStart; s.Ln == b.Ln && b.Col < s.Col {
		s.Col = b.Col // passed: the next press goes on
	}
}

// deleteBackFrom is where Ctrl-w (line false) or Ctrl-u stops, back from the cursor on its line.
func (c *EditorCore) deleteBackFrom(line bool) int {
	b := &c.buf
	cs := b.LineClusters(b.Ln)
	stop := 0
	if line {
		lead := 0 // the leading blanks, all of a blank line's (Vim's beginline(BL_WHITE))
		for lead < len(cs) && isWS(cs[lead]) {
			lead++
		}
		if b.Col > lead {
			stop = lead // the indent stays
		}
	}
	if s := c.insStart; s.Ln == b.Ln && s.Col < b.Col && s.Col > stop {
		stop = s.Col // what this Insert typed goes first
	}
	from := stop
	if !line {
		from = b.Col
		for from > stop && isWS(cs[from-1]) {
			from--
		}
		if from > stop {
			class := wordClass(cs[from-1], false)
			for from > stop && !isWS(cs[from-1]) && wordClass(cs[from-1], false) == class {
				from--
			}
		}
	}
	return from
}
