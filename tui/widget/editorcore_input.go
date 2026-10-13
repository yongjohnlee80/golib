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
// host's leader menu), Esc in Normal mode with nothing pending, a key with Alt or Super.
func (c *EditorCore) HandleKey(k tui.KeyEvent) bool {
	ln, col := c.buf.ln, c.buf.col
	defer c.cursorMoved(ln, col)
	if k.Kind == tui.KeyRelease {
		return false
	}
	if c.keys.mode == ModeInsert {
		return c.handleInsertKey(k)
	}
	c.dotStart()
	if c.dot.on && !c.dot.inInsert && !c.dot.replaying {
		c.dot.atoms = append(c.dot.atoms, dotAtom{kind: dotKey, key: k})
	}
	c.macroKey(k)
	handled := c.handleCommandKey(k)
	c.settleRegister()
	c.dotEnd()
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
	switch c.keys.mode {
	case ModeVisual:
		// Visual paste replaces the selection (never silently discard the selection boundary).
		lo, hiEx := c.visualRange()
		b.deleteRegion(lo, hiEx)
		c.setMode(ModeNormal)
		b.insertText(text)
		c.clampNormal()
	case ModeVisualLine:
		lo, hi := c.visualLines()
		c.setMode(ModeNormal)
		b.anchor = nil
		b.lines = append(b.lines[:lo], append([]string{""}, b.lines[hi+1:]...)...)
		b.touch(lo)
		b.log.splice(lo+1, hi-lo, 0) // the first line stays, emptied, for the paste
		b.ln, b.col = lo, 0
		b.insertText(text)
		c.clampNormal()
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
	c.keys.chordCancel = nil
	if c.keys.pendingRune != 0 {
		r := c.keys.pendingRune
		c.keys.pendingRune = 0
		c.beginGroup()
		c.buf.insertText(string(r))
		c.edited()
		c.dotTypedText(string(r)) // typed: what the tick committed is text, not the chord
	}
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
	if c.keys.mode == ModeVisual || c.keys.mode == ModeVisualLine {
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
	c.reveal()
	c.markDirty()
	if c.canSelect {
		c.dragging = true
		c.dragFrom = taPos{ln: b.ln, col: min(b.col, c.normalMax(b.ln))}
	}
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
	if c.readOnly {
		if k.Text != "" || k.Code == tui.KeyEnter || k.Code == tui.KeyTab || k.Code == tui.KeyBackspace || k.Code == tui.KeyDelete {
			return true
		}
	}
	b := &c.buf
	ctrl := k.Mods&tui.ModCtrl != 0
	code := k.Code
	if k.Text != "" && k.Mods&nonTextMods == 0 {
		code = []rune(k.Text)[0]
	}
	kc := KeyChord{Mode: ModeInsert, Code: code, Ctrl: ctrl}

	// 1. Explicit unbind sentinel: unhandled keystroke bubbles up to application.
	if c.keys.unbound[kc] {
		c.settlePendingRune()
		return false
	}

	// 2. Configured keymap actions (custom bindings, Nano/Standard profiles, etc.).
	if act, bound := c.keys.keymap[kc]; bound {
		c.settlePendingRune()
		c.dotInsertKey(k)
		return c.execAction(act, 1)
	}

	isText := k.Text != "" && k.Mods&nonTextMods == 0 && k.Code != tui.KeyTab

	// Chord state machine first (only in modal editing).
	if c.keys.modal && c.keys.pendingRune != 0 {
		if isText && []rune(k.Text)[0] == c.keys.chord[1] {
			// Second chord rune dispatched before the tick: escape.
			c.keys.pendingRune = 0
			if c.keys.chordCancel != nil {
				c.keys.chordCancel()
				c.keys.chordCancel = nil
			}
			c.macroKey(tui.KeyEvent{Code: tui.KeyEscape}) // the chord is an Esc, to a macro
			c.exitInsert()
			return true
		}
		// Commit the held rune, then process THIS key from the top of the Insert state machine —
		// it may itself be a fresh chord start, so "jjk" commits the first j and escapes on the
		// second j plus k.
		c.settlePendingRune()
	}
	if c.keys.modal && isText && c.keys.chord != nil && c.keys.pendingRune == 0 && []rune(k.Text)[0] == c.keys.chord[0] {
		c.keys.pendingRune = c.keys.chord[0]
		if c.ctx != nil {
			c.keys.chordCancel = c.ctx.After(c.keys.chordTimeout)
		}
		return true
	}

	if c.tableKey(k) || c.listKey(k) {
		c.dotInsertKey(k)
		return true
	}
	switch k.Code {
	case tui.KeyTab, tui.KeyEnter, tui.KeyBackspace, tui.KeyDelete,
		tui.KeyLeft, tui.KeyRight, tui.KeyUp, tui.KeyDown, tui.KeyHome, tui.KeyEnd:
		c.dotInsertKey(k) // replayed through this same handler: Enter re-runs auto-indent
	case tui.KeyPageUp, tui.KeyPageDown:
		c.macroKey(k)
		c.dotJump() // a jump splits the change
	}
	switch k.Code {
	case tui.KeyTab:
		// Insert mode consumes Tab as text; traversal belongs to Normal mode, where Tab bubbles.
		c.beginGroup()
		b.insertText("\t")
		c.edited()
		return true
	case tui.KeyEscape:
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
	case tui.KeyEnter:
		c.beginGroup()
		c.insertIndentedNewline()
		c.edited()
		return true
	case tui.KeyBackspace:
		if b.col > 0 {
			c.beginGroup()
			b.deleteRegion(taPos{b.ln, b.col - 1}, taPos{b.ln, b.col})
			c.edited()
		} else if b.ln > 0 {
			c.beginGroup()
			b.deleteRegion(taPos{b.ln - 1, len(b.lineClusters(b.ln - 1))}, taPos{b.ln, 0})
			c.edited()
		}
		return true
	case tui.KeyDelete:
		if b.col < len(b.lineClusters(b.ln)) {
			c.beginGroup()
			b.deleteRegion(taPos{b.ln, b.col}, taPos{b.ln, b.col + 1})
			c.edited()
		} else if b.ln < len(b.lines)-1 {
			c.beginGroup()
			b.deleteRegion(taPos{b.ln, b.col}, taPos{b.ln + 1, 0})
			c.edited()
		}
		return true
	case tui.KeyLeft:
		c.hist.close()
		b.desired = -1
		b.moveCursor(b.ln, b.col-1, false)
		c.reveal()
		c.markDirty()
		return true
	case tui.KeyRight:
		c.hist.close()
		b.desired = -1
		b.moveCursor(b.ln, b.col+1, false)
		c.reveal()
		c.markDirty()
		return true
	case tui.KeyUp, tui.KeyDown:
		c.hist.close()
		delta := 1
		if k.Code == tui.KeyUp {
			delta = -1
		}
		ln, col := b.verticalTarget(delta, c.measure)
		d := b.desired
		b.moveCursor(ln, col, false)
		b.desired = d
		c.reveal()
		c.markDirty()
		return true
	case tui.KeyHome:
		c.hist.close()
		b.desired = -1
		b.moveCursor(b.ln, 0, false)
		c.markDirty()
		return true
	case tui.KeyEnd:
		c.hist.close()
		b.desired = -1
		b.moveCursor(b.ln, len(b.lineClusters(b.ln)), false)
		c.markDirty()
		return true
	case tui.KeyPageUp:
		c.move(ActPageUp, 1)
		return true
	case tui.KeyPageDown:
		c.move(ActPageDown, 1)
		return true
	}

	if isText {
		c.typeText(k.Text)
		c.dotTypedText(k.Text)
		return true
	}
	return false
}

// handleCommandKey: Normal/Visual dispatch — digits, the double-key pending buffer, then the
// keymap. Unbound keys clear pending state and bubble.
func (c *EditorCore) handleCommandKey(k tui.KeyEvent) bool {
	b := &c.buf
	ctrl := k.Mods&tui.ModCtrl != 0

	if k.Code == tui.KeyEscape {
		hadPending := c.commandPending()
		c.keys.count, c.keys.pendingAct, c.keys.findArmed = 0, ActUnbound, ActUnbound
		c.keys.regArmed, c.keys.reg, c.keys.markArmed, c.keys.macroArmed = false, 0, ActUnbound, ActUnbound
		c.keys.dropOp()
		if c.keys.mode == ModeVisual || c.keys.mode == ModeVisualLine {
			c.exitVisual()
			return true
		}
		// Normal mode with nothing pending: Esc cancels the active find (its marks, n and N) when
		// there is one. Otherwise it is a vim no-op, so it BUBBLES: consuming it made an Editor
		// inside a modal float undismissable, since the host never saw the key.
		return hadPending || c.escapeFind()
	}

	// An armed f, F, t or T takes this key's text as its character, whatever it is (a digit or a
	// space included): the typed cluster, never the code. A key with no text, or one with a command
	// modifier, cancels the find and is processed afresh.
	if armed := c.keys.findArmed; armed != ActUnbound {
		count := c.keys.findCount
		c.keys.findArmed, c.keys.findCount = ActUnbound, 0
		if k.Text != "" && k.Mods&nonTextMods == 0 {
			f := charFindOf(armed, firstCluster(k.Text))
			c.keys.lastFind = &f
			if op := c.keys.op; op != ActUnbound {
				c.keys.dropOp()
				if to, kind, ok := c.findTarget(f, count, false); ok {
					c.runOp(op, to, kind)
				}
				return true
			}
			c.findChar(f, count, false)
			return true
		}
		c.keys.dropOp() // a key with no text: no find, and no operator over it
	}

	// An armed r takes this key's text as its character, a digit or a space included, and Enter as a
	// line break; a key with neither cancels it and is processed afresh.
	if c.keys.replArmed {
		count := c.keys.replCount
		c.keys.replArmed, c.keys.replCount = false, 0
		visual := c.keys.mode == ModeVisual || c.keys.mode == ModeVisualLine
		switch {
		case visual && k.Text != "" && k.Mods&nonTextMods == 0:
			c.visualOp(ActVisualReplace, count, firstCluster(k.Text)) // every character of the selection
			return true
		case visual:
			// Enter, or a key with no text: r is cancelled, Visual stays
		case k.Code == tui.KeyEnter && k.Mods&nonTextMods == 0:
			c.replaceChars("\n", count)
			return true
		case k.Text != "" && k.Mods&nonTextMods == 0:
			c.replaceChars(firstCluster(k.Text), count)
			return true
		}
	}

	// An armed text object (i or a, after an operator or in Visual) takes this key's text as the
	// object's name. A key with no text cancels it, and the operator, and is processed afresh.
	if obj := c.keys.objArmed; obj != "" {
		op, count := c.keys.op, max(c.keys.opCount, 1)*max(c.keys.objCount, 1)
		c.keys.dropOp()
		c.keys.count = 0
		if k.Text != "" && k.Mods&nonTextMods == 0 {
			r, found := c.textObject(obj == "i", firstCluster(k.Text), count)
			switch {
			case !found:
			case op != ActUnbound:
				c.runObject(op, r)
			default:
				c.selectObject(r)
			}
			return true
		}
	}

	// An armed m, ' or ` takes this key's text as the mark's name (' and ` too, for the previous
	// jump); a key with no text cancels it, and an operator waiting on it, and is processed afresh.
	if armed := c.keys.markArmed; armed != ActUnbound {
		c.keys.markArmed = ActUnbound
		if k.Text != "" && k.Mods&nonTextMods == 0 {
			c.markKey(armed, []rune(k.Text)[0])
			return true
		}
		c.keys.dropOp()
	}

	// An armed q or @ takes this key's text as the register's name; a key with none cancels it.
	if armed := c.keys.macroArmed; armed != ActUnbound {
		count := c.keys.macroCount
		c.keys.macroArmed, c.keys.macroCount = ActUnbound, 0
		if k.Text != "" && k.Mods&nonTextMods == 0 {
			if name := []rune(k.Text)[0]; armed == ActMacroRecord {
				c.macroRecordKey(name)
			} else {
				c.macroPlay(name, count)
			}
			return true
		}
	}

	// An armed " takes this key's text as the register's name ("a, "A, "0, "+, "_); a key that
	// names no register cancels it and is processed afresh. A count typed before it is kept (3"ayy).
	if c.keys.regArmed {
		c.keys.regArmed = false
		if k.Text != "" && k.Mods&nonTextMods == 0 {
			if name := []rune(k.Text)[0]; validRegister(name) {
				c.keys.reg, c.keys.regFresh = name, true
				return true
			}
		}
	}

	// Count accumulation: 1-9 always; 0 only extends an existing count. Clamp BEFORE assignment
	// so the cap is a hard ceiling.
	if !ctrl && k.Text != "" {
		r := []rune(k.Text)[0]
		if r >= '1' && r <= '9' || (r == '0' && c.keys.count > 0) {
			c.keys.pendingAct = ActUnbound
			c.keys.count = min(c.keys.count*10+int(r-'0'), 1_000_000)
			if n := len(c.dot.atoms); c.dot.on && n > 0 {
				c.dot.atoms[n-1].count = true // N. replaces it
			}
			return true
		}
	}

	// A key carrying a COMMAND modifier other than Ctrl is not this core's to consume, and must
	// bubble to the host.
	//
	// KeyChord identity is (Mode, Code, Ctrl) — Alt is not part of it. So without this check
	// Alt+h built the SAME chord as plain h and was swallowed as a motion, which silently denied
	// the host every Alt binding while looking like the key had simply done nothing. Found from
	// autodb, which needs Alt+h/j/k/l for pane motion precisely because a browser keeps Ctrl-L for
	// its address bar and will not surrender it.
	//
	// Ctrl is excluded from this rule because Ctrl IS part of a chord (Ctrl-r is redo), so a Ctrl
	// key the keymap does not bind already falls through below.
	if k.Mods&(tui.ModAlt|tui.ModSuper|tui.ModMeta|tui.ModHyper) != 0 {
		return false
	}

	code := k.Code
	if k.Text != "" && k.Mods&nonTextMods == 0 {
		code = []rune(k.Text)[0] // shifted letters arrive via Text ("G")
	}
	kc := KeyChord{Mode: modeClass(c.keys.mode), Code: code, Ctrl: ctrl}

	if c.keys.unbound[kc] {
		c.keys.count = 0
		c.keys.pendingAct = ActUnbound
		c.keys.dropOp()
		return false
	}

	// The g prefix, keyed by the ARMING CHORD, so a rebound prefix completes on its own chord
	// rather than a hard-coded rune: the same chord again is gg, e and E are ge and gE; any other
	// key clears it and is processed normally. Under a pending operator the completion is the
	// operator's motion (dgg, dge).
	if c.keys.pendingAct != ActUnbound {
		chord := c.keys.pendingChord
		op := c.keys.op
		hadCount := c.keys.pendingCount > 0 || c.keys.opCount > 0
		count := max(c.keys.pendingCount, 1) * max(c.keys.opCount, 1)
		c.keys.pendingAct = ActUnbound
		c.keys.pendingCount = 0
		back := ActUnbound
		if !kc.Ctrl && kc.Code == 'e' {
			back = ActWordEndBack
		} else if !kc.Ctrl && kc.Code == 'E' {
			back = ActWORDEndBack
		}
		caseOp := ActUnbound
		if !kc.Ctrl {
			caseOp = caseOpOf(kc.Code)
		}
		switch {
		case caseOp != ActUnbound && op == caseOp: // g~g~, gugu, gUgU: the doubled form
			c.keys.dropOp()
			c.runOp(op, taPos{min(b.ln+count-1, len(b.lines)-1), 0}, linewise)
			return true
		case caseOp != ActUnbound && op == ActUnbound: // g~ gu gU: an operator, armed
			if c.readOnly {
				return false // refused, and the host's (#285's rule)
			}
			c.keys.op, c.keys.opChord, c.keys.opCount = caseOp, kc, 0
			if hadCount {
				c.keys.opCount = count
			}
			return true
		case !kc.Ctrl && kc.Code == 'J' && op == ActUnbound: // gJ
			if c.readOnly {
				return false
			}
			c.joinLines(count, true)
			return true
		case kc == chord && op != ActUnbound:
			c.keys.dropOp()
			c.runOp(op, c.gotoLineTarget(count, hadCount), linewise) // [count]dgg
			return true
		case kc == chord:
			c.goToLine(hadCount, count, false) // [count]gg
			return true
		case back != ActUnbound && op != ActUnbound:
			c.keys.dropOp()
			c.opMotion(op, back, count, hadCount) // dge
			return true
		case back != ActUnbound:
			c.move(back, count)
			return true
		}
		c.keys.dropOp()
		// Fall through: reprocess this key from scratch (count consumed).
	}

	// i and a name a text object after an operator (diw), and in Visual (viw): the next key says
	// which. Neither is Visual's own key, so it is read before the keymap's answer.
	visual := c.keys.mode == ModeVisual || c.keys.mode == ModeVisualLine
	if !kc.Ctrl && (kc.Code == 'i' || kc.Code == 'a') && (c.keys.op != ActUnbound || visual) {
		c.keys.objArmed, c.keys.objCount = string(kc.Code), c.keys.count
		c.keys.count = 0
		return true
	}

	// The doubled form (dd, cc, yy, >>, g~~, guu, gUU) is the arming chord again, whatever that
	// chord means alone: gU's U is no key of its own, and must still complete gUU.
	if op := c.keys.op; op != ActUnbound && kc == c.keys.opChord {
		total := max(c.keys.opCount, 1) * max(c.keys.count, 1)
		c.keys.count = 0
		c.keys.dropOp()
		c.runOp(op, taPos{min(b.ln+total-1, len(b.lines)-1), 0}, linewise)
		return true
	}

	act, bound := c.keys.keymap[kc]
	if !bound {
		c.keys.count = 0 // an unbound key cancels the pending count and bubbles
		c.keys.dropOp()
		return false
	}

	hadCount := c.keys.count > 0
	count := max(c.keys.count, 1)
	c.keys.count = 0

	// Under a pending operator: the arming chord again is the doubled, linewise form (dd, cc, yy);
	// g arms its second key and f t F T their character, keeping the operator; a motion runs it;
	// any other key cancels it and is processed afresh.
	if op := c.keys.op; op != ActUnbound {
		total := max(c.keys.opCount, 1) * count
		hadAny := hadCount || c.keys.opCount > 0
		switch {
		case kc == c.keys.opChord:
			c.keys.dropOp()
			c.runOp(op, taPos{min(b.ln+total-1, len(b.lines)-1), 0}, linewise)
			return true
		case act == ActGoPrefix:
			c.keys.pendingAct, c.keys.pendingChord, c.keys.pendingCount = act, kc, 0
			if hadCount {
				c.keys.pendingCount = count
			}
			return true
		case act == ActFindCharForward || act == ActFindCharBack || act == ActTillCharForward || act == ActTillCharBack:
			c.keys.findArmed, c.keys.findCount = act, total
			return true
		case act == ActMarkLine || act == ActMarkExact: // d'a, y`a: the mark's name completes it
			c.keys.markArmed = act
			return true
		case opMotion(act):
			c.keys.dropOp()
			c.opMotion(op, act, total, hadAny)
			return true
		}
		c.keys.dropOp()
	}

	switch act {
	case ActDeletePrefix, ActYankPrefix, ActChangePrefix, ActIndentPrefix, ActOutdentPrefix:
		if act != ActYankPrefix && c.readOnly {
			return false // refused, and the host's: a viewer never changes text (execAction's rule)
		}
		if act == ActYankPrefix && !c.reg.yankAllowed() {
			return true // consumed, refused: a yank-less core copies nothing
		}
		c.keys.op, c.keys.opChord, c.keys.opCount = act, kc, 0
		if hadCount {
			c.keys.opCount = count // multiplies the motion's (2d3w)
		}
		return true
	case ActGoPrefix:
		c.keys.pendingAct = act
		c.keys.pendingChord = kc
		c.keys.pendingCount = 0
		if hadCount {
			c.keys.pendingCount = count // preserved for the completion (5gg)
		}
		return true
	case ActGoBottom:
		c.goToLine(hadCount, count, true) // [count]G
		return true
	case ActFindCharForward, ActFindCharBack, ActTillCharForward, ActTillCharBack:
		c.keys.findArmed, c.keys.findCount = act, count // the next key's text completes it
		return true
	case ActReplaceChar, ActVisualReplace:
		if c.readOnly {
			return false // refused, and the host's
		}
		c.keys.replArmed, c.keys.replCount = true, count // the next key's text completes it
		return true
	case ActMatchPair:
		c.hist.close()
		if hadCount {
			c.percentLine(count) // N%
		} else {
			c.matchPair()
		}
		return true
	case ActHalfPageDown, ActHalfPageUp:
		c.hist.close()
		c.halfPage(act == ActHalfPageDown, hadCount, count)
		return true
	case ActRepeat:
		c.repeatLast(hadCount, count)
		return true
	case ActMacroRecord, ActMacroPlay:
		if c.readOnly {
			return false // a viewer's q and @ are the host's: a macro could change nothing there
		}
		if act == ActMacroRecord && c.macro.reg != 0 {
			c.macroStop()
			return true
		}
		c.keys.macroArmed, c.keys.macroCount = act, count
		return true
	case ActMarkSet, ActMarkLine, ActMarkExact:
		c.keys.markArmed = act // the next key's text names the mark
		return true
	case ActRegisterPrefix:
		c.keys.regArmed = true
		if hadCount {
			c.keys.count = count // 3"ayy: the count stays for the command
		}
		return true
	case ActUndo, ActRedo:
		c.dot.on = false // undoing is not a change . repeats
	}
	return c.execAction(act, count)
}
