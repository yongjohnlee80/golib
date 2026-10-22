package widget

import (
	"fmt"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
	"sort"
)

// THE EDITOR'S CHORD API: KeyChord, Keymap and the legacy tables, kept working as
// an adapter over the command declarations (editor_keytables.go). VimKeymap, NanoKeymap and
// StandardKeymap return each keyset's table as it always was; a host's overlay (WithKeymap,
// OverlayKeymap) is written in chords.

// KeyChord addresses one binding: a mode class and a normalized key. A chord has one spelling: a
// letter's Shift is its case (Code 'G'), and Shift is set only on a key that is not a letter
// (Shift+Tab, Shift+Down). A keyed literal written before Alt, Super and Shift existed means the same
// chord it did; see applyOverlay for a functional key's Shift.
type KeyChord struct {
	Mode  EditorMode // ModeNormal, ModeVisual, or ModeInsert
	Code  rune       // Unicode codepoint or a tui.Key* constant
	Ctrl  bool
	Alt   bool
	Super bool
	Shift bool // a key that is not a letter; a letter's Shift is its Code
}

// String returns a human-readable display label for the key chord (e.g. "Ctrl+K", "Home", "w").
func (kc KeyChord) String() string {
	// a functional key by the name the key package writes it with, as key-map help shows it; a
	// character as typed, case kept: in a vim keyset w and W are two keys
	s, named := tuikey.Name(kc.Code)
	switch {
	case named:
	case kc.Code >= 0x20 && kc.Code < 0xE000:
		s = string(kc.Code)
	default:
		s = fmt.Sprintf("Key(0x%x)", kc.Code)
	}
	prefix := ""
	if kc.Ctrl {
		prefix += "Ctrl+"
	}
	if kc.Alt {
		prefix += "Alt+"
	}
	if kc.Super {
		prefix += "Super+"
	}
	if kc.Shift {
		prefix += "Shift+"
	}
	return prefix + s
}

// KeyBinding represents a concrete key chord mapped to an action at runtime.
type KeyBinding struct {
	Mode        EditorMode `json:"mode"`
	Chord       KeyChord   `json:"chord"`
	Key         string     `json:"key"`
	Action      Action     `json:"action"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
}

// KeymapSnapshot provides a structural runtime reflection of the editor's keymap.
type KeymapSnapshot struct {
	Keyset      Keyset       `json:"keyset"`
	KeysetName  string       `json:"keyset_name"`
	Modal       bool         `json:"modal"`
	EscapeChord string       `json:"escape_chord,omitempty"`
	Bindings    []KeyBinding `json:"bindings"`
}

// Bindings returns all bindings in the keymap sorted deterministically by mode, key, and action.
func (km Keymap) Bindings() []KeyBinding {
	list := make([]KeyBinding, 0, len(km))
	for kc, act := range km {
		list = append(list, KeyBinding{
			Mode:        kc.Mode,
			Chord:       kc,
			Key:         kc.String(),
			Action:      act,
			Name:        act.String(),
			Description: act.Description(),
		})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Mode != list[j].Mode {
			return list[i].Mode < list[j].Mode
		}
		if list[i].Key != list[j].Key {
			return list[i].Key < list[j].Key
		}
		return list[i].Action < list[j].Action
	})
	return list
}

// Keymap maps chords to actions. Overlays passed to WithKeymap replace (or,
// with ActUnbound, clear) entries in the default table. Chords carry their
// modes, and disallowed mode/action combinations panic at construction.
type Keymap map[KeyChord]Action

// DefaultKeymap returns a fresh COPY of the default Vim binding table.
func DefaultKeymap() Keymap {
	return VimKeymap()
}

// VimKeymap returns a fresh COPY of the modal Vim keymap: the projection of its declarations
// without the editing commands, so it is the table it always was.
func VimKeymap() Keymap { return declsOf(KeysetVim).projection() }

// NanoKeymap returns a fresh COPY of the non-modal Nano-style keymap: the projection of its declarations
// without the editing commands, so it is the table it always was.
func NanoKeymap() Keymap { return declsOf(KeysetNano).projection() }

// StandardKeymap returns a fresh COPY of the standard GUI / TextEdit-style keymap: the projection of its declarations
// without the editing commands, so it is the table it always was.
func StandardKeymap() Keymap { return declsOf(KeysetStandard).projection() }

// validateKeymapEntry panics on an entry the Editor cannot honor.
func validateKeymapEntry(kc KeyChord, act Action) {
	if kc.Mode != ModeNormal && kc.Mode != ModeVisual && kc.Mode != ModeInsert {
		panic(fmt.Sprintf("widget: WithKeymap: chord %+v: bindings exist only for ModeNormal/ModeVisual/ModeInsert", kc))
	}
	if act >= actMax {
		panic(fmt.Sprintf("widget: WithKeymap: chord %+v: unknown action %d", kc, act))
	}
	if act == ActUnbound {
		return // always allowed: removes the default
	}
	nOK, vOK, iOK := actionModes(act)
	if (kc.Mode == ModeNormal && !nOK) || (kc.Mode == ModeVisual && !vOK) || (kc.Mode == ModeInsert && !iOK) {
		panic(fmt.Sprintf("widget: WithKeymap: chord %+v: action %d is not supported in that mode", kc, act))
	}
}
