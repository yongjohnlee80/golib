package widget

import (
	"fmt"
	"time"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Editor provides an embedded, configurable multi-line text editor designed for
// code editing, configuration editing, and interactive query authoring.
//
// # Architecture & Editing Profiles
//
// Editor supports configurable editing profiles and keysets ([Keyset]):
//
//   - [KeysetVim] (default): Classical Vim tripartite modal state machine (Normal, Insert, Visual).
//   - [KeysetNano]: Modeless terminal editor with Nano-style control shortcuts (Ctrl+K cut line, Ctrl+U paste, Ctrl+A/E line start/end).
//   - [KeysetStandard]: Modeless desktop/GUI editor with standard shortcuts (Ctrl+A, Ctrl+C, Ctrl+V, Ctrl+X, Ctrl+Z, etc.).
//
// In addition to predefined keysets, the editor supports custom keymap overlays ([WithKeymap]),
// configurable escape chords ([WithEscapeChord]), and toggling modal vs modeless editing ([WithModalEditing]).
//
// # Modal State Machine Architecture (Vim Keyset)
//
// When modal editing is active ([KeysetVim] or [WithModalEditing](true)), Editor implements
// the classical Vim tripartite modal state machine:
//
//	     ┌────────────────────────────────────────────────────────┐
//	     │                      NORMAL MODE                       │
//	     │  - Navigation (h, j, k, l, w, b, e, 0, $, gg, G)       │
//	     │  - Operators (d, y, p, P, x, u, Ctrl+R)                │
//	     │  - Numeric counts (e.g. 5j, 3dd, 10w)                  │
//	     │  - Unbound keys (Space!) BUBBLE for app leader menus   │
//	     └───────────┬───────────────────────────────▲────────────┘
//	i, a, o, O       │                               │  Esc or "jk"
//	enters insert    │                               │  chord timeout
//	                 ▼                               │
//	     ┌───────────────────────────────┐           │
//	     │          INSERT MODE          │           │
//	     │  - Direct text typing         │───────────┘
//	     │  - Real-cursor IME anchoring  │
//	     │  - "jk" fast escape chord     │
//	     └───────────────────────────────┘
//	                 ▲
//	     v, V        │                               │ Esc
//	     enters      │                               │ returns to Normal
//	     visual      ▼                               │
//	     ┌───────────────────────────────────────────┴┐
//	     │            VISUAL / VISUAL-LINE            │
//	     │  - Character-wise (v) or Line-wise (V)     │
//	     │  - Active selection highlighting           │
//	     │  - Actions (y: yank, d/x: delete)          │
//	     └────────────────────────────────────────────┘
//
// # Architectural Invariants and Capabilities
//
//  1. Unbound Key Bubbling and Leader Menus:
//     In Normal and Visual modes, any key not explicitly bound in the keymap—including
//     the Space bar—is NOT consumed (HandleEvent returns false). The event bubbles up
//     the component tree, allowing the hosting application to attach global leader-key
//     menus (e.g. "Space f f" to open finder, "Space w" to switch splits) with ZERO
//     custom editor cooperation or intercepted event wrappers.
//
//  2. Fast Escape Chord ("jk"):
//     In Insert mode, Editor supports two-key escape chords (default "jk"). When the
//     first rune is typed, it is held temporarily. If the second rune arrives within
//     chordTimeout (default 300ms), the editor transitions to Normal mode without modifying
//     buffer text. If the timeout expires or an unrelated key arrives, the held rune
//     is flushed into the buffer as normal text.
//
//  3. Single Unnamed Register & Linewise Semantics:
//     Yank and delete operations populate an internal unnamed register. Linewise
//     operations (dd, yy, VisualLine) record their linewise attribute so that paste
//     (p / P) opens and populates lines above or below the cursor.
//
//  4. System Clipboard & Secret Safety:
//     Explicit yanks ('y' in visual mode or 'yy' in normal mode) copy text to the
//     operating system clipboard (if supported by the backend) and publish [YankEvent].
//     Crucially, deletions (d, dd, x) fill only the internal register and NEVER copy
//     to the system clipboard or emit YankEvent. This ensures sensitive strings
//     (passwords, tokens) deleted during editing are never leaked to external clipboards.
//
//  5. Bounded Undo Ring with Edit Grouping:
//     The undo history is ring-bounded ([editHistoryCap] = 64 snapshots). Continuous
//     typing in Insert mode is batched into a single undo transaction, so a single 'u'
//     in Normal mode reverts the entire insert session.
//
//  6. Hardware Cursor Reporting and Shaping:
//     Editor implements [tui.CursorReporter] and [tui.CursorShaper]. In Normal mode,
//     the terminal cursor is a block in Normal and Visual modes; in Insert mode,
//     a vertical beam.
//
//  7. Configurable Capabilities:
//     Editor capabilities can be fine-tuned or restricted:
//     - Selection: Visual modes and range selection can be enabled or disabled ([canSelect]).
//     - Yank & System Clipboard: OS clipboard and internal register copying can be enabled or restricted ([WithYank]).
//     - Bounded Undo Ring: Undo and redo tracking can be enabled or bypassed ([WithUndo]).
//     - Modality: The editor can run modally (Vim tripartite) or modelessly (Nano, Standard) ([WithModalEditing], [WithKeyset]).
//
//  8. Structural Runtime Keymap Reflection:
//     Hosts and overlays can introspect the editor's live key configuration at runtime:
//     - Query active profile ([Editor.Keyset]) and escape chord ([Editor.EscapeChord]).
//     - Query structured keybindings with semantic actions and human-readable descriptions ([Editor.Bindings], [Editor.BindingsForMode]).
//     - Export serializable snapshots for command palettes or help popups ([Editor.SnapshotKeymap]).
//     - Resolve chord actions ([Editor.ActionForChord]) or find chords for an action ([Editor.ChordsForAction]).
//
// # Concurrency Model
//
//   - Ownership: loop-goroutine-owned. All editing methods, mode switches, and buffer mutations
//     must run on the application event loop goroutine.
//   - Timers: Escape chord timeouts are managed via internal loop ticks, eliminating background goroutines.
//
// # Usage Examples
//
// 1. Embedding in a titled panel with status hints:
//
//	ed := widget.NewEditor(
//		widget.WithEditorReadOnly(false),
//	)
//	panel := widget.NewBox(ed,
//		widget.WithTitle("Configuration Editor"),
//		widget.WithStatus("i: insert | Esc: normal | u: undo"),
//	)
//
// 2. Custom escape chord and initial text:
//
//	codeEditor := widget.NewEditor(
//		widget.WithEscapeChord("jk"),
//	)
//	codeEditor.SetValue("package main\n\nfunc main() {\n\tprintln(\"hello world\")\n}\n")
//
// 3. Modeless standard editor with runtime keymap reflection:
//
//	ed := widget.NewEditor(
//		widget.WithStandardKeymap(),
//	)
//	snap := ed.SnapshotKeymap()
//	for _, b := range snap.Bindings {
//		fmt.Printf("%s: %s (%s)\n", b.Chord, b.Name, b.Description)
//	}
//
// # Core and view
//
// Editor is an [EditorCore] and its cell layout (ADR 1791385086): the core holds the text, the
// modes, the keys and undo; the Editor lays it out in cells, hit-tests the pointer, scrolls, and
// paints. [Editor.Core] hands the core to code that drives it apart from this view.
type Editor struct {
	Base
	core  *EditorCore // the behaviour; a field, never embedded
	cells cellLayout  // the geometry, in cells: implements EditorLayout
	// shown is the cursor and viewport Layout last kept the cursor in view for: a layout pass
	// reveals it only when either changed, so the wheel's scroll stays
	shown viewMark

	menu *EditorMenu // the right-click menu: off unless a consumer turns it on
}

// cellLayout is the Editor's view of its core in terminal cells: the viewport, the gutter, the
// ruler, the looks, and the drag's edge auto-scroll. It is the EditorLayout the core reveals,
// measures and pages through.
type cellLayout struct {
	ed *Editor // the widget it lays out: its context measures and repaints

	// viewport (same discipline as TextArea)
	wrap WrapMode
	top  int
	left int
	w, h int // the text's area: the gutter's columns are not in w

	// numbers shows each line's number in a gutter at the left; gutter is the columns it took at
	// the last layout (0 without numbers)
	numbers bool
	gutter  int
	// ruler is the column a vertical guide marks, 1-based; 0 for none (WithRuler).
	ruler int

	// cursorColor is the hardware cursor's colour while cursorColored (tui.CursorColorer)
	cursorColor   style.Color
	cursorColored bool
	// numberColor is the line numbers' colour while numberColored; muted and faint otherwise
	numberColor   style.Color
	numberColored bool

	styles TextInputStyles

	// dragEdge is -1 or +1 while the dragging pointer is above or below the view, which keeps
	// scrolling on dragTick (a GUI editor's auto-scroll); dragX is the pointer's column.
	dragEdge   int
	dragX      int
	dragCancel func()
}

var (
	_ tui.Focusable      = (*Editor)(nil)
	_ tui.CursorReporter = (*Editor)(nil)
	_ tui.CursorShaper   = (*Editor)(nil)
	_ EditorLayout       = (*cellLayout)(nil)
)

// Reveal scrolls (line, col) into the viewport.
func (l *cellLayout) Reveal(line, col int) { l.ed.reveal(line, col) }

// PageLines is the viewport's height: a page.
func (l *cellLayout) PageLines() int { return max(l.h, 1) }

// Measure is s's width in cells, under the App's width policy.
func (l *cellLayout) Measure(s string) int { return l.ed.measure(s) }

// Changed repaints, and lays out again when the line count's digits move the gutter's width.
func (l *cellLayout) Changed(int) {
	if l.numbers && l.ed.gutterWidth() != l.gutter {
		l.ed.RequestLayout() // the lines' count has another number of digits: the gutter's width moves
	}
	l.ed.MarkDirty()
}

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

// Core is the editor's behaviour, for code that drives it apart from this view.
func (e *Editor) Core() *EditorCore { return e.core }

// Init implements tui.Component: the core publishes, copies and times through this context.
func (e *Editor) Init(ctx *tui.Context) {
	e.Base.Init(ctx)
	e.core.Bind(ctx, &e.cells)
}

// defaultEditorStyles are an Editor's looks before any option.
func defaultEditorStyles() TextInputStyles {
	return TextInputStyles{Placeholder: mutedPlaceholder(), Selection: style.New().Reverse(true)}
}

// WithEditorStyles overrides the style hooks (TextInput slots).
// WithRuler marks column col (1-based: 120 marks the 120th) with a vertical guide, where text is
// meant to wrap: vim's colorcolumn, as a line. It is drawn in the empty cells past each line's
// text, in the placeholder's muted look, so a line that crosses it stays readable. 0 draws none.
func WithRuler(col int) EditorOption { return func(e *Editor) { e.cells.ruler = max(col, 0) } }

// SetRuler moves the guide to column col; 0 removes it.
func (e *Editor) SetRuler(col int) {
	e.cells.ruler = max(col, 0)
	e.MarkDirty()
}

func WithEditorStyles(st TextInputStyles) EditorOption {
	return func(e *Editor) {
		e.cells.styles = TextInputStyles{
			Text:        st.Text.Inherit(e.cells.styles.Text),
			Placeholder: st.Placeholder.Inherit(e.cells.styles.Placeholder),
			Selection:   st.Selection.Inherit(e.cells.styles.Selection),
			Error:       st.Error.Inherit(e.cells.styles.Error),
		}
	}
}

// SetCursorColor gives the hardware cursor a colour of its own over the text, a theme's accent,
// so it is seen on any page; the terminal's own colour otherwise.
func (e *Editor) SetCursorColor(c style.Color) {
	e.cells.cursorColor, e.cells.cursorColored = c, true
	e.MarkDirty()
}

// SetLineNumberColor gives the line numbers a colour of their own, a theme's dim tone, so they
// stay out of the text's way; muted and faint otherwise.
func (e *Editor) SetLineNumberColor(c style.Color) {
	e.cells.numberColor, e.cells.numberColored = c, true
	e.MarkDirty()
}

// CursorColor implements tui.CursorColorer.
func (e *Editor) CursorColor() (style.Color, bool) { return e.cells.cursorColor, e.cells.cursorColored }

// WithEditorLineNumbers shows each line's number in a gutter at the left.
func WithEditorLineNumbers(v bool) EditorOption { return func(e *Editor) { e.cells.numbers = v } }

// SetLineNumbers shows or hides the gutter of line numbers.
func (e *Editor) SetLineNumbers(v bool) {
	if e.cells.numbers == v {
		return
	}
	e.cells.numbers = v
	e.RequestLayout()
	e.MarkDirty()
}

// SetWrap selects WrapNone or WrapSoft while the editor runs, as WithEditorWrap does at
// construction; the cursor stays where it is.
func (e *Editor) SetWrap(m WrapMode) {
	if m != WrapNone && m != WrapSoft {
		panic(fmt.Sprintf("widget: Editor.SetWrap: mode %d is not WrapNone or WrapSoft", m))
	}
	if e.cells.wrap == m {
		return
	}
	e.cells.wrap, e.cells.left = m, 0
	e.RequestLayout()
	e.MarkDirty()
}

// GutterWidth is the columns the line numbers take for the text as it is now, the gap after them
// included; 0 while they are hidden. A host sizing the editor to hold a width of text adds
// it: the gutter's columns come out of the editor's width.
func (e *Editor) GutterWidth() int { return e.gutterWidth() }

// gutterGap is the blank columns between the line numbers and the text.
const gutterGap = 2

// gutterWidth is the columns the line numbers take: the widest number, four digits at least so the
// gutter keeps its width as a note grows, and the gap after it; 0 without numbers.
func (e *Editor) gutterWidth() int {
	if !e.cells.numbers {
		return 0
	}
	return max(len(fmt.Sprint(len(e.core.buf.lines))), 4) + gutterGap
}

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

// NewEditor builds an empty editor initialized with the default Vim keymap,
// modal editing enabled, and the "jk" escape chord armed. Custom options
// can select alternative keysets (e.g. WithNanoKeymap, WithStandardKeymap)
// or customize capabilities and styles.
func NewEditor(opts ...EditorOption) *Editor {
	e := &Editor{core: newEditorCore()}
	e.cells = cellLayout{ed: e, wrap: WrapNone, styles: defaultEditorStyles()}
	e.core.Bind(nil, &e.cells)
	e.menu = NewEditorMenu(e, e.core)
	e.menu.SetRows(func() []MenuItemModel { return EditorContextItems(e) })
	for _, o := range opts {
		if o != nil {
			o(e)
		}
	}
	e.core.settleOptions()
	return e
}

// Value returns the buffer joined with newlines.
func (e *Editor) Value() string { return e.core.Value() }

// SetValue is a document-boundary operation: pending input
// settles, the editor returns to Normal mode (or Insert mode if modeless),
// cursor and command state reset, content is replaced, and undo/redo history
// is CLEARED. The register is preserved.
func (e *Editor) SetValue(s string) {
	e.cells.top, e.cells.left = 0, 0 // a new document starts at its top
	e.core.SetValue(s)
}

// Mode reports the current mode.
func (e *Editor) Mode() EditorMode { return e.core.Mode() }

// ReadOnly reports whether edits are refused.
func (e *Editor) ReadOnly() bool { return e.core.ReadOnly() }

// SetReadOnly makes the editor a VIEWER: motions, counts, visual
// selection, yank, and search all work; every mutating action (insert
// entry, delete, paste, undo/redo, typed text) is refused, and an active
// Insert session returns to Normal (in modal mode). Hosts use it for panels the user
// navigates but must not change.
func (e *Editor) SetReadOnly(v bool) { e.core.SetReadOnly(v) }

// Line reports the cursor position (0-based) for status bars.
func (e *Editor) Line() (row, col int) { return e.core.Line() }

// SetLine moves the cursor to row/col (both clamped to the document) and
// scrolls it into view — the programmatic sibling of the motions, for
// hosts driving search, jump-to-error, and reveal. Pending input settles
// first; the mode is left alone.
func (e *Editor) SetLine(row, col int) { e.core.SetLine(row, col) }

// SetCursorPosition moves the cursor to a position in the document, counted
// as Qt's TextEdit.cursorPosition counts it — characters (grapheme clusters),
// a line break one — and scrolls it into view, as SetLine does. A position
// past the end is the end.
func (e *Editor) SetCursorPosition(pos int) { e.core.SetCursorPosition(pos) }

// Lines returns a snapshot of the document's lines — what a host needs to
// search without re-splitting Value().
func (e *Editor) Lines() []string { return e.core.Lines() }

// AcceptsFocus implements tui.Focusable.
func (e *Editor) AcceptsFocus() bool { return true }

// CursorShape implements tui.CursorShaper: block for Normal and Visual,
// bar for Insert. Visual mode is already shown in the status line and selection.
func (e *Editor) CursorShape() tui.CursorShape {
	switch e.core.keys.mode {
	case ModeInsert:
		return tui.CursorShapeBar
	}
	return tui.CursorShapeBlock
}

// --- runtime keymap reflection -------------------------------------------

// Keyset reports the active editing & keymap profile.
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
func (e *Editor) SetKeyset(ks Keyset) { e.core.SetKeyset(ks) }

// Keymap returns a defensive copy of the editor's active keymap.
func (e *Editor) Keymap() Keymap { return e.core.Keymap() }

// EscapeChord returns the configured two-rune escape chord (e.g. "jk"), or "" if disabled.
func (e *Editor) EscapeChord() string { return e.core.EscapeChord() }

// Bindings returns all discrete key chords configured in this editor's active keymap,
// sorted deterministically by mode, key chord, and action.
//
// Multi-rune escape sequences (such as the modal Insert-mode "jk" chord) operate via
// the chord timeout engine rather than single-chord mappings, and are reported via
// [Editor.EscapeChord] and [KeymapSnapshot.EscapeChord].
func (e *Editor) Bindings() []KeyBinding { return e.core.Bindings() }

// BindingsForMode returns all active bindings available when the editor is in mode m.
func (e *Editor) BindingsForMode(m EditorMode) []KeyBinding { return e.core.BindingsForMode(m) }

// SnapshotKeymap generates a complete, serializable runtime reflection snapshot of the
// editor's active key configuration, profile, and action mappings.
func (e *Editor) SnapshotKeymap() KeymapSnapshot { return e.core.SnapshotKeymap() }

// ActionForChord looks up the bound action for a given key chord.
func (e *Editor) ActionForChord(kc KeyChord) (Action, bool) { return e.core.ActionForChord(kc) }

// ChordsForAction returns all key chords that map to the specified action.
func (e *Editor) ChordsForAction(act Action) []KeyChord { return e.core.ChordsForAction(act) }

// --- semantic commands ----------------------------------------------------------

// Copy yanks the selected text, or the current line without a selection, to
// the unnamed register and attempts to export it to the system clipboard.
// When yanking is disabled it changes neither destination.
func (e *Editor) Copy() { e.core.Copy() }

// Cut deletes the selection, or the current line without a selection, into
// the unnamed register. As with keyboard deletes, it does NOT export to the
// system clipboard; a read-only editor refuses the mutation.
func (e *Editor) Cut() { e.core.Cut() }

// Paste inserts the unnamed editor register at the cursor, not the system
// clipboard. A read-only editor refuses the mutation.
func (e *Editor) Paste() { e.core.Paste() }

// Undo reverts the last edit group, as u does; Redo reapplies it, as Ctrl+R does. A read-only
// editor, or one without undo history, refuses both.
func (e *Editor) Undo() { e.core.Undo() }

// Redo reapplies the most recently undone edit group.
func (e *Editor) Redo() { e.core.Redo() }

// CanUndo reports whether Undo would change the text now.
func (e *Editor) CanUndo() bool { return e.core.CanUndo() }

// CanRedo reports whether Redo would change the text now.
func (e *Editor) CanRedo() bool { return e.core.CanRedo() }

// SelectedText returns the visual selection ("" outside visual modes or when selection is disabled).
func (e *Editor) SelectedText() string { return e.core.SelectedText() }

// SelectionRange returns the visual selection as a region from (row, col) up to, not including,
// (endRow, endCol): rows and columns from 0, a column counting grapheme clusters, as Line reports
// the cursor. A line-wise selection runs from the start of its first line to the end of its last.
// ok is false outside the visual modes, or when selection is disabled. It is the region
// SelectedText returns the text of.
func (e *Editor) SelectionRange() (row, col, endRow, endCol int, ok bool) {
	return e.core.SelectionRange()
}

// SetRegister imports text into the unnamed register (the application's
// value-inspect copy path).
func (e *Editor) SetRegister(text string, linewise bool) { e.core.SetRegister(text, linewise) }

// Register returns the unnamed register's content.
func (e *Editor) Register() (text string, linewise bool) { return e.core.Register() }

// --- event handling -----------------------------------------------------------

// HandleEvent handles mouse, bracketed paste, focus, chord timer ticks, and keyboard events
// across modal and modeless editing profiles. The pointer is the view's: it becomes a buffer
// position here; everything else is the core's.
func (e *Editor) HandleEvent(ev tui.Event) bool {
	switch t := ev.(type) {
	case tui.MouseEvent:
		return e.handleMouse(t)
	case tui.PointerCaptureLostEvent:
		e.core.EndDrag() // the selection made so far stays
		e.setDragEdge(0)
		return true
	case tui.PasteEvent:
		return e.core.HandlePaste(t.Text)
	case tui.FocusEvent:
		if !t.Gained {
			e.core.FocusLost()
		}
		return false // focus events are informational; let them bubble
	case tui.TickEvent:
		if e.core.Dragging() && e.cells.dragEdge != 0 {
			e.dragTick() // the drag's auto-scroll: a press settled any pending rune
			return true
		}
		return e.core.HandleTick() // the chord timeout
	case tui.KeyEvent:
		return e.core.HandleKey(t)
	}
	return false
}

// --- viewport & rendering (TextArea discipline) ------------------------------

func (e *Editor) wrapWidth() int { return wrapUsableWidth(e.core.buf.lines, e.view()) }

func (e *Editor) scrollable() bool { return wrapScrollable(e.core.buf.lines, e.view()) }

func (e *Editor) rowsOfLine(i int) int { return wrapRowsOfLine(e.core.buf.lines, i, e.view()) }

// ensureVisible scrolls the cursor into the viewport.
func (e *Editor) ensureVisible() { e.reveal(e.core.buf.ln, e.core.buf.col) }

// reveal scrolls (ln, col) into the viewport.
func (e *Editor) reveal(ln, col int) {
	l, lines := &e.cells, e.core.buf.lines
	if l.h <= 0 || l.w <= 0 {
		return
	}
	if ln < l.top {
		l.top = ln
	}
	l.top = lowestTop(lines, l.top, ln, l.h, e.view())
	l.top = max(0, min(l.top, len(lines)-1))
	if l.wrap == WrapNone {
		cx := e.core.buf.cellsAt(ln, col, e.measure)
		w := e.wrapWidth()
		if cx < l.left {
			l.left = cx
		}
		if cx >= l.left+w {
			l.left = cx - w + 1
		}
		l.left = max(l.left, 0)
	} else {
		l.left = 0
	}
}

// Layout is greedy on both axes. The gutter, when the numbers show, takes its columns from the
// text's area, never all of it.
func (e *Editor) Layout(c tui.Constraints) tui.Size {
	total := boundedMax(c.MaxW, max(c.MinW, 1))
	e.cells.gutter = min(e.gutterWidth(), total-1)
	e.cells.w = total - e.cells.gutter
	e.cells.h = boundedMax(c.MaxH, max(c.MinH, 1))
	if e.shown.moved(e.core.buf.ln*1_000_003+e.core.buf.col, e.cells.h*100_003+e.cells.w) {
		e.ensureVisible()
	}
	return c.Constrain(tui.Size{W: total, H: e.cells.h})
}

// Cursor implements tui.CursorReporter.
func (e *Editor) Cursor() (int, int, bool) {
	l, b := &e.cells, &e.core.buf
	if b.ln < l.top {
		return 0, 0, false
	}
	if l.wrap == WrapNone {
		x := b.cellsAt(b.ln, b.col, e.measure) - l.left
		y := b.ln - l.top
		if y >= l.h && l.h > 0 {
			return 0, 0, false
		}
		return l.gutter + max(x, 0), max(y, 0), true
	}
	y := 0
	v := e.view().settled(b.lines)
	for i := l.top; i < b.ln; i++ {
		y += wrapRowsOfLine(b.lines, i, v)
	}
	row, x := wrapPosOf(b.lines, b.ln, b.col, v)
	y += row
	if l.h > 0 && y >= l.h {
		return 0, 0, false
	}
	return l.gutter + x, y, true
}

// handleMouse implements the pointer contract.
//
// A press is a COMMAND BOUNDARY, not merely a cursor move, because the core holds
// modal state that a click has to resolve one way or the other (EditorCore.PressAt). The wheel
// scrolls the viewport and never moves the caret, so a reader can scroll while a caret
// stays where they left it.
func (e *Editor) handleMouse(m tui.MouseEvent) bool {
	switch {
	case m.Kind == tui.MouseWheel && m.Button == tui.WheelUp:
		return e.scrollLines(-1)
	case m.Kind == tui.MouseWheel && m.Button == tui.WheelDown:
		return e.scrollLines(1)
	case m.Kind == tui.MousePress && m.Button == tui.MouseLeft:
		x := max(m.X-e.cells.gutter, 0) // a press in the gutter is at the line's start
		return e.pressAt(x, m.Y)
	case m.Kind == tui.MousePress && m.Button == tui.MouseRight && e.menu.Enabled():
		// The selection is left as it is: the menu's Copy and Cut act on it.
		return e.menu.OpenAt(tui.Point{X: m.X, Y: m.Y})
	case m.Kind == tui.MouseMotion && m.Button == tui.MouseLeft && e.core.Dragging():
		return e.dragTo(m.X-e.cells.gutter, m.Y)
	case m.Kind == tui.MouseRelease && e.core.Dragging():
		e.endDrag()
		return true
	}
	return false
}

// pressAt places the caret at a clicked cell (EditorCore.PressAt), and keeps the pointer for a
// drag selection from there, so the drag goes on past the editor's edges.
func (e *Editor) pressAt(x, y int) bool {
	// The scroll-indicator column is not text. A press there is inert, and
	// consumed rather than bubbled: the column belongs to this widget.
	if e.scrollable() && x >= e.wrapWidth() {
		return true
	}
	ln, col := e.posAt(x, y)
	handled := e.core.PressAt(ln, col)
	if e.core.Dragging() {
		if ctx := e.Context(); ctx != nil {
			ctx.CapturePointer()
		}
	}
	return handled
}

// dragTo extends the drag-selection to the viewport cell (x, y), in visual mode: the same
// selection v makes, so y (vim) or Ctrl+C (Text mode) copies it. Past the top or bottom edge the
// view scrolls a line, and keeps scrolling while the pointer stays there (dragTick), as a GUI
// editor does.
func (e *Editor) dragTo(x, y int) bool {
	e.cells.dragX = x
	switch {
	case y < 0:
		e.setDragEdge(-1)
		e.scrollLines(-1)
	case e.cells.h > 0 && y >= e.cells.h:
		e.setDragEdge(1)
		e.scrollLines(1)
	default:
		e.setDragEdge(0)
	}
	e.extendDrag(x, min(max(y, 0), max(e.cells.h-1, 0)))
	return true
}

// dragTick is the auto-scroll's step while the pointer is held past an edge.
func (e *Editor) dragTick() {
	e.scrollLines(e.cells.dragEdge)
	row := 0
	if e.cells.dragEdge > 0 {
		row = max(e.cells.h-1, 0)
	}
	e.extendDrag(e.cells.dragX, row)
}

// setDragEdge starts or stops the auto-scroll as the pointer leaves or re-enters the view.
func (e *Editor) setDragEdge(edge int) {
	l := &e.cells
	l.dragEdge = edge
	switch {
	case edge != 0 && l.dragCancel == nil:
		if ctx := e.Context(); ctx != nil {
			l.dragCancel = ctx.Every(dragScrollInterval)
		}
	case edge == 0 && l.dragCancel != nil:
		l.dragCancel()
		l.dragCancel = nil
	}
}

// dragScrollInterval is the auto-scroll's pace: a line per tick while the pointer is past an edge.
const dragScrollInterval = 50 * time.Millisecond

// extendDrag moves the selection's moving end to the viewport cell (x, row).
func (e *Editor) extendDrag(x, row int) {
	ln, col := e.posAt(max(x, 0), row)
	e.core.DragTo(ln, col)
}

// endDrag ends the drag; the selection, if any, stays for the key combos to copy.
func (e *Editor) endDrag() {
	e.core.EndDrag()
	e.setDragEdge(0)
	if ctx := e.Context(); ctx != nil && ctx.HasPointerCapture() {
		ctx.ReleasePointer()
	}
}

// scrollLines scrolls by whole LOGICAL lines in both wrap modes.
//
// One MouseEvent is one step: the event carries a wheel direction and no
// magnitude, so N notches arrive as N events and this never multiplies. Visual
// rows are deliberately not the unit — the viewport stores a logical-line origin
// only (top/left), so an intra-line offset is not representable without new
// state and new invariants across render, Cursor, ensureVisible, click inversion
// and clamping. That is deferred to its own ADR.
func (e *Editor) scrollLines(delta int) bool {
	before := e.cells.top
	e.cells.top = min(max(e.cells.top+delta, 0), max(len(e.core.buf.lines)-1, 0))
	if e.cells.top != before {
		e.MarkDirty()
	}
	return true
}

// posAt inverts the viewport mapping in Cursor(): a viewport cell becomes a
// buffer position. It is the exact inverse of the forward path for each wrap
// mode, so a click resolves to the position the caret would be drawn at.
//
// Clamping: past end-of-line lands on the last column, below the last painted
// line lands on the last buffer line. A wide grapheme resolves to the cell it
// STARTS at, which is why the column walk accumulates measured widths
// instead of counting cells.
func (e *Editor) posAt(x, y int) (ln, col int) {
	l, b := &e.cells, &e.core.buf
	if len(b.lines) == 0 {
		return 0, 0
	}
	lastLn := len(b.lines) - 1

	if l.wrap == WrapNone {
		ln = min(l.top+max(y, 0), lastLn)
		cs := b.lineClusters(ln)
		return ln, e.colAtCells(cs, 0, len(cs), l.left+max(x, 0))
	}

	// WrapSoft: walk the same wrap computation the renderer used, rather than
	// dividing by width — one logical line spans several visual rows.
	remaining := max(y, 0)
	v := e.view().settled(b.lines)
	for i := l.top; i <= lastLn; i++ {
		rows := wrapRowsOfLine(b.lines, i, v)
		if remaining < rows || i == lastLn {
			cs := b.lineClusters(i)
			ranges := wrapRanges(cs, v.usable, e.measure)
			r := ranges[min(remaining, len(ranges)-1)]
			// Bounded to THIS row: wrapRanges is [start,end), and a word-wrapped
			// row can be shorter than the viewport, so an unbounded scan would run
			// through its blank tail into clusters painted on the NEXT visual row.
			return i, e.colAtCells(cs, r[0], r[1], max(x, 0))
		}
		remaining -= rows
	}
	return lastLn, e.core.normalMax(lastLn)
}

// colAtCells walks clusters in [from,end), accumulating measured cell widths, and
// returns the column whose cell span contains `cells`. A click in the trailing
// half of a double-width grapheme resolves to that grapheme, not the next one.
// Past the end of the range it clamps to the range's LAST column, which is what
// keeps a wrapped row's blank tail on its own row.
func (e *Editor) colAtCells(cs []string, from, end, cells int) int {
	end = min(end, len(cs))
	acc := 0
	for i := from; i < end; i++ {
		w := e.measure(cs[i])
		if cells < acc+w {
			return i
		}
		acc += w
	}
	return max(from, end-1)
}

func (e *Editor) wrapPos(ln, col int) (row, x int) {
	return wrapPosOf(e.core.buf.lines, ln, col, e.view())
}

// Render paints the viewport with the visual-selection fill.
func (e *Editor) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	if g := e.cells.gutter; g > 0 {
		e.renderGutter(s)
		s = s.Sub(tui.Rect{X: g, W: sz.W - g, H: sz.H})
	}
	e.renderText(s)
}

// renderGutter paints the line numbers, each line's on its first screen row, right-aligned: dimmed,
// so they stay out of the text's way, in their own colour (SetLineNumberColor) or muted and faint;
// the cursor's line in the text's colour, as Vim's CursorLineNr, so it shows where you are.
func (e *Editor) renderGutter(s tui.Surface) {
	l, b := &e.cells, &e.core.buf
	h := s.Size().H
	dim := l.styles.Text.Foreground(style.TokenTextMuted).Faint(true)
	if l.numberColored {
		dim = l.styles.Text.Foreground(l.numberColor)
	}
	s.Fill(tui.Rect{W: l.gutter, H: h}, " ", l.styles.Text)
	y := 0
	v := e.view().settled(b.lines)
	for ln := l.top; ln < len(b.lines) && y < h; ln++ {
		num := fmt.Sprint(ln + 1)
		st := dim
		if ln == b.ln {
			st = l.styles.Text
		}
		drawText(s, l.gutter-gutterGap-len(num), y, num, st)
		y += max(wrapRowsOfLine(b.lines, ln, v), 1)
	}
}

// renderText paints the text's area.
func (e *Editor) renderText(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	l, c := &e.cells, e.core
	// The whole area wears the text's look first, as a TextInput's does (Qt's
	// base behind a TextEdit): the cells past each line's text match the
	// cells under it, whatever the editor sits on.
	s.Fill(tui.Rect{W: sz.W, H: sz.H}, " ", l.styles.Text)
	w := e.wrapWidth()
	hlf := e.beginHighlightFrame()
	var lineStyles []highlight.Style
	styledLn := -1
	paintCluster := func(x, y int, cl string, ln, col int) {
		st := l.styles.Text
		if ln != styledLn {
			lineStyles, styledLn = hlf.Styles(ln), ln
		}
		if c.Highlighting() {
			k := highlight.Normal
			if col < len(lineStyles) {
				k = lineStyles[col]
			}
			if sst, ok := c.SyntaxStyle(k); ok {
				st = sst.Inherit(st)
			}
		}
		if e.focused() && c.Selected(ln, col) {
			st = l.styles.Selection.Inherit(st)
		}
		s.SetCell(x, y, cl, st)
	}
	lineFill := func(y, ln int) {
		// A line-wise highlight covers the WHOLE screen row (S2), text or
		// not; clusters then paint over the fill.
		if c.keys.mode == ModeVisualLine && e.focused() && c.Selected(ln, 0) {
			s.Fill(tui.Rect{X: 0, Y: y, W: w, H: 1}, " ", l.styles.Selection.Inherit(l.styles.Text))
		}
	}
	// ends is where each screen row's text ends, for the ruler
	ends := make([]int, sz.H)
	y := 0
	for ln := l.top; ln < len(c.buf.lines) && y < sz.H; ln++ {
		cs := c.buf.lineClusters(ln)
		if l.wrap == WrapNone {
			lineFill(y, ln)
			x := -l.left
			for col, cl := range cs {
				cw := s.StringWidth(cl)
				if x+cw > w {
					break
				}
				if x >= 0 {
					paintCluster(x, y, cl, ln, col)
				}
				x += cw
			}
			ends[y] = x
			y++
			continue
		}
		for _, r := range wrapRanges(cs, w, s.StringWidth) {
			if y >= sz.H {
				break
			}
			lineFill(y, ln)
			x := 0
			for col := r[0]; col < r[1]; col++ {
				paintCluster(x, y, cs[col], ln, col)
				x += s.StringWidth(cs[col])
			}
			ends[y] = x
			y++
		}
	}
	if l.ruler > 0 {
		x := l.ruler - 1
		if l.wrap == WrapNone {
			x -= l.left
		}
		if x >= 0 && x < w {
			st := l.styles.Placeholder.Inherit(l.styles.Text)
			for row := 0; row < sz.H; row++ {
				if ends[row] <= x {
					s.SetCell(x, row, "│", st)
				}
			}
		}
	}
	if e.scrollable() {
		paintScrollIndicator(s, sz.W-1, sz.H, l.top, wrapMaxTop(c.buf.lines, e.view()))
	}
}

// view is the layout state the shared soft-wrap geometry needs. It is the
// only place this widget's viewport is handed to textBuffer.
func (e *Editor) view() wrapView {
	return wrapView{w: e.cells.w, h: e.cells.h, wrap: e.cells.wrap, measure: e.measure}
}
