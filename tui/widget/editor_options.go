package widget

import "fmt"

// THE EDITOR'S OPTIONS: how an Editor is configured at construction. Most set the core's own
// options (WithCore); the rest set the view's.

// EditorOption customizes an Editor under construction.
type EditorOption func(*Editor)

// WithCore applies core options to an Editor: every behaviour EditorOption is one.
func WithCore(opts ...CoreOption) EditorOption {
	return func(e *Editor) {
		for _, o := range opts {
			if o != nil {
				o(e.core)
			}
		}
	}
}

// WithRuler marks column col (1-based: 120 marks the 120th) with a vertical guide, where text is
// meant to wrap: vim's colorcolumn, as a line. It is drawn in the empty cells past each line's
// text, in the placeholder's muted look, so a line that crosses it stays readable. 0 draws none.
func WithRuler(col int) EditorOption { return func(e *Editor) { e.cells.ruler = max(col, 0) } }

// WithEditorStyles overrides the style hooks (TextInput slots); zero fields keep defaults.
func WithEditorStyles(st TextInputStyles) EditorOption {
	return func(e *Editor) {
		e.cells.styles = st.over(e.cells.styles)
	}
}

// WithEditorLineNumbers shows each line's number in a gutter at the left.
func WithEditorLineNumbers(v bool) EditorOption { return func(e *Editor) { e.cells.numbers = v } }

// WithEditorWrap selects WrapNone (default) or WrapSoft.
func WithEditorWrap(m WrapMode) EditorOption {
	if m != WrapNone && m != WrapSoft {
		panic(fmt.Sprintf("widget: WithEditorWrap: mode %d is not WrapNone or WrapSoft", m))
	}
	return func(e *Editor) { e.cells.wrap = m }
}

// WithInitialText seeds the buffer (cursor at the document start, initial editing
// mode, empty undo history).
func WithInitialText(s string) EditorOption { return WithCore(CoreInitialText(s)) }

// WithEscapeChord sets the Insert-mode escape chord (default "jk"): exactly
// two unmodified printable runes, or "" to disable (Esc alone). Anything
// else panics.
func WithEscapeChord(chord string) EditorOption { return WithCore(CoreEscapeChord(chord)) }

// WithKeymap overlays entries onto the default table. ActUnbound removes a
// default binding; every entry is validated at construction (panics on
// unknown actions or unsupported mode/action combinations).
func WithKeymap(overlay Keymap) EditorOption { return WithCore(CoreKeymap(overlay)) }

// WithModalEditing configures whether the editor operates the Vim tripartite
// modal state machine (Normal, Insert, Visual) or acts as a modeless editor.
func WithModalEditing(modal bool) EditorOption { return WithCore(CoreModal(modal)) }

// WithEditorReadOnly configures whether the editor is in read-only viewer mode.
func WithEditorReadOnly(ro bool) EditorOption { return WithCore(CoreReadOnly(ro)) }

// WithOnModeChange calls fn with the new mode whenever the editor's mode
// changes — Normal to Insert, Insert to Visual — and not when it is set to the
// mode it already has.
//
// It is the constructor-time twin of the ModeChangedEvent this widget publishes
// on the bus. A caller that builds the editor before it is mounted — a
// declarative adapter, say — has no Context to subscribe with yet, and would
// otherwise have to wrap the widget to find out, which is exactly the kind of
// embedding that bypasses methods the wrapper thinks it has overridden.
func WithOnModeChange(fn func(EditorMode)) EditorOption { return WithCore(CoreOnModeChange(fn)) }

// WithOnChange calls fn after every EDIT — the same moment the widget publishes
// a ChangeEvent. It is not called by SetValue: a program replacing the buffer
// has made no edit, and reporting one would mark a freshly loaded file dirty.
func WithOnChange(fn func()) EditorOption { return WithCore(CoreOnChange(fn)) }

// WithOnCursorPositionChange calls fn whenever the cursor moves to another line or column: by a
// key, a click, an edit, or the program (SetValue, SetLine, SetCursorPosition), as Qt's
// TextEdit.cursorPositionChanged fires. It is told once per event, after the event is handled.
func WithOnCursorPositionChange(fn func()) EditorOption {
	return WithCore(CoreOnCursorPositionChange(fn))
}

// WithVimKeymap configures the modal Vim keymap and editing model.
// Also ensures the fast escape chord "jk" is armed by default.
func WithVimKeymap() EditorOption { return WithCore(CoreKeyset(KeysetVim)) }

// WithNanoKeymap configures the non-modal Nano-style editing profile.
func WithNanoKeymap() EditorOption { return WithCore(CoreKeyset(KeysetNano)) }

// WithStandardKeymap configures the standard GUI/TextEdit editing profile.
func WithStandardKeymap() EditorOption { return WithCore(CoreKeyset(KeysetStandard)) }

// WithKeyset selects a predefined keyset and editing profile.
func WithKeyset(ks Keyset) EditorOption { return WithCore(CoreKeyset(ks)) }

// normalizeKeyset maps anything outside the defined profiles onto Vim, which
// is the editor's default: a Keyset is a closed enum, and an out-of-range one
// is a caller bug that must not leave the editor with no keymap at all.
func normalizeKeyset(ks Keyset) Keyset {
	switch ks {
	case KeysetNano, KeysetStandard:
		return ks
	default:
		return KeysetVim
	}
}
