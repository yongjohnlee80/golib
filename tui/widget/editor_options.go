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
