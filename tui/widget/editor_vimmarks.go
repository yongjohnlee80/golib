package widget

// VIM MARKS — m{a–z} sets a mark at the cursor; '{a–z} goes to its line's first non-blank
// (linewise), `{a–z} to its exact place (exclusive); '' and `` go to where the last jump left. Each
// is an operator's motion too (d'a, y`a). Marks follow the text as Vim's do: the buffer logs every
// change to its line structure (textbuffer.go lineEdit) and the marks replay the log after each
// edit, so a mark on a removed line is gone, one below an inserted or removed line moves, and a
// split or join carries the marks on the moving text along (Vim's mark_adjust, mark_col_adjust).
// Undo and redo bring back the marks a change moved or erased (Vim's u_undoredo).

// markSet is the editor's marks: a–z, and the previous jump's place.
type markSet struct {
	pos     [26]taPos
	set     [26]bool
	prev    taPos
	prevSet bool
}

// isMarkName reports a mark m{name} can set: a–z.
func isMarkName(name rune) bool { return name >= 'a' && name <= 'z' }

// get is mark name's place: a–z, or ' and ` for the previous jump's. ok is false for a mark not set,
// or erased with its line.
func (m *markSet) get(name rune) (taPos, bool) {
	switch {
	case isMarkName(name):
		return m.pos[name-'a'], m.set[name-'a']
	case name == '\'' || name == '`':
		return m.prev, m.prevSet
	}
	return taPos{}, false
}

// apply moves the marks with one change to the line structure.
func (m *markSet) apply(e lineEdit) {
	if e.reset {
		*m = markSet{}
		return
	}
	adjust := func(p *taPos, set *bool) {
		switch {
		case !*set:
		case e.move:
			if p.ln == e.from && (e.all || p.col >= e.cut) {
				p.ln, p.col = e.to, e.at+max(p.col-e.cut, 0)
			}
		case p.ln >= e.ln && p.ln < e.ln+e.del:
			*set = false // its line was removed
		case p.ln >= e.ln+e.del:
			p.ln += e.ins - e.del
		}
	}
	for i := range m.pos {
		adjust(&m.pos[i], &m.set[i])
	}
	adjust(&m.prev, &m.prevSet)
}

// restore is undo's or redo's: each mark that was set when the change began goes back to where it
// was then; a mark set since keeps its place (Vim's u_undoredo restores only uh_namedm's set marks).
func (m *markSet) restore(was markSet) {
	for i := range was.pos {
		if was.set[i] {
			m.pos[i], m.set[i] = was.pos[i], true
		}
	}
}

// followEdits replays the buffer's logged edits on the marks: edited runs it after every change.
func (c *EditorCore) followEdits() {
	for _, e := range c.buf.log.take() {
		c.marks.apply(e)
	}
}

// setJumpMark records the cursor as the previous jump's place, before a jump (the ' and ` marks
// go back to it): G, gg, %, N%, H M L, n N, a mark's jump, :N.
func (c *EditorCore) setJumpMark() {
	c.marks.prev, c.marks.prevSet = taPos{c.buf.ln, c.buf.col}, true
}

// markTarget is where mark name sends the cursor: its line's first non-blank for ' (linewise), its
// place for ` (exclusive), the column kept inside the line. ok is false for a mark not set.
func (c *EditorCore) markTarget(name rune, exact bool) (to taPos, kind motionKind, ok bool) {
	p, ok := c.marks.get(name)
	if !ok || p.ln >= len(c.buf.lines) {
		return taPos{}, exclusive, false
	}
	if !exact {
		return taPos{p.ln, c.firstNonBlank(p.ln)}, linewise, true
	}
	return taPos{p.ln, c.buf.clampCol(p.ln, p.col)}, exclusive, true
}

// markKey completes an armed m, ' or ` with the key's text: m sets the mark; ' and ` jump to it, or
// run the pending operator over the motion. A name that is no mark, or a mark not set, does nothing
// (an operator is dropped), as in Vim, where it fails.
func (c *EditorCore) markKey(armed Action, name rune) {
	if armed == ActMarkSet {
		if isMarkName(name) {
			c.marks.pos[name-'a'], c.marks.set[name-'a'] = taPos{c.buf.ln, c.buf.col}, true
		}
		return
	}
	op := c.keys.op
	c.keys.dropOp()
	to, kind, ok := c.markTarget(name, armed == ActMarkExact)
	if !ok {
		return
	}
	if op != ActUnbound {
		c.opRegOne = armed == ActMarkExact // Vim keeps a ` delete in register 1
		c.runOp(op, to, kind)
		c.opRegOne = false
		return
	}
	from := taPos{c.buf.ln, c.buf.col}
	c.hist.close()
	c.buf.desired = -1
	c.buf.moveCursor(to.ln, to.col, false)
	c.marks.prev, c.marks.prevSet = from, true // '' and `` return here
	c.clampNormal()
	c.reveal()
	c.markDirty()
}
