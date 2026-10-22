package widget

import (
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
//     customization or partial overriding via [CoreKeymap].
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
