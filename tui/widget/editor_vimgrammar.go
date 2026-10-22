package widget

import "github.com/yongjohnlee80/golib/tui"

// THE VIM KEY GRAMMAR: a key in Normal or Visual mode, read as Vim reads it. handleCommandKey runs
// its stages in order: Esc, then the key an armed command waits on (f t F T, r, a text object, a
// mark, a macro, a register), a count's digit, the g prefix's second key, a text object's i or a,
// an operator's doubled form, a bound key under a pending operator, and last the key's own action.

// handleCommandKey: Normal/Visual dispatch — digits, the double-key pending buffer, then the
// keymap. Unbound keys clear pending state and bubble.
func (c *EditorCore) handleCommandKey(k tui.KeyEvent) bool {
	if k.Code == tui.KeyEscape {
		return c.commandEscape()
	}
	// a key an armed command waits on is that command's
	for _, take := range []func(tui.KeyEvent) bool{c.takeFindChar, c.takeReplaceChar, c.takeObjectName,
		c.takeMarkName, c.takeMacroName, c.takeRegisterName} {
		if take(k) {
			return true
		}
	}
	// a pending key's sub-scope first: the g prefix's second key, or the i or a that names a text
	// object, however the key map binds them, before the root table and its Alt and Super bubbling
	if done, handled := c.pendingSubScope(k); done {
		return handled
	}
	if c.takeCountDigit(k) {
		return true
	}

	// A key with Hyper or Meta held matches nothing, and bubbles.
	s, ok := c.seqOf(k)
	if !ok {
		return false
	}
	kc, act, bound, unbound := c.keys.lookupSeq(modeClass(c.keys.mode), s)

	// An ALT OR SUPER CHORD NO KEY MAP BINDS is not this core's to consume, and must bubble to the
	// host, untouched by whatever is pending.
	//
	// Before chords carried Alt, Alt+h built the SAME chord as plain h and was swallowed as a
	// motion, which silently denied the host every Alt binding while looking like the key had
	// simply done nothing. Found from autodb, which needs Alt+h/j/k/l for pane motion precisely
	// because a browser keeps Ctrl-L for its address bar and will not surrender it. A chord now
	// carries Alt and Super, so a key map may bind one; one it does not bind still bubbles here.
	if (kc.Alt || kc.Super) && !bound {
		return false
	}

	if unbound {
		c.keys.count = 0
		c.keys.pendingAct = ActUnbound
		c.keys.dropOp()
		return false
	}

	// A key after g that the g sub-scope did not take (pendingSubScope): the prefix and any operator
	// go, and the key is processed as any key is.
	if c.keys.pendingAct != ActUnbound {
		c.completeGoPrefix(goNone, kc) // a key the g sub-scope does not bind: the prefix goes
	}

	// i and a name a text object after an operator (diw), and in Visual (viw): the next key says
	// which. Neither is Visual's own key, so it is read before the keymap's answer.
	// (after a g the sub-scope did not take, as before the sub-scopes: Visual's g then i)
	if around, ok := c.objectOf(k); ok && (c.keys.op != ActUnbound || c.visual()) {
		c.armObject(around)
		return true
	}

	// The doubled form (dd, cc, yy, >>, g~~, guu, gUU) is the arming chord again, whatever that
	// chord means alone: gU's U is no key of its own, and must still complete gUU.
	if op := c.keys.op; op != ActUnbound && kc == c.keys.opChord {
		total := max(c.keys.opCount, 1) * max(c.keys.count, 1)
		c.keys.count = 0
		c.keys.dropOp()
		c.runLines(op, total)
		return true
	}

	if !bound {
		c.keys.count = 0 // an unbound key cancels the pending count and bubbles
		c.keys.dropOp()
		return false
	}

	hadCount := c.keys.count > 0
	count := max(c.keys.count, 1)
	c.keys.count = 0

	if c.continueOperator(act, kc, count, hadCount) {
		return true
	}
	return c.commandAction(act, kc, count, hadCount)
}

// continueOperator is a bound key under a pending operator, its doubled form taken already: g arms
// its second key and f t F T their character, keeping the operator; a motion runs it; any other
// key cancels it and is processed afresh. It reports whether it took the key.
func (c *EditorCore) continueOperator(act Action, kc KeyChord, count int, hadCount bool) bool {
	op := c.keys.op
	if op == ActUnbound {
		return false
	}
	total := max(c.keys.opCount, 1) * count
	hadAny := hadCount || c.keys.opCount > 0
	switch {
	case act == ActGoPrefix:
		c.armGoPrefix(count, hadCount)
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
	return false
}

// commandAction is a bound key with no command pending: an operator, a prefix or a command that
// waits on the next key is armed; any other action runs, count times.
func (c *EditorCore) commandAction(act Action, kc KeyChord, count int, hadCount bool) bool {
	switch act {
	case ActDeletePrefix, ActYankPrefix, ActChangePrefix, ActIndentPrefix, ActOutdentPrefix:
		if act != ActYankPrefix && c.readOnly {
			return false // refused, and the host's: a viewer never changes text (execAction's rule)
		}
		if act == ActYankPrefix && !c.reg.yankAllowed() {
			return true // consumed, refused: a yank-less core copies nothing
		}
		c.armOperator(act, kc, count, hadCount)
		return true
	case ActGoPrefix:
		c.armGoPrefix(count, hadCount)
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
	case ActJumpBack, ActJumpForward:
		c.jumpStep(act == ActJumpBack, count)
		return true
	case ActStarForward, ActStarBack:
		c.starSearch(act == ActStarBack, true, count)
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

// completeGoPrefix is the key after an armed g prefix, g the command it is (goNone for a key the g
// sub-scope does not bind): gg goes to a line, ge and gE back to a word's end, under a pending
// operator the operator's motion (dgg, dge). done reports whether the key was the prefix's, and
// handled whether it is consumed; any other key clears the prefix and the operator, and is
// processed afresh, its count consumed. kc is the key's chord, which a case operator it arms
// keeps for its doubled form (gUU).
func (c *EditorCore) completeGoPrefix(g goKey, kc KeyChord) (done, handled bool) {
	op := c.keys.op
	hadCount := c.keys.pendingCount > 0 || c.keys.opCount > 0
	count := max(c.keys.pendingCount, 1) * max(c.keys.opCount, 1)
	c.keys.pendingAct = ActUnbound
	c.keys.pendingCount = 0
	back := ActUnbound
	switch g {
	case goWordEndBack:
		back = ActWordEndBack
	case goWORDEndBack:
		back = ActWORDEndBack
	}
	caseOp := caseOpOfGo(g)
	rows := g == goRowDown || g == goRowUp
	switch {
	case caseOp != ActUnbound && op == caseOp: // g~g~, gugu, gUgU: the doubled form
		c.keys.dropOp()
		c.runLines(op, count)
		return true, true
	case caseOp != ActUnbound && op == ActUnbound: // g~ gu gU: an operator, armed
		if c.readOnly {
			return true, false // refused, and the host's (#285's rule)
		}
		c.armOperator(caseOp, kc, count, hadCount)
		return true, true
	case rows && op != ActUnbound: // dgj ygk
		c.keys.dropOp()
		if to, moved := c.rowTarget(g == goRowDown, count); moved {
			c.runOp(op, to, exclusive)
		}
		return true, true
	case rows: // gj gk
		c.rowMove(g == goRowDown, count)
		return true, true
	case (g == goStarPartial || g == goHashPartial) && op == ActUnbound: // g* g#
		c.starSearch(g == goHashPartial, false, count)
		return true, true
	case g == goJoinRaw && op == ActUnbound: // gJ
		if c.readOnly {
			return true, false
		}
		c.joinLines(count, true)
		return true, true
	case g == goLine && op != ActUnbound:
		c.keys.dropOp()
		c.runOp(op, c.gotoLineTarget(count, hadCount), linewise) // [count]dgg
		return true, true
	case g == goLine:
		c.goToLine(hadCount, count, false) // [count]gg
		return true, true
	case back != ActUnbound && op != ActUnbound:
		c.keys.dropOp()
		c.opMotion(op, back, count, hadCount) // dge
		return true, true
	case back != ActUnbound:
		c.move(back, count)
		return true, true
	}
	c.keys.dropOp()
	return false, false
}

// pendingSubScope is a pending key looked up in its sub-scope (editor_vimsubscopes.go): after an
// armed g, the g commands; after an operator or in Visual, the text-object extents. done reports
// whether the sub-scope took the key, handled whether it is consumed; a key it does not bind goes
// on as any key does.
func (c *EditorCore) pendingSubScope(k tui.KeyEvent) (done, handled bool) {
	if c.keys.pendingAct != ActUnbound {
		g := c.goKeyOf(k)
		if g == goNone {
			return false, false
		}
		kc, _ := c.chordOf(k) // goKeyOf matched it, so it has one
		return c.completeGoPrefix(g, kc)
	}
	if c.keys.op != ActUnbound || c.visual() {
		if around, ok := c.objectOf(k); ok {
			c.armObject(around)
			return true, true
		}
	}
	return false, false
}

// armObject arms a text object's name, around or inner, with the count typed before it.
func (c *EditorCore) armObject(around bool) {
	c.keys.objArmed, c.keys.objCount = "i", c.keys.count
	if around {
		c.keys.objArmed = "a"
	}
	c.keys.count = 0
}

// runLines runs op over n lines from the caret's, linewise: the doubled form (dd, 3yy, g~g~).
func (c *EditorCore) runLines(op Action, n int) {
	b := &c.buf
	c.runOp(op, taPos{min(b.ln+n-1, len(b.textLines)-1), 0}, linewise)
}

// armOperator arms op, kc its chord; a count typed before it multiplies the motion's (2d3w).
func (c *EditorCore) armOperator(op Action, kc KeyChord, count int, hadCount bool) {
	c.keys.op, c.keys.opChord, c.keys.opCount = op, kc, 0
	if hadCount {
		c.keys.opCount = count
	}
}

// armGoPrefix arms the g prefix; a count typed before it is kept for the completion (5gg).
func (c *EditorCore) armGoPrefix(count int, hadCount bool) {
	c.keys.pendingAct, c.keys.pendingCount = ActGoPrefix, 0
	if hadCount {
		c.keys.pendingCount = count
	}
}

// commandEscape is Esc in Normal or Visual: what was pending is dropped, Visual ends, and in Normal
// with nothing pending the active find is cancelled; otherwise Esc bubbles.
func (c *EditorCore) commandEscape() bool {
	hadPending := c.commandPending()
	c.keys.dropPending()
	if c.visual() {
		c.exitVisual()
		return true
	}
	// Normal mode with nothing pending: Esc cancels the active find (its marks, n and N) when
	// there is one. Otherwise it is a vim no-op, so it BUBBLES: consuming it made an Editor
	// inside a modal float undismissable, since the host never saw the key.
	return hadPending || c.escapeFind()
}

// takeFindChar: an armed f, F, t or T takes this key's text as its character, whatever it is (a
// digit or a space included): the typed cluster, never the code. A key with no text, or one with a
// command modifier, cancels the find and is processed afresh.
// It reports whether it took the key.
func (c *EditorCore) takeFindChar(k tui.KeyEvent) bool {
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
	return false
}

// takeReplaceChar: an armed r takes this key's text as its character, a digit or a space included,
// and Enter as a line break; a key with neither cancels it and is processed afresh. It reports
// whether it took the key.
func (c *EditorCore) takeReplaceChar(k tui.KeyEvent) bool {
	if c.keys.replArmed {
		count := c.keys.replCount
		c.keys.replArmed, c.keys.replCount = false, 0
		visual := c.visual()
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
	return false
}

// takeObjectName: an armed text object (i or a, after an operator or in Visual) takes this key's
// text as the object's name. A key with no text cancels it, and the operator, and is processed
// afresh. It reports whether it took the key.
func (c *EditorCore) takeObjectName(k tui.KeyEvent) bool {
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
	return false
}

// takeMarkName: an armed m, ' or ` takes this key's text as the mark's name (' and ` too, for the
// previous jump); a key with no text cancels it, and an operator waiting on it, and is processed
// afresh. It reports whether it took the key.
func (c *EditorCore) takeMarkName(k tui.KeyEvent) bool {
	if armed := c.keys.markArmed; armed != ActUnbound {
		c.keys.markArmed = ActUnbound
		if k.Text != "" && k.Mods&nonTextMods == 0 {
			c.markKey(armed, []rune(k.Text)[0])
			return true
		}
		c.keys.dropOp()
	}
	return false
}

// takeMacroName: an armed q or @ takes this key's text as the register's name; a key with none
// cancels it. It reports whether it took the key.
func (c *EditorCore) takeMacroName(k tui.KeyEvent) bool {
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
	return false
}

// takeRegisterName: an armed " takes this key's text as the register's name ("a, "A, "0, "+, "_); a
// key that names no register cancels it and is processed afresh. A count typed before it is kept
// (3"ayy). It reports whether it took the key.
func (c *EditorCore) takeRegisterName(k tui.KeyEvent) bool {
	if c.keys.regArmed {
		c.keys.regArmed = false
		if k.Text != "" && k.Mods&nonTextMods == 0 {
			if name := []rune(k.Text)[0]; validRegister(name) {
				c.keys.reg, c.keys.regFresh = name, true
				return true
			}
		}
	}
	return false
}

// takeCountDigit takes a count's digit (countDigit: a digit key with no modifier held): 1-9 always,
// 0 only extending a count, capped at editorCountMax. It reports whether it took the key.
func (c *EditorCore) takeCountDigit(k tui.KeyEvent) bool {
	d, ok := countDigit(k)
	if !ok || !addCount(&c.keys.count, d, editorCountMax) {
		return false
	}
	c.keys.pendingAct = ActUnbound
	if n := len(c.dot.atoms); c.dot.on && n > 0 {
		c.dot.atoms[n-1].count = true // N. replaces it
	}
	return true
}
