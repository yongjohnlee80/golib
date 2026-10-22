package widget

import (
	"sort"
	"time"

	tuikey "github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// KEY DISPATCH — which key means which action, and what is pending between keys: the mode, the
// keyset's base table with the host's overlay replayed on top, the chords explicitly unbound, a
// count or a double-key prefix being typed, and the escape chord's held first rune. It decides
// nothing about the text: the editor routes each key through it and acts on what it resolves.

type keyDispatch struct {
	mode    EditorMode
	keyset  Keyset // active editing & keymap profile
	modal   bool   // true = Vim tripartite state machine; false = modeless editor
	keymap  Keymap
	late    map[KeyChord]bool // chords bound to editing commands, run late in Insert (keyDecl.late)
	unbound map[KeyChord]bool // explicitly unbound chords (via ActUnbound)
	// overlay is every host-supplied binding, kept apart from the profile's
	// base table so a keyset switch can replay it. A host that rebinds a key
	// means it for the editor, not for one profile of it: without this, the
	// binding would survive or vanish depending on the order the options ran
	// in, and would vanish outright on [Editor.SetKeyset].
	overlay Keymap

	// Normal/Visual command state.
	count        int    // pending count; 0 = none
	pendingAct   Action // pending double-key prefix action; ActUnbound = none
	pendingCount int    // count captured when the prefix was armed
	// findArmed is an f, F, t or T waiting for its character; findCount its count. lastFind is the
	// last one completed, which ; and , repeat (a mode change keeps it, as in Vim).
	findArmed Action
	findCount int
	lastFind  *charFind
	// op is a pending operator (d, c, y; ActUnbound for none), opChord the chord that armed it
	// (dd completes on it again) and opCount its count. objArmed is "i" or "a" after an operator,
	// or in Visual: the next key names a text object, objCount the count typed before it.
	op       Action
	opChord  KeyChord
	opCount  int
	objArmed string
	objCount int
	// replArmed is an r waiting for its character, replCount its count.
	replArmed bool
	replCount int
	// regArmed is a " waiting for the register's name; reg is the register the command names (0:
	// none), kept until the command completes, as Vim's clearop drops it. regFresh marks the key
	// that just named it, which completes no command.
	regArmed bool
	reg      rune
	regFresh bool
	// markArmed is an m, ' or ` waiting for the mark's name (ActUnbound: none); a pending operator
	// stays, for d'a.
	markArmed Action
	// macroArmed is a q or @ waiting for the register's name (ActUnbound: none); macroCount is
	// @'s count.
	macroArmed Action
	macroCount int

	// builtFrom is the key map the table was built from (nil: every default), so the core notices
	// the App installing another one and rebuilds.
	builtFrom *keymap.Map

	// Escape chord (Insert mode).
	chord        []rune // exactly two runes, or nil = disabled
	pendingRune  rune   // held first chord rune; 0 = none
	chordCancel  func() // cancels the addressed tick
	chordTimeout time.Duration
}

// newKeyDispatch is the default: the Vim keymap, modal, the "jk" escape chord.
func newKeyDispatch() keyDispatch {
	table, late := keymapFrom(nil, KeysetVim)
	return keyDispatch{
		keyset:       KeysetVim,
		modal:        true,
		keymap:       table,
		late:         late,
		unbound:      make(map[KeyChord]bool),
		chord:        []rune{'j', 'k'},
		chordTimeout: 300 * time.Millisecond,
	}
}

// applyKeyset installs a profile's table as m binds it (keymapFrom; nil m: every default) and replays
// the host's keymap overlay on top, so a host's Go overlay still wins over a key-mapping scheme. Mode
// is NOT decided here: construction and a live switch want different transitions, so each caller
// sets it.
func (k *keyDispatch) applyKeyset(ks Keyset, m *keymap.Map) {
	switch normalizeKeyset(ks) {
	case KeysetNano:
		k.keyset, k.modal = KeysetNano, false
	case KeysetStandard:
		k.keyset, k.modal = KeysetStandard, false
	default:
		k.keyset, k.modal = KeysetVim, true
	}
	k.keymap, k.late = keymapFrom(m, k.keyset)
	k.builtFrom = m
	k.unbound = make(map[KeyChord]bool)
	k.applyOverlay(k.overlay)
}

// applyOverlay folds host bindings onto the live table. Entries are validated
// by the caller that first accepted them, so a profile switch cannot panic on
// an overlay the editor already took: validateKeymapEntry checks the chord's
// mode and the action, neither of which depends on the keyset.
//
// An overlay chord on a functional key without Shift ({Code: KeyDown}) is replayed onto its Shift
// twin as well, unless the overlay names the twin itself: before chords had Shift, the editor read
// Shift+Down as Down, so the old literal meant both, and still does.
func (k *keyDispatch) applyOverlay(ov Keymap) {
	set := func(kc KeyChord, act Action) {
		delete(k.late, kc) // a host's binding is early, whatever its action
		if act == ActUnbound {
			delete(k.keymap, kc)
			if k.unbound == nil {
				k.unbound = make(map[KeyChord]bool)
			}
			k.unbound[kc] = true
			return
		}
		k.keymap[kc] = act
		delete(k.unbound, kc)
	}
	for kc, act := range ov {
		set(kc, act)
		if twin := kc; functionalCode(kc.Code) && !kc.Shift {
			twin.Shift = true
			if _, named := ov[twin]; !named {
				set(twin, act)
			}
		}
	}
}

// lookupSeq is the chord s is in class and what it is bound to, trying key.Candidates in order (a
// legacy terminal's Ctrl+Shift+R is Ctrl+R): the first candidate the table binds or explicitly
// unbinds wins. With neither, kc is s's own chord.
func (k *keyDispatch) lookupSeq(class EditorMode, s tuikey.Seq) (kc KeyChord, act Action, bound, unbound bool) {
	for _, cand := range tuikey.Candidates(s) {
		c := chordOfSeq(class, cand)
		if k.unbound[c] {
			return c, ActUnbound, false, true
		}
		if a, ok := k.keymap[c]; ok {
			return c, a, true, false
		}
	}
	return chordOfSeq(class, s), ActUnbound, false, false
}

// lookup is the action bound to kc; an explicitly unbound chord is none.
func (k *keyDispatch) lookup(kc KeyChord) (Action, bool) {
	if k.unbound[kc] {
		return ActUnbound, false
	}
	act, ok := k.keymap[kc]
	return act, ok
}

// chordsFor is every chord bound to act, sorted by mode, then by its spelling.
func (k *keyDispatch) chordsFor(act Action) []KeyChord {
	var chords []KeyChord
	for kc, a := range k.keymap {
		if a == act {
			chords = append(chords, kc)
		}
	}
	sort.Slice(chords, func(i, j int) bool {
		if chords[i].Mode != chords[j].Mode {
			return chords[i].Mode < chords[j].Mode
		}
		return chords[i].String() < chords[j].String()
	})
	return chords
}

// dropPending forgets a command being typed — a count, a double-key prefix, an armed f or r, an
// operator, and a key waiting for a name: input addressed to a moment that has passed (a menu
// action, a keyset switch, an overlay, a press, the focus leaving).
func (k *keyDispatch) dropPending() {
	k.count, k.pendingCount = 0, 0
	k.pendingAct = ActUnbound
	k.findArmed, k.findCount = ActUnbound, 0
	// A key waiting for a name — a register's, a mark's, a macro's — is pending too: left armed,
	// it would take the next key as that name, whatever ended the command around it.
	k.regArmed, k.reg = false, 0
	k.markArmed = ActUnbound
	k.macroArmed, k.macroCount = ActUnbound, 0
	k.dropOp()
}

// dropOp forgets a pending operator and a pending text object.
func (k *keyDispatch) dropOp() {
	k.op, k.opChord, k.opCount = ActUnbound, KeyChord{}, 0
	k.objArmed, k.objCount = "", 0
	k.replArmed, k.replCount = false, 0
}

// takeRune is the escape chord's held first rune, cleared with its timer; ok is false with none.
// The caller commits it as text.
func (k *keyDispatch) takeRune() (r rune, ok bool) {
	if k.pendingRune == 0 {
		return 0, false
	}
	r, k.pendingRune = k.pendingRune, 0
	if k.chordCancel != nil {
		k.chordCancel()
		k.chordCancel = nil
	}
	return r, true
}
