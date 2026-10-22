package widget

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/indent"
	"github.com/yongjohnlee80/golib/tui"
	tuikey "github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/keymap"
)

// EDITOR CORE — the Editor's behaviour, apart from any view of it (ADR 1791385086).
//
// EditorCore is the text, the cursor, the modes and the keys, undo and the register, the visual
// selection and the highlighting: everything an editor does, and nothing of how it is drawn. A
// widget holds one, hands it its events, and draws what it holds. The tui Editor draws it in
// cells; another widget may draw it in pixels. The core reaches its widget only through an
// EditorLayout, in the widget's own units, so it never knows which.
//
// Each part is a module of its own, held as a field and never embedded (embedding a module would
// let one of its methods call another on its own receiver, past an override; internal/audit
// counts every such call). The shown document is the one exception, and it embeds data only: the
// shown Buffer's bufferState, whose fields (hist, marks, rev, …) read as the core's own, and which
// has no methods to promote (buffer.go).
//
//	EditorCore
//	├── *bufferState        the shown buffer: its text, undo, marks, revision, highlighting (buffer.go)
//	├── buf  textBuffer     the cursor over the shown buffer's text, the sticky column (textbuffer.go)
//	├── reg  editRegister   the unnamed register and the yank policy (editregister.go)
//	├── keys keyDispatch    the mode, the keymap, the input pending between keys (editorkeys.go)
//	├── hl   highlightCache the shown buffer's lines painted in this editor's looks (editor_highlight.go)
//	└── layout EditorLayout the widget's geometry, in its units
//
// # Concurrency Model
//
//   - Ownership: loop-goroutine-owned, as the widget that holds it.

// EditorLayout is what the core needs of the widget that draws it, in that widget's units:
// cells for the tui Editor, pixels for a graphical one.
type EditorLayout interface {
	// Reveal brings the buffer position (line, col) into view: line counts lines, col clusters.
	Reveal(line, col int)
	// PageLines is how many lines PageUp and PageDown move.
	PageLines() int
	// Measure is the width of s. j and k keep their column in these units (the sticky column).
	Measure(s string) int
	// Changed says the text changed from line fromLine on: lay it out again and repaint.
	Changed(fromLine int)
}

// EditorCore is an editor's behaviour: text, cursor, modes, keys, undo, register, selection and
// highlighting. See the package's EDITOR CORE notes.
type EditorCore struct {
	*bufferState                          // the shown buffer's, data only (buffer.go)
	placed         map[*bufferState]bool  // the buffers holding a place of this editor's (SetBuffer, Detach)
	buf            textBuffer             // the cursor over the shown buffer's text
	dot            dotRecorder            // the change . repeats, and the one being recorded
	opRegOne       bool                   // the operator's motion is one whose delete Vim keeps in register 1 (regOneMotion)
	insStart       taPos                  // where this Insert began (editor_viminsert.go)
	oneShot        insertOneShot          // Insert's Ctrl-o (editor_vimoneshot.go)
	blockIns       *blockInsert           // a block's I, A or c waiting for its Insert to end (editor_vimblock.go)
	findHost       func(FindRequest) bool // * and #'s host, when it keeps its own find (editor_vimstar.go)
	replace        *replaceState          // R's, while Replace lasts (editor_vimreplace.go)
	rowSticky      rowSticky              // gj and gk's x (editor_vimrows.go)
	keyDepth       int                    // HandleKey calls in progress: a macro's and .'s keys are nested
	macro          macroRecorder          // the macro being recorded, and what plays (editor_vimmacros.go)
	jumps          *JumpList              // Ctrl+O and Ctrl+I's list: the host's, or the editor's own (editor_jumps.go)
	jumpView       func() string          // the host's view of it (SetJumpView); nil: ""
	jumpGo         func(JumpStep)         // how the host goes to a place in another document or view
	clipPut        *clipPut               // a "+p waiting for the system clipboard (editor_clipboard.go)
	reg            editRegister           // the registers
	keys           keyDispatch            // modes and key routing
	hl             highlightCache         // per-line styles
	highlightFrame *HighlightFrame
	autoIndent     bool
	indentUnit     string
	indentMarks    IndentMarks
	layout         EditorLayout // the widget's geometry; set by Bind
	ctx            *tui.Context // the widget's: publish, clipboard, timers; set by Bind

	vAnchor taPos // the Visual anchor (where the selection started)
	// vEOL says the Visual selection Select made ends with its last line's break: the cursor sits
	// just past that line's last cluster, and the break is selected (a range ending at a line's
	// start). Any change of mode ends it.
	vEOL bool
	// vPinned says the Visual selection was made by Select: it is drawn while the editor is not
	// focused too (a replace session's match, behind the card that holds the keyboard). Any change
	// of mode ends it.
	vPinned   bool
	canSelect bool // visual selection is allowed
	readOnly  bool // a viewer: motions and yank only
	// listEditing continues, ends and nests Markdown list items in Insert mode (editor_lists.go)
	listEditing bool
	listWhere   func(ln int) bool // the lines list editing applies on; nil: every line
	// tableEditing walks a Markdown table's cells with Tab in Insert mode (editor_tables.go)
	tableEditing bool

	// The constructor-time listeners for what the core also publishes on the bus: a widget built
	// before it is mounted has no Context to subscribe with.
	onModeChange func(EditorMode)
	// onKeyScopeChange and lastScope report KeyScope moving (CoreOnKeyScopeChange).
	onKeyScopeChange func(keymap.Scope)
	lastScope        keymap.Scope
	onChange         func()
	chordHook        func(KeyChord) bool // SetChordHook's: a host's chords before the keymap
	onCursorMove     func()              // the cursor moved to another line or column
	// toggleRendered answers ActToggleRendered for a widget with a Rendered view; nil: none
	toggleRendered func() bool

	// dragging is a pointer drag selecting from dragFrom, in buffer positions.
	dragging bool
	dragFrom taPos

	// following is ReplaceFollowing's, while its edit runs: the cursor it keeps with the text
	following *selfFollow
	// batch counts the open Batch calls; batchEdited says an edit inside them is waiting to be
	// announced when the outermost one returns.
	batch       int
	batchEdited bool

	find findState // the active find (editorcore_find.go)

	commands    *Commands                   // the command line's verbs; nil: no command line
	onWrite     func(quit bool) error       // w, wq, x: the host saves
	onQuit      func(force bool) error      // q, q!: the host quits
	replaceAsk  func(*ReplaceSession) error // replace's questions, asked by a widget; nil: none
	commandLine func() bool                 // answers ActCommandLine: the widget's prompt; nil: none
	// joinGroup makes an edit join the undo group open before it: a replace session's answers
	joinGroup bool
	// onEscape answers Esc in Normal mode with nothing pending and no active find (SetOnEscape)
	onEscape func() bool
}

// CoreOption configures an EditorCore under construction.
type CoreOption func(*EditorCore)

// newEditorCore is the default core: an empty line, the Vim keymap, modal, the "jk" escape chord,
// selection, yank and undo on. The options have not run yet.
func newEditorCore() *EditorCore {
	s := newBuffer().s
	c := &EditorCore{
		bufferState: s,
		placed:      map[*bufferState]bool{},
		buf:         textBuffer{bufferText: s.text, desired: -1},
		hl:          highlightCache{text: &s.hlText},
		keys:        newKeyDispatch(),
		canSelect:   true,
		reg:         editRegister{yank: true},
		commands:    defaultCommands(),
	}
	s.editors = append(s.editors, c)
	return c
}

// finishConstruction is the end of every constructor, once the options have run: a modeless
// profile starts in Insert, and the scope it starts in is the one CoreOnKeyScopeChange reports
// moves from.
func (c *EditorCore) finishConstruction() {
	if !c.keys.modal && c.keys.mode == ModeNormal {
		c.setMode(ModeInsert)
	}
	c.lastScope = c.KeyScope() // after every option: CoreKeyset may follow CoreOnKeyScopeChange
}

// NewEditorCore builds a core with the default Vim keymap, modal editing and the "jk" escape
// chord. It draws nothing: a widget binds it to its layout and context (Bind).
func NewEditorCore(opts ...CoreOption) *EditorCore {
	c := newEditorCore()
	for _, o := range opts {
		if o != nil {
			o(c)
		}
	}
	c.finishConstruction()
	return c
}

// Bind gives the core its widget's context and layout. The widget's Init calls it; a widget may
// also bind its layout before it is mounted, with a nil ctx. Before a context, publishing, the
// clipboard and timers are skipped, as a widget's are before mount.
func (c *EditorCore) Bind(ctx *tui.Context, l EditorLayout) {
	c.ctx, c.layout = ctx, l
	// A layout just bound lays out every line: what changed before is the highlighter's alone.
	c.hlFrom = min(c.hlFrom, c.buf.takeChanged())
}

// --- options ---------------------------------------------------------------

// CoreSelection configures whether visual selection is enabled.
func CoreSelection(enabled bool) CoreOption { return func(c *EditorCore) { c.canSelect = enabled } }

// CoreYank configures whether yanking to the register and the system clipboard is enabled.
func CoreYank(enabled bool) CoreOption { return func(c *EditorCore) { c.reg.yank = enabled } }

// CoreUndo configures whether the core keeps undo/redo history.
func CoreUndo(enabled bool) CoreOption { return func(c *EditorCore) { c.hist.setEnabled(enabled) } }

// CoreHighlighter sets the highlighter; nil is none.
func CoreHighlighter(h highlight.Highlighter) CoreOption {
	return func(c *EditorCore) { c.hl.text.hl = h }
}

// CoreSyntaxStyles sets what each highlight style looks like.
func CoreSyntaxStyles(st SyntaxStyles) CoreOption { return func(c *EditorCore) { c.hl.syntax = st } }

// CoreInitialText seeds the buffer: the cursor at the document start, the initial editing mode,
// an empty undo history.
func CoreInitialText(s string) CoreOption {
	return func(c *EditorCore) {
		c.buf.setValue(s)
		c.followEdits() // its reset now, or the first edit would take it for a later one
		c.buf.ln, c.buf.col = 0, 0
	}
}

// CoreEscapeChord sets the Insert-mode escape chord (default "jk"): exactly two unmodified
// printable runes, or "" to disable (Esc alone). Anything else panics.
func CoreEscapeChord(chord string) CoreOption {
	rs := []rune(chord)
	if chord != "" && len(rs) != 2 {
		panic(fmt.Sprintf("widget: CoreEscapeChord: %q is not exactly two runes (or empty to disable)", chord))
	}
	for _, r := range rs {
		if !unicode.IsPrint(r) {
			panic(fmt.Sprintf("widget: CoreEscapeChord: %q contains a non-printable rune", chord))
		}
	}
	return func(c *EditorCore) {
		if chord == "" {
			c.keys.chord = nil
		} else {
			c.keys.chord = rs
		}
	}
}

// CoreKeymap overlays entries onto the default table. ActUnbound removes a default binding; every
// entry is validated when the option applies (panics on unknown actions or unsupported
// mode/action combinations).
func CoreKeymap(overlay Keymap) CoreOption {
	return func(c *EditorCore) {
		// Validate the whole overlay before folding any of it in: a panic halfway through would
		// otherwise leave half the bindings applied.
		for kc, act := range overlay {
			validateKeymapEntry(kc, act)
		}
		if c.keys.overlay == nil {
			c.keys.overlay = make(Keymap, len(overlay))
		}
		for kc, act := range overlay {
			c.keys.overlay[kc] = act
		}
		c.keys.applyOverlay(overlay)
	}
}

// CoreModal configures whether the core runs the Vim modal state machine (Normal, Insert, Visual)
// or edits modelessly.
func CoreModal(modal bool) CoreOption {
	return func(c *EditorCore) {
		c.keys.modal = modal
		if !modal {
			c.setMode(ModeInsert)
		}
	}
}

// CoreReadOnly configures whether the core is a viewer.
func CoreReadOnly(ro bool) CoreOption { return func(c *EditorCore) { c.readOnly = ro } }

// CoreKeyset selects a predefined keyset and editing profile. Vim also arms the "jk" escape
// chord when none is set.
func CoreKeyset(ks Keyset) CoreOption {
	switch normalizeKeyset(ks) {
	case KeysetNano:
		return func(c *EditorCore) {
			c.keys.applyKeyset(KeysetNano, nil)
			c.setMode(ModeInsert)
		}
	case KeysetStandard:
		return func(c *EditorCore) {
			c.keys.applyKeyset(KeysetStandard, nil)
			c.setMode(ModeInsert)
		}
	default:
		return func(c *EditorCore) {
			c.keys.applyKeyset(KeysetVim, nil)
			if len(c.keys.chord) == 0 {
				c.keys.chord = []rune{'j', 'k'}
				c.keys.chordTimeout = 300 * time.Millisecond
			}
		}
	}
}

// CoreOnModeChange calls fn with the new mode whenever the mode changes, and not when it is set to
// the mode it already has. It is the constructor-time twin of ModeChangedEvent.
func CoreOnModeChange(fn func(EditorMode)) CoreOption {
	return func(c *EditorCore) { c.onModeChange = fn }
}

// CoreOnKeyScopeChange calls fn with KeyScope each time it moves: a mode change into another binding
// class, or a keyset switch (Standard to Nano moves it with the mode unchanged).
func CoreOnKeyScopeChange(fn func(keymap.Scope)) CoreOption {
	return func(c *EditorCore) { c.onKeyScopeChange = fn }
}

// noteKeyScope reports KeyScope to CoreOnKeyScopeChange when it moved.
func (c *EditorCore) noteKeyScope() {
	if s := c.KeyScope(); s != c.lastScope {
		c.lastScope = s
		if c.onKeyScopeChange != nil {
			c.onKeyScopeChange(s)
		}
	}
}

// CoreOnChange calls fn after every EDIT, the moment ChangeEvent is published. SetValue is not an
// edit.
func CoreOnChange(fn func()) CoreOption { return func(c *EditorCore) { c.onChange = fn } }

// CoreOnCursorPositionChange calls fn whenever the cursor moves to another line or column: by a
// key, a click, an edit, or the program (SetValue, SetLine, SetCursorPosition). It is told once
// per input, after the input is handled.
func CoreOnCursorPositionChange(fn func()) CoreOption {
	return func(c *EditorCore) { c.onCursorMove = fn }
}

// --- plumbing: what the core asks of its widget ------------------------------

// publish enqueues v on the App bus; nothing before the core is bound to a context.
func (c *EditorCore) publish(v any) {
	if c.ctx != nil {
		c.ctx.Bus().Publish(v)
	}
}

// markDirty asks the widget for a repaint.
func (c *EditorCore) markDirty() {
	if c.ctx != nil {
		c.ctx.MarkDirty()
	}
}

// nodeID is the widget's node, the Owner of what the core publishes; 0 before it is bound.
func (c *EditorCore) nodeID() tui.NodeID {
	if c.ctx == nil {
		return 0
	}
	return c.ctx.ID()
}

// changed tells the layout the first line changed since it was last told, and keeps that line for
// the highlighter too.
func (c *EditorCore) changed() {
	from := c.buf.takeChanged()
	if from == noChange {
		return
	}
	c.hlFrom = min(c.hlFrom, from)
	if c.layout != nil {
		c.layout.Changed(min(from, max(len(c.buf.textLines)-1, 0)))
	}
	for _, o := range c.editors {
		if o != c {
			o.otherEdited(from)
		}
	}
}

// takeHighlightChanged is the first line changed since the highlighter last asked, noChange for
// none. It leaves the buffer's mark for changed() to notify the layout after the edit.
func (c *EditorCore) takeHighlightChanged() int {
	ln := min(c.hlFrom, c.buf.changedFrom)
	c.hlFrom = noChange
	return ln
}

// reveal brings the cursor into view.
func (c *EditorCore) reveal() {
	if c.layout != nil {
		c.layout.Reveal(c.buf.ln, c.buf.col)
	}
}

// measure is the width of s in the layout's units; cells before a layout is bound.
func (c *EditorCore) measure(s string) int {
	if c.layout != nil {
		return c.layout.Measure(s)
	}
	return tui.StringWidth(s)
}

// pageLines is how many lines a page moves.
func (c *EditorCore) pageLines() int {
	if c.layout != nil {
		return max(c.layout.PageLines(), 1)
	}
	return 1
}

// cursorMoved tells onCursorMove when the cursor is no longer at (ln, col).
func (c *EditorCore) cursorMoved(ln, col int) {
	moved := c.buf.ln != ln || c.buf.col != col
	if moved && !c.rowSticky.kept {
		c.rowSticky.ok = false // any move but gj and gk ends their x, as Vim's curswant
	}
	c.rowSticky.kept = false
	if c.onCursorMove != nil && moved {
		c.onCursorMove()
	}
}

// --- the document ------------------------------------------------------------

// Value returns the buffer joined with newlines.
func (c *EditorCore) Value() string { return c.buf.value() }

// SetValue is a document-boundary operation: pending input settles, the core returns to Normal
// mode (or Insert mode if modeless), cursor and command state reset, content is replaced, and
// undo/redo history is CLEARED. The register is preserved.
func (c *EditorCore) SetValue(s string) {
	ln, col := c.buf.ln, c.buf.col
	defer c.cursorMoved(ln, col)
	c.resetInput()
	c.hist.reset()
	c.buf.setValue(s)
	c.followEdits()         // a new document: no marks
	c.rev = c.newRevision() // a new document, unmodified: the text it was given is its saved state
	c.savedRev = c.rev
	if c.sourceFactory != nil {
		c.renewSource()
	}
	c.buf.ln, c.buf.col = 0, 0
	if c.keys.modal {
		c.setMode(ModeNormal)
	} else {
		c.setMode(ModeInsert)
	}
	c.changed()
	c.reveal()
	c.markDirty()
}

// Mode reports the current mode.
func (c *EditorCore) Mode() EditorMode {
	if c.replace != nil && c.keys.mode == ModeInsert {
		return ModeReplace // Insert underneath (editor_vimreplace.go)
	}
	return c.keys.mode
}

// ReadOnly reports whether edits are refused.
func (c *EditorCore) ReadOnly() bool { return c.readOnly }

// SetReadOnly makes the core a VIEWER: motions, counts, visual selection, yank, and search all
// work; every mutating action is refused, and an active Insert session returns to Normal (in modal
// mode).
func (c *EditorCore) SetReadOnly(v bool) {
	if c.readOnly == v {
		return
	}
	c.readOnly = v
	if v && (c.keys.mode == ModeInsert) && c.keys.modal {
		c.settlePendingRune()
		c.setMode(ModeNormal)
		c.clampNormal()
	}
	c.markDirty()
}

// Line reports the cursor position (0-based).
func (c *EditorCore) Line() (row, col int) { return c.buf.ln, c.buf.col }

// SetLine moves the cursor to row/col (both clamped to the document) and reveals it. Pending input
// settles first; the mode is left alone.
func (c *EditorCore) SetLine(row, col int) {
	ln, was := c.buf.ln, c.buf.col
	defer c.cursorMoved(ln, was)
	c.settlePendingRune()
	c.buf.ln = max(0, min(row, len(c.buf.textLines)-1))
	c.buf.col = max(0, col)
	if c.keys.modal {
		c.clampNormal()
	}
	c.reveal()
	c.markDirty()
}

// SetCursorPosition moves the cursor to a position in the document, counted as Qt's
// TextEdit.cursorPosition counts it — characters (grapheme clusters), a line break one — and
// reveals it, as SetLine does. A position past the end is the end.
func (c *EditorCore) SetCursorPosition(pos int) {
	pos = max(pos, 0)
	for row, line := range c.buf.textLines {
		n := len(clusters(line))
		if pos <= n || row == len(c.buf.textLines)-1 {
			c.SetLine(row, min(pos, n))
			return
		}
		pos -= n + 1
	}
}

// Lines returns a snapshot of the document's lines.
func (c *EditorCore) Lines() []string { return append([]string(nil), c.buf.textLines...) }

// SetHighlighter replaces the highlighter; nil turns highlighting off. Every line is highlighted
// afresh.
func (c *EditorCore) SetHighlighter(h highlight.Highlighter) {
	c.retireHighlightFrame()
	c.sourceFactory, c.source = nil, highlight.Source{Highlighter: h}
	c.hl.text.setHighlighter(h)
	hostSource(c.bufferState) // the host's highlighter replaced the finds' own
	c.markDirty()
}

// CoreSourceFactory selects a fresh paired source for each document.
func CoreSourceFactory(factory func() highlight.Source) CoreOption {
	return func(c *EditorCore) { c.SetSourceFactory(factory) }
}

// SetSourceFactory replaces both source behavior and lexical-state ownership.
func (c *EditorCore) SetSourceFactory(factory func() highlight.Source) {
	c.sourceFactory = factory
	c.renewSource()
	c.markDirty()
}

func (c *EditorCore) renewSource() {
	c.retireHighlightFrame()
	source := highlight.Source{}
	if c.sourceFactory != nil {
		source = c.sourceFactory()
	}
	c.hl.text.setSource(source)
	c.source = source
	hostSource(c.bufferState)
}

// CoreAutoIndent opts an editor into source-language indentation.
func CoreAutoIndent(enabled bool) CoreOption { return func(c *EditorCore) { c.autoIndent = enabled } }

// SetAutoIndent enables language-aware typing; paste remains literal.
func (c *EditorCore) SetAutoIndent(enabled bool) { c.autoIndent = enabled }

// CoreIndentUnit overrides the language's indent unit. It must be whitespace.
func CoreIndentUnit(unit string) CoreOption {
	if !indent.Whitespace(unit) {
		panic("widget: indent unit must contain only spaces and tabs")
	}
	return func(c *EditorCore) { c.indentUnit = unit }
}

// IndentUnitError describes an invalid runtime indentation unit.
type IndentUnitError struct{ Unit string }

func (e *IndentUnitError) Error() string {
	return "widget: indent unit must contain only spaces and tabs"
}

// SetIndentUnit changes the unit without changing text; invalid input keeps the old unit.
func (c *EditorCore) SetIndentUnit(unit string) error {
	if !indent.Whitespace(unit) {
		return &IndentUnitError{Unit: unit}
	}
	c.indentUnit = unit
	return nil
}

// SetHighlightOverlay installs a paint-only decorator without invalidating source states.
func (c *EditorCore) SetHighlightOverlay(overlay highlight.Overlay) {
	c.find.host = overlay
	c.hl.overlay = c.findOverlay() // the active find's marks stay over it
	c.InvalidateHighlightPaint()
}

// InvalidateHighlightPaint refreshes marks without changing verified lexical context.
func (c *EditorCore) InvalidateHighlightPaint() { c.hl.paintRevision++; c.markDirty() }

// SetSyntaxStyles replaces what each highlight style looks like.
func (c *EditorCore) SetSyntaxStyles(st SyntaxStyles) {
	c.hl.syntax = st
	c.markDirty()
}

// --- runtime keymap reflection ---------------------------------------------------

// Keyset reports the active editing & keymap profile.
func (c *EditorCore) Keyset() Keyset { return c.keys.keyset }

// SetKeyset switches the editing profile on a LIVE core. The document survives: text, cursor,
// undo/redo history, and the register. In-flight input does not: a pending count or prefix, a
// visual selection and an open undo group are discarded; a half-typed escape chord is committed
// as text. Vim lands in Normal, the modeless profiles in Insert. Host bindings (CoreKeymap) are
// replayed onto the new profile. Switching to the active profile is a no-op.
func (c *EditorCore) SetKeyset(ks Keyset) {
	ks = normalizeKeyset(ks)
	if ks == c.keys.keyset {
		return
	}
	c.resetInput()
	c.dot = dotRecorder{} // a change recorded under another profile is not this one's to repeat
	c.hist.close()
	c.buf.anchor, c.vAnchor = nil, taPos{}
	c.keys.applyKeyset(ks, c.liveKeymap())
	if c.keys.modal {
		c.setMode(ModeNormal)
		c.clampNormal()
	} else {
		c.setMode(ModeInsert)
	}
	c.buf.desired = -1
	c.reveal()
	c.markDirty()
	c.noteKeyScope()
}

// OverlayKeymap folds overlay into the keymap of a core already built, as CoreKeymap does at
// construction: an entry rebinds its chord, and ActUnbound removes one, so that key goes unhandled
// to the host (a host whose own Shortcut owns a letter the profile binds). The overlay is kept and
// replayed onto another keyset as CoreKeymap's is. Every entry is validated before any applies
// (panics on an unknown action or an unsupported mode and action), so a bad overlay changes nothing.
// A key sequence pending under the old bindings is dropped.
func (c *EditorCore) OverlayKeymap(overlay Keymap) {
	CoreKeymap(overlay)(c)
	c.keys.dropPending()
	c.markDirty()
}

// Keymap returns a defensive copy of the active keymap.
func (c *EditorCore) Keymap() Keymap {
	cp := make(Keymap, len(c.keys.keymap))
	for k, v := range c.keys.keymap {
		cp[k] = v
	}
	return cp
}

// EscapeChord returns the configured two-rune escape chord (e.g. "jk"), or "" if disabled.
func (c *EditorCore) EscapeChord() string { return string(c.keys.chord) }

// Bindings returns every discrete key chord of the active keymap, sorted by mode, chord and
// action. The Insert-mode escape chord is reported by EscapeChord.
func (c *EditorCore) Bindings() []KeyBinding { return c.keys.keymap.Bindings() }

// BindingsForMode returns the active bindings in mode m.
func (c *EditorCore) BindingsForMode(m EditorMode) []KeyBinding {
	all := c.Bindings()
	filtered := make([]KeyBinding, 0, len(all))
	targetMode := modeClass(m)
	for _, b := range all {
		if modeClass(b.Mode) == targetMode {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

// SnapshotKeymap is a serializable reflection of the active key configuration.
func (c *EditorCore) SnapshotKeymap() KeymapSnapshot {
	return KeymapSnapshot{
		Keyset:      c.keys.keyset,
		KeysetName:  c.keys.keyset.String(),
		Modal:       c.keys.modal,
		EscapeChord: string(c.keys.chord),
		Bindings:    c.Bindings(),
	}
}

// ActionForChord looks up the action bound to a chord.
func (c *EditorCore) ActionForChord(kc KeyChord) (Action, bool) { return c.keys.lookup(kc) }

// ActionOf is the action key k is bound to in the current mode, read as HandleKey reads it (key.Of,
// then key.Candidates; an explicit unbind honoured), without acting on it. A release, or a key with
// Hyper or Meta held, is bound to nothing. For a widget that shows the document another way and still
// answers the editor's keys.
func (c *EditorCore) ActionOf(k tui.KeyEvent) (Action, bool) {
	c.syncKeymap()
	s, ok := c.seqOf(k)
	if !ok {
		return ActUnbound, false
	}
	_, act, bound, _ := c.keys.lookupSeq(modeClass(c.keys.mode), s)
	return act, bound
}

// seqOf is k as a key (key.Of): false for a release, and for a key with Hyper or Meta held.
func (c *EditorCore) seqOf(k tui.KeyEvent) (tuikey.Seq, bool) {
	if k.Kind == tui.KeyRelease {
		return tuikey.Seq{}, false
	}
	return tuikey.Of(k.Code, k.Mods, k.Text)
}

// chordOf is k as the keymap reads it in the mode the core is in: false for a release, or a key
// with Hyper or Meta held. An Alt or Super chord is a chord: a key map may bind one; unbound, it
// misses the table and the core hands it to the host.
func (c *EditorCore) chordOf(k tui.KeyEvent) (KeyChord, bool) {
	s, ok := c.seqOf(k)
	if !ok {
		return KeyChord{}, false
	}
	return chordOfSeq(modeClass(c.keys.mode), s), true
}

// liveKeymap is the key map the App holds, nil without a mounted Context.
func (c *EditorCore) liveKeymap() *keymap.Map { return c.ctx.Keymap() }

// syncKeymap rebuilds the table when the App holds a key map other than the one it was built from (a
// reloaded scheme). The keyset, the mode and the document stay; pending grammar (a count, an
// operator, a g prefix, an armed f, r, ", m or q) is dropped, as Esc drops it, because the chord that
// armed it may have moved.
func (c *EditorCore) syncKeymap() {
	if m := c.liveKeymap(); m != c.keys.builtFrom {
		c.settlePendingRune()
		c.keys.dropPending()
		c.keys.applyKeyset(c.keys.keyset, m)
	}
}

// KeyScope is the scope of the keyset and mode class the core is in: "tui.editor.vim.normal".
func (c *EditorCore) KeyScope() keymap.Scope { return editorScope(c.keys.keyset, c.keys.mode) }

// SetChordHook gives a host the chords of its own: in Normal or Visual mode with nothing pending
// (no count, operator, armed f/t/m/q/" key, register), every chord but Esc goes to hook before the
// keymap, and one hook reports taken is the host's alone: not run by the keymap, not recorded for
// . or a macro. golib keeps no policy of what a host does with it (a command line, a script's
// function). nil removes it.
func (c *EditorCore) SetChordHook(hook func(KeyChord) bool) { c.chordHook = hook }

// ChordsForAction returns every chord bound to act.
func (c *EditorCore) ChordsForAction(act Action) []KeyChord { return c.keys.chordsFor(act) }

// resetInput ends the input in flight at a boundary — a new document, a buffer switch, a keyset
// switch: a pending insert rune settles where it was typed, and a command being typed (dropPending),
// Ctrl-o's one-shot, a block insert and Replace are dropped, never completed against what follows.
func (c *EditorCore) resetInput() {
	c.settlePendingRune()
	c.keys.dropPending()
	c.oneShot, c.blockIns, c.replace = insertOneShot{}, nil, nil
}

// --- mode & cursor invariants -------------------------------------------

func (c *EditorCore) setMode(m EditorMode) {
	if c.keys.mode == m {
		return
	}
	c.keys.mode = m
	if m == ModeInsert {
		c.oneShot = insertOneShot{} // entering Insert ends Ctrl-o's command
	} else {
		c.replace = nil // Replace is Insert's: leaving Insert ends it
	}
	c.keys.findArmed, c.keys.findCount = ActUnbound, 0 // an f armed in one mode never completes in another
	c.keys.dropOp()                                    // nor an operator, nor a text object
	c.vEOL, c.vPinned = false, false
	c.markDirty()
	c.publish(ModeChangedEvent{Owner: c.nodeID(), Mode: c.Mode()})
	if c.onModeChange != nil {
		c.onModeChange(c.Mode())
	}
	c.noteKeyScope()
}

// normalMax is the max Normal-mode column of line ln (cursor ON a grapheme).
func (c *EditorCore) normalMax(ln int) int {
	return max(0, len(c.buf.lineClusters(ln))-1)
}

// lastCol is the furthest column a motion reaches on line ln: past the last grapheme in Insert,
// where text is typed there, and on it otherwise (normalMax).
func (c *EditorCore) lastCol(ln int) int {
	if c.keys.mode == ModeInsert {
		return len(c.buf.lineClusters(ln))
	}
	return c.normalMax(ln)
}

// clampNormal enforces the Normal/Visual cursor invariant.
func (c *EditorCore) clampNormal() {
	c.buf.col = min(c.buf.col, c.normalMax(c.buf.ln))
}

func (c *EditorCore) enterInsert() {
	c.keys.count, c.keys.pendingAct = 0, ActUnbound
	c.hist.close() // group opens lazily on the first mutation
	c.setMode(ModeInsert)
	c.insStart = taPos{c.buf.ln, c.buf.col} // where Ctrl-w and Ctrl-u stop once (editor_viminsert.go)
}

// exitInsert implements Insert→Normal in modal mode: cursor one cluster left, clamped. In
// modeless editing it is a no-op: the core stays in Insert.
func (c *EditorCore) exitInsert() {
	if !c.keys.modal {
		return
	}
	c.blockInsertDone() // a block's I, A or c: the typed text on its other lines, in this step
	c.replaceDone()     // R with a count: the text again, as many times more
	c.dotInsertEnded()
	c.hist.close()
	c.buf.anchor = nil // a table cell's selection is Insert mode's
	c.buf.col = max(0, c.buf.col-1)
	c.clampNormal()
	c.buf.desired = -1
	c.setMode(ModeNormal)
	c.reveal()
	c.markDirty()
}

// exitVisual clears the selection anchor and leaves visual mode: to Normal when modal, to Insert
// when modeless.
func (c *EditorCore) exitVisual() {
	c.buf.anchor = nil
	if c.keys.modal {
		c.setMode(ModeNormal)
		c.clampNormal()
	} else {
		c.setMode(ModeInsert)
	}
	c.markDirty()
}

// --- escape chord ---------------------------------------------------------

// settlePendingRune commits a held first chord rune as an insertion: every non-chord input
// settles the pending rune first.
func (c *EditorCore) settlePendingRune() {
	r, ok := c.keys.takeRune()
	if !ok {
		return
	}
	c.beginGroup()
	c.buf.insertText(string(r))
	c.edited()
	c.dotTypedText(string(r))
}

// typeText inserts text as typing does: the closing alignment runs when it types a closer.
func (c *EditorCore) typeText(s string) {
	if c.replace != nil {
		c.replaceText(s) // Replace overwrites
		return
	}
	c.beginGroup()
	c.buf.insertText(s)
	if strings.ContainsAny(s, "})] \t") {
		c.alignClosing()
	}
	c.edited()
}

// --- semantic commands -------------------------------------------------------

// menuAction starts a semantic editor command independent of the active keyset. A menu can take
// focus midway through a chord or counted operator: keep a held insert rune as text, but never
// complete a half-entered prefix or carry its count into a later keystroke. Keep the visual
// selection for the action.
func (c *EditorCore) menuAction(act Action) {
	c.settlePendingRune()
	c.hist.close()
	c.keys.dropPending()
	c.execAction(act, 1)
}

// Copy yanks the selected text, or the current line without a selection, to the unnamed register
// and attempts to export it to the system clipboard. When yanking is disabled it changes neither.
func (c *EditorCore) Copy() { c.menuAction(ActCopy) }

// Cut deletes the selection, or the current line without a selection, into the unnamed register.
// It does NOT export to the system clipboard; a read-only core refuses it.
func (c *EditorCore) Cut() { c.menuAction(ActCut) }

// Paste inserts the unnamed register at the cursor, not the system clipboard. A read-only core
// refuses it.
func (c *EditorCore) Paste() { c.menuAction(ActPaste) }

// Undo reverts the last edit group, as u does. A read-only core, or one without undo history,
// refuses it.
func (c *EditorCore) Undo() { c.menuAction(ActUndo) }

// Redo reapplies the most recently undone edit group, as Ctrl+R does.
func (c *EditorCore) Redo() { c.menuAction(ActRedo) }

// CanUndo reports whether Undo would change the text now.
func (c *EditorCore) CanUndo() bool { return !c.readOnly && c.hist.canUndo() }

// CanRedo reports whether Redo would change the text now.
func (c *EditorCore) CanRedo() bool { return !c.readOnly && c.hist.canRedo() }

// mutatingAction reports the actions refused in read-only mode (motions, visual entry, and yank
// stay available — a viewer still navigates and copies).
func mutatingAction(act Action) bool { return act < actMax && actionSpecs[act].mutating }

// execAction runs one bound action with the (already consumed) count.
func (c *EditorCore) execAction(act Action, count int) bool {
	if c.readOnly && mutatingAction(act) {
		// Refused, and NOT consumed: a viewer never mutates, and a key it has no use for is the
		// host's — a dialog's mnemonic (Select's s, from a read-only preview), a window's shortcut.
		// Swallowing it would leave the host deaf to it while the viewer did nothing.
		return false
	}
	if act >= actMax {
		return false
	}
	switch actionSpecs[act].exec {
	case actMotion:
		return c.execMotion(act, count)
	case actInsertEntry:
		return c.execInsertEntry(act, count)
	case actEdit:
		return c.execEdit(act, count)
	case actVisualMode:
		return c.execVisual(act, count)
	case actInsertAssist:
		return c.execInsertAssist(act, count)
	case actInsertEdit: // bound early, by a host's overlay
		return c.execInsertEdit(act, tui.KeyEvent{})
	case actOther:
		return c.execOther(act, count)
	}
	return false
}

// execMotion runs the Motion actions (actionSpecs[act].exec).
func (c *EditorCore) execMotion(act Action, count int) bool {
	switch act {
	// Motions.
	case ActLeft, ActDown, ActUp, ActRight, ActLineStart, ActLineEnd,
		ActWordForward, ActWordBack, ActWordEnd, ActParaForward, ActParaBack,
		ActPageUp, ActPageDown,
		ActLineDownFirst, ActLineUpFirst, ActFirstNonBlank, ActLeftWrap,
		ActFindCharRepeat, ActFindCharReverse, ActViewTop, ActViewMiddle, ActViewBottom,
		ActWORDForward, ActWORDBack, ActWORDEnd, ActWordEndBack, ActWORDEndBack,
		ActSentenceForward, ActSentenceBack:
		switch act {
		case ActViewTop, ActViewMiddle, ActViewBottom, ActParaForward, ActParaBack,
			ActSentenceForward, ActSentenceBack:
			c.setJumpMark() // H M L, { } and ( ) are jumps
		}
		c.move(act, count)
		return true
	}
	return false
}

// execInsertEntry runs the InsertEntry actions (actionSpecs[act].exec).
func (c *EditorCore) execInsertEntry(act Action, count int) bool {
	b := &c.buf
	switch act {
	// Insert entries.
	case ActInsert:
		c.enterInsert()
		return true
	case ActAppend:
		if len(b.lineClusters(b.ln)) > 0 {
			b.col++
		}
		c.enterInsert()
		return true
	case ActInsertLineStart:
		b.col = 0
		c.enterInsert()
		return true
	case ActAppendLineEnd:
		b.col = len(b.lineClusters(b.ln))
		c.enterInsert()
		return true
	case ActOpenBelow:
		c.beginGroup()
		prefix := c.indentDecision(indent.OpenBelow, len(b.textLines[b.ln])).Prefix
		b.insertLines(b.ln+1, prefix)
		b.ln, b.col = b.ln+1, len(clusters(prefix))
		c.enterInsert()
		c.hist.keepOpen() // the open-line already began this group
		c.edited()
		return true
	case ActOpenAbove:
		c.beginGroup()
		prefix := c.indentDecision(indent.OpenAbove, 0).Prefix
		b.insertLines(b.ln, prefix)
		b.col = len(clusters(prefix))
		c.enterInsert()
		c.hist.keepOpen()
		c.edited()
		return true

		// Normal-mode edits.
	}
	return false
}

// execEdit runs the Edit actions (actionSpecs[act].exec).
func (c *EditorCore) execEdit(act Action, count int) bool {
	b := &c.buf
	switch act {
	case ActDeleteChar:
		if c.inFold(b.ln) { // x is dl, which takes a fold whole (Vim's fold-behavior)
			c.runOp(ActDeletePrefix, taPos{b.ln, b.col + 1}, exclusive)
			return true
		}
		cs := b.lineClusters(b.ln)
		if len(cs) == 0 {
			return true
		}
		n := min(count, len(cs)-b.col)
		c.beginGroup()
		c.regDelete(strings.Join(cs[b.col:b.col+n], ""), false)
		b.deleteRegion(taPos{b.ln, b.col}, taPos{b.ln, b.col + n})
		c.clampNormal()
		c.edited()
		return true
	case ActDeleteToEnd:
		if c.inFold(b.ln) { // D is d$, which takes a fold whole
			c.runOp(ActDeletePrefix, taPos{b.ln, b.col + 1}, exclusive)
			return true
		}
		cs := b.lineClusters(b.ln)
		if b.col < len(cs) {
			c.beginGroup()
			c.regDelete(strings.Join(cs[b.col:], ""), false)
			b.deleteRegion(taPos{b.ln, b.col}, taPos{b.ln, len(cs)})
			c.clampNormal()
			c.edited()
		}
		return true
	case ActChangeToEnd, ActYankToEnd: // C and Y: c$ and y$
		op := ActChangePrefix
		if act == ActYankToEnd {
			op = ActYankPrefix
		}
		if to, kind, ok := c.opTarget(ActLineEnd, count, true); ok {
			c.runOp(op, to, kind)
		}
		return true
	case ActSubstChar: // s: c over count characters, Insert on an empty line
		n := len(b.lineClusters(b.ln))
		c.runOp(ActChangePrefix, taPos{b.ln, min(b.col+count, n)}, exclusive)
		return true
	case ActJoin:
		c.joinLines(count, false)
		return true
	case ActJoinRaw:
		c.joinLines(count, true)
		return true
	case ActToggleCase:
		c.toggleCaseChars(count)
		return true
	case ActSubstLine: // S: cc
		c.runOp(ActChangePrefix, taPos{min(b.ln+count-1, len(b.textLines)-1), 0}, linewise)
		return true
	case ActPasteAfter:
		c.pasteRegister(true, count)
		return true
	case ActPasteBefore:
		c.pasteRegister(false, count)
		return true
	case ActUndo:
		c.doUndo()
		return true
	case ActRedo:
		c.doRedo()
		return true

		// Visual entry/exit.
	}
	return false
}

// execVisual runs the VisualMode actions (actionSpecs[act].exec).
func (c *EditorCore) execVisual(act Action, count int) bool {
	b := &c.buf
	switch act {
	case ActVisual:
		if !c.canSelect {
			return true
		}
		switch c.keys.mode {
		case ModeVisual:
			c.exitVisual()
		case ModeVisualLine, ModeVisualBlock:
			c.setMode(ModeVisual)
		default:
			c.vAnchor = taPos{ln: b.ln, col: b.col}
			c.setMode(ModeVisual)
		}
		return true
	case ActVisualBlock:
		if !c.canSelect {
			return true
		}
		switch c.keys.mode {
		case ModeVisualBlock:
			c.exitVisual()
		case ModeVisual, ModeVisualLine:
			c.setMode(ModeVisualBlock)
		default:
			c.vAnchor = taPos{ln: b.ln, col: b.col}
			c.setMode(ModeVisualBlock)
		}
		return true
	case ActVisualLine:
		if !c.canSelect {
			return true
		}
		switch c.keys.mode {
		case ModeVisualLine:
			c.exitVisual()
		case ModeVisual, ModeVisualBlock:
			c.setMode(ModeVisualLine)
		default:
			c.vAnchor = taPos{ln: b.ln, col: b.col}
			c.setMode(ModeVisualLine)
		}
		return true

	// Visual operations.
	case ActVisualYank:
		if !c.reg.yankAllowed() {
			c.exitVisual()
			return true
		}
		if s, ok := c.visualSelection(); ok {
			s.yank()
		}
		c.exitVisual()
		c.reveal()
		return true
	case ActVisualDelete, ActVisualChange, ActVisualToggleCase, ActVisualLower, ActVisualUpper,
		ActVisualIndent, ActVisualOutdent, ActVisualInsert, ActVisualAppend:
		c.visualOp(act, count, "") // editor_vimvisual.go: . repeats it over a region of its size
		return true
	case ActVisualOther:
		c.visualOther()
		return true
	case ActVisualOtherCol:
		if c.keys.mode == ModeVisualBlock {
			c.blockOther()
		} else {
			c.visualOther() // O is o outside a block
		}
		return true
	}
	return false
}

// execInsertAssist runs the InsertAssist actions (actionSpecs[act].exec).
func (c *EditorCore) execInsertAssist(act Action, count int) bool {
	switch act {
	case ActDeleteWordBack, ActDeleteLineBack:
		c.insertDeleteBack(act == ActDeleteLineBack)
		return true
	case ActInsertOneShot:
		return c.insertOneShotStart()
	case ActReplaceMode:
		c.enterReplace(count)
		return true

		// General actions (Nano / Standard).
	}
	return false
}

// execOther runs the Other actions (actionSpecs[act].exec).
func (c *EditorCore) execOther(act Action, count int) bool {
	b := &c.buf
	switch act {
	case ActCut:
		if c.visual() {
			return c.execAction(ActVisualDelete, count)
		}
		c.deleteLines(b.ln, b.ln)
		return true

	case ActCopy:
		if !c.reg.yankAllowed() {
			if c.visual() {
				c.exitVisual()
			}
			return true
		}
		if c.visual() {
			return c.execAction(ActVisualYank, count)
		}
		if b.ln < len(b.textLines) {
			c.regYank(b.textLines[b.ln], true)
		}
		return true

	case ActPaste:
		c.pasteRegister(false, 1)
		return true

	case ActCommandLine:
		// the prompt is the widget's: with none, ':' reaches the application
		if c.commandLine == nil || c.commands == nil {
			return false
		}
		opened := c.commandLine()
		c.oneShot.cmdline = c.oneShot.on && opened // Ctrl-o's Insert waits for the line
		return opened

	case ActFindNext, ActFindPrev:
		// with no active find the key bubbles: an application may have its own n
		from := taPos{b.ln, b.col}
		found := c.FindNext(act == ActFindPrev)
		if found {
			c.jumpedFrom(from) // n and N are jumps
		}
		return found

	case ActToggleRendered:
		// A view's concern: the widget that has a Rendered view answers it (SetToggleRendered).
		// With none, the key is not consumed, so it reaches the application.
		if c.toggleRendered == nil {
			return false
		}
		return c.toggleRendered()

	case ActSelectAll:
		if !c.canSelect || len(b.textLines) == 0 {
			return true
		}
		c.vAnchor = taPos{ln: 0, col: 0}
		lastLn := len(b.textLines) - 1
		b.ln = lastLn
		b.col = max(0, len(b.lineClusters(lastLn))-1)
		c.setMode(ModeVisual)
		return true
	}
	return false
}
