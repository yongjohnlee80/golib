package widget

import (
	"github.com/yongjohnlee80/golib/tui"
)

// THE CORE'S INPUT. A widget hands its EditorCore the input it does not keep for itself: keys,
// bracketed paste, the chord timer's tick, focus loss, and a pointer already turned into a buffer
// position (the widget's hit-test). Each entry point reports the cursor's move to
// CoreOnCursorPositionChange once, after the input is handled.

// HandleKey handles a key in every mode, across modal and modeless profiles. It reports false for
// a key the core does not consume, which bubbles: an unbound key in Normal mode (Space, for a
// host's leader menu), Esc in Normal mode with nothing pending, an Alt or Super chord no key map
// binds. Its keys are the editor's commands (EditorCommands), as the App's key map binds them.
func (c *EditorCore) HandleKey(k tui.KeyEvent) bool {
	ln, col := c.buf.ln, c.buf.col
	defer c.cursorMoved(ln, col)
	if k.Kind == tui.KeyRelease {
		return false
	}
	c.syncKeymap()
	c.keyDepth++ // a macro and . hand their keys back in
	defer func() { c.keyDepth-- }()
	if c.keys.mode == ModeInsert {
		return c.handleInsertKey(k)
	}
	if c.oneShotKey(k) {
		c.macroKey(k)
		return true
	}
	// a host's chord: before . and a macro record it, so neither replays what the host did
	if c.chordHook != nil && k.Code != tui.KeyEscape && !c.commandPending() {
		// The hook is shown what it always was (hookChord), never an Alt or Super chord. A host
		// that wants them asks when one needs them.
		if kc, ok := c.hookChord(k); ok && c.chordHook(kc) {
			if c.keyDepth <= c.oneShot.depth {
				c.oneShotSettle() // Insert's Ctrl-o: the host's chord is its one command, and Insert resumes
			}
			return true
		}
	}
	c.dotStart()
	if c.dot.on && !c.dot.inInsert && !c.dot.replaying {
		c.dot.atoms = append(c.dot.atoms, dotAtom{kind: dotKey, key: k})
	}
	c.macroKey(k)
	handled := c.handleCommandKey(k)
	c.settleRegister()
	c.dotEnd()
	if c.keyDepth <= c.oneShot.depth {
		c.oneShotSettle() // Insert's Ctrl-o: the command complete, Insert resumes
	}
	return handled
}

// settleRegister drops the register a command named once the command has completed (Vim's clearop):
// "aj then yy yanks into the unnamed register. The key that named it completes nothing.
func (c *EditorCore) settleRegister() {
	k := &c.keys
	switch {
	case k.regFresh:
		k.regFresh = false
	case !k.regArmed && !c.opPending():
		k.reg = 0
	}
}

// HandlePaste inserts pasted text as one atomic edit. In Visual it replaces the selection. A
// read-only core consumes and refuses it.
func (c *EditorCore) HandlePaste(text string) bool {
	ln, col := c.buf.ln, c.buf.col
	defer c.cursorMoved(ln, col)
	if c.readOnly {
		return true // a viewer never mutates (bracketed paste included)
	}
	c.macroAtom(dotAtom{kind: dotPasted, text: text})
	b := &c.buf
	c.settlePendingRune()
	c.beginGroup()
	if s, ok := c.visualSelection(); ok {
		// a Visual paste replaces the selection (never silently discard the selection boundary)
		s.remove()
		c.setMode(ModeNormal)
		b.insertText(text)
		c.clampNormal()
		c.edited()
		return true
	}
	switch c.keys.mode {
	case ModeInsert:
		if c.replace != nil {
			c.replaceText(text) // Replace overwrites, a paste too
		} else {
			b.insertText(text) // one atomic literal insertion
		}
		c.dotPastedText(text)
	default:
		b.insertText(text) // one atomic literal insertion
		if c.keys.mode != ModeInsert {
			c.clampNormal()
		}
		c.dotPastedText(text)
	}
	c.edited()
	return true
}

// HandleTick is the escape chord's timeout (the tick the core asked for): the held rune is
// committed as an insertion.
func (c *EditorCore) HandleTick() bool {
	ln, col := c.buf.ln, c.buf.col
	defer c.cursorMoved(ln, col)
	c.keys.chordCancel = nil // fired: nothing left to cancel
	c.settlePendingRune()    // typed: what the tick committed is text, not the chord
	return true
}

// FocusLost settles the chord rune, ends the Insert undo group (the mode is unchanged), and clears
// EVERY partial command: a pending count and a double-key prefix must not survive a focus
// round-trip.
func (c *EditorCore) FocusLost() {
	ln, col := c.buf.ln, c.buf.col
	defer c.cursorMoved(ln, col)
	c.settlePendingRune()
	c.hist.close()
	c.keys.dropPending()
}

// PressAt is a click at buffer position (line, col), the widget's hit-test of the pointer. A press
// is a COMMAND BOUNDARY, not merely a cursor move: a pending insert rune settles where it was
// typed, the Insert undo group closes, pending command state is discarded (never completed
// against the clicked place), and Visual exits. Then the caret moves there, and a drag selection
// may start from it (DragTo); nothing is selected until the pointer moves.
func (c *EditorCore) PressAt(line, col int) bool {
	was, wasCol := c.buf.ln, c.buf.col
	defer c.cursorMoved(was, wasCol)
	b := &c.buf
	// ---- the command boundary, in this order ----
	//
	// A pending insert rune is SETTLED FIRST, at the caret it was typed at, and before the caret
	// moves: every non-chord input settles it, and discarding it would delete a character the user
	// typed. This is the only way a press changes buffer text.
	if c.keys.mode == ModeInsert {
		c.settlePendingRune()
		// A click is a deliberate discontinuity, so text typed before and after it undo apart.
		c.hist.close()
	}
	// Pending COMMAND state is discarded, never completed: completing `2d` against a clicked
	// place would turn a mis-click into a destructive edit.
	c.keys.dropPending()
	c.dotDrop() // a press is a jump: it splits an Insert being recorded
	// Visual exits and the anchor is cleared: keeping it would make the next motion extend a
	// selection the user believes they dismissed.
	if c.visual() {
		if c.keys.modal {
			c.setMode(ModeNormal)
		} else {
			c.setMode(ModeInsert)
		}
		c.vAnchor = taPos{}
	}

	b.ln, b.col = line, col
	if c.keys.mode != ModeInsert {
		c.clampNormal()
	}
	b.desired = -1
	c.replaceRestart() // a press moved the cursor: Replace starts a new stack there
	c.reveal()
	c.markDirty()
	if c.canSelect {
		c.dragging = true
		c.dragFrom = taPos{ln: b.ln, col: min(b.col, c.normalMax(b.ln))}
	}
	c.oneShotSettle() // a press is Ctrl-o's command
	return true
}

// Dragging reports whether a press started a drag selection that has not ended: the widget keeps
// the pointer (Context.CapturePointer) while it lasts.
func (c *EditorCore) Dragging() bool { return c.dragging }

// DragTo extends the drag selection to buffer position (line, col), in Visual: the same selection
// v makes. Until the pointer leaves the pressed position, a click stays a click.
func (c *EditorCore) DragTo(line, col int) {
	if !c.dragging {
		return
	}
	was, wasCol := c.buf.ln, c.buf.col
	defer c.cursorMoved(was, wasCol)
	if c.keys.mode != ModeVisual {
		if line == c.dragFrom.ln && col == c.dragFrom.col {
			return // not moved off the pressed position yet: still a click
		}
		c.vAnchor = c.dragFrom
		c.setMode(ModeVisual)
	}
	c.buf.ln, c.buf.col = line, col
	c.clampNormal()
	c.buf.desired = -1
	c.reveal()
	c.markDirty()
}

// EndDrag ends the drag; the selection, if any, stays for the key combos to copy.
func (c *EditorCore) EndDrag() { c.dragging = false }

// handleInsertKey: structural Insert handling (text, chord, Esc, editing keys). Tab INSERTS a tab
// in Insert mode; traversal belongs to Normal mode, where Tab bubbles.
func (c *EditorCore) handleInsertKey(k tui.KeyEvent) bool {
	if c.readOnly && insertEdits(k) {
		return true // consumed, refused: a viewer never changes text
	}
	var act Action
	var bound, unbound bool
	if s, ok := c.seqOf(k); ok {
		_, act, bound, unbound = c.keys.lookupSeq(ModeInsert, s)
	}

	// 1. Explicit unbind sentinel: unhandled keystroke bubbles up to application.
	if unbound {
		c.settlePendingRune()
		return false
	}

	// 2. Configured keymap actions (custom bindings, Nano/Standard profiles, etc.).
	if bound {
		c.settlePendingRune()
		if act == ActInsertOneShot {
			c.macroKey(k) // a macro keeps it; . does not: the Insert before it ends there
		} else {
			c.dotInsertKey(k)
		}
		return c.execAction(act, 1)
	}

	isText := k.Text != "" && k.Mods&nonTextMods == 0 && k.Code != tui.KeyTab

	if c.keys.modal && c.takeEscapeChord(k, isText) {
		return true
	}
	if c.replace != nil && c.replaceKey(k) {
		return true
	}
	for _, assist := range insertAssists {
		if assist(c, k) {
			c.dotInsertKey(k)
			return true
		}
	}
	c.recordInsertKey(k)
	if done, handled := c.insertEditKey(k); done {
		return handled
	}
	if act, caret := insertCaretKeys[k.Code]; caret {
		c.move(act, 1)
		return true
	}

	if isText {
		c.typeText(k.Text)
		c.dotTypedText(k.Text)
		return true
	}
	return false
}

// insertAssists are the Insert-mode assists, offered a key in this order before Insert's own
// handling. Each checks its own switch (CoreTableEditing, CoreListEditing) and reports whether it
// took the key; a key one takes is kept for . and a macro.
var insertAssists = [...]func(*EditorCore, tui.KeyEvent) bool{(*EditorCore).tableKey, (*EditorCore).listKey}

// insertEdits reports whether k, in Insert, would change the text: text, Enter, Tab, Backspace or
// Delete.
func insertEdits(k tui.KeyEvent) bool {
	switch k.Code {
	case tui.KeyEnter, tui.KeyTab, tui.KeyBackspace, tui.KeyDelete:
		return true
	}
	return k.Text != ""
}

// takeEscapeChord is the modal escape chord (jk, jj): its first rune is held until the second
// completes it as an Esc; any other key, or the tick, commits it as text. It reports whether it
// took the key.
func (c *EditorCore) takeEscapeChord(k tui.KeyEvent, isText bool) bool {
	if c.keys.pendingRune != 0 {
		if isText && []rune(k.Text)[0] == c.keys.chord[1] {
			c.keys.takeRune()                             // the second rune before the tick: an escape
			c.macroKey(tui.KeyEvent{Code: tui.KeyEscape}) // the chord is an Esc, to a macro
			c.exitInsert()
			return true
		}
		// Commit the held rune, then process THIS key afresh — it may itself be a fresh chord
		// start, so "jjk" commits the first j and escapes on the second j plus k.
		c.settlePendingRune()
	}
	if isText && c.keys.chord != nil && []rune(k.Text)[0] == c.keys.chord[0] {
		c.keys.pendingRune = c.keys.chord[0]
		if c.ctx != nil {
			c.keys.chordCancel = c.ctx.After(c.keys.chordTimeout)
		}
		return true
	}
	return false
}

// recordInsertKey keeps an Insert editing or caret key for . and a macro: replayed through
// handleInsertKey, so Enter re-runs auto-indent; a page jump splits the change.
func (c *EditorCore) recordInsertKey(k tui.KeyEvent) {
	switch k.Code {
	case tui.KeyTab, tui.KeyEnter, tui.KeyBackspace, tui.KeyDelete,
		tui.KeyLeft, tui.KeyRight, tui.KeyUp, tui.KeyDown, tui.KeyHome, tui.KeyEnd:
		c.dotInsertKey(k)
	case tui.KeyPageUp, tui.KeyPageDown:
		c.macroKey(k)
		c.dotJump()
	}
}

// insertEditKey is Insert's editing keys: Tab, Esc, Enter, Backspace and Delete. done reports
// whether k is one, and handled whether it is consumed.
func (c *EditorCore) insertEditKey(k tui.KeyEvent) (done, handled bool) {
	b := &c.buf
	switch k.Code {
	case tui.KeyTab:
		c.beginGroup()
		b.insertText("\t")
		c.edited()
		return true, true
	case tui.KeyEscape:
		if c.keys.modal {
			c.macroKey(k)
			c.exitInsert()
			return true, true
		}
		if c.vAnchor != (taPos{}) {
			c.vAnchor = taPos{}
			c.markDirty()
			return true, true
		}
		// modeless, Esc is Normal mode's: it cancels the active find, then is the host's
		return true, c.escapeFind()
	case tui.KeyEnter:
		c.beginGroup()
		c.insertIndentedNewline()
		c.edited()
		return true, true
	case tui.KeyBackspace, tui.KeyDelete:
		region := b.forwardRegion
		if k.Code == tui.KeyBackspace {
			region = b.backspaceRegion
		}
		if lo, hi, ok := region(); ok {
			c.beginGroup()
			b.deleteRegion(lo, hi)
			c.edited()
		}
		return true, true
	}
	return false, false
}

// insertCaretKeys are Insert mode's caret keys, each the motion it is: move reveals the caret and,
// in Insert, reaches past a line's last character.
var insertCaretKeys = map[rune]Action{
	tui.KeyLeft: ActLeft, tui.KeyRight: ActRight, tui.KeyUp: ActUp, tui.KeyDown: ActDown,
	tui.KeyHome: ActLineStart, tui.KeyEnd: ActLineEnd, tui.KeyPageUp: ActPageUp, tui.KeyPageDown: ActPageDown,
}

// hookChord is k as SetChordHook has always been shown it, so a host's hook written against it keeps
// matching: the key's code (a shifted letter by its text, a Ctrl letter as the event spells it), Ctrl,
// and nothing else. A key with Alt, Super, Hyper or Meta held is never shown.
func (c *EditorCore) hookChord(k tui.KeyEvent) (KeyChord, bool) {
	if k.Kind == tui.KeyRelease || k.Mods&(tui.ModAlt|tui.ModSuper|tui.ModMeta|tui.ModHyper) != 0 {
		return KeyChord{}, false
	}
	code := k.Code
	if k.Text != "" && k.Mods&nonTextMods == 0 {
		code = []rune(k.Text)[0]
	}
	return KeyChord{Mode: modeClass(c.keys.mode), Code: code, Ctrl: k.Mods&tui.ModCtrl != 0}, true
}
