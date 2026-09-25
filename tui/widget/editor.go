package widget

import (
	"fmt"
	"github.com/yongjohnlee80/golib/highlight"
	"sort"
	"strings"
	"time"
	"unicode"

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
//     The undo history is ring-bounded ([editorUndoCap] = 64 snapshots). Continuous
//     typing in Insert mode is batched into a single undo transaction, so a single 'u'
//     in Normal mode reverts the entire insert session.
//
//  6. Hardware Cursor Reporting and Shaping:
//     Editor implements [tui.CursorReporter] and [tui.CursorShaper]. In Normal mode,
//     the terminal cursor is configured as a block; in Insert mode, as a vertical beam.
//
//  7. Configurable Capabilities:
//     Editor capabilities can be fine-tuned or restricted:
//     - Selection: Visual modes and range selection can be enabled or disabled ([canSelect]).
//     - Yank & System Clipboard: OS clipboard and internal register copying can be enabled or restricted ([canYank]).
//     - Bounded Undo Ring: Undo and redo tracking can be enabled or bypassed ([canUndo]).
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
type Editor struct {
	readOnly bool // viewer mode: motions and yank only

	// hl colours the buffer (editor_highlight.go); syntax is what each
	// highlight style looks like; hlCache remembers each line's colours and
	// what they were computed from.
	hl      highlight.Highlighter
	syntax  SyntaxStyles
	hlCache []hlLine

	// onModeChange and onChange are the constructor-time listeners for the two
	// notifications this widget also publishes on the bus. A caller that builds
	// the widget before it is mounted has no Context to subscribe with, and
	// these are how it hears — the same shape as Button's WithOnActivate.
	onModeChange func(EditorMode)
	onChange     func()
	Base
	textBuffer

	// viewport (same discipline as TextArea)
	wrap WrapMode
	top  int
	left int
	w, h int

	styles  TextInputStyles
	keymap  Keymap
	unbound map[KeyChord]bool // explicitly unbound chords (via ActUnbound)
	// overlay is every host-supplied binding, kept apart from the profile's
	// base table so a keyset switch can replay it. A host that rebinds a key
	// means it for the editor, not for one profile of it: without this, the
	// binding would survive or vanish depending on the order the options ran
	// in, and would vanish outright on [Editor.SetKeyset].
	overlay Keymap

	mode EditorMode

	// Normal/Visual command state.
	count        int      // pending count; 0 = none
	pendingAct   Action   // pending double-key prefix action; ActUnbound = none
	pendingChord KeyChord // the chord that armed it (completion = same chord)
	pendingCount int      // count captured when the prefix was armed
	vAnchor      taPos    // visual anchor (chord start of the selection)

	// Escape chord (Insert mode).
	chord       []rune // exactly two runes, or nil = disabled
	pendingRune rune   // held first chord rune; 0 = none
	chordCancel func() // cancels the addressed tick

	// Register & undo.
	regText     string
	regLinewise bool
	undo, redo  []editorSnap
	groupOpen   bool // an Insert-mode edit group is open

	chordTimeout time.Duration

	// Configurable capabilities.
	modal     bool   // true = Vim tripartite state machine; false = modeless editor
	canSelect bool   // true = visual / selection active
	canYank   bool   // true = system clipboard & register yanking active
	canUndo   bool   // true = bounded undo / redo history active
	keyset    Keyset // active editing & keymap profile
}

var (
	_ tui.Focusable      = (*Editor)(nil)
	_ tui.CursorReporter = (*Editor)(nil)
	_ tui.CursorShaper   = (*Editor)(nil)
)

// EditorOption customizes an Editor under construction.
type EditorOption func(*Editor)

// defaultEditorStyles are an Editor's looks before any option.
func defaultEditorStyles() TextInputStyles {
	return TextInputStyles{Selection: style.New().Reverse(true)}
}

// WithEditorStyles overrides the style hooks (TextInput slots).
func WithEditorStyles(st TextInputStyles) EditorOption {
	return func(e *Editor) {
		e.styles = TextInputStyles{
			Text:        st.Text.Inherit(e.styles.Text),
			Placeholder: st.Placeholder.Inherit(e.styles.Placeholder),
			Selection:   st.Selection.Inherit(e.styles.Selection),
			Error:       st.Error.Inherit(e.styles.Error),
		}
	}
}

// WithEditorWrap selects WrapNone (default) or WrapSoft.
func WithEditorWrap(m WrapMode) EditorOption {
	if m != WrapNone && m != WrapSoft {
		panic(fmt.Sprintf("widget: WithEditorWrap: mode %d is not WrapNone or WrapSoft", m))
	}
	return func(e *Editor) { e.wrap = m }
}

// WithInitialText seeds the buffer (cursor at the document start, initial editing
// mode, empty undo history).
func WithInitialText(s string) EditorOption {
	return func(e *Editor) {
		e.setValue(s)
		e.ln, e.col = 0, 0
	}
}

// WithEscapeChord sets the Insert-mode escape chord (default "jk"): exactly
// two unmodified printable runes, or "" to disable (Esc alone). Anything
// else panics.
func WithEscapeChord(chord string) EditorOption {
	rs := []rune(chord)
	if chord != "" && len(rs) != 2 {
		panic(fmt.Sprintf("widget: WithEscapeChord: %q is not exactly two runes (or empty to disable)", chord))
	}
	for _, r := range rs {
		if !unicode.IsPrint(r) {
			panic(fmt.Sprintf("widget: WithEscapeChord: %q contains a non-printable rune", chord))
		}
	}
	return func(e *Editor) {
		if chord == "" {
			e.chord = nil
		} else {
			e.chord = rs
		}
	}
}

// WithKeymap overlays entries onto the default table. ActUnbound removes a
// default binding; every entry is validated at construction (panics on
// unknown actions or unsupported mode/action combinations).
func WithKeymap(overlay Keymap) EditorOption {
	return func(e *Editor) {
		// Validate the whole overlay before folding any of it in: a panic
		// halfway through would otherwise leave half the bindings applied.
		for kc, act := range overlay {
			validateKeymapEntry(kc, act)
		}
		if e.overlay == nil {
			e.overlay = make(Keymap, len(overlay))
		}
		for kc, act := range overlay {
			e.overlay[kc] = act
		}
		e.applyOverlay(overlay)
	}
}

// WithModalEditing configures whether the editor operates the Vim tripartite
// modal state machine (Normal, Insert, Visual) or acts as a modeless editor.
func WithModalEditing(modal bool) EditorOption {
	return func(e *Editor) {
		e.modal = modal
		if !modal {
			e.setMode(ModeInsert)
		}
	}
}

// WithEditorReadOnly configures whether the editor is in read-only viewer mode.
func WithEditorReadOnly(ro bool) EditorOption {
	return func(e *Editor) {
		e.readOnly = ro
	}
}

// WithOnModeChange calls fn with the new mode whenever the editor's mode
// changes — Normal to Insert, Insert to Visual — and not when it is set to the
// mode it already has.
//
// It is the constructor-time twin of the ModeChangedEvent this widget publishes
// on the bus. A caller that builds the editor before it is mounted — a
// declarative adapter, say — has no Context to subscribe with yet, and would
// otherwise have to wrap the widget to find out, which is exactly the kind of
// embedding that bypasses methods the wrapper thinks it has overridden.
func WithOnModeChange(fn func(EditorMode)) EditorOption {
	return func(e *Editor) { e.onModeChange = fn }
}

// WithOnChange calls fn after every EDIT — the same moment the widget publishes
// a ChangeEvent. It is not called by SetValue: a program replacing the buffer
// has made no edit, and reporting one would mark a freshly loaded file dirty.
func WithOnChange(fn func()) EditorOption {
	return func(e *Editor) { e.onChange = fn }
}

// WithVimKeymap configures the modal Vim keymap and editing model.
// Also ensures the fast escape chord "jk" is armed by default.
func WithVimKeymap() EditorOption {
	return func(e *Editor) {
		e.applyKeyset(KeysetVim)
		if len(e.chord) == 0 {
			e.chord = []rune{'j', 'k'}
			e.chordTimeout = 300 * time.Millisecond
		}
	}
}

// WithNanoKeymap configures the non-modal Nano-style editing profile.
func WithNanoKeymap() EditorOption {
	return func(e *Editor) {
		e.applyKeyset(KeysetNano)
		e.setMode(ModeInsert)
	}
}

// WithStandardKeymap configures the standard GUI/TextEdit editing profile.
func WithStandardKeymap() EditorOption {
	return func(e *Editor) {
		e.applyKeyset(KeysetStandard)
		e.setMode(ModeInsert)
	}
}

// WithKeyset selects a predefined keyset and editing profile.
func WithKeyset(ks Keyset) EditorOption {
	switch normalizeKeyset(ks) {
	case KeysetNano:
		return WithNanoKeymap()
	case KeysetStandard:
		return WithStandardKeymap()
	default:
		return WithVimKeymap()
	}
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

// applyKeyset installs a profile's base tables and replays the host's keymap
// overlay on top. Mode is NOT decided here: construction and a live switch
// want different transitions, so each caller sets it.
func (e *Editor) applyKeyset(ks Keyset) {
	switch normalizeKeyset(ks) {
	case KeysetNano:
		e.keyset, e.modal, e.keymap = KeysetNano, false, NanoKeymap()
	case KeysetStandard:
		e.keyset, e.modal, e.keymap = KeysetStandard, false, StandardKeymap()
	default:
		e.keyset, e.modal, e.keymap = KeysetVim, true, VimKeymap()
	}
	e.unbound = make(map[KeyChord]bool)
	e.applyOverlay(e.overlay)
}

// applyOverlay folds host bindings onto the live table. Entries are validated
// by the caller that first accepted them, so a profile switch cannot panic on
// an overlay the editor already took: validateKeymapEntry checks the chord's
// mode and the action, neither of which depends on the keyset.
func (e *Editor) applyOverlay(ov Keymap) {
	for kc, act := range ov {
		if act == ActUnbound {
			delete(e.keymap, kc)
			if e.unbound == nil {
				e.unbound = make(map[KeyChord]bool)
			}
			e.unbound[kc] = true
			continue
		}
		e.keymap[kc] = act
		delete(e.unbound, kc)
	}
}

// NewEditor builds an empty editor initialized with the default Vim keymap,
// modal editing enabled, and the "jk" escape chord armed. Custom options
// can select alternative keysets (e.g. WithNanoKeymap, WithStandardKeymap)
// or customize capabilities and styles.
func NewEditor(opts ...EditorOption) *Editor {
	e := &Editor{
		textBuffer:   newTextBuffer(),
		wrap:         WrapNone,
		styles:       defaultEditorStyles(),
		keymap:       DefaultKeymap(),
		unbound:      make(map[KeyChord]bool),
		chord:        []rune{'j', 'k'},
		chordTimeout: 300 * time.Millisecond,
		modal:        true,
		canSelect:    true,
		canYank:      true,
		canUndo:      true,
		keyset:       KeysetVim,
	}
	for _, o := range opts {
		if o != nil {
			o(e)
		}
	}
	if !e.modal && e.mode == ModeNormal {
		e.setMode(ModeInsert)
	}
	return e
}

// Value returns the buffer joined with newlines.
func (e *Editor) Value() string { return e.value() }

// SetValue is a document-boundary operation: pending input
// settles, the editor returns to Normal mode (or Insert mode if modeless),
// cursor and command state reset, content is replaced, and undo/redo history
// is CLEARED. The register is preserved.
func (e *Editor) SetValue(s string) {
	e.settlePendingRune()
	e.count, e.pendingAct = 0, ActUnbound
	e.groupOpen = false
	e.undo, e.redo = nil, nil
	e.setValue(s)
	e.ln, e.col = 0, 0
	if e.modal {
		e.setMode(ModeNormal)
	} else {
		e.setMode(ModeInsert)
	}
	e.top, e.left = 0, 0
	e.ensureVisible()
	e.MarkDirty()
}

// Mode reports the current mode.
func (e *Editor) Mode() EditorMode { return e.mode }

// ReadOnly reports whether edits are refused.
func (e *Editor) ReadOnly() bool { return e.readOnly }

// SetReadOnly makes the editor a VIEWER: motions, counts, visual
// selection, yank, and search all work; every mutating action (insert
// entry, delete, paste, undo/redo, typed text) is refused, and an active
// Insert session returns to Normal (in modal mode). Hosts use it for panels the user
// navigates but must not change.
func (e *Editor) SetReadOnly(v bool) {
	if e.readOnly == v {
		return
	}
	e.readOnly = v
	if v && (e.mode == ModeInsert) && e.modal {
		e.settlePendingRune()
		e.setMode(ModeNormal)
		e.clampNormal()
	}
	e.MarkDirty()
}

// Line reports the cursor position (0-based) for status bars.
func (e *Editor) Line() (row, col int) { return e.ln, e.col }

// SetLine moves the cursor to row/col (both clamped to the document) and
// scrolls it into view — the programmatic sibling of the motions, for
// hosts driving search, jump-to-error, and reveal. Pending input settles
// first; the mode is left alone.
func (e *Editor) SetLine(row, col int) {
	e.settlePendingRune()
	e.ln = max(0, min(row, len(e.lines)-1))
	e.col = max(0, col)
	if e.modal {
		e.clampNormal()
	}
	e.ensureVisible()
	e.MarkDirty()
}

// Lines returns a snapshot of the document's lines — what a host needs to
// search without re-splitting Value().
func (e *Editor) Lines() []string { return append([]string(nil), e.lines...) }

// AcceptsFocus implements tui.Focusable.
func (e *Editor) AcceptsFocus() bool { return true }

// CursorShape implements tui.CursorShaper: block/underline/bar for
// Normal/Visual/Insert.
func (e *Editor) CursorShape() tui.CursorShape {
	switch e.mode {
	case ModeInsert:
		return tui.CursorShapeBar
	case ModeVisual, ModeVisualLine:
		return tui.CursorShapeUnderline
	}
	return tui.CursorShapeBlock
}

// --- runtime keymap reflection -------------------------------------------

// Keyset reports the active editing & keymap profile.
func (e *Editor) Keyset() Keyset { return e.keyset }

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
func (e *Editor) SetKeyset(ks Keyset) {
	ks = normalizeKeyset(ks)
	if ks == e.keyset {
		return
	}
	e.settlePendingRune()
	e.count, e.pendingCount = 0, 0
	e.pendingAct, e.pendingChord = ActUnbound, KeyChord{}
	e.groupOpen = false
	e.anchor, e.vAnchor = nil, taPos{}
	e.applyKeyset(ks)
	if e.modal {
		e.setMode(ModeNormal)
		e.clampNormal()
	} else {
		e.setMode(ModeInsert)
	}
	e.desired = -1
	e.ensureVisible()
	e.MarkDirty()
}

// Keymap returns a defensive copy of the editor's active keymap.
func (e *Editor) Keymap() Keymap {
	cp := make(Keymap, len(e.keymap))
	for k, v := range e.keymap {
		cp[k] = v
	}
	return cp
}

// EscapeChord returns the configured two-rune escape chord (e.g. "jk"), or "" if disabled.
func (e *Editor) EscapeChord() string {
	return string(e.chord)
}

// Bindings returns all discrete key chords configured in this editor's active keymap,
// sorted deterministically by mode, key chord, and action.
//
// Multi-rune escape sequences (such as the modal Insert-mode "jk" chord) operate via
// the chord timeout engine rather than single-chord mappings, and are reported via
// [Editor.EscapeChord] and [KeymapSnapshot.EscapeChord].
func (e *Editor) Bindings() []KeyBinding {
	return e.keymap.Bindings()
}

// BindingsForMode returns all active bindings available when the editor is in mode m.
func (e *Editor) BindingsForMode(m EditorMode) []KeyBinding {
	all := e.Bindings()
	filtered := make([]KeyBinding, 0, len(all))
	targetMode := modeClass(m)
	for _, b := range all {
		if modeClass(b.Mode) == targetMode {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

// SnapshotKeymap generates a complete, serializable runtime reflection snapshot of the
// editor's active key configuration, profile, and action mappings.
func (e *Editor) SnapshotKeymap() KeymapSnapshot {
	return KeymapSnapshot{
		Keyset:      e.keyset,
		KeysetName:  e.keyset.String(),
		Modal:       e.modal,
		EscapeChord: string(e.chord),
		Bindings:    e.Bindings(),
	}
}

// ActionForChord looks up the bound action for a given key chord.
func (e *Editor) ActionForChord(kc KeyChord) (Action, bool) {
	if e.unbound[kc] {
		return ActUnbound, false
	}
	act, ok := e.keymap[kc]
	return act, ok
}

// ChordsForAction returns all key chords that map to the specified action.
func (e *Editor) ChordsForAction(act Action) []KeyChord {
	var chords []KeyChord
	for kc, a := range e.keymap {
		if a == act {
			chords = append(chords, kc)
		}
	}
	sort.Slice(chords, func(i, j int) bool {
		if chords[i].Mode != chords[j].Mode {
			return chords[i].Mode < chords[j].Mode
		}
		return chords[i].String() < chords[j].String()
	})
	return chords
}

// --- mode & cursor invariants -------------------------------------------

func (e *Editor) setMode(m EditorMode) {
	if e.mode == m {
		return
	}
	e.mode = m
	e.MarkDirty()
	e.publish(ModeChangedEvent{Owner: e.NodeID(), Mode: m})
	if e.onModeChange != nil {
		e.onModeChange(m)
	}
}

// normalMax is the max Normal-mode column of line ln (cursor ON a grapheme).
func (e *Editor) normalMax(ln int) int {
	return max(0, len(e.lineClusters(ln))-1)
}

// clampNormal enforces the Normal/Visual cursor invariant.
func (e *Editor) clampNormal() {
	e.col = min(e.col, e.normalMax(e.ln))
}

func (e *Editor) enterInsert() {
	e.count, e.pendingAct = 0, ActUnbound
	e.groupOpen = false // group opens lazily on the first mutation
	e.setMode(ModeInsert)
}

// exitInsert implements Insert→Normal in modal mode: cursor one cluster left, clamped.
// In modeless editing, this is a no-op as the editor remains in Insert mode.
func (e *Editor) exitInsert() {
	if !e.modal {
		return
	}
	e.groupOpen = false
	e.col = max(0, e.col-1)
	e.clampNormal()
	e.desired = -1
	e.setMode(ModeNormal)
	e.ensureVisible()
	e.MarkDirty()
}

// exitVisual clears the selection anchor and transitions out of visual mode:
// returning to Normal mode if modal editing is active, or to Insert mode if modeless.
func (e *Editor) exitVisual() {
	e.anchor = nil
	if e.modal {
		e.setMode(ModeNormal)
		e.clampNormal()
	} else {
		e.setMode(ModeInsert)
	}
	e.MarkDirty()
}

// --- escape chord ---------------------------------------------------------

// settlePendingRune commits a held first chord rune as an insertion: every
// non-chord input settles the pending rune first.
func (e *Editor) settlePendingRune() {
	if e.pendingRune == 0 {
		return
	}
	r := e.pendingRune
	e.pendingRune = 0
	if e.chordCancel != nil {
		e.chordCancel()
		e.chordCancel = nil
	}
	e.beginGroup()
	e.insertText(string(r))
	e.edited()
}

// mutatingActions are refused in read-only mode (motions, visual entry,
// and yank stay available — a viewer still navigates and copies).
func mutatingAction(act Action) bool {
	switch act {
	case ActInsert, ActAppend, ActInsertLineStart, ActAppendLineEnd,
		ActOpenBelow, ActOpenAbove, ActDeleteChar, ActDeleteToEnd,
		ActPasteAfter, ActPasteBefore, ActUndo, ActRedo,
		ActDeletePrefix, ActVisualDelete, ActCut, ActPaste:
		return true
	}
	return false
}

// execAction runs one bound action with the (already consumed) count.
func (e *Editor) execAction(act Action, count int) bool {
	if e.readOnly && mutatingAction(act) {
		return true // consumed and refused: a viewer never mutates
	}
	switch act {
	// Motions.
	case ActLeft, ActDown, ActUp, ActRight, ActLineStart, ActLineEnd,
		ActWordForward, ActWordBack, ActWordEnd, ActParaForward, ActParaBack,
		ActPageUp, ActPageDown:
		e.move(act, count)
		return true

	// Insert entries.
	case ActInsert:
		e.enterInsert()
		return true
	case ActAppend:
		if len(e.lineClusters(e.ln)) > 0 {
			e.col++
		}
		e.enterInsert()
		return true
	case ActInsertLineStart:
		e.col = 0
		e.enterInsert()
		return true
	case ActAppendLineEnd:
		e.col = len(e.lineClusters(e.ln))
		e.enterInsert()
		return true
	case ActOpenBelow:
		e.beginGroup()
		e.lines = append(e.lines[:e.ln+1], append([]string{""}, e.lines[e.ln+1:]...)...)
		e.ln, e.col = e.ln+1, 0
		e.enterInsert()
		e.groupOpen = true // the open-line already began this group
		e.edited()
		return true
	case ActOpenAbove:
		e.beginGroup()
		e.lines = append(e.lines[:e.ln], append([]string{""}, e.lines[e.ln:]...)...)
		e.col = 0
		e.enterInsert()
		e.groupOpen = true
		e.edited()
		return true

	// Normal-mode edits.
	case ActDeleteChar:
		cs := e.lineClusters(e.ln)
		if len(cs) == 0 {
			return true
		}
		n := min(count, len(cs)-e.col)
		e.beginGroup()
		e.yankSet(strings.Join(cs[e.col:e.col+n], ""), false)
		e.deleteRegion(taPos{e.ln, e.col}, taPos{e.ln, e.col + n})
		e.clampNormal()
		e.edited()
		return true
	case ActDeleteToEnd:
		cs := e.lineClusters(e.ln)
		if e.col < len(cs) {
			e.beginGroup()
			e.yankSet(strings.Join(cs[e.col:], ""), false)
			e.deleteRegion(taPos{e.ln, e.col}, taPos{e.ln, len(cs)})
			e.clampNormal()
			e.edited()
		}
		return true
	case ActPasteAfter:
		e.pasteRegister(true)
		return true
	case ActPasteBefore:
		e.pasteRegister(false)
		return true
	case ActUndo:
		e.doUndo()
		return true
	case ActRedo:
		e.doRedo()
		return true

	// Visual entry/exit.
	case ActVisual:
		if !e.canSelect {
			return true
		}
		switch e.mode {
		case ModeVisual:
			e.exitVisual()
		default:
			e.vAnchor = taPos{ln: e.ln, col: e.col}
			e.setMode(ModeVisual)
		}
		return true
	case ActVisualLine:
		if !e.canSelect {
			return true
		}
		switch e.mode {
		case ModeVisualLine:
			e.exitVisual()
		case ModeVisual:
			e.setMode(ModeVisualLine)
		default:
			e.vAnchor = taPos{ln: e.ln, col: e.col}
			e.setMode(ModeVisualLine)
		}
		return true

	// Visual operations.
	case ActVisualYank:
		if !e.canYank {
			e.exitVisual()
			return true
		}
		if e.mode == ModeVisualLine {
			lo, hi := e.visualLines()
			text := strings.Join(e.lines[lo:hi+1], "\n")
			e.yankSet(text, true)
			e.exportYank(text)
			e.ln, e.col = lo, 0
		} else {
			lo, hiEx := e.visualRange()
			text := e.textIn(lo, hiEx)
			e.yankSet(text, false)
			e.exportYank(text)
			e.ln, e.col = lo.ln, lo.col
		}
		e.exitVisual()
		e.ensureVisible()
		return true
	case ActVisualDelete:
		if e.mode == ModeVisualLine {
			lo, hi := e.visualLines()
			e.exitVisual()
			e.deleteLines(lo, hi)
		} else {
			lo, hiEx := e.visualRange()
			e.beginGroup()
			e.yankSet(e.textIn(lo, hiEx), false)
			e.deleteRegion(lo, hiEx)
			e.exitVisual()
			e.edited()
		}
		return true

	// General actions (Nano / Standard).
	case ActCut:
		if e.mode == ModeVisual || e.mode == ModeVisualLine {
			return e.execAction(ActVisualDelete, count)
		}
		e.deleteLines(e.ln, e.ln)
		return true

	case ActCopy:
		if !e.canYank {
			if e.mode == ModeVisual || e.mode == ModeVisualLine {
				e.exitVisual()
			}
			return true
		}
		if e.mode == ModeVisual || e.mode == ModeVisualLine {
			return e.execAction(ActVisualYank, count)
		}
		if e.ln < len(e.lines) {
			text := e.lines[e.ln]
			e.yankSet(text, true)
			e.exportYank(text)
		}
		return true

	case ActPaste:
		e.pasteRegister(false)
		return true

	case ActSelectAll:
		if !e.canSelect || len(e.lines) == 0 {
			return true
		}
		e.vAnchor = taPos{ln: 0, col: 0}
		lastLn := len(e.lines) - 1
		e.ln = lastLn
		e.col = max(0, len(e.lineClusters(lastLn))-1)
		e.setMode(ModeVisual)
		return true
	}
	return false
}

// --- event handling -----------------------------------------------------------

// HandleEvent handles mouse, bracketed paste, focus, chord timer ticks, and keyboard events
// across modal and modeless editing profiles.
func (e *Editor) HandleEvent(ev tui.Event) bool {
	switch t := ev.(type) {
	case tui.MouseEvent:
		return e.handleMouse(t)
	case tui.PasteEvent:
		if e.readOnly {
			return true // a viewer never mutates (bracketed paste included)
		}
		e.settlePendingRune()
		e.beginGroup()
		switch e.mode {
		case ModeVisual:
			// Visual paste replaces the selection (S3 — never silently
			// discard the selection boundary).
			lo, hiEx := e.visualRange()
			e.deleteRegion(lo, hiEx)
			e.setMode(ModeNormal)
			e.insertText(t.Text)
			e.clampNormal()
		case ModeVisualLine:
			lo, hi := e.visualLines()
			e.setMode(ModeNormal)
			e.anchor = nil
			e.lines = append(e.lines[:lo], append([]string{""}, e.lines[hi+1:]...)...)
			e.ln, e.col = lo, 0
			e.insertText(t.Text)
			e.clampNormal()
		default:
			e.insertText(t.Text) // one atomic literal insertion
			if e.mode != ModeInsert {
				e.clampNormal()
			}
		}
		e.edited()
		return true
	case tui.FocusEvent:
		if !t.Gained {
			// Focus loss settles the chord rune, ends the Insert undo
			// group (mode unchanged), and clears EVERY partial command:
			// pending count and the double-key prefix must not survive a
			// focus round-trip.
			e.settlePendingRune()
			e.groupOpen = false
			e.count = 0
			e.pendingAct = ActUnbound
			e.pendingCount = 0
		}
		return false // focus events are informational; let them bubble
	case tui.TickEvent:
		// The chord timeout: commit the held rune as an insertion.
		e.chordCancel = nil
		if e.pendingRune != 0 {
			r := e.pendingRune
			e.pendingRune = 0
			e.beginGroup()
			e.insertText(string(r))
			e.edited()
		}
		return true
	case tui.KeyEvent:
		return e.handleKey(t)
	}
	return false
}

func (e *Editor) handleKey(k tui.KeyEvent) bool {
	if k.Kind == tui.KeyRelease {
		return false
	}
	if e.mode == ModeInsert {
		return e.handleInsertKey(k)
	}
	return e.handleCommandKey(k)
}

// handleInsertKey: structural Insert handling (text, chord, Esc, editing
// keys). Tab INSERTS a tab in Insert mode; traversal
// belongs to Normal mode, where Tab bubbles.
func (e *Editor) handleInsertKey(k tui.KeyEvent) bool {
	ctrl := k.Mods&tui.ModCtrl != 0
	code := k.Code
	if k.Text != "" && k.Mods&nonTextMods == 0 {
		code = []rune(k.Text)[0]
	}
	kc := KeyChord{Mode: ModeInsert, Code: code, Ctrl: ctrl}

	// 1. Explicit unbind sentinel: unhandled keystroke bubbles up to application.
	if e.unbound[kc] {
		e.settlePendingRune()
		return false
	}

	// 2. Configured keymap actions (custom bindings, Nano/Standard profiles, etc.).
	if act, bound := e.keymap[kc]; bound {
		e.settlePendingRune()
		return e.execAction(act, 1)
	}

	isText := k.Text != "" && k.Mods&nonTextMods == 0 && k.Code != tui.KeyTab

	// Chord state machine first (only in modal editing).
	if e.modal && e.pendingRune != 0 {
		if isText && []rune(k.Text)[0] == e.chord[1] {
			// Second chord rune dispatched before the tick: escape.
			e.pendingRune = 0
			if e.chordCancel != nil {
				e.chordCancel()
				e.chordCancel = nil
			}
			e.exitInsert()
			return true
		}
		// Commit the held rune, then process THIS key from the top of the
		// Insert state machine — it may itself be a fresh chord start, so
		// "jjk" commits the first j and escapes on the second j plus k.
		e.settlePendingRune()
	}
	if e.modal && isText && e.chord != nil && e.pendingRune == 0 && []rune(k.Text)[0] == e.chord[0] {
		e.pendingRune = e.chord[0]
		if ctx := e.Context(); ctx != nil {
			e.chordCancel = ctx.After(e.chordTimeout)
		}
		return true
	}

	switch k.Code {
	case tui.KeyTab:
		// Insert mode consumes Tab as text; traversal
		// belongs to Normal mode, where Tab bubbles.
		e.beginGroup()
		e.insertText("\t")
		e.edited()
		return true
	case tui.KeyEscape:
		if e.modal {
			e.exitInsert()
			return true
		}
		if e.vAnchor != (taPos{}) {
			e.vAnchor = taPos{}
			e.MarkDirty()
			return true
		}
		return false
	case tui.KeyEnter:
		e.beginGroup()
		e.insertText("\n")
		e.edited()
		return true
	case tui.KeyBackspace:
		if e.col > 0 {
			e.beginGroup()
			e.deleteRegion(taPos{e.ln, e.col - 1}, taPos{e.ln, e.col})
			e.edited()
		} else if e.ln > 0 {
			e.beginGroup()
			e.deleteRegion(taPos{e.ln - 1, len(e.lineClusters(e.ln - 1))}, taPos{e.ln, 0})
			e.edited()
		}
		return true
	case tui.KeyDelete:
		if e.col < len(e.lineClusters(e.ln)) {
			e.beginGroup()
			e.deleteRegion(taPos{e.ln, e.col}, taPos{e.ln, e.col + 1})
			e.edited()
		} else if e.ln < len(e.lines)-1 {
			e.beginGroup()
			e.deleteRegion(taPos{e.ln, e.col}, taPos{e.ln + 1, 0})
			e.edited()
		}
		return true
	case tui.KeyLeft:
		e.groupOpen = false
		e.desired = -1
		e.moveCursor(e.ln, e.col-1, false)
		e.ensureVisible()
		e.MarkDirty()
		return true
	case tui.KeyRight:
		e.groupOpen = false
		e.desired = -1
		e.moveCursor(e.ln, e.col+1, false)
		e.ensureVisible()
		e.MarkDirty()
		return true
	case tui.KeyUp, tui.KeyDown:
		e.groupOpen = false
		delta := 1
		if k.Code == tui.KeyUp {
			delta = -1
		}
		ln, col := e.verticalTarget(delta, e.measure)
		d := e.desired
		e.moveCursor(ln, col, false)
		e.desired = d
		e.ensureVisible()
		e.MarkDirty()
		return true
	case tui.KeyHome:
		e.groupOpen = false
		e.desired = -1
		e.moveCursor(e.ln, 0, false)
		e.MarkDirty()
		return true
	case tui.KeyEnd:
		e.groupOpen = false
		e.desired = -1
		e.moveCursor(e.ln, len(e.lineClusters(e.ln)), false)
		e.MarkDirty()
		return true
	case tui.KeyPageUp:
		e.move(ActPageUp, 1)
		return true
	case tui.KeyPageDown:
		e.move(ActPageDown, 1)
		return true
	}

	if isText {
		e.beginGroup()
		e.insertText(k.Text)
		e.edited()
		return true
	}
	return false
}

// handleCommandKey: Normal/Visual dispatch — digits, the double-key pending
// buffer, then the keymap. Unbound keys clear pending state and bubble.
func (e *Editor) handleCommandKey(k tui.KeyEvent) bool {
	ctrl := k.Mods&tui.ModCtrl != 0

	if k.Code == tui.KeyEscape {
		hadPending := e.count != 0 || e.pendingAct != ActUnbound
		e.count, e.pendingAct = 0, ActUnbound
		if e.mode == ModeVisual || e.mode == ModeVisualLine {
			e.exitVisual()
			return true
		}
		// Normal mode with nothing pending: Esc is a vim no-op, so it
		// BUBBLES. Consuming it here made an Editor inside a modal float
		// undismissable — the host never saw the key (autodb M6: a
		// read-only script viewer that Esc could not close).
		return hadPending
	}

	// Count accumulation: 1-9 always; 0 only extends an existing count.
	// Clamp BEFORE assignment so the cap is a hard ceiling.
	if !ctrl && k.Text != "" {
		r := []rune(k.Text)[0]
		if r >= '1' && r <= '9' || (r == '0' && e.count > 0) {
			e.pendingAct = ActUnbound
			e.count = min(e.count*10+int(r-'0'), 1_000_000)
			return true
		}
	}

	// A key carrying a COMMAND modifier other than Ctrl is not this Editor's to
	// consume, and must bubble to the host.
	//
	// KeyChord identity is (Mode, Code, Ctrl) — Alt is not part of it. So without
	// this check Alt+h built the SAME chord as plain h and was swallowed as a
	// motion, which silently denied the host every Alt binding while looking like
	// the key had simply done nothing. Found from autodb, which needs Alt+h/j/k/l
	// for pane motion precisely because a browser keeps Ctrl-L for its address bar
	// and will not surrender it.
	//
	// Ctrl is excluded from this rule because Ctrl IS part of a chord (Ctrl-r is
	// redo), so a Ctrl key the keymap does not bind already falls through below.
	if k.Mods&(tui.ModAlt|tui.ModSuper|tui.ModMeta|tui.ModHyper) != 0 {
		return false
	}

	code := k.Code
	if k.Text != "" && k.Mods&nonTextMods == 0 {
		code = []rune(k.Text)[0] // shifted letters arrive via Text ("G")
	}
	kc := KeyChord{Mode: modeClass(e.mode), Code: code, Ctrl: ctrl}

	if e.unbound[kc] {
		e.count = 0
		e.pendingAct = ActUnbound
		return false
	}

	// Double-key pending buffer, keyed by the ARMING CHORD, so a rebound
	// prefix completes on its own chord rather than a hard-coded rune:
	// only the same chord again completes; any other key clears the
	// pending state and is processed normally.
	if e.pendingAct != ActUnbound {
		act, chord := e.pendingAct, e.pendingChord
		hadCount := e.pendingCount > 0
		count := max(e.pendingCount, 1)
		e.pendingAct = ActUnbound
		e.pendingCount = 0
		if kc == chord {
			switch act {
			case ActDeletePrefix:
				if e.readOnly {
					return true // dd on a viewer: consumed, refused
				}
				e.deleteLines(e.ln, min(e.ln+count-1, len(e.lines)-1))
			case ActYankPrefix:
				if e.canYank {
					text := strings.Join(e.lines[e.ln:min(e.ln+count-1, len(e.lines)-1)+1], "\n")
					e.yankSet(text, true)
					e.exportYank(text)
				}
			case ActGoPrefix:
				e.goToLine(hadCount, count, false) // [count]gg
			}
			return true
		}
		// Fall through: reprocess this key from scratch (count consumed).
	}

	act, bound := e.keymap[kc]
	if !bound {
		e.count = 0 // an unbound key cancels the pending count and bubbles
		return false
	}

	hadCount := e.count > 0
	count := max(e.count, 1)
	e.count = 0

	switch act {
	case ActDeletePrefix, ActYankPrefix, ActGoPrefix:
		if act == ActYankPrefix && !e.canYank {
			return true
		}
		e.pendingAct = act
		e.pendingChord = kc
		e.pendingCount = 0
		if hadCount {
			e.pendingCount = count // preserved for the completion (2dd, 5gg)
		}
		return true
	case ActGoBottom:
		e.goToLine(hadCount, count, true) // [count]G
		return true
	}
	return e.execAction(act, count)
}

// --- viewport & rendering (TextArea discipline) ------------------------------

func (e *Editor) wrapWidth() int { return wrapUsableWidth(e.lines, e.view()) }

func (e *Editor) scrollable() bool { return wrapScrollable(e.lines, e.view()) }

func (e *Editor) rowsOfLine(i int) int { return wrapRowsOfLine(e.lines, i, e.view()) }

func (e *Editor) ensureVisible() {
	if e.h <= 0 || e.w <= 0 {
		return
	}
	if e.ln < e.top {
		e.top = e.ln
	}
	for e.top < e.ln {
		rows := 0
		for i := e.top; i <= e.ln && rows <= e.h; i++ {
			rows += e.rowsOfLine(i)
		}
		if rows <= e.h {
			break
		}
		e.top++
	}
	e.top = max(0, min(e.top, len(e.lines)-1))
	if e.wrap == WrapNone {
		cx := e.cellsAt(e.ln, e.col, e.measure)
		w := e.wrapWidth()
		if cx < e.left {
			e.left = cx
		}
		if cx >= e.left+w {
			e.left = cx - w + 1
		}
		e.left = max(e.left, 0)
	} else {
		e.left = 0
	}
}

// Layout is greedy on both axes.
func (e *Editor) Layout(c tui.Constraints) tui.Size {
	e.w = boundedMax(c.MaxW, max(c.MinW, 1))
	e.h = boundedMax(c.MaxH, max(c.MinH, 1))
	e.ensureVisible()
	return c.Constrain(tui.Size{W: e.w, H: e.h})
}

// Cursor implements tui.CursorReporter.
func (e *Editor) Cursor() (int, int, bool) {
	if e.ln < e.top {
		return 0, 0, false
	}
	if e.wrap == WrapNone {
		x := e.cellsAt(e.ln, e.col, e.measure) - e.left
		y := e.ln - e.top
		if y >= e.h && e.h > 0 {
			return 0, 0, false
		}
		return max(x, 0), max(y, 0), true
	}
	y := 0
	for i := e.top; i < e.ln; i++ {
		y += e.rowsOfLine(i)
	}
	row, x := e.wrapPos(e.ln, e.col)
	y += row
	if e.h > 0 && y >= e.h {
		return 0, 0, false
	}
	return x, y, true
}

// handleMouse implements the pointer contract.
//
// A press is a COMMAND BOUNDARY, not merely a cursor move, because Editor holds
// modal state that a click has to resolve one way or the other. The wheel scrolls
// the viewport and never moves the caret, so a reader can scroll while a caret
// stays where they left it.
func (e *Editor) handleMouse(m tui.MouseEvent) bool {
	switch {
	case m.Kind == tui.MouseWheel && m.Button == tui.WheelUp:
		return e.scrollLines(-1)
	case m.Kind == tui.MouseWheel && m.Button == tui.WheelDown:
		return e.scrollLines(1)
	case m.Kind == tui.MousePress && m.Button == tui.MouseLeft:
		return e.pressAt(m.X, m.Y)
	}
	return false
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
	before := e.top
	e.top = min(max(e.top+delta, 0), max(len(e.lines)-1, 0))
	if e.top != before {
		e.MarkDirty()
	}
	return true
}

// pressAt places the caret at a clicked cell and settles modal state.
func (e *Editor) pressAt(x, y int) bool {
	// The scroll-indicator column is not text. A press there is inert, and
	// consumed rather than bubbled: the column belongs to this widget.
	if e.scrollable() && x >= e.wrapWidth() {
		return true
	}
	ln, col := e.posAt(x, y)

	// ---- the command boundary, in this order ----
	//
	// A pending insert rune is SETTLED FIRST, at the caret it was typed at, and
	// before the caret moves. binds every non-chord input to settle the
	// pending rune, and a click is a non-chord input like any other; discarding it
	// would delete a character the user physically typed. This is the only way a
	// press changes buffer text.
	if e.mode == ModeInsert {
		e.settlePendingRune()
		// A click is a deliberate discontinuity, so text typed before and after it
		// undo separately.
		e.groupOpen = false
	}
	// Pending COMMAND state is discarded, never completed. Completing `2d`
	// against a clicked location would turn a mis-click into a destructive edit,
	// and the pointer carries no evidence the operator was meant to apply there.
	// Discarding it modifies nothing.
	e.count, e.pendingCount = 0, 0
	e.pendingAct, e.pendingChord = ActUnbound, KeyChord{}
	// Visual exits and the anchor is cleared: extending a selection by clicking is
	// drag-selection, which this revision defers. Keeping the anchor would make the
	// next motion extend a selection the user believes they dismissed.
	if e.mode == ModeVisual || e.mode == ModeVisualLine {
		if e.modal {
			e.setMode(ModeNormal)
		} else {
			e.setMode(ModeInsert)
		}
		e.vAnchor = taPos{}
	}

	e.ln, e.col = ln, col
	if e.mode != ModeInsert {
		e.clampNormal()
	}
	e.desired = -1
	e.ensureVisible()
	e.MarkDirty()
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
	if len(e.lines) == 0 {
		return 0, 0
	}
	lastLn := len(e.lines) - 1

	if e.wrap == WrapNone {
		ln = min(e.top+max(y, 0), lastLn)
		cs := e.lineClusters(ln)
		return ln, e.colAtCells(cs, 0, len(cs), e.left+max(x, 0))
	}

	// WrapSoft: walk the same wrap computation the renderer used, rather than
	// dividing by width — one logical line spans several visual rows.
	remaining := max(y, 0)
	for i := e.top; i <= lastLn; i++ {
		rows := e.rowsOfLine(i)
		if remaining < rows || i == lastLn {
			cs := e.lineClusters(i)
			ranges := wrapRanges(cs, e.wrapWidth(), e.measure)
			r := ranges[min(remaining, len(ranges)-1)]
			// Bounded to THIS row: wrapRanges is [start,end), and a word-wrapped
			// row can be shorter than the viewport, so an unbounded scan would run
			// through its blank tail into clusters painted on the NEXT visual row.
			return i, e.colAtCells(cs, r[0], r[1], max(x, 0))
		}
		remaining -= rows
	}
	return lastLn, e.normalMax(lastLn)
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
	return wrapPosOf(e.lines, ln, col, e.view())
}

// Render paints the viewport with the visual-selection fill.
func (e *Editor) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	w := e.wrapWidth()
	hlf := e.beginHighlightFrame()
	var lineStyles []highlight.Style
	styledLn := -1
	paintCluster := func(x, y int, cl string, ln, col int) {
		st := e.styles.Text
		if ln != styledLn {
			lineStyles, styledLn = e.highlighted(ln, hlf), ln
		}
		if e.hl != nil {
			k := highlight.Normal
			if col < len(lineStyles) {
				k = lineStyles[col]
			}
			if sst, ok := e.syntaxStyle(k); ok {
				st = sst.Inherit(st)
			}
		}
		if e.focused() && e.inVisual(ln, col) {
			st = e.styles.Selection.Inherit(st)
		}
		s.SetCell(x, y, cl, st)
	}
	lineFill := func(y, ln int) {
		// A line-wise highlight covers the WHOLE screen row (S2), text or
		// not; clusters then paint over the fill.
		if e.mode == ModeVisualLine && e.focused() && e.inVisual(ln, 0) {
			s.Fill(tui.Rect{X: 0, Y: y, W: w, H: 1}, " ", e.styles.Selection.Inherit(e.styles.Text))
		}
	}
	y := 0
	for ln := e.top; ln < len(e.lines) && y < sz.H; ln++ {
		cs := e.lineClusters(ln)
		if e.wrap == WrapNone {
			lineFill(y, ln)
			x := -e.left
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
			y++
		}
	}
	if e.scrollable() {
		paintScrollIndicator(s, sz.W-1, sz.H, e.top, len(e.lines))
	}
}

// view is the layout state the shared soft-wrap geometry needs. It is the
// only place this widget's viewport is handed to textBuffer.
func (e *Editor) view() wrapView {
	return wrapView{w: e.w, h: e.h, wrap: e.wrap, measure: e.measure}
}
