package widget

import (
	"fmt"
	"sort"

	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
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
	// ModeVisualBlock is a block of lines and display columns (Vim's Ctrl-v). It shares Visual's
	// bindings, as ModeVisualLine does.
	ModeVisualBlock
	// ModeReplace is Replace mode (Vim's R): typing overwrites. It is Insert underneath, and shares
	// Insert's bindings; Mode reports it while it lasts.
	ModeReplace
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
	case ModeVisualBlock:
		return "V-BLOCK"
	case ModeReplace:
		return "REPLACE"
	}
	return "?"
}

// ModeChangedEvent publishes every Editor mode transition.
type ModeChangedEvent struct {
	Owner tui.NodeID
	Mode  EditorMode
}

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
	ActEscape    // Escape / return to Normal mode

	// ActToggleRendered switches an editor with a Rendered view between it and Raw (Ctrl+T).
	// An editor without one does not take it: the key bubbles.
	ActToggleRendered

	// ActFindNext and ActFindPrev go to the active find's next and previous match (n, N in the Vim
	// keyset), wrapping. With no active find the key is not consumed: it reaches the application.
	ActFindNext
	ActFindPrev

	// ActCommandLine opens the editor's ':' prompt (the Vim keyset's ':'), when its widget has
	// one (SetCommandLine). Without one the key is not consumed.
	ActCommandLine

	// Vim line and character motions (Normal + Visual, count-prefixable): appended, so no
	// earlier action's value moves.
	ActLineDownFirst   // Enter and + (Vim): count lines down, to the first non-blank
	ActLineUpFirst     // - (Vim): count lines up, to the first non-blank
	ActFirstNonBlank   // ^ (Vim): the line's first non-blank
	ActLeftWrap        // Backspace (Vim): left, onto the previous line's end
	ActFindCharForward // f{char} (Vim): the countth {char} after the cursor on the line
	ActFindCharBack    // F{char} (Vim): the countth {char} before the cursor on the line
	ActTillCharForward // t{char} (Vim): just before the countth {char} after the cursor
	ActTillCharBack    // T{char} (Vim): just after the countth {char} before the cursor
	ActFindCharRepeat  // ; (Vim): the last f, F, t or T again
	ActFindCharReverse // , (Vim): the last f, F, t or T, the other way
	ActMatchPair       // % (Vim): the bracket matching the one at or after the cursor; N% goes to N% of the text
	ActViewTop         // H (Vim): the countth line from the view's top
	ActViewMiddle      // M (Vim): the view's middle line
	ActViewBottom      // L (Vim): the countth line from the view's bottom
	ActHalfPageDown    // Ctrl-d (Vim): half a page down, or count lines
	ActHalfPageUp      // Ctrl-u (Vim): half a page up, or count lines
	ActWORDForward     // W (Vim): the next whitespace-delimited WORD
	ActWORDBack        // B (Vim): the previous WORD's start
	ActWORDEnd         // E (Vim): the WORD's end
	ActWordEndBack     // ge (Vim): back to the previous word's end
	ActWORDEndBack     // gE (Vim): back to the previous WORD's end
	ActChangePrefix    // c{motion}, cc (Vim): change — delete, then type in its place
	ActChangeToEnd     // C (Vim): change to the line's end (c$)
	ActSubstChar       // s (Vim): change count characters (cl)
	ActSubstLine       // S (Vim): change the line (cc)
	ActYankToEnd       // Y (Vim, as Neovim maps it): yank to the line's end (y$)
	ActReplaceChar     // r{char} (Vim): replace count clusters with {char}
	ActJoin            // J (Vim): join count lines, a blank between
	ActJoinRaw         // gJ (Vim): join count lines as they are
	ActToggleCase      // ~ (Vim): toggle the case of count clusters, and move past them
	ActCaseToggleOp    // g~{motion} (Vim): toggle the case of the text
	ActLowerOp         // gu{motion} (Vim): lowercase the text
	ActUpperOp         // gU{motion} (Vim): uppercase the text
	ActIndentPrefix    // >{motion}, >> (Vim): indent the lines by one unit
	ActOutdentPrefix   // <{motion}, << (Vim): outdent the lines by one unit
	ActRepeat          // . (Vim): repeat the last change
	ActRegisterPrefix  // "{reg} (Vim): name the register the next yank, delete or put uses
	ActMarkSet         // m{a-z} (Vim): set a mark at the cursor
	ActMarkLine        // '{a-z} (Vim): to the mark's line, first non-blank ('' the previous jump)
	ActMarkExact       // `{a-z} (Vim): to the mark's exact place (`` the previous jump)
	ActMacroRecord     // q{a-z} … q (Vim): record a macro into the register (A-Z append)
	ActMacroPlay       // @{a-z} (Vim): play the register's macro (@@ the last played)
	ActJumpBack        // Ctrl-o (Vim): back along the jump list
	ActJumpForward     // Ctrl-i (Vim): forward along the jump list

	ActVisualChange     // c, s (Vim Visual): change the selection
	ActVisualReplace    // r{c} (Vim Visual): every character of the selection becomes c
	ActVisualToggleCase // ~ (Vim Visual): toggle the selection's case
	ActVisualLower      // u (Vim Visual): lowercase the selection
	ActVisualUpper      // U (Vim Visual): uppercase the selection
	ActVisualIndent     // > (Vim Visual): indent the selected lines
	ActVisualOutdent    // < (Vim Visual): outdent the selected lines
	ActVisualOther      // o (Vim Visual): to the selection's other end

	ActDeleteWordBack // Ctrl-w (Vim Insert): delete the word before the cursor
	ActDeleteLineBack // Ctrl-u (Vim Insert): delete back to the indent, then the line's start
	ActInsertOneShot  // Ctrl-o (Vim Insert): one Normal-mode command, then Insert again

	ActVisualBlock    // Ctrl-v (Vim Normal: enter Visual block; Visual: switch/exit)
	ActVisualOtherCol // O (Vim Visual): in a block, the cursor and the anchor swap columns
	ActVisualInsert   // I (Vim Visual): Insert before the block, on each line; else at the first line's start
	ActVisualAppend   // A (Vim Visual): Insert after the block, on each line; else at the last line's end

	ActSentenceForward // ) (Vim): to the next sentence's start
	ActSentenceBack    // ( (Vim): to this sentence's start, or the previous one's
	ActStarForward     // * (Vim): the whole word under the cursor, found on
	ActStarBack        // # (Vim): the whole word under the cursor, found back
	ActReplaceMode     // R (Vim): Replace mode, typing overwrites

	actMax // sentinel for validation
)

// String returns the canonical identifier name for an Action.
func (a Action) String() string {
	if a < actMax {
		return actionSpecs[a].name
	}
	return fmt.Sprintf("Action(%d)", a)
}

// Description returns a concise human-readable explanation of the action.
func (a Action) Description() string {
	if a < actMax {
		return actionSpecs[a].desc
	}
	return "Custom action"
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

// modeClass maps an editor mode onto its binding class.
func modeClass(m EditorMode) EditorMode {
	if m == ModeVisualLine || m == ModeVisualBlock {
		return ModeVisual
	}
	if m == ModeReplace {
		return ModeInsert
	}
	return m
}

// actionModes reports which binding classes accept an action.
func actionModes(a Action) (normal, visual, insert bool) {
	if a >= actMax {
		return false, false, false
	}
	m := actionSpecs[a].modes
	return m&inNormal != 0, m&inVisual != 0, m&inInsert != 0
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

	// Motions (Normal + Visual): each bound once, here.
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
		mv(ch(']'), ActParaForward) // v1 aliases of } and {
		mv(ch('['), ActParaBack)
		mv(ch('G'), ActGoBottom)
		mv(ch('g'), ActGoPrefix)
		mv(ctrl('b'), ActPageUp)
		mv(ch(tui.KeyPageUp), ActPageUp)
		mv(ctrl('f'), ActPageDown)
		mv(ch(tui.KeyPageDown), ActPageDown)
		// line and character motions (Space stays the host's: AutoDoc's leader)
		mv(ch(tui.KeyEnter), ActLineDownFirst)
		mv(ch('+'), ActLineDownFirst)
		mv(ch(')'), ActSentenceForward)
		mv(ch('('), ActSentenceBack)
		mv(ch('-'), ActLineUpFirst)
		mv(ch('^'), ActFirstNonBlank)
		mv(ch(tui.KeyBackspace), ActLeftWrap)
		mv(ch('f'), ActFindCharForward)
		mv(ch('F'), ActFindCharBack)
		mv(ch('t'), ActTillCharForward)
		mv(ch('T'), ActTillCharBack)
		mv(ch(';'), ActFindCharRepeat)
		mv(ch(','), ActFindCharReverse)
		mv(ch('%'), ActMatchPair)
		mv(ch('H'), ActViewTop)
		mv(ch('M'), ActViewMiddle)
		mv(ch('L'), ActViewBottom)
		mv(ctrl('d'), ActHalfPageDown)
		mv(ctrl('u'), ActHalfPageUp)
		mv(ch('W'), ActWORDForward)
		mv(ch('B'), ActWORDBack)
		mv(ch('E'), ActWORDEnd)
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
	km[n('c')] = ActChangePrefix
	km[n('C')] = ActChangeToEnd
	km[n('s')] = ActSubstChar
	km[n('S')] = ActSubstLine
	km[n('Y')] = ActYankToEnd
	km[n('r')] = ActReplaceChar
	km[n('J')] = ActJoin
	km[n('~')] = ActToggleCase
	km[n('>')] = ActIndentPrefix
	km[n('<')] = ActOutdentPrefix
	km[n('.')] = ActRepeat
	km[n('"')], km[v('"')] = ActRegisterPrefix, ActRegisterPrefix
	km[n('m')], km[v('m')] = ActMarkSet, ActMarkSet
	km[n('\'')], km[v('\'')] = ActMarkLine, ActMarkLine
	km[n('`')], km[v('`')] = ActMarkExact, ActMarkExact
	km[n('q')] = ActMacroRecord
	km[n('@')] = ActMacroPlay
	km[n('*')], km[n('#')] = ActStarForward, ActStarBack
	km[n('R')] = ActReplaceMode
	km[n('p')] = ActPasteAfter
	km[n('P')] = ActPasteBefore
	km[n('u')] = ActUndo
	km[KeyChord{Mode: ModeNormal, Code: 'r', Ctrl: true}] = ActRedo
	km[KeyChord{Mode: ModeInsert, Code: 'w', Ctrl: true}] = ActDeleteWordBack
	km[KeyChord{Mode: ModeInsert, Code: 'u', Ctrl: true}] = ActDeleteLineBack
	km[KeyChord{Mode: ModeInsert, Code: 'o', Ctrl: true}] = ActInsertOneShot
	km[KeyChord{Mode: ModeNormal, Code: 'o', Ctrl: true}] = ActJumpBack
	km[KeyChord{Mode: ModeNormal, Code: 'i', Ctrl: true}] = ActJumpForward // Tab isn't: in a terminal it is Ctrl-i, and focus's
	km[n('n')], km[v('n')] = ActFindNext, ActFindNext
	km[n('N')], km[v('N')] = ActFindPrev, ActFindPrev
	km[n(':')], km[v(':')] = ActCommandLine, ActCommandLine
	km[n('v')] = ActVisual
	km[n('V')] = ActVisualLine

	// Visual-only commands.
	km[v('y')] = ActVisualYank
	km[v('d')] = ActVisualDelete
	km[v('x')] = ActVisualDelete
	km[v('c')], km[v('s')] = ActVisualChange, ActVisualChange
	km[v('r')] = ActVisualReplace
	km[v('~')] = ActVisualToggleCase
	km[v('u')], km[v('U')] = ActVisualLower, ActVisualUpper
	km[v('>')], km[v('<')] = ActVisualIndent, ActVisualOutdent
	km[v('o')] = ActVisualOther
	km[v('O')] = ActVisualOtherCol
	km[v('I')], km[v('A')] = ActVisualInsert, ActVisualAppend
	km[KeyChord{Mode: ModeNormal, Code: 'v', Ctrl: true}] = ActVisualBlock
	km[KeyChord{Mode: ModeVisual, Code: 'v', Ctrl: true}] = ActVisualBlock
	km[v('v')] = ActVisual
	km[v('V')] = ActVisualLine

	toggleRendered(km)
	return km
}

// toggleRendered binds Ctrl+T to ActToggleRendered in every mode: unbound in all three keysets
// (Nano's line end is Ctrl+E), so no profile loses a key to it.
func toggleRendered(km Keymap) {
	for _, m := range []EditorMode{ModeNormal, ModeVisual, ModeInsert} {
		km[KeyChord{Mode: m, Code: 't', Ctrl: true}] = ActToggleRendered
	}
}

// NanoKeymap returns a fresh COPY of the non-modal Nano-style keymap.
func NanoKeymap() Keymap {
	ins := func(code rune, ctrl bool) KeyChord {
		return KeyChord{Mode: ModeInsert, Code: code, Ctrl: ctrl}
	}
	vis := func(code rune, ctrl bool) KeyChord {
		return KeyChord{Mode: ModeVisual, Code: code, Ctrl: ctrl}
	}
	km := Keymap{
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
	toggleRendered(km)
	return km
}

// StandardKeymap returns a fresh COPY of the standard GUI / TextEdit-style keymap.
func StandardKeymap() Keymap {
	ins := func(code rune, ctrl bool) KeyChord {
		return KeyChord{Mode: ModeInsert, Code: code, Ctrl: ctrl}
	}
	vis := func(code rune, ctrl bool) KeyChord {
		return KeyChord{Mode: ModeVisual, Code: code, Ctrl: ctrl}
	}
	km := Keymap{
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
	toggleRendered(km)
	return km
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

// String returns the human-readable profile name.
func (k Keyset) String() string {
	switch k {
	case KeysetVim:
		return "Vim"
	case KeysetNano:
		return "Nano"
	case KeysetStandard:
		return "Standard"
	}
	return "Custom"
}

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
