package widget

import (
	"testing"
)

func TestEditorModeString(t *testing.T) {
	cases := []struct {
		mode EditorMode
		want string
	}{
		{ModeNormal, "NORMAL"},
		{ModeInsert, "INSERT"},
		{ModeVisual, "VISUAL"},
		{ModeVisualLine, "V-LINE"},
		{EditorMode(99), "?"},
	}
	for _, tc := range cases {
		if got := tc.mode.String(); got != tc.want {
			t.Errorf("Mode %d.String() = %q, want %q", tc.mode, got, tc.want)
		}
	}
}

func TestDefaultKeymapBindings(t *testing.T) {
	km := DefaultKeymap()
	if act, ok := km[KeyChord{Mode: ModeNormal, Code: 'j'}]; !ok || act != ActDown {
		t.Errorf("normal 'j' -> (%v, %v), want ActDown, true", act, ok)
	}
	if act, ok := km[KeyChord{Mode: ModeNormal, Code: 'k'}]; !ok || act != ActUp {
		t.Errorf("normal 'k' -> (%v, %v), want ActUp, true", act, ok)
	}
	if act, ok := km[KeyChord{Mode: ModeNormal, Code: 'i'}]; !ok || act != ActInsert {
		t.Errorf("normal 'i' -> (%v, %v), want ActInsert, true", act, ok)
	}
	if act, ok := km[KeyChord{Mode: ModeNormal, Code: 'd'}]; !ok || act != ActDeletePrefix {
		t.Errorf("normal 'd' -> (%v, %v), want ActDeletePrefix, true", act, ok)
	}
	if act, ok := km[KeyChord{Mode: ModeVisual, Code: 'y'}]; !ok || act != ActVisualYank {
		t.Errorf("visual 'y' -> (%v, %v), want ActVisualYank, true", act, ok)
	}
}

func TestNanoKeymapBindings(t *testing.T) {
	km := NanoKeymap()
	if act, ok := km[KeyChord{Mode: ModeInsert, Code: 'k', Ctrl: true}]; !ok || act != ActCut {
		t.Errorf("nano Ctrl+K -> (%v, %v), want ActCut, true", act, ok)
	}
	if act, ok := km[KeyChord{Mode: ModeInsert, Code: 'u', Ctrl: true}]; !ok || act != ActPaste {
		t.Errorf("nano Ctrl+U -> (%v, %v), want ActPaste, true", act, ok)
	}
	if act, ok := km[KeyChord{Mode: ModeInsert, Code: 'a', Ctrl: true}]; !ok || act != ActLineStart {
		t.Errorf("nano Ctrl+A -> (%v, %v), want ActLineStart, true", act, ok)
	}
	if act, ok := km[KeyChord{Mode: ModeInsert, Code: 'e', Ctrl: true}]; !ok || act != ActLineEnd {
		t.Errorf("nano Ctrl+E -> (%v, %v), want ActLineEnd, true", act, ok)
	}
}

func TestStandardKeymapBindings(t *testing.T) {
	km := StandardKeymap()
	if act, ok := km[KeyChord{Mode: ModeInsert, Code: 'z', Ctrl: true}]; !ok || act != ActUndo {
		t.Errorf("standard Ctrl+Z -> (%v, %v), want ActUndo, true", act, ok)
	}
	if act, ok := km[KeyChord{Mode: ModeInsert, Code: 'y', Ctrl: true}]; !ok || act != ActRedo {
		t.Errorf("standard Ctrl+Y -> (%v, %v), want ActRedo, true", act, ok)
	}
	if act, ok := km[KeyChord{Mode: ModeInsert, Code: 'c', Ctrl: true}]; !ok || act != ActCopy {
		t.Errorf("standard Ctrl+C -> (%v, %v), want ActCopy, true", act, ok)
	}
	if act, ok := km[KeyChord{Mode: ModeInsert, Code: 'v', Ctrl: true}]; !ok || act != ActPaste {
		t.Errorf("standard Ctrl+V -> (%v, %v), want ActPaste, true", act, ok)
	}
	if act, ok := km[KeyChord{Mode: ModeInsert, Code: 'x', Ctrl: true}]; !ok || act != ActCut {
		t.Errorf("standard Ctrl+X -> (%v, %v), want ActCut, true", act, ok)
	}
	if act, ok := km[KeyChord{Mode: ModeInsert, Code: 'a', Ctrl: true}]; !ok || act != ActSelectAll {
		t.Errorf("standard Ctrl+A -> (%v, %v), want ActSelectAll, true", act, ok)
	}
}

func TestKeymapOverlayUnbind(t *testing.T) {
	chord := KeyChord{Mode: ModeNormal, Code: 'j'}
	overlay := Keymap{
		chord: ActUnbound,
	}
	ed := NewEditor(WithKeymap(overlay))
	if act, ok := ed.keymap[chord]; ok {
		t.Errorf("chord should be unbound, but got action %v", act)
	}
}
