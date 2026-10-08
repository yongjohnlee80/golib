package widget

import "testing"

// A keyset switch installs the profile's table and replays the host's overlay on it: a rebound
// key means it for the editor, and an unbound one stays unbound, whatever the profile.
func TestKeyDispatchKeysetReplaysTheOverlay(t *testing.T) {
	k := newKeyDispatch()
	undo := KeyChord{Mode: ModeInsert, Code: 'q', Ctrl: true}
	gone := KeyChord{Mode: ModeNormal, Code: 'x'}
	k.overlay = Keymap{undo: ActUndo, gone: ActUnbound}
	k.applyOverlay(k.overlay)
	for _, ks := range []Keyset{KeysetNano, KeysetStandard, KeysetVim} {
		k.applyKeyset(ks)
		if k.keyset != ks || k.modal != (ks == KeysetVim) {
			t.Fatalf("%v: keyset %v, modal %v", ks, k.keyset, k.modal)
		}
		if act, ok := k.lookup(undo); !ok || act != ActUndo {
			t.Errorf("%v: the host's Ctrl+Q is %v,%v; want ActUndo", ks, act, ok)
		}
		if _, ok := k.lookup(gone); ok {
			t.Errorf("%v: the host's unbound x is bound", ks)
		}
	}
}

func TestKeyDispatchChordsForAreSorted(t *testing.T) {
	k := newKeyDispatch()
	k.keymap = Keymap{
		{Mode: ModeVisual, Code: 'y'}:             ActCopy,
		{Mode: ModeNormal, Code: 'y'}:             ActCopy,
		{Mode: ModeNormal, Code: 'c', Ctrl: true}: ActCopy,
		{Mode: ModeNormal, Code: 'p'}:             ActPaste,
	}
	got := k.chordsFor(ActCopy)
	if len(got) != 3 || got[0].Mode != ModeNormal || got[2].Mode != ModeVisual || got[0].String() > got[1].String() {
		t.Fatalf("chords for ActCopy %v; want Normal's in spelling order, then Visual's", got)
	}
}

func TestKeyDispatchPendingInput(t *testing.T) {
	k := newKeyDispatch()
	k.count, k.pendingCount, k.pendingAct, k.pendingChord = 3, 2, ActDeletePrefix, KeyChord{Code: 'd'}
	k.dropPending()
	if k.count != 0 || k.pendingCount != 0 || k.pendingAct != ActUnbound || k.pendingChord != (KeyChord{}) {
		t.Fatalf("after dropPending: %+v", k)
	}
	if _, ok := k.takeRune(); ok {
		t.Fatal("a rune taken with none held")
	}
	cancelled := false
	k.pendingRune, k.chordCancel = 'j', func() { cancelled = true }
	if r, ok := k.takeRune(); !ok || r != 'j' || !cancelled || k.pendingRune != 0 || k.chordCancel != nil {
		t.Fatalf("takeRune: %q %v, timer cancelled %v, left %q", r, ok, cancelled, k.pendingRune)
	}
}
