package widget

import (
	"strconv"

	"github.com/yongjohnlee80/golib/tui"
)

// VIM REPEAT — . replays the last change. A Normal-mode command is recorded as its keys from where
// nothing was pending (each key marked when it was taken as a count digit); it is a change when the
// text's revision moved or Insert began. An Insert it began is recorded as ATOMS taken where the
// changes reach the buffer, not as keys, since keys there have timing (the jk escape chord commits
// a held j on a tick): typed text after the chord resolved, pasted text, and the non-text keys
// Insert handled (Enter, Backspace, Delete, Tab, and the non-jump moves). A jump in Insert (PageUp,
// PageDown, a press) splits the change: what follows is an Insert of its own. A replay is one undo
// step (the history holds its group), and N. drops the recorded count digits for N.

// dotKind is what a recorded atom is.
type dotKind int

const (
	dotKey    dotKind = iota // a key, replayed through HandleKey
	dotTyped                 // text Insert committed as typed, replayed through the typed path
	dotPasted                // text a paste inserted, replayed literally
)

type dotAtom struct {
	kind  dotKind
	key   tui.KeyEvent
	text  string
	count bool // a key taken as a count digit
}

// dotRecorder is the change being recorded and the last one.
type dotRecorder struct {
	on        bool      // a command is being recorded
	atoms     []dotAtom // its keys and atoms so far
	rev       uint64    // the text's revision when it began
	inInsert  bool      // it began Insert: recorded until Insert ends
	last      []dotAtom // the last change, which . replays
	replaying bool
}

// commandPending reports a Normal-mode command still being typed.
func (c *EditorCore) commandPending() bool {
	return c.opPending() || c.keys.regArmed || c.keys.reg != 0
}

// opPending reports a command's own state still pending: a count, an operator, a prefix, an armed
// key. A register named alone completes nothing, so it is not counted here.
func (c *EditorCore) opPending() bool {
	k := &c.keys
	return k.count != 0 || k.op != ActUnbound || k.pendingAct != ActUnbound || k.findArmed != ActUnbound ||
		k.objArmed != "" || k.replArmed
}

// dotStart begins recording a Normal-mode command when nothing is pending, before its first key.
func (c *EditorCore) dotStart() {
	d := &c.dot
	if d.replaying || c.keys.mode != ModeNormal || !c.keys.modal || c.commandPending() {
		return
	}
	*d = dotRecorder{on: true, rev: c.rev, last: d.last}
}

// dotEnd closes a command once nothing is pending: a change (the revision moved) is the last one; a
// command that began Insert goes on being recorded; anything else is forgotten.
func (c *EditorCore) dotEnd() {
	d := &c.dot
	if !d.on || d.inInsert || c.commandPending() {
		return
	}
	switch {
	case c.keys.mode == ModeInsert:
		d.inInsert = true
	case c.keys.mode == ModeNormal && c.rev != d.rev:
		d.last, d.on = d.atoms, false
	default:
		d.on = false
	}
}

// dotInsert reports an Insert session being recorded.
func (c *EditorCore) dotInsert() bool { return c.dot.on && c.dot.inInsert && !c.dot.replaying }

func (c *EditorCore) dotTypedText(s string) {
	if c.dotInsert() {
		c.dot.atoms = append(c.dot.atoms, dotAtom{kind: dotTyped, text: s})
	}
}

func (c *EditorCore) dotPastedText(s string) {
	if c.dotInsert() {
		c.dot.atoms = append(c.dot.atoms, dotAtom{kind: dotPasted, text: s})
	}
}

func (c *EditorCore) dotInsertKey(k tui.KeyEvent) {
	if c.dotInsert() {
		c.dot.atoms = append(c.dot.atoms, dotAtom{kind: dotKey, key: k})
	}
}

// dotJump splits the Insert being recorded: what came before is the last change when it changed
// the text, and what follows is an Insert of its own, at the cursor.
func (c *EditorCore) dotJump() {
	if c.dotInsert() {
		c.dotKeepSegment()
		c.dot.atoms = []dotAtom{{kind: dotKey, key: tui.KeyEvent{Code: 'i', Text: "i"}}}
		c.dot.rev = c.rev
	}
}

// dotInsertEnded closes the Insert being recorded: the change it ends is the last one, when it
// changed the text (i<Esc>, or a jump with nothing after it, leaves the last change as it was).
func (c *EditorCore) dotInsertEnded() {
	if c.dotInsert() {
		c.dotKeepSegment()
		c.dot.on, c.dot.inInsert = false, false
	}
}

// dotKeepSegment makes the segment being recorded the last change, if it moved the text.
func (c *EditorCore) dotKeepSegment() {
	if c.rev != c.dot.rev {
		c.dot.last = c.dot.atoms
	}
}

// dotDrop splits an Insert being recorded when a press moves the cursor: a press is a jump. (A
// command half-typed needs nothing: the next key, with nothing pending, records afresh.)
func (c *EditorCore) dotDrop() { c.dotJump() }

// repeatLast is .: the last change again, its count replaced by count when one was typed, as one
// undo step.
func (c *EditorCore) repeatLast(hadCount bool, count int) {
	d := &c.dot
	d.on = false
	if d.replaying || len(d.last) == 0 {
		return
	}
	atoms := d.last
	if hadCount {
		var digits []dotAtom
		for _, r := range strconv.Itoa(count) {
			digits = append(digits, dotAtom{kind: dotKey, key: tui.KeyEvent{Code: r, Text: string(r)}, count: true})
		}
		kept := digits
		for _, a := range atoms {
			if !a.count {
				kept = append(kept, a)
			}
		}
		atoms = kept
	}
	d.replaying = true
	rev := c.rev
	c.hist.hold(c.snapshot)
	for _, a := range atoms {
		switch a.kind {
		case dotKey:
			c.HandleKey(a.key)
		case dotTyped:
			c.typeText(a.text)
		case dotPasted:
			c.HandlePaste(a.text)
		}
	}
	if c.keys.mode == ModeInsert && c.keys.modal {
		c.exitInsert()
	}
	c.hist.release(c.rev != rev)
	d.replaying = false
}
