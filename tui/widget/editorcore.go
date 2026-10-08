package widget

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/yongjohnlee80/golib/highlight"
	"github.com/yongjohnlee80/golib/tui"
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
// counts every such call):
//
//	EditorCore
//	├── buf  textBuffer     the lines, the cursor, the sticky column (textbuffer.go)
//	├── hist editHistory    undo and redo (edithistory.go)
//	├── reg  editRegister   the unnamed register and the yank policy (editregister.go)
//	├── keys keyDispatch    the mode, the keymap, the input pending between keys (editorkeys.go)
//	├── hl   highlightCache each line's syntax styles (editor_highlight.go)
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
	buf  textBuffer     // the text and the cursor
	hist editHistory    // undo/redo
	reg  editRegister   // the unnamed register
	keys keyDispatch    // modes and key routing
	hl   highlightCache // per-line styles
	// hlFrom is the first line changed that the highlighter has not looked at yet: an edit takes
	// the buffer's change mark for the layout and leaves it here (takeHighlightChanged).
	hlFrom int
	layout EditorLayout // the widget's geometry; set by Bind
	ctx    *tui.Context // the widget's: publish, clipboard, timers; set by Bind

	vAnchor   taPos // the Visual anchor (where the selection started)
	canSelect bool  // visual selection is allowed
	readOnly  bool  // a viewer: motions and yank only
	// listEditing continues, ends and nests Markdown list items in Insert mode (editor_lists.go)
	listEditing bool

	// The constructor-time listeners for what the core also publishes on the bus: a widget built
	// before it is mounted has no Context to subscribe with.
	onModeChange func(EditorMode)
	onChange     func()
	onCursorMove func() // the cursor moved to another line or column
	// toggleRendered answers ActToggleRendered for a widget with a Rendered view; nil: none
	toggleRendered func() bool

	// dragging is a pointer drag selecting from dragFrom, in buffer positions.
	dragging bool
	dragFrom taPos
}

// CoreOption configures an EditorCore under construction.
type CoreOption func(*EditorCore)

// newEditorCore is the default core: an empty line, the Vim keymap, modal, the "jk" escape chord,
// selection, yank and undo on. The options have not run yet.
func newEditorCore() *EditorCore {
	return &EditorCore{
		buf:       newTextBuffer(),
		hlFrom:    noChange,
		keys:      newKeyDispatch(),
		canSelect: true,
		reg:       editRegister{yank: true},
		hist:      editHistory{enabled: true},
	}
}

// settleOptions is the end of construction: a modeless profile starts in Insert.
func (c *EditorCore) settleOptions() {
	if !c.keys.modal && c.keys.mode == ModeNormal {
		c.setMode(ModeInsert)
	}
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
	c.settleOptions()
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
func CoreHighlighter(h highlight.Highlighter) CoreOption { return func(c *EditorCore) { c.hl.hl = h } }

// CoreSyntaxStyles sets what each highlight style looks like.
func CoreSyntaxStyles(st SyntaxStyles) CoreOption { return func(c *EditorCore) { c.hl.syntax = st } }

// CoreInitialText seeds the buffer: the cursor at the document start, the initial editing mode,
// an empty undo history.
func CoreInitialText(s string) CoreOption {
	return func(c *EditorCore) {
		c.buf.setValue(s)
		c.buf.ln, c.buf.col = 0, 0
	}
}

// CoreEscapeChord sets the Insert-mode escape chord (default "jk"): exactly two unmodified
// printable runes, or "" to disable (Esc alone). Anything else panics.
func CoreEscapeChord(chord string) CoreOption {
	rs := []rune(chord)
	if chord != "" && len(rs) != 2 {
		panic(fmt.Sprintf("widget: WithEscapeChord: %q is not exactly two runes (or empty to disable)", chord))
	}
	for _, r := range rs {
		if !unicode.IsPrint(r) {
			panic(fmt.Sprintf("widget: WithEscapeChord: %q contains a non-printable rune", chord))
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
			c.keys.applyKeyset(KeysetNano)
			c.setMode(ModeInsert)
		}
	case KeysetStandard:
		return func(c *EditorCore) {
			c.keys.applyKeyset(KeysetStandard)
			c.setMode(ModeInsert)
		}
	default:
		return func(c *EditorCore) {
			c.keys.applyKeyset(KeysetVim)
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
		c.layout.Changed(min(from, max(len(c.buf.lines)-1, 0)))
	}
}

// takeHighlightChanged is the first line changed since the highlighter last asked, noChange for
// none, and forgets it: the highlight cache's view of textBuffer.takeChanged.
func (c *EditorCore) takeHighlightChanged() int {
	ln := min(c.hlFrom, c.buf.takeChanged())
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
	if c.onCursorMove != nil && (c.buf.ln != ln || c.buf.col != col) {
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
	c.settlePendingRune()
	c.keys.count, c.keys.pendingAct = 0, ActUnbound
	c.hist.reset()
	c.buf.setValue(s)
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
func (c *EditorCore) Mode() EditorMode { return c.keys.mode }

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
	c.buf.ln = max(0, min(row, len(c.buf.lines)-1))
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
	for row, line := range c.buf.lines {
		n := len(clusters(line))
		if pos <= n || row == len(c.buf.lines)-1 {
			c.SetLine(row, min(pos, n))
			return
		}
		pos -= n + 1
	}
}

// Lines returns a snapshot of the document's lines.
func (c *EditorCore) Lines() []string { return append([]string(nil), c.buf.lines...) }

// SetHighlighter replaces the highlighter; nil turns highlighting off. Every line is highlighted
// afresh.
func (c *EditorCore) SetHighlighter(h highlight.Highlighter) {
	c.hl.setHighlighter(h)
	c.markDirty()
}

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
	c.settlePendingRune()
	c.keys.dropPending()
	c.hist.close()
	c.buf.anchor, c.vAnchor = nil, taPos{}
	c.keys.applyKeyset(ks)
	if c.keys.modal {
		c.setMode(ModeNormal)
		c.clampNormal()
	} else {
		c.setMode(ModeInsert)
	}
	c.buf.desired = -1
	c.reveal()
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

// ChordsForAction returns every chord bound to act.
func (c *EditorCore) ChordsForAction(act Action) []KeyChord { return c.keys.chordsFor(act) }

// --- mode & cursor invariants -------------------------------------------

func (c *EditorCore) setMode(m EditorMode) {
	if c.keys.mode == m {
		return
	}
	c.keys.mode = m
	c.markDirty()
	c.publish(ModeChangedEvent{Owner: c.nodeID(), Mode: m})
	if c.onModeChange != nil {
		c.onModeChange(m)
	}
}

// normalMax is the max Normal-mode column of line ln (cursor ON a grapheme).
func (c *EditorCore) normalMax(ln int) int {
	return max(0, len(c.buf.lineClusters(ln))-1)
}

// clampNormal enforces the Normal/Visual cursor invariant.
func (c *EditorCore) clampNormal() {
	c.buf.col = min(c.buf.col, c.normalMax(c.buf.ln))
}

func (c *EditorCore) enterInsert() {
	c.keys.count, c.keys.pendingAct = 0, ActUnbound
	c.hist.close() // group opens lazily on the first mutation
	c.setMode(ModeInsert)
}

// exitInsert implements Insert→Normal in modal mode: cursor one cluster left, clamped. In
// modeless editing it is a no-op: the core stays in Insert.
func (c *EditorCore) exitInsert() {
	if !c.keys.modal {
		return
	}
	c.hist.close()
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
func (c *EditorCore) execAction(act Action, count int) bool {
	if c.readOnly && mutatingAction(act) {
		return true // consumed and refused: a viewer never mutates
	}
	b := &c.buf
	switch act {
	// Motions.
	case ActLeft, ActDown, ActUp, ActRight, ActLineStart, ActLineEnd,
		ActWordForward, ActWordBack, ActWordEnd, ActParaForward, ActParaBack,
		ActPageUp, ActPageDown:
		c.move(act, count)
		return true

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
		b.lines = append(b.lines[:b.ln+1], append([]string{""}, b.lines[b.ln+1:]...)...)
		b.touch(b.ln + 1)
		b.ln, b.col = b.ln+1, 0
		c.enterInsert()
		c.hist.keepOpen() // the open-line already began this group
		c.edited()
		return true
	case ActOpenAbove:
		c.beginGroup()
		b.lines = append(b.lines[:b.ln], append([]string{""}, b.lines[b.ln:]...)...)
		b.touch(b.ln)
		b.col = 0
		c.enterInsert()
		c.hist.keepOpen()
		c.edited()
		return true

	// Normal-mode edits.
	case ActDeleteChar:
		cs := b.lineClusters(b.ln)
		if len(cs) == 0 {
			return true
		}
		n := min(count, len(cs)-b.col)
		c.beginGroup()
		c.yankSet(strings.Join(cs[b.col:b.col+n], ""), false)
		b.deleteRegion(taPos{b.ln, b.col}, taPos{b.ln, b.col + n})
		c.clampNormal()
		c.edited()
		return true
	case ActDeleteToEnd:
		cs := b.lineClusters(b.ln)
		if b.col < len(cs) {
			c.beginGroup()
			c.yankSet(strings.Join(cs[b.col:], ""), false)
			b.deleteRegion(taPos{b.ln, b.col}, taPos{b.ln, len(cs)})
			c.clampNormal()
			c.edited()
		}
		return true
	case ActPasteAfter:
		c.pasteRegister(true)
		return true
	case ActPasteBefore:
		c.pasteRegister(false)
		return true
	case ActUndo:
		c.doUndo()
		return true
	case ActRedo:
		c.doRedo()
		return true

	// Visual entry/exit.
	case ActVisual:
		if !c.canSelect {
			return true
		}
		switch c.keys.mode {
		case ModeVisual:
			c.exitVisual()
		default:
			c.vAnchor = taPos{ln: b.ln, col: b.col}
			c.setMode(ModeVisual)
		}
		return true
	case ActVisualLine:
		if !c.canSelect {
			return true
		}
		switch c.keys.mode {
		case ModeVisualLine:
			c.exitVisual()
		case ModeVisual:
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
		if c.keys.mode == ModeVisualLine {
			lo, hi := c.visualLines()
			text := strings.Join(b.lines[lo:hi+1], "\n")
			c.yankSet(text, true)
			c.exportYank(text)
			b.ln, b.col = lo, 0
		} else {
			lo, hiEx := c.visualRange()
			text := b.textIn(lo, hiEx)
			c.yankSet(text, false)
			c.exportYank(text)
			b.ln, b.col = lo.ln, lo.col
		}
		c.exitVisual()
		c.reveal()
		return true
	case ActVisualDelete:
		if c.keys.mode == ModeVisualLine {
			lo, hi := c.visualLines()
			c.exitVisual()
			c.deleteLines(lo, hi)
		} else {
			lo, hiEx := c.visualRange()
			c.beginGroup()
			c.yankSet(b.textIn(lo, hiEx), false)
			b.deleteRegion(lo, hiEx)
			c.exitVisual()
			c.edited()
		}
		return true

	// General actions (Nano / Standard).
	case ActCut:
		if c.keys.mode == ModeVisual || c.keys.mode == ModeVisualLine {
			return c.execAction(ActVisualDelete, count)
		}
		c.deleteLines(b.ln, b.ln)
		return true

	case ActCopy:
		if !c.reg.yankAllowed() {
			if c.keys.mode == ModeVisual || c.keys.mode == ModeVisualLine {
				c.exitVisual()
			}
			return true
		}
		if c.keys.mode == ModeVisual || c.keys.mode == ModeVisualLine {
			return c.execAction(ActVisualYank, count)
		}
		if b.ln < len(b.lines) {
			text := b.lines[b.ln]
			c.yankSet(text, true)
			c.exportYank(text)
		}
		return true

	case ActPaste:
		c.pasteRegister(false)
		return true

	case ActToggleRendered:
		// A view's concern: the widget that has a Rendered view answers it (SetToggleRendered).
		// With none, the key is not consumed, so it reaches the application.
		if c.toggleRendered == nil {
			return false
		}
		return c.toggleRendered()

	case ActSelectAll:
		if !c.canSelect || len(b.lines) == 0 {
			return true
		}
		c.vAnchor = taPos{ln: 0, col: 0}
		lastLn := len(b.lines) - 1
		b.ln = lastLn
		b.col = max(0, len(b.lineClusters(lastLn))-1)
		c.setMode(ModeVisual)
		return true
	}
	return false
}
