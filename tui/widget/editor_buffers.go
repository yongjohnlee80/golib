package widget

import "slices"

// THE SHOWN BUFFER — which Buffer an editor shows, and how it changes (buffer.go).
//
// SetBuffer is not an edit: it publishes no ChangeEvent and calls no onChange, so a host that marks a
// document dirty on a change does not mark one dirty by showing it. Undo, redo, the marks, Modified
// and Revision are the shown buffer's from the switch on; the registers, the macro and . stay the
// editor's, as Vim's are global.

// Buffer is the buffer the editor shows.
func (c *EditorCore) Buffer() *Buffer { return c.handle }

// CoreBuffer makes the editor show b from the start, in place of an empty buffer of its own.
func CoreBuffer(b *Buffer) CoreOption {
	return func(c *EditorCore) {
		if b != nil {
			c.show(b.s, false)
		}
	}
}

// SetBuffer shows b: input pending between keys settles, a . recording ends, this editor's cursor is
// left in the shown buffer as its place, and its place in b comes back (line 1 the first time).
// Showing the buffer already shown does nothing.
func (c *EditorCore) SetBuffer(b *Buffer) {
	if b == nil || b.s == c.bufferState {
		return
	}
	ln, col := c.buf.ln, c.buf.col
	defer c.cursorMoved(ln, col)
	c.settlePendingRune()
	c.keys.count, c.keys.pendingAct = 0, ActUnbound
	c.oneShot, c.blockIns, c.replace = insertOneShot{}, nil, nil
	// a recording never spans two buffers, so . never compares one buffer's revision with another's
	c.dotInsertEnded() // an Insert being recorded ends in its own buffer: its change, if any, is the last
	c.dot.on = false   // a command half-recorded is dropped, as a half-typed command is
	c.hist.close()     // an Insert's undo group never spans two buffers
	c.show(b.s, true)
}

// Detach stops c showing any shared buffer: it leaves the shown buffer's editors and drops its place
// in every buffer it has left one in, so no buffer keeps it reachable. It then shows an empty buffer
// of its own. For a headless core whose work is done, and an editor being destroyed.
func (c *EditorCore) Detach() {
	c.leave(false)
	for s := range c.placed {
		delete(s.places, c)
	}
	clear(c.placed)
	c.show(newBuffer().s, false)
}

// show makes s the shown buffer, leaving the current one (with a place, when keep).
func (c *EditorCore) show(s *bufferState, keep bool) {
	c.leave(keep)
	c.bufferState = s
	s.editors = append(s.editors, c)
	p, seen := s.places[c]
	if !seen {
		p.desired = -1 // the first time here: line 1, no sticky column
	}
	c.buf = textBuffer{bufferText: s.text, desired: p.desired}
	c.buf.ln = max(0, min(p.at.ln, len(s.text.lines)-1))
	c.buf.col = c.buf.clampCol(c.buf.ln, p.at.col)
	c.vAnchor = taPos{}
	c.hl.text, c.hl.paint = &s.hlText, nil
	c.hl.paintRevision++
	c.find.cur, c.find.curOn = TextRange{}, false
	if c.find.term != "" && s.hlText.hl == nil {
		s.hlText.setHighlighter(plainHighlighter) // the find's marks need a walk to paint on
		c.find.plain = true
	}
	c.hl.overlay = c.findOverlay()
	if c.keys.modal {
		c.setMode(ModeNormal)
		c.clampNormal()
	} else {
		c.setMode(ModeInsert)
	}
	if c.layout != nil {
		c.layout.Changed(0)
	}
	if v, ok := c.layout.(EditorViewport); ok && seen {
		if first, _ := v.VisibleLines(); p.top != first {
			v.ScrollLines(p.top - first)
		}
	}
	c.reveal()
	c.markDirty()
}

// leave stops c showing its buffer: its place is kept there when keep, the find's own plain
// highlighter is taken back when no other editor's find needs it, and the highlight frame retires.
func (c *EditorCore) leave(keep bool) {
	s := c.bufferState
	if s == nil {
		return
	}
	c.retireHighlightFrame()
	s.editors = slices.DeleteFunc(s.editors, func(e *EditorCore) bool { return e == c })
	if keep {
		top := 0
		if v, ok := c.layout.(EditorViewport); ok {
			top, _ = v.VisibleLines()
		}
		s.places[c] = place{at: taPos{c.buf.ln, c.buf.col}, desired: c.buf.desired, top: top}
		c.placed[s] = true
	}
	if c.find.plain {
		c.find.plain = false
		if !slices.ContainsFunc(s.editors, func(e *EditorCore) bool { return e.find.plain }) {
			s.hlText.setHighlighter(nil)
		}
	}
}

// otherEdited is an edit to the shown buffer made through another editor, from line from on: this
// editor's cursor is kept in the text (followEdits moved it), and its layout and paint follow.
func (c *EditorCore) otherEdited(from int) {
	c.buf.ln = max(0, min(c.buf.ln, len(c.buf.lines)-1))
	c.buf.col = c.buf.clampCol(c.buf.ln, c.buf.col)
	if c.keys.modal && c.keys.mode == ModeNormal {
		c.clampNormal()
	}
	if c.layout != nil {
		c.layout.Changed(min(from, max(len(c.buf.lines)-1, 0)))
	}
	c.markDirty()
}

// ReplaceFollowing is Replace for a host's edit the user did not make, a reload from disk: one edit
// and one undo step, but this editor's cursor and first shown line stay with their text, as another
// editor's would, instead of landing at the replacement's end. A selection is left, as any edit
// leaves it.
func (c *EditorCore) ReplaceFollowing(r TextRange, text string) error {
	f := &selfFollow{at: taPos{c.buf.ln, c.buf.col}}
	if v, ok := c.layout.(EditorViewport); ok {
		f.top, _ = v.VisibleLines()
		f.view = v
	}
	c.following = f
	_, err := c.Replace(r, text)
	c.following = nil
	if err != nil {
		return err
	}
	c.buf.ln = max(0, min(f.at.ln, len(c.buf.lines)-1))
	c.buf.col = c.buf.clampCol(c.buf.ln, f.at.col)
	if c.keys.modal && c.keys.mode == ModeNormal {
		c.clampNormal()
	}
	if f.view != nil {
		if first, _ := f.view.VisibleLines(); f.top != first {
			f.view.ScrollLines(f.top - first)
		}
	}
	c.reveal()
	c.markDirty()
	return nil
}

// selfFollow is ReplaceFollowing's: the editor's own cursor and first shown line, as they were,
// moved by the edit's logged changes (followEdits).
type selfFollow struct {
	at   taPos
	top  int
	view EditorViewport
}

// follow moves the saved cursor and top with one change.
func (f *selfFollow) follow(e lineEdit) {
	followCursorPos(&f.at, e)
	t := taPos{ln: f.top}
	followCursorPos(&t, e)
	f.top = t.ln
}
