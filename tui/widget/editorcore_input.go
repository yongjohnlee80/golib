package widget

import (
	"strings"

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
	return c.handleCommandKey(k)
}

// HandlePaste inserts pasted text as one atomic edit. In Visual it replaces the selection. A
// read-only core consumes and refuses it.
func (c *EditorCore) HandlePaste(text string) bool {
	ln, col := c.buf.ln, c.buf.col
	defer c.cursorMoved(ln, col)
	if c.readOnly {
		return true // a viewer never mutates (bracketed paste included)
	}
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
		b.ln, b.col = lo, 0
		b.insertText(text)
		c.clampNormal()
	default:
		b.insertText(text) // one atomic literal insertion
		if c.keys.mode != ModeInsert {
			c.clampNormal()
		}
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

	if c.listKey(k) {
		return true
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
			c.exitInsert()
			return true
		}
		if c.vAnchor != (taPos{}) {
			c.vAnchor = taPos{}
			c.markDirty()
			return true
		}
		return false
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
		c.beginGroup()
		b.insertText(k.Text)
		if strings.ContainsAny(k.Text, "})] \t") {
			c.alignClosing()
		}
		c.edited()
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
		hadPending := c.keys.count != 0 || c.keys.pendingAct != ActUnbound
		c.keys.count, c.keys.pendingAct = 0, ActUnbound
		if c.keys.mode == ModeVisual || c.keys.mode == ModeVisualLine {
			c.exitVisual()
			return true
		}
		// Normal mode with nothing pending: Esc is a vim no-op, so it BUBBLES. Consuming it here
		// made an Editor inside a modal float undismissable — the host never saw the key (autodb
		// M6: a read-only script viewer that Esc could not close).
		return hadPending
	}

	// Count accumulation: 1-9 always; 0 only extends an existing count. Clamp BEFORE assignment
	// so the cap is a hard ceiling.
	if !ctrl && k.Text != "" {
		r := []rune(k.Text)[0]
		if r >= '1' && r <= '9' || (r == '0' && c.keys.count > 0) {
			c.keys.pendingAct = ActUnbound
			c.keys.count = min(c.keys.count*10+int(r-'0'), 1_000_000)
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
		return false
	}

	// Double-key pending buffer, keyed by the ARMING CHORD, so a rebound prefix completes on its
	// own chord rather than a hard-coded rune: only the same chord again completes; any other key
	// clears the pending state and is processed normally.
	if c.keys.pendingAct != ActUnbound {
		act, chord := c.keys.pendingAct, c.keys.pendingChord
		hadCount := c.keys.pendingCount > 0
		count := max(c.keys.pendingCount, 1)
		c.keys.pendingAct = ActUnbound
		c.keys.pendingCount = 0
		if kc == chord {
			switch act {
			case ActDeletePrefix:
				if c.readOnly {
					return true // dd on a viewer: consumed, refused
				}
				c.deleteLines(b.ln, min(b.ln+count-1, len(b.lines)-1))
			case ActYankPrefix:
				if c.reg.yankAllowed() {
					text := strings.Join(b.lines[b.ln:min(b.ln+count-1, len(b.lines)-1)+1], "\n")
					c.yankSet(text, true)
					c.exportYank(text)
				}
			case ActGoPrefix:
				c.goToLine(hadCount, count, false) // [count]gg
			}
			return true
		}
		// Fall through: reprocess this key from scratch (count consumed).
	}

	act, bound := c.keys.keymap[kc]
	if !bound {
		c.keys.count = 0 // an unbound key cancels the pending count and bubbles
		return false
	}

	hadCount := c.keys.count > 0
	count := max(c.keys.count, 1)
	c.keys.count = 0

	switch act {
	case ActDeletePrefix, ActYankPrefix, ActGoPrefix:
		if act == ActYankPrefix && !c.reg.yankAllowed() {
			return true
		}
		c.keys.pendingAct = act
		c.keys.pendingChord = kc
		c.keys.pendingCount = 0
		if hadCount {
			c.keys.pendingCount = count // preserved for the completion (2dd, 5gg)
		}
		return true
	case ActGoBottom:
		c.goToLine(hadCount, count, true) // [count]G
		return true
	}
	return c.execAction(act, count)
}
