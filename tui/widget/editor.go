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
// In addition to predefined keysets, the editor supports custom keymap overlays ([CoreKeymap]),
// configurable escape chords ([CoreEscapeChord]), and toggling modal vs modeless editing ([CoreModal]),
// each given to [WithCore].
//
// # Modal State Machine Architecture (Vim Keyset)
//
// When modal editing is active ([KeysetVim] or [CoreModal](true)), Editor implements
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
//     - Yank & System Clipboard: OS clipboard and internal register copying can be enabled or restricted ([CoreYank]).
//     - Bounded Undo Ring: Undo and redo tracking can be enabled or bypassed ([CoreUndo]).
//     - Modality: The editor can run modally (Vim tripartite) or modelessly (Nano, Standard) ([CoreModal], [CoreKeyset]).
//
//  8. Structural Runtime Keymap Reflection:
//     Hosts and overlays can introspect the editor's live key configuration at runtime:
//     - Query active profile ([EditorCore.Keyset]) and escape chord ([EditorCore.EscapeChord]).
//     - Query structured keybindings with semantic actions and human-readable descriptions ([EditorCore.Bindings], [EditorCore.BindingsForMode]).
//     - Export serializable snapshots for command palettes or help popups ([EditorCore.SnapshotKeymap]).
//     - Resolve chord actions ([EditorCore.ActionForChord]) or find chords for an action ([EditorCore.ChordsForAction]).
//     Each is the core's, through [Editor.Core].
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
//		widget.WithCore(widget.CoreReadOnly(false)),
//	)
//	panel := widget.NewBox(ed,
//		widget.WithTitle("Configuration Editor"),
//		widget.WithStatus("i: insert | Esc: normal | u: undo"),
//	)
//
// 2. Custom escape chord and initial text:
//
//	codeEditor := widget.NewEditor(
//		widget.WithCore(widget.CoreEscapeChord("jk")),
//	)
//	codeEditor.SetValue("package main\n\nfunc main() {\n\tprintln(\"hello world\")\n}\n")
//
// 3. Modeless standard editor with runtime keymap reflection:
//
//	ed := widget.NewEditor(
//		widget.WithCore(widget.CoreKeyset(widget.KeysetStandard)),
//	)
//	snap := ed.Core().SnapshotKeymap()
//	for _, b := range snap.Bindings {
//		fmt.Printf("%s: %s (%s)\n", b.Chord, b.Name, b.Description)
//	}
//
// # Core and view
//
// Editor is an [EditorCore] and its cell layout (ADR 1791385086): the core holds the text, the
// modes, the keys and undo; the Editor lays it out in cells, hit-tests the pointer, scrolls, and
// paints. [Editor.Core] hands the core to code that drives it apart from this view, and is the one
// way to reach its behaviour (its text, cursor, modes, keys, registers and undo); [WithCore] is the
// one way to configure it. What the Editor itself holds has its own accessor: the ':' prompt
// ([Editor.CommandLine]) and the right-click menu ([Editor.ContextMenu]).
type Editor struct {
	Base
	core      *EditorCore  // the behaviour; a field, never embedded
	doc       DocumentSlot // a Rendered view drawn in place of the text (editor_document.go)
	docToggle bool         // the core's Rendered toggle is wired (wireToggle)
	cells     cellLayout   // the geometry, in cells: implements EditorLayout
	keepPlace bool         // a text set keeps the reader's place (SetKeepPlace, ReplaceValue)
	// shown is the cursor and viewport Layout last kept the cursor in view for: a layout pass
	// reveals it only when either changed, so the wheel's scroll stays
	shown viewMark

	menu    *EditorMenu    // the right-click menu: off unless a consumer turns it on
	pointer *EditorPointer // the press, the drag, and the context menu's keys (editor_pointer.go)
	cmdline *CommandLine   // the ':' prompt: on unless a consumer turns it off
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
	if v := e.doc.Document(); v != nil { // given before the mount
		ctx.Mount(v)
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
// can select alternative keysets (WithCore(CoreKeyset(KeysetNano)), say)
// or customize capabilities and styles.
func NewEditor(opts ...EditorOption) *Editor {
	e := &Editor{core: newEditorCore()}
	e.cells = cellLayout{ed: e, wrap: WrapNone, styles: defaultTextInputStyles()}
	e.core.Bind(nil, &e.cells)
	e.menu = NewEditorMenu(e, e.core)
	e.pointer = NewEditorPointer(e.core, e.menu, editorPointerView{e})
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

// --- semantic commands ----------------------------------------------------------

// --- event handling -----------------------------------------------------------

// HandleEvent handles mouse, bracketed paste, focus, chord timer ticks, and keyboard events
// across modal and modeless editing profiles. The pointer is the view's: it becomes a buffer
// position here; everything else is the core's.
func (e *Editor) HandleEvent(ev tui.Event) bool {
	if e.doc.Shown() { // a key the document view left; the hidden text takes nothing
		if k, ok := ev.(tui.KeyEvent); ok {
			return e.documentKey(k)
		}
		return false
	}
	switch t := ev.(type) {
	case tui.MouseEvent:
		return e.wheel(t) || e.pointer.HandleEvent(t)
	case tui.PointerCaptureLostEvent:
		return e.pointer.HandleEvent(t)
	case tui.PasteEvent:
		return e.core.HandlePaste(t.Text)
	case tui.FocusEvent:
		if !t.Gained {
			e.core.FocusLost()
		}
		return false // focus events are informational; let them bubble
	case tui.TickEvent:
		// the drag's auto-scroll, else the chord timeout
		return e.pointer.HandleEvent(t) || e.core.HandleTick()
	case tui.KeyEvent:
		if e.pointer.MenuKey(t) { // the menu at the caret; off-screen, the key bubbles, the core untouched
			return e.pointer.OpenMenu()
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
