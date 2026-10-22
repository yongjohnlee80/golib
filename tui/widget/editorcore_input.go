package widget

import (
	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
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

// insertKey is an Insert key resolved once, before any stage reads it (resolveInsert).
type insertKey struct {
	act     Action
	bound   bool     // a binding matched: exactly, or by the bare-key fallback
	unbound bool     // the exact chord is explicitly unbound
	late    bool     // the matched binding is an editing command's (keyDecl.late)
	qual    tui.Mods // the pressed modifiers the matched binding does not name
	text    bool     // typed text no binding takes: Text set, no Ctrl/Alt/Super/Hyper/Meta, not Tab
}

// resolveInsert is k in the Insert class. An exact binding or explicit unbind wins. A modified key
// no binding names reaches the editing command its bare key is bound to, the modifiers handed on
// as qualifiers (so Ctrl+Tab inserts a tab, and Shift+Tab is Tab with Shift). An editing key whose
// bare key is bound early (Nano's Home, an overlay's Right) reaches its own editing command, so
// Ctrl+Home still goes to the line's start; any other early binding, or an unbind of the bare key,
// lends it nothing.
func (c *EditorCore) resolveInsert(k tui.KeyEvent) insertKey {
	var r insertKey
	if s, ok := c.seqOf(k); ok {
		kc, act, bound, unbound := c.keys.lookupSeq(ModeInsert, s)
		switch {
		case unbound:
			return insertKey{unbound: true}
		case bound:
			return insertKey{act: act, bound: true, late: c.keys.late[kc]}
		}
		if s.Mods != 0 {
			bare := chordOfSeq(ModeInsert, tuikey.Seq{Code: s.Code})
			if act, ok := c.keys.keymap[bare]; ok {
				if c.keys.late[bare] {
					return insertKey{act: act, bound: true, late: true, qual: k.Mods}
				}
				if act, ok := editingActOf(s.Code); ok {
					return insertKey{act: act, bound: true, late: true, qual: k.Mods}
				}
			}
		}
	}
	r.text = k.Text != "" && k.Mods&nonTextMods == 0 && k.Code != tui.KeyTab
	return r
}

// handleInsertKey runs an Insert key through insertStages, in order, until one settles it. A key
// none settles is not consumed: it bubbles.
func (c *EditorCore) handleInsertKey(k tui.KeyEvent) bool {
	r := c.resolveInsert(k)
	for _, stage := range insertStages {
		if done, handled := stage(c, k, r); done {
			return handled
		}
	}
	return false
}

// insertStage is one step of Insert's key handling: done when it settled the key, handled whether
// the key is consumed.
type insertStage func(c *EditorCore, k tui.KeyEvent, r insertKey) (done, handled bool)

// insertStages is Insert's order, the contract hosts rely on: each stage's comment is its
// contract.
var insertStages = [...]insertStage{
	(*EditorCore).refuseReadOnly, // read-only: text, an editing key and a late mutating command are consumed, refused
	(*EditorCore).bubbleUnbound,  // an explicitly unbound key: the held rune settles; it bubbles
	(*EditorCore).runEarly,       // an early binding: the held rune settles; recorded, then run
	(*EditorCore).escapeChord,    // modal: the held rune and its timeout; a held rune settles on any other key
	(*EditorCore).replaceStage,   // Replace mode: offered the key and its command
	(*EditorCore).assistStage,    // the table, then the list assist: offered the key and its command
	(*EditorCore).recordLate,     // . and a macro keep an editing command's key (settles nothing)
	(*EditorCore).runLate,        // an editing command runs
	(*EditorCore).typeStage,      // typed text
}

func (c *EditorCore) refuseReadOnly(k tui.KeyEvent, r insertKey) (bool, bool) {
	refuse := c.readOnly && (k.Text != "" || textKeys[k.Code] || (r.late && mutatingAction(r.act)))
	return refuse, refuse // a viewer never changes text
}

// textKeys are the keys that edit the text whatever a scheme binds them to, so a read-only core
// consumes them unconditionally.
var textKeys = map[rune]bool{tui.KeyTab: true, tui.KeyEnter: true, tui.KeyBackspace: true, tui.KeyDelete: true}

func (c *EditorCore) bubbleUnbound(_ tui.KeyEvent, r insertKey) (bool, bool) {
	if !r.unbound {
		return false, false
	}
	c.settlePendingRune()
	return true, false
}

func (c *EditorCore) runEarly(k tui.KeyEvent, r insertKey) (bool, bool) {
	if !r.bound || r.late {
		return false, false
	}
	c.settlePendingRune()
	if r.act == ActInsertOneShot {
		c.macroKey(k) // a macro keeps it; . does not: the Insert before it ends there
	} else {
		c.dotInsertKey(k)
	}
	return true, c.execAction(r.act, 1)
}

func (c *EditorCore) escapeChord(k tui.KeyEvent, r insertKey) (bool, bool) {
	if c.keys.modal && c.takeEscapeChord(k, r.text) {
		return true, true
	}
	return false, false
}

func (c *EditorCore) replaceStage(k tui.KeyEvent, r insertKey) (bool, bool) {
	if c.replace != nil && c.replaceKey(k, r) {
		return true, true
	}
	return false, false
}

func (c *EditorCore) assistStage(k tui.KeyEvent, r insertKey) (bool, bool) {
	for _, assist := range insertAssists {
		if assist(c, k, r) {
			c.dotInsertKey(k)
			return true, true
		}
	}
	return false, false
}

func (c *EditorCore) recordLate(k tui.KeyEvent, r insertKey) (bool, bool) {
	if r.late {
		c.recordInsertKey(r.act, k)
	}
	return false, false
}

func (c *EditorCore) runLate(k tui.KeyEvent, r insertKey) (bool, bool) {
	if !r.late {
		return false, false
	}
	return true, c.execInsertEdit(r.act, k)
}

func (c *EditorCore) typeStage(k tui.KeyEvent, r insertKey) (bool, bool) {
	if !r.text {
		return false, false
	}
	c.typeText(k.Text)
	c.dotTypedText(k.Text)
	return true, true
}

// insertAssists are the Insert-mode assists, offered a key and its command in this order, after
// Replace and before the editing commands run. Each checks its own switch (CoreTableEditing,
// CoreListEditing) and reports whether it took the key; a key one takes is kept for . and a macro.
var insertAssists = [...]func(*EditorCore, tui.KeyEvent, insertKey) bool{(*EditorCore).tableKey, (*EditorCore).listKey}

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

// recordInsertKey keeps an editing command's key for . and a macro: replayed through
// handleInsertKey, so Enter re-runs auto-indent; a page jump splits the change. Esc is the macro's
// in execInsertEdit.
func (c *EditorCore) recordInsertKey(act Action, k tui.KeyEvent) {
	switch act {
	case ActInsertTab, ActNewline, ActBackspace, ActDeleteForward,
		ActLeft, ActRight, ActUp, ActDown, ActLineStart, ActLineEnd:
		c.dotInsertKey(k)
	case ActPageUp, ActPageDown:
		c.macroKey(k)
		c.dotJump()
	}
}

// execInsertEdit runs an editing command: Tab, Enter, Backspace and Delete edit; Esc leaves Insert
// (modal), or clears the selection anchor or the find, else is the host's (modeless); a caret
// motion moves, past a line's last character as Insert may. k is the key that ran it, for a macro.
func (c *EditorCore) execInsertEdit(act Action, k tui.KeyEvent) bool {
	b := &c.buf
	switch act {
	case ActInsertTab:
		c.beginGroup()
		b.insertText("\t")
		c.edited()
	case ActNewline:
		c.beginGroup()
		c.insertIndentedNewline()
		c.edited()
	case ActBackspace, ActDeleteForward:
		region := b.forwardRegion
		if act == ActBackspace {
			region = b.backspaceRegion
		}
		if lo, hi, ok := region(); ok {
			c.beginGroup()
			b.deleteRegion(lo, hi)
			c.edited()
		}
	case ActEscape:
		if c.keys.modal {
			c.macroKey(k)
			c.exitInsert()
			return true
		}
		if c.vAnchor != (taPos{}) {
			c.vAnchor = taPos{}
			c.markDirty()
			return true
		}
		// modeless, Esc is Normal mode's: it cancels the active find, then is the host's
		return c.escapeFind()
	default:
		c.move(act, 1)
	}
	return true
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
