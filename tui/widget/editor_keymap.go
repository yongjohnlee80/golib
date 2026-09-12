package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
)

// Editor keymap definitions, modal enumeration, and action dispatch bindings.
//
// # Architectural Model
//
// The keymap subsystem translates incoming [tui.KeyEvent] input chords into semantic [Action]
// enumerations. Key bindings are organized by [EditorMode], allowing identical keystrokes to
// perform different actions depending on whether the editor is in [ModeNormal], [ModeVisual],
// or [ModeInsert].
//
// Supported keymap styles:
//   - [VimKeymap] / [DefaultKeymap]: Modal Vim keyset (Normal, Insert, Visual).
//   - [NanoKeymap]: Non-modal Nano keyset (Ctrl+K cut line, Ctrl+U paste, Ctrl+A/E line start/end).
//   - [StandardKeymap]: Standard GUI / TextEdit keyset (Ctrl+Z undo, Ctrl+Y redo, Ctrl+C/X/V clipboard, Ctrl+A select all).
//
// # Architectural Invariants
//
//  1. Keyset Composability: Keymap is a flat map from [KeyChord] to [Action], allowing full
//     customization or partial overriding via [WithKeymap].
//  2. Visual Line Shared Bindings: [ModeVisualLine] dynamically shares the exact binding set of
//     [ModeVisual], ensuring consistent operator selection behavior.
//  3. Unbound Bubbling Sentinel: [ActUnbound] explicitly unbinds a default chord, permitting the
//     unhandled keystroke to bubble up the component hierarchy to application leader handlers.
//
// # Concurrency Model
//
//   - Ownership: loop-goroutine-owned. Keymap evaluation occurs synchronously during event handling.
//   - Immutable Tables: Factory functions return fresh map copies safe for caller mutation.

// EditorMode is the Editor's modal state.
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
type KeyChord struct {
	Mode EditorMode // ModeNormal, ModeVisual, or ModeInsert
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

	// Double-key prefixes (the one-key pending buffer).
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

	// General non-modal & standard editor actions (Nano / TextEdit).
	ActCut       // Cut selection or current line (Nano Ctrl+K, Standard Ctrl+X)
	ActCopy      // Copy selection or line (Standard Ctrl+C)
	ActPaste     // Paste at cursor (Nano Ctrl+U, Standard Ctrl+V)
	ActSelectAll // Select entire buffer (Standard Ctrl+A)

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
		ActUndo, ActRedo,
		ActCut, ActCopy, ActPaste, ActSelectAll:
		return true, false
	case ActVisualYank, ActVisualDelete:
		return false, true
	}
	return false, false
}

// Keymap maps chords to actions. Overlays passed to WithKeymap replace (or,
// with ActUnbound, clear) entries in the default table. Chords carry their
// modes, and disallowed mode/action combinations panic at construction.
type Keymap map[KeyChord]Action

// DefaultKeymap returns a fresh COPY of the default Vim binding table.
func DefaultKeymap() Keymap {
	return VimKeymap()
}

// VimKeymap returns a fresh COPY of the modal Vim keymap.
func VimKeymap() Keymap {
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
		'[': ActParaBack, ']': ActParaForward, // v1 aliases
		'G':           ActGoBottom,
		tui.KeyPageUp: ActPageUp, tui.KeyPageDown: ActPageDown,
		'g': ActGoPrefix,
		'v': ActVisual, 'V': ActVisualLine,
	}
	for code, act := range motions {
		km[n(code)] = act
		km[v(code)] = act
	}

	// Normal-only.
	// Motions (Normal + Visual).
	mv := func(kc KeyChord, act Action) { km[kc] = act }
	for _, m := range []EditorMode{ModeNormal, ModeVisual} {
		ch := func(code rune) KeyChord { return KeyChord{Mode: m, Code: code} }
		ctrl := func(code rune) KeyChord { return KeyChord{Mode: m, Code: code, Ctrl: true} }
		mv(ch('h'), ActLeft)
		mv(ch(tui.KeyLeft), ActLeft)
		mv(ch('l'), ActRight)
		mv(ch(tui.KeyRight), ActRight)
		mv(ch('k'), ActUp)
		mv(ch(tui.KeyUp), ActUp)
		mv(ch('j'), ActDown)
		mv(ch(tui.KeyDown), ActDown)
		mv(ch('0'), ActLineStart)
		mv(ch(tui.KeyHome), ActLineStart)
		mv(ch('$'), ActLineEnd)
		mv(ch(tui.KeyEnd), ActLineEnd)
		mv(ch('w'), ActWordForward)
		mv(ch('b'), ActWordBack)
		mv(ch('e'), ActWordEnd)
		mv(ch('}'), ActParaForward)
		mv(ch('{'), ActParaBack)
		mv(ch('G'), ActGoBottom)
		mv(ch('g'), ActGoPrefix)
		mv(ctrl('b'), ActPageUp)
		mv(ch(tui.KeyPageUp), ActPageUp)
		mv(ctrl('f'), ActPageDown)
		mv(ch(tui.KeyPageDown), ActPageDown)
	}

	// Normal-only commands.
	km[n('i')] = ActInsert
	km[n('a')] = ActAppend
	km[n('I')] = ActInsertLineStart
	km[n('A')] = ActAppendLineEnd
	km[n('o')] = ActOpenBelow
	km[n('O')] = ActOpenAbove
	km[n('x')] = ActDeleteChar
	km[n('D')] = ActDeleteToEnd
	km[n('d')] = ActDeletePrefix
	km[n('y')] = ActYankPrefix
	km[n('p')] = ActPasteAfter
	km[n('P')] = ActPasteBefore
	km[n('u')] = ActUndo
	km[KeyChord{Mode: ModeNormal, Code: 'r', Ctrl: true}] = ActRedo
	km[n('v')] = ActVisual
	km[n('V')] = ActVisualLine

	// Visual-only commands.
	km[v('y')] = ActVisualYank
	km[v('d')] = ActVisualDelete
	km[v('x')] = ActVisualDelete
	km[v('v')] = ActVisual
	km[v('V')] = ActVisualLine

	return km
}

// NanoKeymap returns a fresh COPY of the non-modal Nano-style keymap.
func NanoKeymap() Keymap {
	ins := func(code rune, ctrl bool) KeyChord {
		return KeyChord{Mode: ModeInsert, Code: code, Ctrl: ctrl}
	}
	vis := func(code rune, ctrl bool) KeyChord {
		return KeyChord{Mode: ModeVisual, Code: code, Ctrl: ctrl}
	}
	return Keymap{
		ins('k', true):              ActCut,
		ins('u', true):              ActPaste,
		ins('a', true):              ActLineStart,
		ins('e', true):              ActLineEnd,
		ins('y', true):              ActPageUp,
		ins('v', true):              ActPageDown,
		ins('z', true):              ActUndo,
		vis('k', true):              ActVisualDelete,
		ins(tui.KeyHome, false):     ActLineStart,
		ins(tui.KeyEnd, false):      ActLineEnd,
		ins(tui.KeyPageUp, false):   ActPageUp,
		ins(tui.KeyPageDown, false): ActPageDown,
	}
}

// StandardKeymap returns a fresh COPY of the standard GUI / TextEdit-style keymap.
func StandardKeymap() Keymap {
	ins := func(code rune, ctrl bool) KeyChord {
		return KeyChord{Mode: ModeInsert, Code: code, Ctrl: ctrl}
	}
	vis := func(code rune, ctrl bool) KeyChord {
		return KeyChord{Mode: ModeVisual, Code: code, Ctrl: ctrl}
	}
	return Keymap{
		ins('z', true):              ActUndo,
		ins('y', true):              ActRedo,
		ins('x', true):              ActCut,
		ins('c', true):              ActCopy,
		ins('v', true):              ActPaste,
		ins('a', true):              ActSelectAll,
		vis('c', true):              ActVisualYank,
		vis('x', true):              ActVisualDelete,
		vis('a', true):              ActSelectAll,
		ins(tui.KeyHome, false):     ActLineStart,
		ins(tui.KeyEnd, false):      ActLineEnd,
		ins(tui.KeyPageUp, false):   ActPageUp,
		ins(tui.KeyPageDown, false): ActPageDown,
	}
}

// Keyset selects a predefined editing and keymap profile.
type Keyset int

const (
	// KeysetVim enables classical modal editing (Normal, Insert, Visual).
	KeysetVim Keyset = iota
	// KeysetNano enables non-modal Nano-style editing (Ctrl+K cut, Ctrl+U paste, etc.).
	KeysetNano
	// KeysetStandard enables non-modal GUI/TextEdit-style editing (Ctrl+X/C/V/Z/A).
	KeysetStandard
)

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
