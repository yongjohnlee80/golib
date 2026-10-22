package widget

import "github.com/yongjohnlee80/golib/highlight"

// Deprecated declarations: kept so a consumer still builds, NOT maintained, and removed in the next
// minor version. Each names its replacement. The editor's core is reached through Core(), and
// configured with WithCore; its command line through CommandLine(), and its menu through
// ContextMenu() (editor-D17).

// Value returns the buffer joined with newlines.
//
// Deprecated: use e.Core().Value. Not maintained; removed in the next minor version.
func (e *Editor) Value() string { return e.core.Value() }

// Mode reports the current mode.
//
// Deprecated: use e.Core().Mode. Not maintained; removed in the next minor version.
func (e *Editor) Mode() EditorMode { return e.core.Mode() }

// ReadOnly reports whether edits are refused.
//
// Deprecated: use e.Core().ReadOnly. Not maintained; removed in the next minor version.
func (e *Editor) ReadOnly() bool { return e.core.ReadOnly() }

// SetReadOnly makes the editor a VIEWER: motions, counts, visual
// selection, yank, and search all work; every mutating action (insert
// entry, delete, paste, undo/redo, typed text) is refused, and an active
// Insert session returns to Normal (in modal mode). Hosts use it for panels the user
// navigates but must not change.
//
// Deprecated: use e.Core().SetReadOnly. Not maintained; removed in the next minor version.
func (e *Editor) SetReadOnly(v bool) { e.core.SetReadOnly(v) }

// Line reports the cursor position (0-based) for status bars.
//
// Deprecated: use e.Core().Line. Not maintained; removed in the next minor version.
func (e *Editor) Line() (row, col int) { return e.core.Line() }

// SetLine moves the cursor to row/col (both clamped to the document) and
// scrolls it into view — the programmatic sibling of the motions, for
// hosts driving search, jump-to-error, and reveal. Pending input settles
// first; the mode is left alone.
//
// Deprecated: use e.Core().SetLine. Not maintained; removed in the next minor version.
func (e *Editor) SetLine(row, col int) { e.core.SetLine(row, col) }

// SetCursorPosition moves the cursor to a position in the document, counted
// as Qt's TextEdit.cursorPosition counts it — characters (grapheme clusters),
// a line break one — and scrolls it into view, as SetLine does. A position
// past the end is the end.
//
// Deprecated: use e.Core().SetCursorPosition. Not maintained; removed in the next minor version.
func (e *Editor) SetCursorPosition(pos int) { e.core.SetCursorPosition(pos) }

// Lines returns a snapshot of the document's lines — what a host needs to
// search without re-splitting Value().
//
// Deprecated: use e.Core().Lines. Not maintained; removed in the next minor version.
func (e *Editor) Lines() []string { return e.core.Lines() }

// Keyset reports the active editing & keymap profile.
//
// Deprecated: use e.Core().Keyset. Not maintained; removed in the next minor version.
func (e *Editor) Keyset() Keyset { return e.core.Keyset() }

// SetKeyset switches the editing profile on a LIVE editor, so a host can offer
// "Vim / TextEdit" as a user preference without rebuilding the widget and
// losing what the operator is working on.
//
// The document survives: text, cursor, viewport, undo/redo history, and the
// yank register are all untouched, because a preference change is not a reason
// to lose a buffer.
//
// In-flight input does NOT survive, because it was addressed to the profile
// being left: a pending count or operator prefix, a visual selection, and an
// open undo group are all discarded, and the next edit starts a fresh group. A
// half-typed escape chord is an exception — it is the operator's TEXT, so it is
// committed to the buffer the way any other key would settle it, rather than
// dropped.
//
// Mode follows the new profile: Vim lands in Normal with the cursor clamped
// onto a grapheme, the modeless profiles land in Insert. Each publishes the
// usual [ModeChangedEvent], so a status bar updates without a special case.
//
// Host bindings from [WithKeymap] are replayed onto the new profile's base
// table — a rebound key means it for the editor, not for one profile of it.
// Switching to the profile already active is a no-op, pending input included.
//
// Deprecated: use e.Core().SetKeyset. Not maintained; removed in the next minor version.
func (e *Editor) SetKeyset(ks Keyset) { e.core.SetKeyset(ks) }

// Keymap returns a defensive copy of the editor's active keymap.
//
// Deprecated: use e.Core().Keymap. Not maintained; removed in the next minor version.
func (e *Editor) Keymap() Keymap { return e.core.Keymap() }

// OverlayKeymap folds overlay into the keymap of an editor already built: see
// [EditorCore.OverlayKeymap]. A declarative host, whose Editor is built from markup, uses it where
// a Go host would pass [WithKeymap].
//
// Deprecated: use e.Core().OverlayKeymap. Not maintained; removed in the next minor version.
func (e *Editor) OverlayKeymap(overlay Keymap) { e.core.OverlayKeymap(overlay) }

// EscapeChord returns the configured two-rune escape chord (e.g. "jk"), or "" if disabled.
//
// Deprecated: use e.Core().EscapeChord. Not maintained; removed in the next minor version.
func (e *Editor) EscapeChord() string { return e.core.EscapeChord() }

// Bindings returns all discrete key chords configured in this editor's active keymap,
// sorted deterministically by mode, key chord, and action.
//
// Multi-rune escape sequences (such as the modal Insert-mode "jk" chord) operate via
// the chord timeout engine rather than single-chord mappings, and are reported via
// [Editor.EscapeChord] and [KeymapSnapshot.EscapeChord].
//
// Deprecated: use e.Core().Bindings. Not maintained; removed in the next minor version.
func (e *Editor) Bindings() []KeyBinding { return e.core.Bindings() }

// BindingsForMode returns all active bindings available when the editor is in mode m.
//
// Deprecated: use e.Core().BindingsForMode. Not maintained; removed in the next minor version.
func (e *Editor) BindingsForMode(m EditorMode) []KeyBinding { return e.core.BindingsForMode(m) }

// SnapshotKeymap generates a complete, serializable runtime reflection snapshot of the
// editor's active key configuration, profile, and action mappings.
//
// Deprecated: use e.Core().SnapshotKeymap. Not maintained; removed in the next minor version.
func (e *Editor) SnapshotKeymap() KeymapSnapshot { return e.core.SnapshotKeymap() }

// ActionForChord looks up the bound action for a given key chord.
//
// Deprecated: use e.Core().ActionForChord. Not maintained; removed in the next minor version.
func (e *Editor) ActionForChord(kc KeyChord) (Action, bool) { return e.core.ActionForChord(kc) }

// ChordsForAction returns all key chords that map to the specified action.
//
// Deprecated: use e.Core().ChordsForAction. Not maintained; removed in the next minor version.
func (e *Editor) ChordsForAction(act Action) []KeyChord { return e.core.ChordsForAction(act) }

// Copy yanks the selected text, or the current line without a selection, to
// the unnamed register and attempts to export it to the system clipboard.
// When yanking is disabled it changes neither destination.
//
// Deprecated: use e.Core().Copy. Not maintained; removed in the next minor version.
func (e *Editor) Copy() { e.core.Copy() }

// Cut deletes the selection, or the current line without a selection, into
// the unnamed register. As with keyboard deletes, it does NOT export to the
// system clipboard; a read-only editor refuses the mutation.
//
// Deprecated: use e.Core().Cut. Not maintained; removed in the next minor version.
func (e *Editor) Cut() { e.core.Cut() }

// Paste inserts the unnamed editor register at the cursor, not the system
// clipboard. A read-only editor refuses the mutation.
//
// Deprecated: use e.Core().Paste. Not maintained; removed in the next minor version.
func (e *Editor) Paste() { e.core.Paste() }

// Undo reverts the last edit group, as u does; Redo reapplies it, as Ctrl+R does. A read-only
// editor, or one without undo history, refuses both.
//
// Deprecated: use e.Core().Undo. Not maintained; removed in the next minor version.
func (e *Editor) Undo() { e.core.Undo() }

// Redo reapplies the most recently undone edit group.
//
// Deprecated: use e.Core().Redo. Not maintained; removed in the next minor version.
func (e *Editor) Redo() { e.core.Redo() }

// CanUndo reports whether Undo would change the text now.
//
// Deprecated: use e.Core().CanUndo. Not maintained; removed in the next minor version.
func (e *Editor) CanUndo() bool { return e.core.CanUndo() }

// CanRedo reports whether Redo would change the text now.
//
// Deprecated: use e.Core().CanRedo. Not maintained; removed in the next minor version.
func (e *Editor) CanRedo() bool { return e.core.CanRedo() }

// SelectedText returns the visual selection ("" outside visual modes or when selection is disabled).
//
// Deprecated: use e.Core().SelectedText. Not maintained; removed in the next minor version.
func (e *Editor) SelectedText() string { return e.core.SelectedText() }

// SelectionRange returns the visual selection as a region from (row, col) up to, not including,
// (endRow, endCol): rows and columns from 0, a column counting grapheme clusters, as Line reports
// the cursor. A line-wise selection runs from the start of its first line to the end of its last.
// ok is false outside the visual modes, or when selection is disabled. It is the region
// SelectedText returns the text of.
//
// Deprecated: use e.Core().SelectionRange. Not maintained; removed in the next minor version.
func (e *Editor) SelectionRange() (row, col, endRow, endCol int, ok bool) {
	return e.core.SelectionRange()
}

// SetRegister imports text into the unnamed register (the application's
// value-inspect copy path).
//
// Deprecated: use e.Core().SetRegister. Not maintained; removed in the next minor version.
func (e *Editor) SetRegister(text string, linewise bool) { e.core.SetRegister(text, linewise) }

// Register returns the unnamed register's content.
//
// Deprecated: use e.Core().Register. Not maintained; removed in the next minor version.
func (e *Editor) Register() (text string, linewise bool) { return e.core.Register() }

// SetJumps makes list the one the editor's Ctrl+O and Ctrl+I walk, and goTo how it reaches a
// place in another document or view (see EditorCore.SetJumps).
//
// Deprecated: use e.Core().SetJumps. Not maintained; removed in the next minor version.
func (e *Editor) SetJumps(list *JumpList, goTo func(JumpStep)) { e.core.SetJumps(list, goTo) }

// SetJumpDoc names the document the editor shows, as the host's jump list knows it.
//
// Deprecated: use e.Core().SetJumpDoc. Not maintained; removed in the next minor version.
func (e *Editor) SetJumpDoc(doc string) { e.core.SetJumpDoc(doc) }

// SetJumpView is how the editor learns the host's view of its document (see EditorCore.SetJumpView).
//
// Deprecated: use e.Core().SetJumpView. Not maintained; removed in the next minor version.
func (e *Editor) SetJumpView(view func() string) { e.core.SetJumpView(view) }

// JumpHere is the cursor's place as a jump list holds it.
//
// Deprecated: use e.Core().JumpHere. Not maintained; removed in the next minor version.
func (e *Editor) JumpHere() JumpPos { return e.core.JumpHere() }

// NamedRegister returns register name's content (see EditorCore.NamedRegister).
//
// Deprecated: use e.Core().NamedRegister. Not maintained; removed in the next minor version.
func (e *Editor) NamedRegister(name rune) (text string, linewise, ok bool) {
	return e.core.NamedRegister(name)
}

// SetNamedRegister sets register name as a yank into it would (see EditorCore.SetNamedRegister).
//
// Deprecated: use e.Core().SetNamedRegister. Not maintained; removed in the next minor version.
func (e *Editor) SetNamedRegister(name rune, text string, linewise bool) bool {
	return e.core.SetNamedRegister(name, text, linewise)
}

// Commands is the editor's command registry, to add to; nil when it has none.
//
// Deprecated: use e.Core().Commands. Not maintained; removed in the next minor version.
func (e *Editor) Commands() *Commands { return e.core.Commands() }

// RunCommand runs one command line, as the prompt does (EditorCore.RunCommand).
//
// Deprecated: use e.Core().RunCommand. Not maintained; removed in the next minor version.
func (e *Editor) RunCommand(line string) error { return e.core.RunCommand(line) }

// SetHighlighter replaces the highlighter at runtime; nil turns highlighting
// off. Every line is highlighted afresh.
//
// Deprecated: use e.Core().SetHighlighter. Not maintained; removed in the next minor version.
func (e *Editor) SetHighlighter(h highlight.Highlighter) { e.core.SetHighlighter(h) }

// SetCommandLineEnabled turns the ':' prompt on or off; off closes it if it is open.
//
// Deprecated: use e.CommandLine().SetEnabled. Not maintained; removed in the next minor version.
func (e *Editor) SetCommandLineEnabled(on bool) { e.cmdline.SetEnabled(on) }

// CommandLineEnabled reports whether ':' opens the prompt.
//
// Deprecated: use e.CommandLine().Enabled. Not maintained; removed in the next minor version.
func (e *Editor) CommandLineEnabled() bool { return e.cmdline.Enabled() }

// CommandLineOpen reports whether the prompt is showing.
//
// Deprecated: use e.CommandLine().IsOpen. Not maintained; removed in the next minor version.
func (e *Editor) CommandLineOpen() bool { return e.cmdline.IsOpen() }

// OpenCommandLine opens the prompt with initial in its field, as ':' does: a menu's or a key's way
// in. False when the prompt is off or there is nowhere to open it.
//
// Deprecated: use e.CommandLine().Open. Not maintained; removed in the next minor version.
func (e *Editor) OpenCommandLine(initial string) bool { return e.cmdline.Open(initial) }

// WithCommandLine turns the ':' prompt on (the default) or off. Off, ':' reaches the application
// and RunCommand still runs lines from code.
//
// Deprecated: use e.CommandLine().SetEnabled(on) after NewEditor. Not maintained; removed in the next minor version.
func WithCommandLine(on bool) EditorOption { return func(e *Editor) { e.cmdline.SetEnabled(on) } }

// ContextMenuOpen reports whether the right-click menu is showing.
//
// Deprecated: use e.ContextMenu().IsOpen. Not maintained; removed in the next minor version.
func (e *Editor) ContextMenuOpen() bool { return e.menu.IsOpen() }

// WithSelection configures whether visual selection is enabled on the editor.
//
// Deprecated: use WithCore(CoreSelection(enabled)). Not maintained; removed in the next minor version.
func WithSelection(enabled bool) EditorOption { return WithCore(CoreSelection(enabled)) }

// WithYank configures whether yanking to system clipboard and registers is enabled.
//
// Deprecated: use WithCore(CoreYank(enabled)). Not maintained; removed in the next minor version.
func WithYank(enabled bool) EditorOption { return WithCore(CoreYank(enabled)) }

// WithOnWrite is how the editor's w, wq and x save (CoreOnWrite).
//
// Deprecated: use WithCore(CoreOnWrite(fn)). Not maintained; removed in the next minor version.
func WithOnWrite(fn func(quit bool) error) EditorOption { return WithCore(CoreOnWrite(fn)) }

// WithOnQuit is how the editor's q and q! quit (CoreOnQuit).
//
// Deprecated: use WithCore(CoreOnQuit(fn)). Not maintained; removed in the next minor version.
func WithOnQuit(fn func(force bool) error) EditorOption { return WithCore(CoreOnQuit(fn)) }

// WithCommands gives the editor a command registry of its own in place of DefaultCommands (nil:
// none, and no prompt). To add to the defaults instead, use Commands().Add after construction.
//
// Deprecated: use WithCore(CoreCommands(s)). Not maintained; removed in the next minor version.
func WithCommands(s *Commands) EditorOption { return WithCore(CoreCommands(s)) }

// WithHighlighter sets the Editor's highlighter; nil is none.
//
// Deprecated: use WithCore(CoreHighlighter(h)). Not maintained; removed in the next minor version.
func WithHighlighter(h highlight.Highlighter) EditorOption { return WithCore(CoreHighlighter(h)) }

// WithSyntaxStyles sets what each highlight style looks like.
//
// Deprecated: use WithCore(CoreSyntaxStyles(st)). Not maintained; removed in the next minor version.
func WithSyntaxStyles(st SyntaxStyles) EditorOption { return WithCore(CoreSyntaxStyles(st)) }

// WithUndo configures whether the editor maintains undo/redo history.
//
// Deprecated: use WithCore(CoreUndo(enabled)). Not maintained; removed in the next minor version.
func WithUndo(enabled bool) EditorOption { return WithCore(CoreUndo(enabled)) }

// WithKeyset selects a predefined keyset and editing profile.
//
// Deprecated: use WithCore(CoreKeyset(ks)). Not maintained; removed in the next minor version.
func WithKeyset(ks Keyset) EditorOption { return WithCore(CoreKeyset(ks)) }

// WithInitialText seeds the buffer (cursor at the document start, initial editing
// mode, empty undo history).
//
// Deprecated: use WithCore(CoreInitialText(s)). Not maintained; removed in the next minor version.
func WithInitialText(s string) EditorOption { return WithCore(CoreInitialText(s)) }

// WithEscapeChord sets the Insert-mode escape chord (default "jk"): exactly
// two unmodified printable runes, or "" to disable (Esc alone). Anything
// else panics.
//
// Deprecated: use WithCore(CoreEscapeChord(chord)). Not maintained; removed in the next minor version.
func WithEscapeChord(chord string) EditorOption { return WithCore(CoreEscapeChord(chord)) }

// WithKeymap overlays entries onto the default table. ActUnbound removes a
// default binding; every entry is validated at construction (panics on
// unknown actions or unsupported mode/action combinations).
//
// Deprecated: use WithCore(CoreKeymap(overlay)). Not maintained; removed in the next minor version.
func WithKeymap(overlay Keymap) EditorOption { return WithCore(CoreKeymap(overlay)) }

// WithModalEditing configures whether the editor operates the Vim tripartite
// modal state machine (Normal, Insert, Visual) or acts as a modeless editor.
//
// Deprecated: use WithCore(CoreModal(modal)). Not maintained; removed in the next minor version.
func WithModalEditing(modal bool) EditorOption { return WithCore(CoreModal(modal)) }

// WithEditorReadOnly configures whether the editor is in read-only viewer mode.
//
// Deprecated: use WithCore(CoreReadOnly(ro)). Not maintained; removed in the next minor version.
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
//
// Deprecated: use WithCore(CoreOnModeChange(fn)). Not maintained; removed in the next minor version.
func WithOnModeChange(fn func(EditorMode)) EditorOption { return WithCore(CoreOnModeChange(fn)) }

// WithOnChange calls fn after every EDIT — the same moment the widget publishes
// a ChangeEvent. It is not called by SetValue: a program replacing the buffer
// has made no edit, and reporting one would mark a freshly loaded file dirty.
//
// Deprecated: use WithCore(CoreOnChange(fn)). Not maintained; removed in the next minor version.
func WithOnChange(fn func()) EditorOption { return WithCore(CoreOnChange(fn)) }

// WithOnCursorPositionChange calls fn whenever the cursor moves to another line or column: by a
// key, a click, an edit, or the program (SetValue, SetLine, SetCursorPosition), as Qt's
// TextEdit.cursorPositionChanged fires. It is told once per event, after the event is handled.
//
// Deprecated: use WithCore(CoreOnCursorPositionChange(fn)). Not maintained; removed in the next minor version.
func WithOnCursorPositionChange(fn func()) EditorOption {
	return WithCore(CoreOnCursorPositionChange(fn))
}

// WithVimKeymap configures the modal Vim keymap and editing model.
// Also ensures the fast escape chord "jk" is armed by default.
//
// Deprecated: use WithCore(CoreKeyset(KeysetVim)). Not maintained; removed in the next minor version.
func WithVimKeymap() EditorOption { return WithCore(CoreKeyset(KeysetVim)) }

// WithNanoKeymap configures the non-modal Nano-style editing profile.
//
// Deprecated: use WithCore(CoreKeyset(KeysetNano)). Not maintained; removed in the next minor version.
func WithNanoKeymap() EditorOption { return WithCore(CoreKeyset(KeysetNano)) }

// WithStandardKeymap configures the standard GUI/TextEdit editing profile.
//
// Deprecated: use WithCore(CoreKeyset(KeysetStandard)). Not maintained; removed in the next minor version.
func WithStandardKeymap() EditorOption { return WithCore(CoreKeyset(KeysetStandard)) }

// WithSyntaxStyles replaces what each highlight style looks like.
//
// Deprecated: use e.Core().SetSyntaxStyles. Not maintained; removed in the next minor version.
func (e *Editor) WithSyntaxStyles(st SyntaxStyles) *Editor {
	e.core.SetSyntaxStyles(st)
	return e
}
