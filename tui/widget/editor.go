package widget

import (
	"fmt"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/keymap"
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
	core      *EditorCore // the behaviour; a field, never embedded
	doc       docSlot     // a Rendered view drawn in place of the text (editor_document.go)
	cells     cellLayout  // the geometry, in cells: implements EditorLayout
	keepPlace bool        // a text set keeps the reader's place (SetKeepPlace, ReplaceValue)
	// shown is the cursor and viewport Layout last kept the cursor in view for: a layout pass
	// reveals it only when either changed, so the wheel's scroll stays
	shown viewMark

	menu    *EditorMenu  // the right-click menu: off unless a consumer turns it on
	cmdline *CommandLine // the ':' prompt: on unless a consumer turns it off
}

var (
	_ tui.Focusable      = (*Editor)(nil)
	_ tui.CursorReporter = (*Editor)(nil)
	_ tui.CursorShaper   = (*Editor)(nil)
	_ EditorLayout       = (*cellLayout)(nil)
	_ EditorViewport     = (*cellLayout)(nil)
)

// Core is the editor's behaviour, for code that drives it apart from this view.
func (e *Editor) Core() *EditorCore { return e.core }

// Init implements tui.Component: the core publishes, copies and times through this context.
func (e *Editor) Init(ctx *tui.Context) {
	e.Base.Init(ctx)
	e.core.Bind(ctx, &e.cells)
	if e.doc.view != nil { // given before the mount
		ctx.Mount(e.doc.view)
	}
}

// SetRuler moves the guide to column col; 0 removes it.
func (e *Editor) SetRuler(col int) {
	e.cells.ruler = max(col, 0)
	e.MarkDirty()
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
	return max(len(fmt.Sprint(len(e.core.buf.textLines))), 4) + gutterGap
}

// NewEditor builds an empty editor initialized with the default Vim keymap,
// modal editing enabled, and the "jk" escape chord armed. Custom options
// can select alternative keysets (e.g. WithNanoKeymap, WithStandardKeymap)
// or customize capabilities and styles.
func NewEditor(opts ...EditorOption) *Editor {
	e := &Editor{core: newEditorCore()}
	e.cells = cellLayout{ed: e, wrap: WrapNone, styles: defaultTextInputStyles()}
	e.core.Bind(nil, &e.cells)
	e.menu = NewEditorMenu(e, e.core)
	e.menu.SetRows(func() []MenuItemModel { return EditorContextItems(e) })
	e.cmdline = NewCommandLine(e, e.core)
	e.cmdline.SetEnabled(true)
	for _, o := range opts {
		if o != nil {
			o(e)
		}
	}
	e.core.finishConstruction()
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

// SetKeepPlace makes a text set keep the reader's place, as ReplaceValue does: decl's keepPlace.
func (e *Editor) SetKeepPlace(v bool) { e.keepPlace = v }

// KeepsPlace reports whether a text set keeps the reader's place (SetKeepPlace).
func (e *Editor) KeepsPlace() bool { return e.keepPlace }

// ReplaceValue replaces the text as SetValue does, but keeps the reader's place: the cursor on
// its line and column as far as the new text has them, and the first shown line. It is for a
// viewer a program refreshes (decl's keepPlace), which scrolling must not lose each time.
func (e *Editor) ReplaceValue(s string) {
	at, top, left := e.core.Cursor(), e.cells.top, e.cells.left
	e.core.SetValue(s)
	e.core.SetCursor(at)
	e.cells.top, e.cells.left = min(top, max(e.core.LineCount()-1, 0)), left
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
// bar for Insert, underline for Replace (as Vim draws it). Visual mode is
// already shown in the status line and selection.
func (e *Editor) CursorShape() tui.CursorShape {
	switch e.core.Mode() {
	case ModeInsert:
		return tui.CursorShapeBar
	case ModeReplace:
		return tui.CursorShapeUnderline
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

// OverlayKeymap folds overlay into the keymap of an editor already built: see
// [EditorCore.OverlayKeymap]. A declarative host, whose Editor is built from markup, uses it where
// a Go host would pass [WithKeymap].
func (e *Editor) OverlayKeymap(overlay Keymap) { e.core.OverlayKeymap(overlay) }

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

// SetJumps makes list the one the editor's Ctrl+O and Ctrl+I walk, and goTo how it reaches a
// place in another document or view (see EditorCore.SetJumps).
func (e *Editor) SetJumps(list *JumpList, goTo func(JumpStep)) { e.core.SetJumps(list, goTo) }

// SetJumpDoc names the document the editor shows, as the host's jump list knows it.
func (e *Editor) SetJumpDoc(doc string) { e.core.SetJumpDoc(doc) }

// SetJumpView is how the editor learns the host's view of its document (see EditorCore.SetJumpView).
func (e *Editor) SetJumpView(view func() string) { e.core.SetJumpView(view) }

// JumpHere is the cursor's place as a jump list holds it.
func (e *Editor) JumpHere() JumpPos { return e.core.JumpHere() }

// NamedRegister returns register name's content (see EditorCore.NamedRegister).
func (e *Editor) NamedRegister(name rune) (text string, linewise, ok bool) {
	return e.core.NamedRegister(name)
}

// SetNamedRegister sets register name as a yank into it would (see EditorCore.SetNamedRegister).
func (e *Editor) SetNamedRegister(name rune, text string, linewise bool) bool {
	return e.core.SetNamedRegister(name, text, linewise)
}

// --- event handling -----------------------------------------------------------

// HandleEvent handles mouse, bracketed paste, focus, chord timer ticks, and keyboard events
// across modal and modeless editing profiles. The pointer is the view's: it becomes a buffer
// position here; everything else is the core's.
func (e *Editor) HandleEvent(ev tui.Event) bool {
	if e.doc.shown { // a key the document view left; the hidden text takes nothing
		if k, ok := ev.(tui.KeyEvent); ok {
			return e.documentKey(k)
		}
		return false
	}
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
		if e.menu.Enabled() && t.Kind == tui.KeyPress { // a repeat opens no second menu
			if _, ok := e.Context().KeyCommand(t, EditorContextMenu); ok {
				return e.OpenContextMenu()
			}
		}
		return e.core.HandleKey(t)
	}
	return false
}

// --- keys ----------------------------------------------------------------------

// KeyScope is the scope of the keyset and mode class the editor is in: "tui.editor.vim.normal".
func (e *Editor) KeyScope() keymap.Scope { return e.core.KeyScope() }

// KeyBindings is what the editor binds in the mode it is in (tui.KeyBinder): its context menu, and
// its live table after the host overlay.
func (e *Editor) KeyBindings(m *keymap.Map) []keymap.Binding {
	var out []keymap.Binding
	if e.menu.Enabled() {
		out = append(out, tui.BindingsOf(m, EditorContextMenu)...)
	}
	return append(out, e.core.keyBindings(m)...)
}
