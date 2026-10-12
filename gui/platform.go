package gui

import "github.com/yongjohnlee80/golib/tui"

// The platform contract: text input, in gui's own terms.
//
// The backend's windowing toolkit (Gio today) is behind an adapter, so the IME
// state machine and the debug recorder see no toolkit type, and a recording
// made on one machine replays on another:
//
//	toolkit ──adapter──> InputEvent ──> imeState ──> tui.KeyEvent ──> App
//	    ▲                                                   │
//	    └──────adapter<── TextInputCmd <────────────────────┘
//
// The events model text input and IME editing, the path that was invisible
// when the macOS Hangul input could not be pinned down. Pointer, clipboard
// and transfer stay the backend's own in v1.

// Mods is the modifier bitmask, in tui's terms: one vocabulary from the
// platform contract to the widgets, with macOS's Command reported as Super.
type Mods = tui.Mods

// InputEvent is one event from the windowing toolkit, in gui's own terms.
type InputEvent interface{ inputEvent() }

// KeyInput is one key press or release, as the toolkit reported it: the key's
// name (a toolkit name such as "Return" or "A", untranslated), the modifiers
// held, and whether the key went down.
type KeyInput struct {
	Name  string
	Mods  Mods
	Press bool
}

// TextEdit replaces the buffer's runes [Start, End) with Text: committed text
// and composition updates both arrive this way, from an input method.
type TextEdit struct {
	Start, End int
	Text       string
}

// Composition is the range the input method is composing; Start < 0 means
// no composition is active.
type Composition struct {
	Start, End int
}

// SelectionSet is the selection the input method set.
type SelectionSet struct {
	Start, End int
}

// SnippetAsk is the input method asking for the buffer's text over a range:
// unanswered, its copy of the text diverges and it cancels the composition.
type SnippetAsk struct {
	Start, End int
}

// FocusChange reports whether the window's keyboard focus changed.
type FocusChange struct {
	Focused bool
}

func (KeyInput) inputEvent()     {}
func (TextEdit) inputEvent()     {}
func (Composition) inputEvent()  {}
func (SelectionSet) inputEvent() {}
func (SnippetAsk) inputEvent()   {}
func (FocusChange) inputEvent()  {}

// TextInputCmd is what gui tells the toolkit back about the text input.
type TextInputCmd interface{ textInputCmd() }

// SnippetReply is the buffer's text over a range: the answer to a SnippetAsk,
// so the toolkit's copy of the text matches the input method's.
type SnippetReply struct {
	Start, End int
	Text       string
}

// SelectionTell reports the selection and where the caret is, for the toolkit
// to place the input method's candidate window.
type SelectionTell struct {
	Start, End int
	Caret      Rect
}

// KeyboardShow asks the toolkit to open the software keyboard: on Wayland,
// the input method attaches only to a window that opened text input.
type KeyboardShow struct{}

// FocusTake asks the toolkit to give the window the keyboard focus.
type FocusTake struct{}

func (SnippetReply) textInputCmd()  {}
func (SelectionTell) textInputCmd() {}
func (KeyboardShow) textInputCmd()  {}
func (FocusTake) textInputCmd()     {}
