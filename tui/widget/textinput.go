package widget

import (
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// Single-Line Text Input & Form Field Architecture
//
// TextInput provides a feature-rich, single-line text input field designed for forms,
// prompt inputs, search bars, and dialog credentials. It features UAX #29 grapheme
// cluster cursor navigation, anchor selection, horizontal viewport scrolling, placeholder
// hints, password character masking, validation hooks, and synchronized form advance.
//
// # Subsystem Role & Responsibilities
//
//  1. Grapheme Cluster Integrity: Values are stored and addressed as Unicode Standard
//     Annex #29 extended grapheme clusters, in a one-line textBuffer, the editor's own.
//     Multi-byte runes, emoji zwj sequences, and combining accents never suffer midpoint
//     truncation or splitting; a combining mark typed after a letter joins it.
//  2. IME & Hardware Cursor Integration: Implements [tui.CursorReporter]. While focused,
//     the runtime places the terminal's hardware cursor exactly at the input caret cell,
//     anchoring OS IME composition popups directly beneath the active insertion point.
//  3. Bracketed Paste Protection: Ingests [tui.PasteEvent] atomically without replaying
//     as simulated keystrokes. Internal newlines are transformed into spaces, preventing
//     malicious or pasted multiline strings from inadvertently triggering early form submission.
//  4. Synchronous Advance Hook ([WithOnSubmit]): Supports synchronous form focus transitions.
//     When Enter is pressed and validation passes, [WithOnSubmit] executes immediately inside
//     the key handler before subsequent buffered keystrokes can leak into the vacated field.
//  5. Readline Keyboard Navigation: Supports common Readline / Emacs motion chords
//     (Ctrl+A for start, Ctrl+E for end, Ctrl+U to clear before cursor, Ctrl+W to delete word)
//     alongside standard arrow keys and Shift+arrow range selection.
//
// # Layout & Cursor Pipeline
//
//	┌─────────────────────────────────────────────────────────────┐
//	│ [TextInput] Single-Line Viewport (Height = 1)               │
//	│                                                             │
//	│  Visible Viewport Cells: [ w = Layout Width ]               │
//	│  ┌────────────────────────────────────────────────────────┐ │
//	│  │ ...scrolled text... [ Active Caret █ ] remainder text  │ │
//	│  └────────────────────────────────────────────────────────┘ │
//	│       ▲                        ▲                            │
//	│   scroll offset           Hardware Cursor Anchor            │
//	└─────────────────────────────────────────────────────────────┘
//
// # Architectural Invariants
//
//  1. Tab Traversal Non-Consumption:
//     TextInput does NOT consume Tab or Shift+Tab, so focus navigation stays framework-owned
//     across sibling components. The one exception is a completer ([WithCompleter]) with
//     something to offer at the cursor: then Tab completes, and Shift+Tab steps back through
//     the choices Tab is walking. With nothing to offer, Tab moves the focus as before.
//  2. Atomic Paste Events:
//     Paste operations generate exactly one [ChangeEvent] and never trigger a [SubmitEvent].
//  3. Validation Preflight on Enter:
//     Failing validation ([WithValidate]) sets the internal error state, visually styles the
//     text with [TextInputStyles.Error], suppresses both [WithOnSubmit] and [SubmitEvent],
//     and consumes Enter.
//  4. Enter Goes On After a Submit:
//     A valid Enter runs [WithOnSubmit], publishes [SubmitEvent], and is NOT consumed, as
//     QLineEdit ignores Return after emitting accepted: in a dialog, the dialog then answers
//     with its default button.
//
// # Concurrency & Goroutine Ownership
//
// TextInput is loop-goroutine-owned. Reading or setting values ([TextInput.Value],
// [TextInput.SetValue]) must be performed on the main application loop goroutine.
//
// # Usage Examples
//
// 1. Creating a validated username field with placeholder:
//
//	usernameInput := widget.NewTextInput(
//		widget.WithPlaceholder("Enter username (min 3 chars)..."),
//		widget.WithValidate(func(val string) error {
//			if len(strings.TrimSpace(val)) < 3 {
//				return errors.New("username too short")
//			}
//			return nil
//		}),
//	)
//
// 2. Creating a password input field:
//
//	passwordInput := widget.NewTextInput(
//		widget.WithPlaceholder("Password..."),
//		widget.WithMask('*'),
//	)
type TextInput struct {
	Base
	// buf is the value: one sanitized line, its cursor and selection anchor in clusters (line 0,
	// col). The editor's buffer, so a TextInput composes graphemes as the Editor does.
	buf    textBuffer
	scroll int // cells scrolled off the left edge
	width  int // last layout width (viewport cells, inside the padding)
	pad    int // cells inside each end: 1 where a native field frames the text, else 0

	placeholder uiText // shown (muted) while the value is empty
	mask        rune
	validate    func(string) error
	onSubmit    func(string)
	onEdit      func(string)
	verr        error

	completer TextCompleter
	walk      *textWalk // Tab's way through the choices; nil once another key is pressed

	styles TextInputStyles
}

var (
	_ tui.Focusable      = (*TextInput)(nil)
	_ tui.CursorReporter = (*TextInput)(nil)
)

// NewTextInput builds an empty single-line editor.
func NewTextInput(opts ...TextInputOption) *TextInput {
	t := &TextInput{
		buf:    newTextBuffer(),
		styles: defaultTextInputStyles(),
	}
	for _, o := range opts {
		if o != nil {
			o(t)
		}
	}
	return t
}

// Value returns the raw value (unmasked).
func (t *TextInput) Value() string { return t.buf.value() }

// clusters is the value as grapheme clusters; cursor and length are the cursor's cluster index and
// their count.
func (t *TextInput) clusters() []string { return t.buf.lineClusters(0) }
func (t *TextInput) cursor() int        { return t.buf.col }
func (t *TextInput) length() int        { return len(t.clusters()) }

// SetValue replaces the value programmatically (loop goroutine): cursor to
// the end, selection and validation error cleared. No ChangeEvent — the
// event reports user edits.
func (t *TextInput) SetValue(s string) {
	t.buf.setValue(sanitizeLine(s))
	t.verr = nil
	t.walk = nil
	t.ensureVisible()
	t.MarkDirty()
}

// Err returns the current validation error (set by a failed Enter, cleared
// on the next edit).
func (t *TextInput) Err() error { return t.verr }

// AcceptsFocus implements tui.Focusable.
func (t *TextInput) AcceptsFocus() bool { return true }

// sanitizeLine strips a single-line value: newlines become spaces, other
// C0 controls are dropped.
func sanitizeLine(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return ' '
		case r < 0x20 || r == 0x7f:
			return -1
		}
		return r
	}, strings.ReplaceAll(s, "\r\n", "\n"))
}

// selection returns the selected cluster range [lo, hi), ok=false when no
// selection exists.
func (t *TextInput) selection() (lo, hi int, ok bool) {
	l, h, ok := t.buf.selection()
	return l.col, h.col, ok
}

// deleteRange removes clusters [lo, hi) and moves the cursor to lo.
func (t *TextInput) deleteRange(lo, hi int) { t.buf.deleteRegion(taPos{col: lo}, taPos{col: hi}) }

// insert places text at the cursor (replacing any selection) as one atomic
// edit, then emits ChangeEvent.
func (t *TextInput) insert(text string) {
	t.buf.insertText(sanitizeLine(text))
	t.edited()
}

// edited finalizes a value mutation: error cleared, scroll fixed, repaint,
// ChangeEvent.
func (t *TextInput) edited() {
	t.verr, t.walk = nil, nil
	t.ensureVisible()
	t.MarkDirty()
	t.publish(ChangeEvent{Owner: t.NodeID(), Value: t.Value()})
	if t.onEdit != nil {
		t.onEdit(t.Value())
	}
}

// moveTo moves the cursor, extending the selection when extend is set and
// clearing it otherwise.
func (t *TextInput) moveTo(i int, extend bool) {
	t.buf.moveCursor(0, i, extend)
	t.walk = nil
	t.ensureVisible()
	t.MarkDirty()
}

// wordLeft/wordRight are readline-style word hops (spaces, then non-spaces).
func (t *TextInput) wordLeft() int {
	_, i := t.buf.wordLeft()
	return i
}

func (t *TextInput) wordRight() int {
	_, i := t.buf.wordRight()
	return i
}

// modMask are the modifiers that veto text insertion.
const nonTextMods = tui.ModCtrl | tui.ModAlt | tui.ModSuper | tui.ModHyper | tui.ModMeta

// HandleEvent implements the key contract.
func (t *TextInput) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.PasteEvent:
		t.insert(e.Text)
		return true
	case tui.KeyEvent:
		return t.handleKey(e)
	}
	return false
}

// KeyScope is a TextInput's key-map scope.
func (t *TextInput) KeyScope() keymap.Scope { return InputScope }

// keyCommand is which of the TextInput's commands e is; completion declines without a completer.
func (t *TextInput) keyCommand(e tui.KeyEvent) (keymap.Command, bool) {
	cmd, ok := t.Context().KeyCommand(e, inputCommands...)
	if ok && (cmd.ID == InputComplete.ID || cmd.ID == InputCompletePrev.ID) && t.completer == nil {
		return keymap.Command{}, false
	}
	return cmd, ok
}

func (t *TextInput) handleKey(e tui.KeyEvent) bool {
	if e.Kind == tui.KeyRelease {
		return false
	}
	cmd, ok := t.keyCommand(e)
	if !ok {
		// Printable text (never Tab — focus traversal is framework-owned).
		if e.Text != "" && e.Mods&nonTextMods == 0 && e.Code != tui.KeyTab {
			t.insert(e.Text)
			return true
		}
		return false
	}
	switch cmd.ID {
	case InputComplete.ID, InputCompletePrev.ID:
		return t.complete(cmd.ID == InputCompletePrev.ID)
	case InputSubmit.ID:
		if t.validate != nil {
			if err := t.validate(t.Value()); err != nil {
				t.verr = err
				t.MarkDirty()
				return true
			}
		}
		t.verr = nil
		t.MarkDirty()
		// The hook runs HERE, on the input lane, before this handler returns
		// and therefore before any further keystroke is dispatched. The event
		// follows and is delivered on the program lane as it always was.
		if t.onSubmit != nil {
			t.onSubmit(t.Value())
		}
		t.publish(SubmitEvent{Owner: t.NodeID(), Value: t.Value()})
		// NOT CONSUMED, as QLineEdit ignores Return after emitting accepted:
		// Enter goes on to the parent, so a field in a dialog lets the dialog
		// answer with its default button. A value the validator refuses (above)
		// does hold Enter — a dialog must not close over a field showing its
		// error.
		return false
	case InputBackspace.ID, InputWordBackspace.ID:
		word := cmd.ID == InputWordBackspace.ID
		switch lo, hi, ok := t.selection(); {
		case ok:
			t.deleteRange(lo, hi)
		case word && t.wordLeft() < t.cursor():
			t.deleteRange(t.wordLeft(), t.cursor())
		case !word && t.cursor() > 0:
			t.deleteRange(t.cursor()-1, t.cursor())
		default:
			return true // consumed; nothing to delete
		}
		t.edited()
	case InputDelete.ID:
		switch lo, hi, ok := t.selection(); {
		case ok:
			t.deleteRange(lo, hi)
		case t.cursor() < t.length():
			t.deleteRange(t.cursor(), t.cursor()+1)
		default:
			return true
		}
		t.edited()
	case InputLeft.ID, InputSelectLeft.ID:
		t.moveTo(t.cursor()-1, cmd.ID == InputSelectLeft.ID)
	case InputWordLeft.ID, InputSelectWordLeft.ID:
		t.moveTo(t.wordLeft(), cmd.ID == InputSelectWordLeft.ID)
	case InputRight.ID, InputSelectRight.ID:
		t.moveTo(t.cursor()+1, cmd.ID == InputSelectRight.ID)
	case InputWordRight.ID, InputSelectWordRight.ID:
		t.moveTo(t.wordRight(), cmd.ID == InputSelectWordRight.ID)
	case InputHome.ID, InputSelectHome.ID:
		t.moveTo(0, cmd.ID == InputSelectHome.ID)
	case InputEnd.ID, InputSelectEnd.ID:
		t.moveTo(t.length(), cmd.ID == InputSelectEnd.ID)
	// Readline subset (Ctrl+A/E/U/W).
	case InputLineStart.ID:
		t.moveTo(0, false)
	case InputLineEnd.ID:
		t.moveTo(t.length(), false)
	case InputKillLine.ID:
		if t.cursor() > 0 {
			t.deleteRange(0, t.cursor())
			t.edited()
		}
	case InputKillWord.ID:
		if w := t.wordLeft(); w < t.cursor() {
			t.deleteRange(w, t.cursor())
			t.edited()
		}
	}
	return true
}

// KeyBindings is the TextInput's commands as m binds them (tui.KeyBinder). Typed text is not a
// command.
func (t *TextInput) KeyBindings(m *keymap.Map) []keymap.Binding {
	return tui.BindingsOf(m, inputCommands...)
}
