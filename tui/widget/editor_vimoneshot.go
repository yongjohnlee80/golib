package widget

import "github.com/yongjohnlee80/golib/tui"

// VIM INSERT'S CTRL-O — one Normal-mode command, then Insert again. Ctrl-o closes the Insert's
// undo step and its . segment, and the mode becomes Normal with the one-shot set (InsertOneShot,
// for a host's "-- (insert) --"). The command is complete once a key leaves nothing pending, no
// Visual and no command line open:
//
//   - a command that entered Insert itself (o, A, cw …) is the end: that Insert is the command's;
//   - one that entered Visual waits, and Visual's operator or Esc ends it;
//   - : waits until the line runs or is cancelled (CommandLineClosed);
//   - otherwise Insert resumes at the cursor. Esc, right after Ctrl-o or with a command half-typed,
//     cancels the command; an unbound key bubbles to the host. Either way Insert resumes.
//
// A macro's keys and .'s come back through HandleKey, nested: Ctrl-o @a plays the whole macro as
// the one command, and a Ctrl-o inside a macro completes there, at its own depth.
//
// At the line's end (past its last cluster) Ctrl-o puts the cursor onto the last cluster, and the
// return puts it back past the end when the command left it there on the same line (Vim's
// ins_at_eol); a $ does the same on any line (Vim's curswant past the column). The Insert after
// is its own undo step and its own change to . (an i and what is typed).

// insertOneShot is Ctrl-o's state while its command runs.
type insertOneShot struct {
	on      bool
	eol     bool // the cursor was past the line's end
	ln      int  // the line it was on
	end     bool // the command was $: the cursor wants the line's end
	cmdline bool // the command opened the command line: Insert waits for it to close
	depth   int  // the key depth Ctrl-o came at: a key nested deeper (a macro's, .'s) is the command's
	replace bool // Ctrl-o came from Replace: Replace resumes (a new stack, as Vim's restart)
}

// InsertOneShot reports Insert's Ctrl-o running its one Normal-mode command: the mode is Normal,
// and Insert resumes when the command is complete.
func (c *EditorCore) InsertOneShot() bool { return c.oneShot.on }

// OneShotMode is the mode Ctrl-o's command returns to: ModeInsert, or ModeReplace from Replace
// (a status line's "(insert)" or "(replace)"); ModeNormal when no one-shot runs.
func (c *EditorCore) OneShotMode() EditorMode {
	switch {
	case !c.oneShot.on:
		return ModeNormal
	case c.oneShot.replace:
		return ModeReplace
	}
	return ModeInsert
}

// insertOneShotStart is Ctrl-o, in Insert.
func (c *EditorCore) insertOneShotStart() bool {
	if !c.keys.modal {
		return false
	}
	b := &c.buf
	c.dotInsertEnded()
	c.hist.close()
	b.anchor = nil   // a table cell's selection is Insert mode's
	c.blockIns = nil // a block's I or A ends here, its text on the first line only
	c.oneShot = insertOneShot{on: true, ln: b.ln, depth: c.keyDepth, replace: c.replace != nil}
	if n := len(b.lineClusters(b.ln)); n > 0 && b.col >= n {
		b.col, c.oneShot.eol = n-1, true
	}
	b.desired = -1
	c.setMode(ModeNormal)
	c.reveal()
	c.markDirty()
	return true
}

// oneShotKey takes Esc in Normal while the one-shot runs: the command is cancelled and Insert
// resumes. It reports whether it took the key.
func (c *EditorCore) oneShotKey(k tui.KeyEvent) bool {
	if !c.oneShot.on || c.keys.mode != ModeNormal || k.Code != tui.KeyEscape || c.keyDepth > c.oneShot.depth {
		return false
	}
	c.cancelCommand()
	c.oneShotResume()
	return true
}

// oneShotSettle ends the one-shot once its command is complete: Insert resumes in Normal, and a
// command that entered Insert has ended it already.
func (c *EditorCore) oneShotSettle() {
	o := &c.oneShot
	switch {
	case !o.on:
	case c.readOnly || !c.keys.modal:
		*o = insertOneShot{}
	case c.keys.mode == ModeNormal && !o.cmdline && !c.commandPending():
		c.oneShotResume()
	}
}

// oneShotResume returns to Insert at the cursor, past the line's end when the command left the
// cursor on its last cluster and either Ctrl-o began past the end on this line or the command was
// $. What is typed now records as an Insert of its own for . (an i).
func (c *EditorCore) oneShotResume() {
	o := c.oneShot
	c.oneShot = insertOneShot{}
	b := &c.buf
	if n := len(b.lineClusters(b.ln)); n > 0 && b.col == n-1 && ((o.eol && b.ln == o.ln) || o.end) {
		b.col = n
	}
	resume := tui.KeyEvent{Code: 'i', Text: "i"}
	if o.replace {
		resume = tui.KeyEvent{Code: 'R', Text: "R"}
		c.replace = &replaceState{ln: b.ln, col: b.col, count: 1}
	}
	if !c.dot.replaying {
		c.dot = dotRecorder{on: true, inInsert: true, rev: c.rev, last: c.dot.last,
			atoms: []dotAtom{{kind: dotKey, key: resume}}}
	}
	c.enterInsert()
	b.desired = -1
	c.reveal()
	c.markDirty()
}

// CommandLineClosed is the ':' prompt (SetCommandLine) closing for good, its line run or
// cancelled. A one-shot waiting on it resumes Insert.
func (c *EditorCore) CommandLineClosed() {
	if c.oneShot.on && c.oneShot.cmdline {
		c.oneShot.cmdline = false
		c.oneShotSettle()
	}
}

// cancelCommand forgets a command half-typed: its count, operator, prefix, armed key, register.
func (c *EditorCore) cancelCommand() {
	k := &c.keys
	k.dropPending()
	k.regArmed, k.reg, k.markArmed, k.macroArmed = false, 0, ActUnbound, ActUnbound
}
