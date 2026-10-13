package widget

import (
	"fmt"
	"sort"

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
	// ModeVisualBlock is a block of lines and display columns (Vim's Ctrl-v). It shares Visual's
	// bindings, as ModeVisualLine does.
	ModeVisualBlock
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

// String returns a human-readable display label for the key chord (e.g. "Ctrl+K", "Home", "w").
func (kc KeyChord) String() string {
	var s string
	switch kc.Code {
	case tui.KeyLeft:
		s = "Left"
	case tui.KeyRight:
		s = "Right"
	case tui.KeyUp:
		s = "Up"
	case tui.KeyDown:
		s = "Down"
	case tui.KeyHome:
		s = "Home"
	case tui.KeyEnd:
		s = "End"
	case tui.KeyPageUp:
		s = "PageUp"
	case tui.KeyPageDown:
		s = "PageDown"
	case tui.KeyEscape:
		s = "Esc"
	case tui.KeyEnter:
		s = "Enter"
	case tui.KeyTab:
		s = "Tab"
	case tui.KeyBackspace:
		s = "Backspace"
	case tui.KeyDelete:
		s = "Delete"
	default:
		if kc.Code >= 0x20 && kc.Code < 0xE000 {
			s = string(kc.Code)
		} else {
			s = fmt.Sprintf("Key(0x%x)", kc.Code)
		}
	}
	if kc.Ctrl {
		return "Ctrl+" + s
	}
	return s
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

	actMax // sentinel for validation
)

// String returns the canonical identifier name for an Action.
func (a Action) String() string {
	switch a {
	case ActUnbound:
		return "Unbound"
	case ActLeft:
		return "Left"
	case ActDown:
		return "Down"
	case ActUp:
		return "Up"
	case ActRight:
		return "Right"
	case ActLineStart:
		return "LineStart"
	case ActLineEnd:
		return "LineEnd"
	case ActWordForward:
		return "WordForward"
	case ActWordBack:
		return "WordBack"
	case ActWordEnd:
		return "WordEnd"
	case ActParaForward:
		return "ParaForward"
	case ActParaBack:
		return "ParaBack"
	case ActGoBottom:
		return "GoBottom"
	case ActPageUp:
		return "PageUp"
	case ActPageDown:
		return "PageDown"
	case ActDeletePrefix:
		return "DeletePrefix"
	case ActYankPrefix:
		return "YankPrefix"
	case ActGoPrefix:
		return "GoPrefix"
	case ActInsert:
		return "Insert"
	case ActAppend:
		return "Append"
	case ActInsertLineStart:
		return "InsertLineStart"
	case ActAppendLineEnd:
		return "AppendLineEnd"
	case ActOpenBelow:
		return "OpenBelow"
	case ActOpenAbove:
		return "OpenAbove"
	case ActDeleteChar:
		return "DeleteChar"
	case ActDeleteToEnd:
		return "DeleteToEnd"
	case ActPasteAfter:
		return "PasteAfter"
	case ActPasteBefore:
		return "PasteBefore"
	case ActUndo:
		return "Undo"
	case ActRedo:
		return "Redo"
	case ActVisual:
		return "Visual"
	case ActVisualLine:
		return "VisualLine"
	case ActVisualYank:
		return "VisualYank"
	case ActVisualDelete:
		return "VisualDelete"
	case ActCut:
		return "Cut"
	case ActCopy:
		return "Copy"
	case ActPaste:
		return "Paste"
	case ActSelectAll:
		return "SelectAll"
	case ActEscape:
		return "Escape"
	case ActToggleRendered:
		return "ToggleRendered"
	case ActFindNext:
		return "FindNext"
	case ActFindPrev:
		return "FindPrev"
	case ActCommandLine:
		return "CommandLine"
	case ActLineDownFirst:
		return "LineDownFirst"
	case ActLineUpFirst:
		return "LineUpFirst"
	case ActFirstNonBlank:
		return "FirstNonBlank"
	case ActLeftWrap:
		return "LeftWrap"
	case ActFindCharForward:
		return "FindCharForward"
	case ActFindCharBack:
		return "FindCharBack"
	case ActTillCharForward:
		return "TillCharForward"
	case ActTillCharBack:
		return "TillCharBack"
	case ActFindCharRepeat:
		return "FindCharRepeat"
	case ActFindCharReverse:
		return "FindCharReverse"
	case ActMatchPair:
		return "MatchPair"
	case ActViewTop:
		return "ViewTop"
	case ActViewMiddle:
		return "ViewMiddle"
	case ActViewBottom:
		return "ViewBottom"
	case ActHalfPageDown:
		return "HalfPageDown"
	case ActHalfPageUp:
		return "HalfPageUp"
	case ActWORDForward:
		return "WORDForward"
	case ActWORDBack:
		return "WORDBack"
	case ActWORDEnd:
		return "WORDEnd"
	case ActWordEndBack:
		return "WordEndBack"
	case ActWORDEndBack:
		return "WORDEndBack"
	case ActChangePrefix:
		return "ChangePrefix"
	case ActChangeToEnd:
		return "ChangeToEnd"
	case ActSubstChar:
		return "SubstChar"
	case ActSubstLine:
		return "SubstLine"
	case ActYankToEnd:
		return "YankToEnd"
	case ActReplaceChar:
		return "ReplaceChar"
	case ActJoin:
		return "Join"
	case ActJoinRaw:
		return "JoinRaw"
	case ActToggleCase:
		return "ToggleCase"
	case ActCaseToggleOp:
		return "CaseToggleOp"
	case ActLowerOp:
		return "LowerOp"
	case ActUpperOp:
		return "UpperOp"
	case ActIndentPrefix:
		return "IndentPrefix"
	case ActOutdentPrefix:
		return "OutdentPrefix"
	case ActRepeat:
		return "Repeat"
	case ActRegisterPrefix:
		return "RegisterPrefix"
	case ActMarkSet:
		return "MarkSet"
	case ActMarkLine:
		return "MarkLine"
	case ActMarkExact:
		return "MarkExact"
	case ActMacroRecord:
		return "MacroRecord"
	case ActMacroPlay:
		return "MacroPlay"
	case ActVisualChange:
		return "VisualChange"
	case ActVisualReplace:
		return "VisualReplace"
	case ActVisualToggleCase:
		return "VisualToggleCase"
	case ActVisualLower:
		return "VisualLower"
	case ActVisualUpper:
		return "VisualUpper"
	case ActVisualIndent:
		return "VisualIndent"
	case ActVisualOutdent:
		return "VisualOutdent"
	case ActVisualOther:
		return "VisualOther"
	case ActDeleteWordBack:
		return "DeleteWordBack"
	case ActDeleteLineBack:
		return "DeleteLineBack"
	case ActInsertOneShot:
		return "InsertOneShot"
	case ActVisualBlock:
		return "VisualBlock"
	case ActVisualOtherCol:
		return "VisualOtherCol"
	case ActVisualInsert:
		return "VisualInsert"
	case ActVisualAppend:
		return "VisualAppend"
	}
	return fmt.Sprintf("Action(%d)", a)
}

// Description returns a concise human-readable explanation of the action.
func (a Action) Description() string {
	switch a {
	case ActUnbound:
		return "Unbound key (bubbles to parent component)"
	case ActLeft:
		return "Move cursor left"
	case ActDown:
		return "Move cursor down"
	case ActUp:
		return "Move cursor up"
	case ActRight:
		return "Move cursor right"
	case ActLineStart:
		return "Move cursor to start of line"
	case ActLineEnd:
		return "Move cursor to end of line"
	case ActWordForward:
		return "Move cursor to start of next word"
	case ActWordBack:
		return "Move cursor to start of previous word"
	case ActWordEnd:
		return "Move cursor to end of current word"
	case ActParaForward:
		return "Move cursor to next paragraph boundary"
	case ActParaBack:
		return "Move cursor to previous paragraph boundary"
	case ActGoBottom:
		return "Move cursor to bottom line"
	case ActPageUp:
		return "Scroll and move cursor up one page"
	case ActPageDown:
		return "Scroll and move cursor down one page"
	case ActDeletePrefix:
		return "Arm linewise delete prefix (dd)"
	case ActYankPrefix:
		return "Arm linewise yank prefix (yy)"
	case ActGoPrefix:
		return "Arm jump to line prefix (gg)"
	case ActInsert:
		return "Enter Insert mode before cursor"
	case ActAppend:
		return "Enter Insert mode after cursor"
	case ActInsertLineStart:
		return "Enter Insert mode at start of line"
	case ActAppendLineEnd:
		return "Enter Insert mode at end of line"
	case ActOpenBelow:
		return "Open new line below and enter Insert mode"
	case ActOpenAbove:
		return "Open new line above and enter Insert mode"
	case ActDeleteChar:
		return "Delete character at cursor"
	case ActDeleteToEnd:
		return "Delete characters to end of line"
	case ActPasteAfter:
		return "Paste register content after cursor"
	case ActPasteBefore:
		return "Paste register content before cursor"
	case ActUndo:
		return "Undo previous edit"
	case ActRedo:
		return "Redo previously undone edit"
	case ActVisual:
		return "Toggle character-wise Visual mode"
	case ActVisualLine:
		return "Toggle line-wise Visual mode"
	case ActVisualYank:
		return "Yank selection to register and system clipboard"
	case ActVisualDelete:
		return "Delete selection to register"
	case ActCut:
		return "Cut selection or current line to register"
	case ActCopy:
		return "Copy selection or current line to register"
	case ActPaste:
		return "Paste register content at cursor"
	case ActSelectAll:
		return "Select entire buffer content"
	case ActEscape:
		return "Escape to Normal mode"
	case ActToggleRendered:
		return "Switch between the Rendered view and Raw"
	case ActFindNext:
		return "Go to the find's next match"
	case ActFindPrev:
		return "Go to the find's previous match"
	case ActCommandLine:
		return "Open the command line"
	case ActLineDownFirst:
		return "Next line, first non-blank"
	case ActLineUpFirst:
		return "Previous line, first non-blank"
	case ActFirstNonBlank:
		return "First non-blank"
	case ActLeftWrap:
		return "Left, across lines"
	case ActFindCharForward:
		return "Find a character forward"
	case ActFindCharBack:
		return "Find a character back"
	case ActTillCharForward:
		return "Till a character forward"
	case ActTillCharBack:
		return "Till a character back"
	case ActFindCharRepeat:
		return "Repeat the character find"
	case ActFindCharReverse:
		return "Repeat the character find, reversed"
	case ActMatchPair:
		return "Matching bracket"
	case ActViewTop:
		return "Top of the view"
	case ActViewMiddle:
		return "Middle of the view"
	case ActViewBottom:
		return "Bottom of the view"
	case ActHalfPageDown:
		return "Half a page down"
	case ActHalfPageUp:
		return "Half a page up"
	case ActWORDForward:
		return "Next WORD"
	case ActWORDBack:
		return "Previous WORD"
	case ActWORDEnd:
		return "End of WORD"
	case ActWordEndBack:
		return "End of the previous word"
	case ActWORDEndBack:
		return "End of the previous WORD"
	case ActChangePrefix:
		return "Change"
	case ActChangeToEnd:
		return "Change to end of line"
	case ActSubstChar:
		return "Change the character"
	case ActSubstLine:
		return "Change the line"
	case ActYankToEnd:
		return "Copy to end of line"
	case ActReplaceChar:
		return "Replace the character"
	case ActJoin:
		return "Join lines"
	case ActJoinRaw:
		return "Join lines as they are"
	case ActToggleCase:
		return "Toggle case"
	case ActCaseToggleOp:
		return "Toggle case of the text"
	case ActLowerOp:
		return "Lowercase the text"
	case ActUpperOp:
		return "Uppercase the text"
	case ActIndentPrefix:
		return "Indent"
	case ActOutdentPrefix:
		return "Outdent"
	case ActRepeat:
		return "Repeat the last change"
	case ActRegisterPrefix:
		return "Name the register"
	case ActMarkSet:
		return "Set a mark"
	case ActMarkLine:
		return "Go to a mark's line"
	case ActMarkExact:
		return "Go to a mark"
	case ActMacroRecord:
		return "Record a macro"
	case ActMacroPlay:
		return "Play a macro"
	case ActVisualChange:
		return "Change the selection"
	case ActVisualReplace:
		return "Replace the selection's characters"
	case ActVisualToggleCase:
		return "Toggle the selection's case"
	case ActVisualLower:
		return "Lowercase the selection"
	case ActVisualUpper:
		return "Uppercase the selection"
	case ActVisualIndent:
		return "Indent the selection"
	case ActVisualOutdent:
		return "Outdent the selection"
	case ActVisualOther:
		return "Go to the selection's other end"
	case ActDeleteWordBack:
		return "Delete the word before the cursor"
	case ActDeleteLineBack:
		return "Delete back to the indent"
	case ActInsertOneShot:
		return "Run one Normal-mode command"
	case ActVisualBlock:
		return "Select a block"
	case ActVisualOtherCol:
		return "Go to the block's other column"
	case ActVisualInsert:
		return "Insert before the block"
	case ActVisualAppend:
		return "Append after the block"
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
	return m
}

// actionModes reports which binding classes accept an action.
func actionModes(a Action) (normal, visual, insert bool) {
	switch a {
	case ActLeft, ActDown, ActUp, ActRight, ActLineStart, ActLineEnd,
		ActWordForward, ActWordBack, ActWordEnd, ActParaForward, ActParaBack,
		ActPageUp, ActPageDown:
		return true, true, true
	case ActGoBottom, ActGoPrefix, ActVisual, ActVisualLine, ActVisualBlock, ActFindNext, ActFindPrev, ActCommandLine,
		ActRegisterPrefix, ActMarkSet, ActMarkLine, ActMarkExact:
		return true, true, false
	case ActDeletePrefix, ActYankPrefix,
		ActInsert, ActAppend, ActInsertLineStart, ActAppendLineEnd,
		ActOpenBelow, ActOpenAbove,
		ActDeleteChar, ActDeleteToEnd, ActPasteAfter, ActPasteBefore,
		ActChangePrefix, ActChangeToEnd, ActSubstChar, ActSubstLine, ActYankToEnd,
		ActReplaceChar, ActJoin, ActJoinRaw, ActToggleCase, ActCaseToggleOp, ActLowerOp, ActUpperOp,
		ActIndentPrefix, ActOutdentPrefix, ActRepeat, ActMacroRecord, ActMacroPlay:
		return true, false, false
	case ActCut, ActCopy, ActPaste, ActSelectAll, ActEscape, ActToggleRendered:
		return true, true, true
	case ActUndo, ActRedo:
		return true, false, true
	case ActDeleteWordBack, ActDeleteLineBack, ActInsertOneShot:
		return false, false, true
	case ActVisualYank, ActVisualDelete, ActVisualChange, ActVisualReplace, ActVisualToggleCase, ActVisualLower,
		ActVisualUpper, ActVisualIndent, ActVisualOutdent, ActVisualOther, ActVisualOtherCol, ActVisualInsert,
		ActVisualAppend:
		return false, true, false
	case ActLineDownFirst, ActLineUpFirst, ActFirstNonBlank, ActLeftWrap,
		ActFindCharForward, ActFindCharBack, ActTillCharForward, ActTillCharBack,
		ActFindCharRepeat, ActFindCharReverse, ActMatchPair,
		ActViewTop, ActViewMiddle, ActViewBottom, ActHalfPageDown, ActHalfPageUp,
		ActWORDForward, ActWORDBack, ActWORDEnd, ActWordEndBack, ActWORDEndBack:
		return true, true, false // the Vim motions Insert has no keys for: Enter and Backspace type there
	}
	return false, false, false
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
		// line and character motions (Space stays the host's: AutoDoc's leader)
		mv(ch(tui.KeyEnter), ActLineDownFirst)
		mv(ch('+'), ActLineDownFirst)
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
	km[n('p')] = ActPasteAfter
	km[n('P')] = ActPasteBefore
	km[n('u')] = ActUndo
	km[KeyChord{Mode: ModeNormal, Code: 'r', Ctrl: true}] = ActRedo
	km[KeyChord{Mode: ModeInsert, Code: 'w', Ctrl: true}] = ActDeleteWordBack
	km[KeyChord{Mode: ModeInsert, Code: 'u', Ctrl: true}] = ActDeleteLineBack
	km[KeyChord{Mode: ModeInsert, Code: 'o', Ctrl: true}] = ActInsertOneShot
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
