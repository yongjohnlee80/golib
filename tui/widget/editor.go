package widget

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// Editor is the vim-modal multi-line editor (ADR-0008 §2.1): the textBuffer
// substrate under a Normal/Insert/Visual state machine with a data-driven
// keymap, a single unnamed register, bounded undo, and a configurable
// escape chord. Unbound keys in Normal/Visual mode — Space included —
// bubble, so an application leader menu needs no editor cooperation.
type Editor struct {
	readOnly bool // viewer mode: motions and yank only
	Base
	textBuffer

	// viewport (same discipline as TextArea)
	wrap WrapMode
	top  int
	left int
	w, h int

	styles TextInputStyles
	keymap Keymap

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
}

type editorSnap struct {
	lines   []string
	ln, col int
}

const editorUndoCap = 64

var (
	_ tui.Focusable      = (*Editor)(nil)
	_ tui.CursorReporter = (*Editor)(nil)
	_ tui.CursorShaper   = (*Editor)(nil)
)

// EditorOption customizes an Editor under construction.
type EditorOption func(*Editor)

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

// WithInitialText seeds the buffer (cursor at the document start, Normal
// mode, empty undo history).
func WithInitialText(s string) EditorOption {
	return func(e *Editor) {
		e.setValue(s)
		e.ln, e.col = 0, 0
	}
}

// WithEscapeChord sets the Insert-mode escape chord (default "jk"): exactly
// two unmodified printable runes, or "" to disable (Esc alone). Anything
// else panics (ADR-0008 §2.1).
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
		for kc, act := range overlay {
			validateKeymapEntry(kc, act)
			if act == ActUnbound {
				delete(e.keymap, kc)
			} else {
				e.keymap[kc] = act
			}
		}
	}
}

// NewEditor builds an empty Normal-mode editor with the default keymap and
// the "jk" escape chord.
func NewEditor(opts ...EditorOption) *Editor {
	e := &Editor{
		textBuffer: newTextBuffer(),
		wrap:       WrapNone,
		styles: TextInputStyles{
			Selection: style.New().Background(style.TokenSecondary).Foreground(style.TokenTextOnSecondary),
		},
		keymap:       DefaultKeymap(),
		chord:        []rune{'j', 'k'},
		chordTimeout: 300 * time.Millisecond,
	}
	for _, o := range opts {
		if o != nil {
			o(e)
		}
	}
	return e
}

// Value returns the buffer joined with newlines.
func (e *Editor) Value() string { return e.value() }

// SetValue is a document-boundary operation (ADR-0008 §2.1): pending input
// settles, the editor returns to Normal mode, cursor and command state
// reset, content is replaced, and undo/redo history is CLEARED. The
// register is preserved.
func (e *Editor) SetValue(s string) {
	e.settlePendingRune()
	e.count, e.pendingAct = 0, ActUnbound
	e.groupOpen = false
	e.undo, e.redo = nil, nil
	e.setValue(s)
	e.ln, e.col = 0, 0
	e.setMode(ModeNormal)
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
// Insert session returns to Normal. Hosts use it for panels the user
// navigates but must not change (ADR-0008 §2.1).
func (e *Editor) SetReadOnly(v bool) {
	if e.readOnly == v {
		return
	}
	e.readOnly = v
	if v && (e.mode == ModeInsert) {
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
	e.clampNormal()
	e.ensureVisible()
	e.MarkDirty()
}

// Lines returns a snapshot of the document's lines — what a host needs to
// search without re-splitting Value().
func (e *Editor) Lines() []string { return append([]string(nil), e.lines...) }

// SelectedText returns the visual selection ("" outside visual modes).
func (e *Editor) SelectedText() string {
	switch e.mode {
	case ModeVisual:
		lo, hi := e.visualRange()
		return e.textIn(lo, hi)
	case ModeVisualLine:
		lo, hi := e.visualLines()
		return strings.Join(e.lines[lo:hi+1], "\n")
	}
	return ""
}

// SetRegister imports text into the unnamed register (the application's
// value-inspect copy path, ADR-0008 §2.1).
func (e *Editor) SetRegister(text string, linewise bool) {
	e.regText, e.regLinewise = text, linewise
}

// Register returns the unnamed register's content.
func (e *Editor) Register() (text string, linewise bool) {
	return e.regText, e.regLinewise
}

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

// --- mode & cursor invariants -------------------------------------------

func (e *Editor) setMode(m EditorMode) {
	if e.mode == m {
		return
	}
	e.mode = m
	e.MarkDirty()
	e.publish(ModeChangedEvent{Owner: e.NodeID(), Mode: m})
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

// exitInsert implements Insert→Normal: cursor one cluster left, clamped.
func (e *Editor) exitInsert() {
	e.groupOpen = false
	e.col = max(0, e.col-1)
	e.clampNormal()
	e.desired = -1
	e.setMode(ModeNormal)
	e.ensureVisible()
	e.MarkDirty()
}

func (e *Editor) exitVisual() {
	e.anchor = nil
	e.setMode(ModeNormal)
	e.clampNormal()
	e.MarkDirty()
}

// --- undo ----------------------------------------------------------------

func (e *Editor) snapshot() editorSnap {
	lines := make([]string, len(e.lines))
	copy(lines, e.lines)
	return editorSnap{lines: lines, ln: e.ln, col: e.col}
}

// beginGroup pushes an undo snapshot for a new edit group: every
// Normal-mode edit is one group; an Insert session is one group opened
// lazily at its first mutation (ADR-0008 §2.1 r3 — a paste during Insert
// stays inside the open group; focus loss closes it without leaving
// Insert).
func (e *Editor) beginGroup() {
	if e.mode == ModeInsert && e.groupOpen {
		return
	}
	e.undo = append(e.undo, e.snapshot())
	if len(e.undo) > editorUndoCap {
		e.undo = e.undo[1:]
	}
	e.redo = nil
	if e.mode == ModeInsert {
		e.groupOpen = true
	}
}

func (e *Editor) doUndo() {
	if len(e.undo) == 0 {
		return
	}
	snap := e.undo[len(e.undo)-1]
	e.undo = e.undo[:len(e.undo)-1]
	e.redo = append(e.redo, e.snapshot())
	e.restore(snap)
}

func (e *Editor) doRedo() {
	if len(e.redo) == 0 {
		return
	}
	snap := e.redo[len(e.redo)-1]
	e.redo = e.redo[:len(e.redo)-1]
	e.undo = append(e.undo, e.snapshot())
	e.restore(snap)
}

func (e *Editor) restore(s editorSnap) {
	e.lines = s.lines
	e.ln = max(0, min(s.ln, len(e.lines)-1))
	e.col = s.col
	e.anchor = nil
	e.clampNormal()
	e.edited()
}

// edited finalizes any buffer mutation: viewport, dirt, change event.
func (e *Editor) edited() {
	e.desired = -1
	e.ensureVisible()
	e.MarkDirty()
	e.publish(ChangeEvent{Owner: e.NodeID(), Value: e.Value()})
}

// --- escape chord ---------------------------------------------------------

// settlePendingRune commits a held first chord rune as an insertion
// (ADR-0008 §2.1: every non-chord input settles the pending rune first).
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

// --- motions ---------------------------------------------------------------

// isWS classifies a grapheme cluster as whitespace for the Editor's word
// motions (MF8: tabs and Unicode whitespace count, not just the literal
// space; the substrate's readline hops keep their own space-only rule).
func isWS(cluster string) bool {
	return strings.TrimSpace(cluster) == ""
}

// vimWordForward implements vim `w`: past the current word run, over
// whitespace, onto the start of the next word (crossing line ends).
func (e *Editor) vimWordForward() (int, int) {
	ln, col := e.ln, e.col
	cs := e.lineClusters(ln)
	i := col
	for i < len(cs) && !isWS(cs[i]) {
		i++ // leave the current run
	}
	for {
		for i < len(cs) && isWS(cs[i]) {
			i++
		}
		if i < len(cs) {
			return ln, i
		}
		if ln >= len(e.lines)-1 {
			return ln, max(0, len(cs)-1)
		}
		ln, cs, i = ln+1, e.lineClusters(ln+1), 0
	}
}

// vimWordBack implements vim `b`: back over whitespace onto the start of
// the previous word run (crossing line ends).
func (e *Editor) vimWordBack() (int, int) {
	ln, col := e.ln, e.col
	cs := e.lineClusters(ln)
	i := col
	for {
		for i > 0 && isWS(cs[i-1]) {
			i--
		}
		if i > 0 {
			for i > 0 && !isWS(cs[i-1]) {
				i--
			}
			return ln, i
		}
		if ln == 0 {
			return 0, 0
		}
		ln--
		cs = e.lineClusters(ln)
		i = len(cs)
	}
}

// wordEnd moves to the end of the current/next word (vim `e`, cluster form).
func (e *Editor) wordEnd() (int, int) {
	ln, col := e.ln, e.col
	for {
		cs := e.lineClusters(ln)
		i := col + 1
		for i < len(cs) && isWS(cs[i]) {
			i++
		}
		if i >= len(cs) {
			if ln < len(e.lines)-1 {
				ln, col = ln+1, -1
				continue
			}
			return ln, max(0, len(cs)-1)
		}
		for i+1 < len(cs) && !isWS(cs[i+1]) {
			i++
		}
		return ln, i
	}
}

// paraForward/paraBack: next/previous blank-line boundary.
func (e *Editor) paraForward(count int) int {
	ln := e.ln
	for ; count > 0; count-- {
		i := ln + 1
		for i < len(e.lines) && strings.TrimSpace(e.lines[i]) != "" {
			i++
		}
		ln = min(i, len(e.lines)-1)
	}
	return ln
}

func (e *Editor) paraBack(count int) int {
	ln := e.ln
	for ; count > 0; count-- {
		i := ln - 1
		for i > 0 && strings.TrimSpace(e.lines[i]) != "" {
			i--
		}
		ln = max(i, 0)
	}
	return ln
}

// move applies a motion action count times, extending the selection in
// visual modes.
func (e *Editor) move(act Action, count int) {
	// Visual highlights derive from vAnchor + cursor; the buffer's own
	// selection anchor stays nil so insertText never sees a stray region.
	const extend = false
	apply := func(ln, col int) {
		e.moveCursor(ln, col, extend)
		if e.mode != ModeInsert {
			e.clampNormal()
		}
		e.ensureVisible()
		e.MarkDirty()
	}
	switch act {
	case ActLeft:
		apply(e.ln, e.col-count)
		e.desired = -1
	case ActRight:
		apply(e.ln, min(e.col+count, e.normalMax(e.ln)))
		e.desired = -1
	case ActDown, ActUp:
		delta := count
		if act == ActUp {
			delta = -count
		}
		ln, col := e.verticalTarget(delta, e.measure)
		d := e.desired
		apply(ln, col)
		e.desired = d
	case ActPageDown, ActPageUp:
		delta := max(e.h, 1) * count
		if act == ActPageUp {
			delta = -delta
		}
		ln, col := e.verticalTarget(delta, e.measure)
		d := e.desired
		apply(ln, col)
		e.desired = d
	case ActLineStart:
		e.desired = -1
		apply(e.ln, 0)
	case ActLineEnd:
		e.desired = -1
		apply(e.ln, e.normalMax(e.ln))
	case ActWordForward:
		e.desired = -1
		for i := 0; i < count; i++ {
			ln, col := e.vimWordForward()
			e.moveCursor(ln, col, false)
		}
		e.clampNormal()
		e.ensureVisible()
		e.MarkDirty()
	case ActWordBack:
		e.desired = -1
		for i := 0; i < count; i++ {
			ln, col := e.vimWordBack()
			e.moveCursor(ln, col, false)
		}
		e.clampNormal()
		e.ensureVisible()
		e.MarkDirty()
	case ActWordEnd:
		e.desired = -1
		for i := 0; i < count; i++ {
			ln, col := e.wordEnd()
			e.moveCursor(ln, col, false)
		}
		e.clampNormal()
		e.ensureVisible()
		e.MarkDirty()
	case ActParaForward:
		e.desired = -1
		apply(e.paraForward(count), 0)
	case ActParaBack:
		e.desired = -1
		apply(e.paraBack(count), 0)
	}
}

// goToLine is the shared gg/G target motion: an EXPLICIT count means
// "line count" (1-based, clamped); without one, gg goes to the top and G
// to the bottom (MF10).
func (e *Editor) goToLine(hadCount bool, count int, bottom bool) {
	e.desired = -1
	ln := 0
	switch {
	case hadCount:
		ln = min(count-1, len(e.lines)-1)
	case bottom:
		ln = len(e.lines) - 1
	}
	e.moveCursor(ln, 0, false)
	e.clampNormal()
	e.ensureVisible()
	e.MarkDirty()
}

// --- visual ranges ----------------------------------------------------------

// visualRange returns the INCLUSIVE charwise selection as an exclusive
// [lo, hiEx) buffer region.
func (e *Editor) visualRange() (lo, hiEx taPos) {
	a, b := e.vAnchor, taPos{ln: e.ln, col: e.col}
	if a.ln > b.ln || (a.ln == b.ln && a.col > b.col) {
		a, b = b, a
	}
	return a, taPos{ln: b.ln, col: min(b.col+1, len(e.lineClusters(b.ln)))}
}

// visualLines returns the inclusive line span of a line-wise selection.
func (e *Editor) visualLines() (lo, hi int) {
	lo, hi = e.vAnchor.ln, e.ln
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

// --- edit operations ---------------------------------------------------------

func (e *Editor) yankSet(text string, linewise bool) {
	e.regText, e.regLinewise = text, linewise
}

// deleteLines removes [lo, hi] inclusive into the register (linewise).
func (e *Editor) deleteLines(lo, hi int) {
	e.beginGroup()
	e.yankSet(strings.Join(e.lines[lo:hi+1], "\n"), true)
	rest := append([]string{}, e.lines[:lo]...)
	rest = append(rest, e.lines[hi+1:]...)
	if len(rest) == 0 {
		rest = []string{""}
	}
	e.lines = rest
	e.ln = min(lo, len(e.lines)-1)
	e.col = 0
	e.anchor = nil
	e.clampNormal()
	e.edited()
}

func (e *Editor) pasteRegister(after bool) {
	if e.regText == "" && !e.regLinewise {
		return
	}
	e.beginGroup()
	if e.regLinewise {
		at := e.ln
		if after {
			at++
		}
		newLines := strings.Split(e.regText, "\n")
		e.lines = append(e.lines[:at], append(append([]string{}, newLines...), e.lines[at:]...)...)
		e.ln, e.col = at, 0
	} else {
		col := e.col
		if after && len(e.lineClusters(e.ln)) > 0 {
			col++
		}
		e.col = e.clampCol(e.ln, col)
		e.insertText(e.regText)
		// vim leaves the cursor ON the last pasted cluster.
		e.col = max(0, e.col-1)
		e.clampNormal()
	}
	e.edited()
}

// mutatingActions are refused in read-only mode (motions, visual entry,
// and yank stay available — a viewer still navigates and copies).
func mutatingAction(act Action) bool {
	switch act {
	case ActInsert, ActAppend, ActInsertLineStart, ActAppendLineEnd,
		ActOpenBelow, ActOpenAbove, ActDeleteChar, ActDeleteToEnd,
		ActPasteAfter, ActPasteBefore, ActUndo, ActRedo,
		ActDeletePrefix, ActVisualDelete:
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
		switch e.mode {
		case ModeVisual:
			e.exitVisual()
		default:
			e.vAnchor = taPos{ln: e.ln, col: e.col}
			e.setMode(ModeVisual)
		}
		return true
	case ActVisualLine:
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
		if e.mode == ModeVisualLine {
			lo, hi := e.visualLines()
			e.yankSet(strings.Join(e.lines[lo:hi+1], "\n"), true)
			e.ln, e.col = lo, 0
		} else {
			lo, hiEx := e.visualRange()
			e.yankSet(e.textIn(lo, hiEx), false)
			e.ln, e.col = lo.ln, lo.col
		}
		e.exitVisual()
		e.ensureVisible()
		return true
	case ActVisualDelete:
		if e.mode == ModeVisualLine {
			lo, hi := e.visualLines()
			e.setMode(ModeNormal)
			e.anchor = nil
			e.deleteLines(lo, hi)
		} else {
			lo, hiEx := e.visualRange()
			e.beginGroup()
			e.yankSet(e.textIn(lo, hiEx), false)
			e.deleteRegion(lo, hiEx)
			e.setMode(ModeNormal)
			e.clampNormal()
			e.edited()
		}
		return true
	}
	return false
}

// --- event handling -----------------------------------------------------------

// HandleEvent implements the modal key contract.
func (e *Editor) HandleEvent(ev tui.Event) bool {
	switch t := ev.(type) {
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
			// focus round-trip (ADR-0008 §2.1, r2).
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
// keys). Tab INSERTS a tab in Insert mode (ADR-0008 §2.1); traversal
// belongs to Normal mode, where Tab bubbles.
func (e *Editor) handleInsertKey(k tui.KeyEvent) bool {
	isText := k.Text != "" && k.Mods&nonTextMods == 0 && k.Code != tui.KeyTab

	// Chord state machine first (ADR-0008 §2.1 — dispatch-ordered).
	if e.pendingRune != 0 {
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
		// Insert state machine — it may itself be a fresh chord start
		// (MF9: "jjk" commits the first j and escapes on the second+k).
		e.settlePendingRune()
	}
	if isText && e.chord != nil && e.pendingRune == 0 && []rune(k.Text)[0] == e.chord[0] {
		e.pendingRune = e.chord[0]
		if ctx := e.Context(); ctx != nil {
			e.chordCancel = ctx.After(e.chordTimeout)
		}
		return true
	}

	switch k.Code {
	case tui.KeyTab:
		// Insert mode consumes Tab as text (ADR-0008 §2.1); traversal
		// belongs to Normal mode, where Tab bubbles.
		e.beginGroup()
		e.insertText("\t")
		e.edited()
		return true
	case tui.KeyEscape:
		e.exitInsert()
		return true
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
		e.desired = -1
		e.moveCursor(e.ln, e.col-1, false)
		e.ensureVisible()
		e.MarkDirty()
		return true
	case tui.KeyRight:
		e.desired = -1
		e.moveCursor(e.ln, e.col+1, false)
		e.ensureVisible()
		e.MarkDirty()
		return true
	case tui.KeyUp, tui.KeyDown:
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
		e.desired = -1
		e.moveCursor(e.ln, 0, false)
		e.MarkDirty()
		return true
	case tui.KeyEnd:
		e.desired = -1
		e.moveCursor(e.ln, len(e.lineClusters(e.ln)), false)
		e.MarkDirty()
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
		e.count, e.pendingAct = 0, ActUnbound
		if e.mode == ModeVisual || e.mode == ModeVisualLine {
			e.exitVisual()
		}
		return true
	}

	// Count accumulation: 1-9 always; 0 only extends an existing count.
	// Clamp BEFORE assignment so the cap is a hard ceiling (MF10).
	if !ctrl && k.Text != "" {
		r := []rune(k.Text)[0]
		if r >= '1' && r <= '9' || (r == '0' && e.count > 0) {
			e.pendingAct = ActUnbound
			e.count = min(e.count*10+int(r-'0'), 1_000_000)
			return true
		}
	}

	code := k.Code
	if k.Text != "" && k.Mods&nonTextMods == 0 {
		code = []rune(k.Text)[0] // shifted letters arrive via Text ("G")
	}
	kc := KeyChord{Mode: modeClass(e.mode), Code: code, Ctrl: ctrl}

	// Double-key pending buffer, keyed by the ARMING CHORD (MF10 — a
	// rebound prefix completes on its own chord, not a hard-coded rune):
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
				e.yankSet(strings.Join(e.lines[e.ln:min(e.ln+count-1, len(e.lines)-1)+1], "\n"), true)
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

func (e *Editor) wrapWidth() int {
	w := e.w
	if e.scrollable() {
		w--
	}
	return max(w, 1)
}

func (e *Editor) scrollable() bool {
	if e.h <= 0 {
		return false
	}
	if e.wrap == WrapNone {
		return len(e.lines) > e.h
	}
	rows := 0
	for i := range e.lines {
		rows += len(wrapRanges(e.lineClusters(i), max(e.w-1, 1), e.measure))
		if rows > e.h {
			return true
		}
	}
	return false
}

func (e *Editor) rowsOfLine(i int) int {
	if e.wrap == WrapNone {
		return 1
	}
	return len(wrapRanges(e.lineClusters(i), e.wrapWidth(), e.measure))
}

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

func (e *Editor) wrapPos(ln, col int) (row, x int) {
	cs := e.lineClusters(ln)
	rows := wrapRanges(cs, e.wrapWidth(), e.measure)
	for i, r := range rows {
		if col < r[0] {
			return i, 0
		}
		if col <= r[1] || i == len(rows)-1 {
			return i, cellsBefore(cs[r[0]:], min(col, r[1])-r[0], e.measure)
		}
	}
	return 0, 0
}

// inVisual reports whether (ln, col) is inside the visual highlight.
func (e *Editor) inVisual(ln, col int) bool {
	switch e.mode {
	case ModeVisual:
		lo, hiEx := e.visualRange()
		p := taPos{ln: ln, col: col}
		return posLE(lo, p.ln, p.col) && !posLE(hiEx, p.ln, p.col)
	case ModeVisualLine:
		lo, hi := e.visualLines()
		return ln >= lo && ln <= hi
	}
	return false
}

// Render paints the viewport with the visual-selection fill.
func (e *Editor) Render(s tui.Surface) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	w := e.wrapWidth()
	paintCluster := func(x, y int, cl string, ln, col int) {
		st := e.styles.Text
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
