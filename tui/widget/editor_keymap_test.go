package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
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

func TestKeyChordString(t *testing.T) {
	cases := []struct {
		chord KeyChord
		want  string
	}{
		{KeyChord{Mode: ModeNormal, Code: 'h'}, "h"},
		{KeyChord{Mode: ModeNormal, Code: 'r', Ctrl: true}, "Ctrl+r"},
		{KeyChord{Mode: ModeNormal, Code: tui.KeyLeft}, "Left"},
		{KeyChord{Mode: ModeNormal, Code: tui.KeyHome}, "Home"},
		{KeyChord{Mode: ModeInsert, Code: 'k', Ctrl: true}, "Ctrl+k"},
	}
	for _, tc := range cases {
		if got := tc.chord.String(); got != tc.want {
			t.Errorf("chord %+v: got %q, want %q", tc.chord, got, tc.want)
		}
	}
}

func TestActionStringAndDescription(t *testing.T) {
	for act := ActUnbound; act < actMax; act++ {
		str := act.String()
		desc := act.Description()
		if str == "" {
			t.Errorf("action %d has empty String()", act)
		}
		if desc == "" {
			t.Errorf("action %d has empty Description()", act)
		}
	}
}

func TestKeysetString(t *testing.T) {
	if got := KeysetVim.String(); got != "Vim" {
		t.Errorf("KeysetVim.String() = %q, want \"Vim\"", got)
	}
	if got := KeysetNano.String(); got != "Nano" {
		t.Errorf("KeysetNano.String() = %q, want \"Nano\"", got)
	}
	if got := KeysetStandard.String(); got != "Standard" {
		t.Errorf("KeysetStandard.String() = %q, want \"Standard\"", got)
	}
}

func TestKeymapBindings(t *testing.T) {
	km := VimKeymap()
	bindings := km.Bindings()
	if len(bindings) == 0 {
		t.Fatal("expected non-empty bindings from VimKeymap()")
	}
	for _, b := range bindings {
		if b.Key == "" || b.Name == "" || b.Description == "" {
			t.Errorf("incomplete binding: %+v", b)
		}
	}
}

func TestInsertKeymapOverlayAndUnbind(t *testing.T) {
	// 1. WithKeymap accepting ModeInsert chord
	overlay := Keymap{
		KeyChord{Mode: ModeInsert, Code: 'q', Ctrl: true}: ActUndo,
	}
	ed := NewEditor(WithKeymap(overlay))
	if act, ok := ed.keymap[KeyChord{Mode: ModeInsert, Code: 'q', Ctrl: true}]; !ok || act != ActUndo {
		t.Fatalf("expected ModeInsert Ctrl+Q -> ActUndo, got (%v, %v)", act, ok)
	}

	// 2. Overlay unbinding a standard ModeInsert chord
	stdWithUnbind := StandardKeymap()
	stdWithUnbind[KeyChord{Mode: ModeInsert, Code: 'z', Ctrl: true}] = ActUnbound
	edStd := NewEditor(WithKeymap(stdWithUnbind), WithModalEditing(false))
	if _, ok := edStd.keymap[KeyChord{Mode: ModeInsert, Code: 'z', Ctrl: true}]; ok {
		t.Fatalf("expected Ctrl+Z to be unbound in edStd")
	}
}

func TestSnapshotKeymapReflection(t *testing.T) {
	ed := NewEditor(WithEscapeChord("jk"))
	snap := ed.SnapshotKeymap()

	if snap.EscapeChord != "jk" {
		t.Errorf("snap.EscapeChord: got %q, want %q", snap.EscapeChord, "jk")
	}
	if len(snap.Bindings) == 0 {
		t.Fatal("expected non-empty snap.Bindings")
	}

	// Check reverse lookup
	chords := ed.ChordsForAction(ActDown)
	found := false
	for _, c := range chords {
		if c.Mode == ModeNormal && c.Code == 'j' && !c.Ctrl {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("ChordsForAction(ActDown) did not contain normal 'j': %+v", chords)
	}

	// Check chord lookup
	act, ok := ed.ActionForChord(KeyChord{Mode: ModeNormal, Code: 'j'})
	if !ok || act != ActDown {
		t.Errorf("ActionForChord(normal 'j'): got (%v, %v), want (ActDown, true)", act, ok)
	}

	// Structural chords in bindings should all have valid non-zero codes
	for _, b := range snap.Bindings {
		if b.Chord.Code == 0 {
			t.Errorf("snap.Bindings contains zero-valued chord: %+v", b)
		}
	}
}
