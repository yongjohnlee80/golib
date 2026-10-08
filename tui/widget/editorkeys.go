package widget

import (
	"sort"
	"time"
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
	unbound map[KeyChord]bool // explicitly unbound chords (via ActUnbound)
	// overlay is every host-supplied binding, kept apart from the profile's
	// base table so a keyset switch can replay it. A host that rebinds a key
	// means it for the editor, not for one profile of it: without this, the
	// binding would survive or vanish depending on the order the options ran
	// in, and would vanish outright on [Editor.SetKeyset].
	overlay Keymap

	// Normal/Visual command state.
	count        int      // pending count; 0 = none
	pendingAct   Action   // pending double-key prefix action; ActUnbound = none
	pendingChord KeyChord // the chord that armed it (completion = same chord)
	pendingCount int      // count captured when the prefix was armed

	// Escape chord (Insert mode).
	chord        []rune // exactly two runes, or nil = disabled
	pendingRune  rune   // held first chord rune; 0 = none
	chordCancel  func() // cancels the addressed tick
	chordTimeout time.Duration
}

// newKeyDispatch is the default: the Vim keymap, modal, the "jk" escape chord.
func newKeyDispatch() keyDispatch {
	return keyDispatch{
		keyset:       KeysetVim,
		modal:        true,
		keymap:       DefaultKeymap(),
		unbound:      make(map[KeyChord]bool),
		chord:        []rune{'j', 'k'},
		chordTimeout: 300 * time.Millisecond,
	}
}

// applyKeyset installs a profile's base tables and replays the host's keymap
// overlay on top. Mode is NOT decided here: construction and a live switch
// want different transitions, so each caller sets it.
func (k *keyDispatch) applyKeyset(ks Keyset) {
	switch normalizeKeyset(ks) {
	case KeysetNano:
		k.keyset, k.modal, k.keymap = KeysetNano, false, NanoKeymap()
	case KeysetStandard:
		k.keyset, k.modal, k.keymap = KeysetStandard, false, StandardKeymap()
	default:
		k.keyset, k.modal, k.keymap = KeysetVim, true, VimKeymap()
	}
	k.unbound = make(map[KeyChord]bool)
	k.applyOverlay(k.overlay)
}

// applyOverlay folds host bindings onto the live table. Entries are validated
// by the caller that first accepted them, so a profile switch cannot panic on
// an overlay the editor already took: validateKeymapEntry checks the chord's
// mode and the action, neither of which depends on the keyset.
func (k *keyDispatch) applyOverlay(ov Keymap) {
	for kc, act := range ov {
		if act == ActUnbound {
			delete(k.keymap, kc)
			if k.unbound == nil {
				k.unbound = make(map[KeyChord]bool)
			}
			k.unbound[kc] = true
			continue
		}
		k.keymap[kc] = act
		delete(k.unbound, kc)
	}
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

// dropPending forgets a count and a double-key prefix being typed: input addressed to a moment
// that has passed (a menu action, a keyset switch, the focus leaving).
func (k *keyDispatch) dropPending() {
	k.count, k.pendingCount = 0, 0
	k.pendingAct, k.pendingChord = ActUnbound, KeyChord{}
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
