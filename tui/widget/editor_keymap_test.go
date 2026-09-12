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
	if act, ok := km[KeyChord{Mode: ModeNormal, Code: 'j'}]; !ok || act != ActLeft && act != ActDown {
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
