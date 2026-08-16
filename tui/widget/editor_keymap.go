package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
)

// EditorMode is the Editor's modal state (ADR-0008 §2.1).
type EditorMode uint8

const (
	// ModeNormal is command mode: the cursor sits ON a grapheme.
	ModeNormal EditorMode = iota
	// ModeInsert is text-entry mode: the cursor is an insertion point.
	ModeInsert
	// ModeVisual is char-wise selection (inclusive at both endpoints).
	ModeVisual
	// ModeVisualLine is line-wise selection.
	ModeVisualLine
)

// String renders the mode for status bars.
func (m EditorMode) String() string {
	switch m {
	case ModeNormal:
		return "NORMAL"
	case ModeInsert:
		return "INSERT"
	case ModeVisual:
		return "VISUAL"
	case ModeVisualLine:
		return "V-LINE"
	}
	return "?"
}

// ModeChangedEvent publishes every Editor mode transition.
type ModeChangedEvent struct {
	Owner tui.NodeID
	Mode  EditorMode
}


// KeyChord addresses one binding: a mode class and a normalized key.
// ModeVisualLine shares ModeVisual's bindings; ModeInsert has no bindings
// (its handling — text, chord, Esc, editing keys — is structural).
type KeyChord struct {
	Mode EditorMode // ModeNormal or ModeVisual only
	Code rune       // Unicode codepoint or a tui.Key* constant
	Ctrl bool
}

// Action is one enumerated Editor operation a chord can bind.
type Action uint8

const (
	// ActUnbound removes a default binding (overlay-only sentinel).
	ActUnbound Action = iota

	// Motions (Normal + Visual, count-prefixable).
	ActLeft
	ActDown
	ActUp
	ActRight
	ActLineStart
	ActLineEnd
	ActWordForward
	ActWordBack
	ActWordEnd
	ActParaForward
	ActParaBack
	ActGoBottom
	ActPageUp
	ActPageDown

	// Double-key prefixes (the one-key pending buffer, ADR-0008 §2.1).
	ActDeletePrefix // d → dd
	ActYankPrefix   // y → yy
	ActGoPrefix     // g → gg

	// Insert entries (Normal only).
	ActInsert          // i
	ActAppend          // a
	ActInsertLineStart // I
	ActAppendLineEnd   // A
	ActOpenBelow       // o
	ActOpenAbove       // O

	// Edits (Normal only).
	ActDeleteChar  // x
	ActDeleteToEnd // D
	ActPasteAfter  // p
	ActPasteBefore // P
	ActUndo        // u
	ActRedo        // Ctrl-R

	// Visual-mode entry/exit and operations.
	ActVisual       // v (Normal: enter; Visual: exit)
	ActVisualLine   // V (Normal: enter; Visual: switch/exit)
	ActVisualYank   // y in visual
	ActVisualDelete // d / x in visual

	actMax // sentinel for validation
)

// modeClass maps an editor mode onto its binding class.
func modeClass(m EditorMode) EditorMode {
	if m == ModeVisualLine {
		return ModeVisual
	}
	return m
}

// actionModes reports which binding classes accept an action.
func actionModes(a Action) (normal, visual bool) {
	switch a {
	case ActLeft, ActDown, ActUp, ActRight, ActLineStart, ActLineEnd,
		ActWordForward, ActWordBack, ActWordEnd, ActParaForward, ActParaBack,
		ActGoBottom, ActPageUp, ActPageDown, ActGoPrefix,
		ActVisual, ActVisualLine:
		return true, true
	case ActDeletePrefix, ActYankPrefix,
		ActInsert, ActAppend, ActInsertLineStart, ActAppendLineEnd,
		ActOpenBelow, ActOpenAbove,
		ActDeleteChar, ActDeleteToEnd, ActPasteAfter, ActPasteBefore,
		ActUndo, ActRedo:
		return true, false
	case ActVisualYank, ActVisualDelete:
		return false, true
	}
	return false, false
}

// Keymap maps chords to actions. Overlays passed to WithKeymap replace (or,
// via ActUnbound, remove) default entries; unknown actions, unsupported
// modes, and disallowed mode/action combinations panic at construction.
type Keymap map[KeyChord]Action

// DefaultKeymap returns a fresh COPY of the default binding table — callers
// can mutate the result without affecting shared state.
func DefaultKeymap() Keymap {
	n := func(code rune) KeyChord { return KeyChord{Mode: ModeNormal, Code: code} }
	v := func(code rune) KeyChord { return KeyChord{Mode: ModeVisual, Code: code} }
	km := Keymap{}

	// Motions in both classes.
	motions := map[rune]Action{
		'h': ActLeft, 'j': ActDown, 'k': ActUp, 'l': ActRight,
		tui.KeyLeft: ActLeft, tui.KeyDown: ActDown, tui.KeyUp: ActUp, tui.KeyRight: ActRight,
		'0': ActLineStart, '$': ActLineEnd,
		tui.KeyHome: ActLineStart, tui.KeyEnd: ActLineEnd,
		'w': ActWordForward, 'b': ActWordBack, 'e': ActWordEnd,
		'{': ActParaBack, '}': ActParaForward,
		'[': ActParaBack, ']': ActParaForward, // v1 aliases (ADR-0008 §2.1)
		'G': ActGoBottom,
		tui.KeyPageUp: ActPageUp, tui.KeyPageDown: ActPageDown,
		'g': ActGoPrefix,
		'v': ActVisual, 'V': ActVisualLine,
	}
	for code, act := range motions {
		km[n(code)] = act
		km[v(code)] = act
	}

	// Normal-only.
	for code, act := range map[rune]Action{
		'd': ActDeletePrefix, 'y': ActYankPrefix,
		'i': ActInsert, 'a': ActAppend, 'I': ActInsertLineStart, 'A': ActAppendLineEnd,
		'o': ActOpenBelow, 'O': ActOpenAbove,
		'x': ActDeleteChar, 'D': ActDeleteToEnd,
		'p': ActPasteAfter, 'P': ActPasteBefore,
		'u': ActUndo,
	} {
		km[n(code)] = act
	}
	km[KeyChord{Mode: ModeNormal, Code: 'r', Ctrl: true}] = ActRedo

	// Visual-only operations.
	km[v('y')] = ActVisualYank
	km[v('d')] = ActVisualDelete
	km[v('x')] = ActVisualDelete

	return km
}

// validateKeymapEntry panics on an entry the Editor cannot honor.
func validateKeymapEntry(kc KeyChord, act Action) {
	if kc.Mode != ModeNormal && kc.Mode != ModeVisual {
		panic(fmt.Sprintf("widget: WithKeymap: chord %+v: bindings exist only for ModeNormal/ModeVisual", kc))
	}
	if act >= actMax {
		panic(fmt.Sprintf("widget: WithKeymap: chord %+v: unknown action %d", kc, act))
	}
	if act == ActUnbound {
		return // always allowed: removes the default
	}
	nOK, vOK := actionModes(act)
	if (kc.Mode == ModeNormal && !nOK) || (kc.Mode == ModeVisual && !vOK) {
		panic(fmt.Sprintf("widget: WithKeymap: chord %+v: action %d is not supported in that mode", kc, act))
	}
}
