package widget

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
		if b.col > 0 {
			for n := b.col - c.deleteBackFrom(line); n > 0; n-- {
				c.replaceBackspace()
			}
		}
		return
	}
	if b.col == 0 {
		if b.ln == 0 {
			return
		}
		c.beginGroup()
		b.deleteRegion(taPos{b.ln - 1, len(b.lineClusters(b.ln - 1))}, taPos{b.ln, 0})
		c.edited()
		return
	}
	from := c.deleteBackFrom(line) // left of the cursor: the stop always is, so a cluster at least goes
	c.beginGroup()
	b.deleteRegion(taPos{b.ln, from}, taPos{b.ln, b.col})
	c.edited()
	if s := &c.insStart; s.ln == b.ln && b.col < s.col {
		s.col = b.col // passed: the next press goes on
	}
}

// deleteBackFrom is where Ctrl-w (line false) or Ctrl-u stops, back from the cursor on its line.
func (c *EditorCore) deleteBackFrom(line bool) int {
	b := &c.buf
	cs := b.lineClusters(b.ln)
	stop := 0
	if line {
		lead := 0 // the leading blanks, all of a blank line's (Vim's beginline(BL_WHITE))
		for lead < len(cs) && isWS(cs[lead]) {
			lead++
		}
		if b.col > lead {
			stop = lead // the indent stays
		}
	}
	if s := c.insStart; s.ln == b.ln && s.col < b.col && s.col > stop {
		stop = s.col // what this Insert typed goes first
	}
	from := stop
	if !line {
		from = b.col
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
