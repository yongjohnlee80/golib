package widget

// EDITOR JUMPS — Ctrl+O and Ctrl+I in Normal mode, over a JumpList (jumplist.go). The editor records
// its jumps (G, gg, %, N%, H M L, { }, n N, a mark's jump, :N) into the list it holds: its own, for
// a host that gives it none, or the host's, shared across the documents the host shows. A step to
// a place in the editor's own document and view is committed at once and moves the cursor; any other
// goes to the host (SetJumps), which owns it: commits it once it shows that place, or leaves the list
// as it was when it doesn't. Entries for the editor's document follow its edits, as Vim's
// mark_adjust moves jumplist entries: one on a removed line goes to the first line after the removal,
// never away.

// jumpList is the list the editor records into: the host's, else its own.
func (c *EditorCore) jumpList() *JumpList {
	if c.jumps == nil {
		c.jumps = NewJumpList(0)
	}
	return c.jumps
}

// jumpPos is place p in the editor's document and view, as a jump list holds it.
func (c *EditorCore) jumpPos(p taPos) JumpPos {
	view := ""
	if c.jumpView != nil {
		view = c.jumpView()
	}
	return JumpPos{Doc: c.jumpDoc, Line: p.ln, Col: p.col, View: view}
}

// JumpHere is the cursor's place as a jump list holds it: what a host records when it leaves the
// document for another.
func (c *EditorCore) JumpHere() JumpPos { return c.jumpPos(taPos{c.buf.ln, c.buf.col}) }

// SetJumps makes list the one the editor records its jumps into and steps along (nil: a list of its
// own), and goTo how it reaches a place in another document or view. goTo owns the step: it commits
// it (list.Commit) once it shows the place, drops it (list.Drop) when the place is gone for good, or
// does nothing when it doesn't go there; with no goTo such a step does nothing.
func (c *EditorCore) SetJumps(list *JumpList, goTo func(JumpStep)) {
	c.jumps, c.jumpGo = list, goTo
}

// SetJumpDoc names the document the editor shows, as the host's jump list knows it: the host sets
// it whenever it shows another.
func (c *EditorCore) SetJumpDoc(doc string) { c.jumpDoc = doc }

// SetJumpView is how the editor learns the host's view of its document (a Rendered lens, say),
// for the places it records; nil: the editor's own, "".
func (c *EditorCore) SetJumpView(view func() string) { c.jumpView = view }

// jumpStep is Ctrl+O (back) and Ctrl+I: count steps along the list. A place in this document and
// view moves the cursor, the step committed; any other goes to the host.
func (c *EditorCore) jumpStep(back bool, count int) {
	l := c.jumpList()
	at := c.JumpHere()
	step, ok := l.Forward(at, count)
	if back {
		step, ok = l.Back(at, count)
	}
	if !ok {
		return
	}
	if to := step.To; to.Doc != at.Doc || to.View != at.View {
		if c.jumpGo != nil {
			c.jumpGo(step)
		}
		return
	}
	if !l.Commit(step) {
		return
	}
	ln := min(max(step.To.Line, 0), len(c.buf.textLines)-1)
	c.hist.close()
	c.buf.desired = -1
	c.buf.moveCursor(ln, c.buf.clampCol(ln, step.To.Col), false)
	c.clampNormal()
	c.reveal()
	c.markDirty()
}

// jumpsFollow moves the list's entries for this document with one change to its line structure,
// as Vim's mark_adjust moves jumplist entries: an entry on a removed line goes to the first line
// after the removal, not away.
func (c *EditorCore) jumpsFollow(e lineEdit) {
	if c.jumps == nil || e.reset { // a new document: the host names it, and the entries are another's
		return
	}
	c.jumps.adjust(c.jumpDoc, func(p *JumpPos) {
		if !e.move && p.Line >= e.ln && p.Line < e.ln+e.del {
			p.Line = e.ln // kept, at the removal
			return
		}
		m := markSet{} // a mark that is never erased here: the removal's case is above
		m.pos[0], m.set[0] = taPos{p.Line, p.Col}, true
		m.apply(e)
		p.Line, p.Col = m.pos[0].ln, m.pos[0].col
	})
}
