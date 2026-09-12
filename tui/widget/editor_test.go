package widget_test

// Editor contract: modal state machine, counts, the
// double-key pending buffer, the escape chord's dispatch-order semantics,
// inclusive visual ranges, register linewise-ness, undo groups, and the
// bubble rule for unbound keys.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func focusedEditor(t *testing.T, w, h int, opts ...widget.EditorOption) (*harness, *widget.Editor, *shell) {
	t.Helper()
	ed := widget.NewEditor(opts...)
	sh := newShell(ed)
	hh := startApp(t, sh, w, h)
	hh.inject(tab())
	hh.barrier(sh)
	return hh, ed, sh
}

// edState reads editor state on the loop goroutine.
func edState(h *harness, ed *widget.Editor) (val string, mode widget.EditorMode, ln, col int) {
	h.onLoop(func() {
		val = ed.Value()
		mode = ed.Mode()
		ln, col = ed.Line()
	})
	return
}

func wantState(t *testing.T, h *harness, ed *widget.Editor, val string, mode widget.EditorMode, ln, col int) {
	t.Helper()
	gv, gm, gl, gc := edState(h, ed)
	if gv != val || gm != mode || gl != ln || gc != col {
		t.Fatalf("state = (%q, %v, %d,%d), want (%q, %v, %d,%d)", gv, gm, gl, gc, val, mode, ln, col)
	}
}

func TestEditorInsertAndChordEscape(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6)
	modes := record[widget.ModeChangedEvent](h)

	h.inject(key('i'))
	h.inject(typeString("hello")...)
	h.inject(typeString("jk")...) // chord: second rune dispatched before the tick
	h.barrier(sh)

	// "jk" must NOT be inserted; Insert→Normal steps the cursor one left.
	wantState(t, h, ed, "hello", widget.ModeNormal, 0, 4)
	if modes.count() != 2 { // →Insert, →Normal
		t.Fatalf("mode events = %d, want 2", modes.count())
	}
}

func TestEditorChordTimeoutCommitsRune(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6)
	h.inject(key('i'))
	h.inject(key('j')) // held pending
	h.barrier(sh)
	time.Sleep(450 * time.Millisecond) // let the addressed tick fire
	h.barrier(sh)
	val, mode, _, _ := edState(h, ed)
	if val != "j" || mode != widget.ModeInsert {
		t.Fatalf("state = (%q, %v), want (\"j\", Insert)", val, mode)
	}
}

func TestEditorChordSettledByOtherKeyAndPaste(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6)
	h.inject(key('i'))
	h.inject(key('j'), key('x'))         // j held, x settles: commit j then insert x
	h.inject(key('j'))                   // held again
	h.inject(tui.PasteEvent{Text: "PP"}) // paste settles the pending rune first
	h.barrier(sh)
	val, mode, _, _ := edState(h, ed)
	if val != "jxjPP" || mode != widget.ModeInsert {
		t.Fatalf("state = (%q, %v), want (\"jxjPP\", Insert)", val, mode)
	}
}

func TestEditorChordDisabled(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithEscapeChord(""))
	h.inject(key('i'))
	h.inject(typeString("jk")...)
	h.barrier(sh)
	val, mode, _, _ := edState(h, ed)
	if val != "jk" || mode != widget.ModeInsert {
		t.Fatalf("state = (%q, %v), want (\"jk\", Insert)", val, mode)
	}
}

func TestEditorEscapeChordValidation(t *testing.T) {
	for _, bad := range []string{"j", "jkl", "\x01k"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("WithEscapeChord(%q) did not panic", bad)
				}
			}()
			widget.NewEditor(widget.WithEscapeChord(bad))
		}()
	}
}

func TestEditorNormalMotionsAndCounts(t *testing.T) {
	h, ed, sh := focusedEditor(t, 40, 8,
		widget.WithInitialText("alpha beta gamma\nsecond line\n\nfourth"))

	step := func(keys string, wantLn, wantCol int) {
		t.Helper()
		h.inject(typeString(keys)...)
		h.barrier(sh)
		_, _, ln, col := edState(h, ed)
		if ln != wantLn || col != wantCol {
			t.Fatalf("after %q: cursor = (%d,%d), want (%d,%d)", keys, ln, col, wantLn, wantCol)
		}
	}

	step("l", 0, 1)    // right
	step("3l", 0, 4)   // count applies
	step("h", 0, 3)    // left
	step("w", 0, 6)    // word forward → "beta"
	step("e", 0, 9)    // word end → "beta"'s a
	step("b", 0, 6)    // word back
	step("$", 0, 15)   // line end (last grapheme)
	step("0", 0, 0)    // line start
	step("10l", 0, 10) // multi-digit count (0 extends the count)
	step("j", 1, 10)   // down, sticky column clamps to line length later
	step("G", 3, 0)    // bottom
	step("{", 2, 0)    // paragraph back → blank line
	step("}", 3, 0)    // paragraph forward
	step("[", 2, 0)    // v1 alias of {
	step("]", 3, 0)    // v1 alias of }
	h.inject(typeString("gg")...)
	h.barrier(sh)
	_, _, ln, col := edState(h, ed)
	if ln != 0 || col != 0 {
		t.Fatalf("gg: cursor = (%d,%d), want (0,0)", ln, col)
	}
}

func TestEditorXRegisterAndPaste(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithInitialText("abcd"))
	h.inject(typeString("2x")...) // delete "ab" into the register
	h.barrier(sh)
	wantState(t, h, ed, "cd", widget.ModeNormal, 0, 0)
	var reg string
	var lw bool
	h.onLoop(func() { reg, lw = ed.Register() })
	if reg != "ab" || lw {
		t.Fatalf("register = (%q, %v), want (\"ab\", charwise)", reg, lw)
	}
	h.inject(key('p')) // paste after cursor: c a b d, cursor on 'b'
	h.barrier(sh)
	wantState(t, h, ed, "cabd", widget.ModeNormal, 0, 2)
	h.inject(key('P')) // paste before cursor
	h.barrier(sh)
	wantState(t, h, ed, "caabbd", widget.ModeNormal, 0, 3)
}

func TestEditorLineDeleteYankPaste(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 8, widget.WithInitialText("one\ntwo\nthree"))

	h.inject(typeString("dd")...) // delete "one" linewise
	h.barrier(sh)
	wantState(t, h, ed, "two\nthree", widget.ModeNormal, 0, 0)
	var reg string
	var lw bool
	h.onLoop(func() { reg, lw = ed.Register() })
	if reg != "one" || !lw {
		t.Fatalf("register = (%q, %v), want (\"one\", linewise)", reg, lw)
	}

	h.inject(key('p')) // paste below current line
	h.barrier(sh)
	wantState(t, h, ed, "two\none\nthree", widget.ModeNormal, 1, 0)

	h.inject(typeString("yy")...) // yank "one"
	h.inject(key('P'))            // paste above
	h.barrier(sh)
	wantState(t, h, ed, "two\none\none\nthree", widget.ModeNormal, 1, 0)

	h.inject(typeString("2dd")...) // count on dd
	h.barrier(sh)
	wantState(t, h, ed, "two\nthree", widget.ModeNormal, 1, 0)
	h.onLoop(func() { reg, lw = ed.Register() })
	if reg != "one\none" || !lw {
		t.Fatalf("register = (%q, %v), want two lines linewise", reg, lw)
	}
}

func TestEditorDoubleKeyCancel(t *testing.T) {
	// `d` followed by anything but `d` cancels the pending
	// buffer and processes the key normally — `dw` deliberately moves.
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithInitialText("alpha beta"))
	h.inject(typeString("dw")...)
	h.barrier(sh)
	wantState(t, h, ed, "alpha beta", widget.ModeNormal, 0, 6)
}

func TestEditorDeleteToEnd(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithInitialText("alpha beta"))
	h.inject(typeString("wD")...) // cursor to "beta", delete to end
	h.barrier(sh)
	wantState(t, h, ed, "alpha ", widget.ModeNormal, 0, 5)
	var reg string
	h.onLoop(func() { reg, _ = ed.Register() })
	if reg != "beta" {
		t.Fatalf("register = %q, want \"beta\"", reg)
	}
}

func TestEditorVisualCharwiseInclusive(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithInitialText("abcdef"))
	h.inject(typeString("v2ly")...) // select a..c inclusive, yank
	h.barrier(sh)
	var reg string
	var lw bool
	h.onLoop(func() { reg, lw = ed.Register() })
	if reg != "abc" || lw {
		t.Fatalf("register = (%q, %v), want (\"abc\", charwise)", reg, lw)
	}
	_, mode, _, _ := edState(h, ed)
	if mode != widget.ModeNormal {
		t.Fatalf("mode after visual yank = %v, want Normal", mode)
	}

	h.inject(typeString("v2ld")...) // delete a..c inclusive
	h.barrier(sh)
	wantState(t, h, ed, "def", widget.ModeNormal, 0, 0)
}

func TestEditorVisualLine(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 8, widget.WithInitialText("one\ntwo\nthree"))
	h.inject(typeString("Vjd")...) // line-select one+two, delete
	h.barrier(sh)
	wantState(t, h, ed, "three", widget.ModeNormal, 0, 0)
	var reg string
	var lw bool
	h.onLoop(func() { reg, lw = ed.Register() })
	if reg != "one\ntwo" || !lw {
		t.Fatalf("register = (%q, %v), want linewise one+two", reg, lw)
	}
}

func TestEditorVisualEscape(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithInitialText("abc"))
	h.inject(typeString("vl")...)
	h.barrier(sh)
	if _, mode, _, _ := edState(h, ed); mode != widget.ModeVisual {
		t.Fatalf("mode = %v, want Visual", mode)
	}
	h.inject(key(tui.KeyEscape))
	h.barrier(sh)
	if _, mode, _, _ := edState(h, ed); mode != widget.ModeNormal {
		t.Fatalf("mode after Esc = %v, want Normal", mode)
	}
}

func TestEditorUndoGroups(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6)

	// One Insert session (entry → chord exit) is ONE undo group.
	h.inject(key('i'))
	h.inject(typeString("hello world")...)
	h.inject(typeString("jk")...)
	h.barrier(sh)
	h.inject(key('u'))
	h.barrier(sh)
	wantState(t, h, ed, "", widget.ModeNormal, 0, 0)

	// Redo restores the whole group.
	h.inject(keyMod('r', tui.ModCtrl))
	h.barrier(sh)
	val, _, _, _ := edState(h, ed)
	if val != "hello world" {
		t.Fatalf("redo: value = %q", val)
	}

	// Each Normal-mode edit is its own group.
	h.inject(typeString("0x")...) // delete 'h'
	h.inject(key('x'))            // delete 'e'
	h.barrier(sh)
	val, _, _, _ = edState(h, ed)
	if val != "llo world" {
		t.Fatalf("after xx: %q", val)
	}
	h.inject(key('u'))
	h.barrier(sh)
	val, _, _, _ = edState(h, ed)
	if val != "ello world" {
		t.Fatalf("undo one x: %q", val)
	}
}

func TestEditorSetValueIsDocumentBoundary(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithInitialText("seed"))
	h.inject(typeString("x")...) // build undo history
	h.inject(key('i'))
	h.barrier(sh)

	h.onLoop(func() { ed.SetRegister("keepme", false); ed.SetValue("fresh doc") })
	h.barrier(sh)
	wantState(t, h, ed, "fresh doc", widget.ModeNormal, 0, 0)

	// Undo history cleared; register preserved.
	h.inject(key('u'))
	h.barrier(sh)
	val, _, _, _ := edState(h, ed)
	if val != "fresh doc" {
		t.Fatalf("undo after SetValue mutated the doc: %q", val)
	}
	var reg string
	h.onLoop(func() { reg, _ = ed.Register() })
	if reg != "keepme" {
		t.Fatalf("register = %q, want preserved", reg)
	}
}

func TestEditorUnboundKeysBubble(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithInitialText("abc"))
	h.inject(key(' ')) // the leader key must reach the application
	h.inject(key('Q')) // unbound
	h.barrier(sh)
	bubbled := sh.bubbledKeys() // [0] is the initial focus Tab
	if len(bubbled) != 3 || bubbled[1].Code != ' ' || bubbled[2].Code != 'Q' {
		t.Fatalf("bubbled = %+v, want Tab, Space, Q", bubbled)
	}
	// A count pending when the unbound key arrives is cancelled, and the
	// key still bubbles.
	h.inject(typeString("3 ")...)
	h.barrier(sh)
	if got := len(sh.bubbledKeys()); got != 4 {
		t.Fatalf("bubbled after count+space = %d, want 4", got)
	}
	h.inject(typeString("l")...) // count was cancelled: moves 1, not 3
	h.barrier(sh)
	_, _, _, col := edState(h, ed)
	if col != 1 {
		t.Fatalf("col = %d, want 1 (count cancelled by unbound key)", col)
	}
}

func TestEditorInsertModeConsumesText(t *testing.T) {
	h, _, sh := focusedEditor(t, 30, 6)
	h.inject(key('i'))
	h.inject(key(' ')) // Space in Insert mode is text, not a leader
	h.barrier(sh)
	if got := len(sh.bubbledKeys()); got != 1 { // just the initial focus Tab
		t.Fatalf("Insert-mode Space bubbled (%d keys)", got)
	}
}

func TestEditorKeymapOverlayAndUnbind(t *testing.T) {
	// Rebind ; to line-end, unbind $.
	overlay := widget.Keymap{
		{Mode: widget.ModeNormal, Code: ';'}: widget.ActLineEnd,
		{Mode: widget.ModeNormal, Code: '$'}: widget.ActUnbound,
	}
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithInitialText("abcdef"), widget.WithKeymap(overlay))
	h.inject(key(';'))
	h.barrier(sh)
	_, _, _, col := edState(h, ed)
	if col != 5 {
		t.Fatalf("; → col %d, want 5", col)
	}
	h.inject(typeString("0$")...)
	h.barrier(sh)
	_, _, _, col = edState(h, ed)
	if col != 0 {
		t.Fatalf("$ unbound but moved to col %d", col)
	}
	if n := len(sh.bubbledKeys()); n != 2 { // focus Tab + the unbound $
		t.Fatalf("unbound $ did not bubble (%d)", n)
	}
}

func TestEditorKeymapValidationPanics(t *testing.T) {
	cases := []widget.Keymap{
		{{Mode: widget.ModeInsert, Code: 'x'}: widget.ActOpenBelow}, // Normal-only action in Insert
		{{Mode: widget.ModeNormal, Code: 'z'}: widget.Action(200)},  // unknown action
		{{Mode: widget.ModeVisual, Code: 'z'}: widget.ActInsert},    // Normal-only action in Visual
	}
	for i, km := range cases {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatalf("case %d: WithKeymap did not panic", i)
				}
			}()
			widget.NewEditor(widget.WithKeymap(km))
		}()
	}
}

func TestEditorOpenLinesAndAppend(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 8, widget.WithInitialText("top\nbottom"))
	h.inject(key('o'))
	h.inject(typeString("mid")...)
	h.inject(key(tui.KeyEscape))
	h.barrier(sh)
	wantState(t, h, ed, "top\nmid\nbottom", widget.ModeNormal, 1, 2)

	h.inject(typeString("ggA!")...)
	h.inject(key(tui.KeyEscape))
	h.barrier(sh)
	wantState(t, h, ed, "top!\nmid\nbottom", widget.ModeNormal, 0, 3)

	h.inject(typeString("O^")...)
	h.inject(key(tui.KeyEscape))
	h.barrier(sh)
	wantState(t, h, ed, "^\ntop!\nmid\nbottom", widget.ModeNormal, 0, 0)
}

func TestEditorDefaultKeymapIsACopy(t *testing.T) {
	a := widget.DefaultKeymap()
	b := widget.DefaultKeymap()
	kc := widget.KeyChord{Mode: widget.ModeNormal, Code: 'h'}
	a[kc] = widget.ActRight
	if b[kc] != widget.ActLeft {
		t.Fatal("DefaultKeymap shares state between calls")
	}
}

// --- implementation-review r1 regressions (2026-08-16) ---

// "jjk" commits the first j and treats the second as a fresh chord
// start — commit-then-process re-enters the chord state machine.
func TestEditorChordDoubledFirstRune(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6)
	h.inject(key('i'))
	h.inject(typeString("jjk")...)
	h.barrier(sh)
	val, mode, _, _ := edState(h, ed)
	if val != "j" || mode != widget.ModeNormal {
		t.Fatalf("state = (%q, %v), want (\"j\", Normal)", val, mode)
	}
}

// Insert-mode Tab inserts; word motions treat tabs as whitespace.
func TestEditorTabInsertAndWhitespaceMotions(t *testing.T) {
	h, ed, sh := focusedEditor(t, 40, 6)
	h.inject(key('i'))
	h.inject(typeString("ab")...)
	h.inject(key(tui.KeyTab))
	h.inject(typeString("cd")...)
	h.inject(key(tui.KeyEscape))
	h.barrier(sh)
	val, _, _, _ := edState(h, ed)
	if val != "ab\tcd" {
		t.Fatalf("value = %q, want tab inserted", val)
	}
	if got := len(sh.bubbledKeys()); got != 1 { // only the focus Tab bubbled
		t.Fatalf("Insert Tab bubbled (%d)", got)
	}
	// w from the start crosses the tab onto "cd".
	h.inject(typeString("0w")...)
	h.barrier(sh)
	_, _, _, col := edState(h, ed)
	if col != 3 {
		t.Fatalf("w over tab: col = %d, want 3", col)
	}
	// b goes back to the start of "ab".
	h.inject(key('b'))
	h.barrier(sh)
	if _, _, _, col = edState(h, ed); col != 0 {
		t.Fatalf("b over tab: col = %d, want 0", col)
	}
}

// The count cap is a hard ceiling; explicit counts drive gg and G.
func TestEditorCountCapAndGoToLine(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 8,
		widget.WithInitialText("l1\nl2\nl3\nl4\nl5"))

	h.inject(typeString("3G")...)
	h.barrier(sh)
	_, _, ln, _ := edState(h, ed)
	if ln != 2 {
		t.Fatalf("3G: line = %d, want 2", ln)
	}
	h.inject(typeString("2gg")...)
	h.barrier(sh)
	if _, _, ln, _ = edState(h, ed); ln != 1 {
		t.Fatalf("2gg: line = %d, want 1", ln)
	}
	h.inject(typeString("G")...)
	h.barrier(sh)
	if _, _, ln, _ = edState(h, ed); ln != 4 {
		t.Fatalf("G: line = %d, want 4 (bottom)", ln)
	}
	// The cap clamps: an absurd count followed by j must not overflow past
	// the last line (and must not exceed 1e6 internally).
	h.inject(typeString("99999999j")...)
	h.barrier(sh)
	if _, _, ln, _ = edState(h, ed); ln != 4 {
		t.Fatalf("capped count j: line = %d, want 4", ln)
	}
}

// A REBOUND prefix completes on its own chord, not a hard-coded rune.
func TestEditorReboundPrefixCompletesOnItsChord(t *testing.T) {
	overlay := widget.Keymap{
		{Mode: widget.ModeNormal, Code: 's'}: widget.ActDeletePrefix, // ss = dd
		{Mode: widget.ModeNormal, Code: 'd'}: widget.ActUnbound,
	}
	h, ed, sh := focusedEditor(t, 30, 8,
		widget.WithInitialText("one\ntwo"), widget.WithKeymap(overlay))
	h.inject(typeString("ss")...)
	h.barrier(sh)
	val, _, _, _ := edState(h, ed)
	if val != "two" {
		t.Fatalf("ss (rebound dd) → %q, want \"two\"", val)
	}
	// 'sd' must NOT complete (d is unbound and bubbles after cancel).
	h.inject(typeString("sd")...)
	h.barrier(sh)
	if val, _, _, _ = edState(h, ed); val != "two" {
		t.Fatalf("sd deleted: %q", val)
	}
}

// S3: paste in Visual replaces the selection.
func TestEditorVisualPasteReplacesSelection(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithInitialText("abcdef"))
	h.inject(typeString("v2l")...) // select abc
	h.inject(tui.PasteEvent{Text: "XY"})
	h.barrier(sh)
	val, mode, _, _ := edState(h, ed)
	if val != "XYdef" || mode != widget.ModeNormal {
		t.Fatalf("state = (%q, %v), want (\"XYdef\", Normal)", val, mode)
	}
}

// r2 residual: focus loss clears the pending double-key prefix and count.
func TestEditorFocusLossClearsPendingCommand(t *testing.T) {
	ed := widget.NewEditor(widget.WithInitialText("one\ntwo"))
	other := widget.NewTextInput()
	sp := widget.NewSplit(widget.Horizontal, ed, other)
	sh := newShell(sp)
	h := startApp(t, sh, 41, 6)
	h.inject(tab()) // focus the editor
	h.barrier(sh)

	h.inject(key('d')) // arm the prefix
	h.inject(tab())    // focus away (loss clears pending state)
	h.inject(shiftTab())
	h.inject(key('d')) // must ARM again, not complete a stale dd
	h.barrier(sh)
	var val string
	h.onLoop(func() { val = ed.Value() })
	if val != "one\ntwo" {
		t.Fatalf("stale dd completed across focus loss: %q", val)
	}
}

// Hosts drive the editor cursor for search / jump-to-line.
func TestEditorSetLineAndLines(t *testing.T) {
	e := widget.NewEditor()
	sh := newShell(e)
	h := startApp(t, sh, 40, 8)
	h.inject(tab())
	h.barrier(sh)

	h.onLoop(func() { e.SetValue("alpha\nbeta\ngamma") })
	h.barrier(sh)

	var lines []string
	var row, col int
	h.onLoop(func() {
		lines = e.Lines()
		e.SetLine(2, 3)
		row, col = e.Line()
	})
	h.barrier(sh)
	if len(lines) != 3 || lines[1] != "beta" {
		t.Fatalf("Lines: %v", lines)
	}
	if row != 2 || col != 3 {
		t.Fatalf("SetLine: got %d,%d want 2,3", row, col)
	}
	// Out-of-range targets clamp instead of panicking.
	h.onLoop(func() {
		e.SetLine(99, 99)
		row, col = e.Line()
	})
	h.barrier(sh)
	if row != 2 || col > 4 {
		t.Fatalf("SetLine clamp: got %d,%d", row, col)
	}
}

// A read-only Editor is a VIEWER: motions, visual selection, and yank
// work; nothing mutates the document.
func TestEditorReadOnlyViewer(t *testing.T) {
	e := widget.NewEditor()
	sh := newShell(e)
	h := startApp(t, sh, 40, 8)
	h.inject(tab())
	h.barrier(sh)

	const doc = "alpha\nbeta\ngamma"
	h.onLoop(func() {
		e.SetValue(doc)
		e.SetReadOnly(true)
	})
	h.barrier(sh)

	// Insert entry, typed text, delete, paste, undo — all refused.
	h.inject(key('i'), key('X'), key('x'), key('d'), key('d'), key('p'), key('u'))
	h.barrier(sh)
	var got string
	var mode widget.EditorMode
	h.onLoop(func() { got, mode = e.Value(), e.Mode() })
	if got != doc {
		t.Fatalf("read-only document changed: %q", got)
	}
	if mode != widget.ModeNormal {
		t.Fatalf("read-only editor entered mode %v", mode)
	}

	// Motions and visual yank still work.
	h.inject(key('j'), key('V'), key('y'))
	h.barrier(sh)
	var reg string
	var row int
	h.onLoop(func() {
		reg, _ = e.Register()
		row, _ = e.Line()
	})
	if row != 1 {
		t.Fatalf("j should move in read-only mode: row = %d", row)
	}
	if !strings.Contains(reg, "beta") {
		t.Fatalf("visual yank should work in read-only mode: register = %q", reg)
	}
	// Paste events cannot sneak text in either.
	h.inject(tui.PasteEvent{Text: "nope"})
	h.barrier(sh)
	h.onLoop(func() { got = e.Value() })
	if got != doc {
		t.Fatalf("paste mutated a read-only document: %q", got)
	}
}

// Esc is a vim no-op in Normal mode: the Editor must let it BUBBLE so a
// host can dismiss the float (or panel) the editor lives in. It still
// consumes Esc when there is something to cancel.
func TestEditorEscBubblesWhenIdle(t *testing.T) {
	e := widget.NewEditor()
	sh := newShell(e)
	h := startApp(t, sh, 40, 8)
	h.inject(tab())
	h.barrier(sh)
	h.onLoop(func() { e.SetValue("alpha\nbeta") })
	h.barrier(sh)

	esc := func() bool {
		var consumed bool
		h.onLoop(func() {
			consumed = e.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
		})
		h.barrier(sh)
		return consumed
	}

	if esc() {
		t.Fatal("idle Normal-mode Esc must bubble, not be consumed")
	}
	// Visual mode: Esc leaves the selection and IS consumed.
	h.inject(key('v'))
	h.barrier(sh)
	if !esc() {
		t.Fatal("Esc in Visual mode must be consumed (it exits Visual)")
	}
	var mode widget.EditorMode
	h.onLoop(func() { mode = e.Mode() })
	if mode != widget.ModeNormal {
		t.Fatalf("Esc should leave Visual: mode = %v", mode)
	}
	if esc() {
		t.Fatal("Esc must keep bubbling once Visual is left")
	}
}

// visual `y` DELIVERS THE EXACT BYTES, multi-byte selection included.
func TestEditorYank_VisualYankDeliversExactUTF8(t *testing.T) {
	h, ed, _ := focusedEditor(t, 40, 6)
	rec := record[widget.YankEvent](h)

	insertLines(h, "日本語テキスト")
	h.inject(key('0'), key('v'), key('$'), key('y'))
	h.settle()

	got := string(h.tb.Clipboard())
	if got != "日本語テキスト" {
		t.Errorf("clipboard = %q, want %q", got, "日本語テキスト")
	}

	evs := rec.events()
	if len(evs) != 1 {
		t.Fatalf("expected exactly one YankEvent, got %d: %+v", len(evs), evs)
	}
	if !evs[0].ClipboardDelivered {
		t.Error("the event reports the copy as undelivered although the backend took it")
	}
	var owner tui.NodeID
	h.onLoop(func() { owner = ed.NodeID() })
	if evs[0].Owner != owner {
		t.Errorf("YankEvent.Owner = %v, want the editor's node %v", evs[0].Owner, owner)
	}
}

// `yy` and `2yy` DELIVER, and the second is what a count-prefixed yank looks
// like — the same call site, so the count must not skip the export.
func TestEditorYank_LinewiseYankDelivers(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []tui.Event
		want string
	}{
		{"yy", []tui.Event{key('y'), key('y')}, "alpha"},
		{"2yy", []tui.Event{key('2'), key('y'), key('y')}, "alpha\nbeta"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, _ := focusedEditor(t, 40, 6)
			rec := record[widget.YankEvent](h)
			insertLines(h, "alpha", "beta", "gamma")
			h.inject(key('g'), key('g'))
			h.inject(tc.keys...)
			h.settle()

			if got := string(h.tb.Clipboard()); got != tc.want {
				t.Errorf("clipboard = %q, want %q", got, tc.want)
			}
			if rec.count() != 1 {
				t.Errorf("expected one YankEvent, got %d", rec.count())
			}
		})
	}
}

// DELETES DO NOT REACH THE SYSTEM CLIPBOARD.
func TestEditorYank_DeletesNeverTouchTheSystemClipboard(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []tui.Event
	}{
		{"x", []tui.Event{key('x')}},
		{"D", []tui.Event{keyShift('d')}},
		{"dd", []tui.Event{key('d'), key('d')}},
		{"2dd", []tui.Event{key('2'), key('d'), key('d')}},
		{"visual d", []tui.Event{key('v'), key('$'), key('d')}},
		{"visual x", []tui.Event{key('v'), key('$'), key('x')}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, ed, _ := focusedEditor(t, 40, 6)
			rec := record[widget.YankEvent](h)
			insertLines(h, "secret-value", "second")
			h.inject(key('g'), key('g'), key('0'))
			h.inject(tc.keys...)
			h.settle()

			if got := h.tb.Clipboard(); len(got) != 0 {
				t.Errorf("%s exported %q to the system clipboard", tc.name, string(got))
			}
			if rec.count() != 0 {
				t.Errorf("%s published a YankEvent (%d), so a parent would report a copy "+
					"that nobody asked for", tc.name, rec.count())
			}
			h.inject(key('p'))
			h.settle()
			val, _, _, _ := edState(h, ed)
			if val == "" {
				t.Errorf("%s left the register empty; the internal yank was broken, not just "+
					"the export", tc.name)
			}
		})
	}
}

type noClipboard struct{ tui.Backend }

func TestEditorYank_UnsupportedBackendReportsUndelivered(t *testing.T) {
	ed := widget.NewEditor()
	sh := newShell(ed)
	tb := tui.NewTestBackend(40, 6)
	h := startAppOpts(t, sh, 40, 6, tui.WithBackend(noClipboard{tb}))
	inject := func(evs ...tui.Event) {
		for _, ev := range evs {
			if err := tb.Inject(ev); err != nil {
				t.Fatal(err)
			}
		}
		h.settle()
	}
	inject(tab())

	rec := record[widget.YankEvent](h)
	inject(key('i'))
	inject(typeString("plain")...)
	inject(key(tui.KeyEscape))
	inject(key('y'), key('y'))

	evs := rec.events()
	if len(evs) != 1 {
		t.Fatalf("expected one YankEvent even without clipboard support, got %d", len(evs))
	}
	if evs[0].ClipboardDelivered {
		t.Error("the event claims delivery on a backend that cannot copy")
	}
	inject(key('p'))
	val, _, _, _ := edState(h, ed)
	if val != "plain\nplain" {
		t.Errorf("the internal yank stopped working without clipboard support: %q", val)
	}
}

func TestEditorYank_EventCarriesNoSecretText(t *testing.T) {
	h, _, _ := focusedEditor(t, 40, 6)
	rec := record[widget.YankEvent](h)
	insertLines(h, "adb_pat_supersecret.value")
	h.inject(key('y'), key('y'))
	h.settle()

	evs := rec.events()
	if len(evs) != 1 {
		t.Fatalf("expected one YankEvent, got %d", len(evs))
	}
	if fmtEvent := fmt.Sprintf("%#v", evs[0]); strings.Contains(fmtEvent, "supersecret") {
		t.Errorf("the YankEvent carries the yanked text (%s) -- an event this shape reaches "+
			"logs and debug dumps", fmtEvent)
	}
}

func insertLines(h *harness, lines ...string) {
	h.inject(key('i'))
	for i, l := range lines {
		if i > 0 {
			h.inject(key(tui.KeyEnter))
		}
		h.inject(typeString(l)...)
	}
	h.inject(key(tui.KeyEscape))
	h.settle()
}

func TestEditorPressDiscardsPendingOperator(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6)
	h.onLoop(func() { ed.SetValue("alpha\nbravo\ncharlie") })
	h.barrier(sh)

	before, _, _, _ := edState(h, ed)

	h.inject(key('2'), key('d'), click(2, 1), key('d')) // click: row 1, column 2
	h.barrier(sh)

	val, mode, ln, col := edState(h, ed)
	if val != before {
		t.Errorf("a press completed the pending operator: text changed\n before: %q\n after:  %q",
			before, val)
	}
	if mode != widget.ModeNormal {
		t.Errorf("mode = %v, want Normal", mode)
	}
	if ln != 1 || col != 2 {
		t.Errorf("caret = (%d,%d), want (1,2)", ln, col)
	}
}

func TestEditorPressSettlesPendingChordRune(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6)
	h.inject(key('i'))
	h.inject(typeString("abc")...)
	h.barrier(sh)

	h.inject(key('j'), click(0, 0))
	h.barrier(sh)

	val, mode, _, _ := edState(h, ed)
	if !strings.Contains(val, "abcj") {
		t.Errorf("the pending chord rune was LOST: value = %q, want it settled as \"abcj\" "+
			"at the caret where it was typed (ADR-0008)", val)
	}
	if mode != widget.ModeInsert {
		t.Errorf("mode = %v, want Insert retained across the click", mode)
	}
}

func TestEditorPressClosesUndoGroup(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6)
	h.inject(key('i'))
	h.inject(typeString("first")...)
	h.barrier(sh)

	h.inject(click(0, 0)) // boundary
	h.barrier(sh)
	h.inject(typeString("X")...)
	h.barrier(sh)

	withBoth, _, _, _ := edState(h, ed)
	h.inject(key(tui.KeyEscape), key('u')) // one undo
	h.barrier(sh)
	afterUndo, _, _, _ := edState(h, ed)

	if afterUndo == withBoth {
		t.Fatalf("undo did nothing (value %q)", withBoth)
	}
	if !strings.Contains(afterUndo, "first") {
		t.Errorf("one undo removed text from BEFORE the click too: %q — the press must "+
			"close the group so the two edits undo separately", afterUndo)
	}
}

func TestEditorPressExitsVisual(t *testing.T) {
	for _, tc := range []struct {
		name  string
		enter rune
	}{{"visual", 'v'}, {"visual-line", 'V'}} {
		t.Run(tc.name, func(t *testing.T) {
			h, ed, sh := focusedEditor(t, 30, 6)
			h.onLoop(func() { ed.SetValue("alpha\nbravo") })
			h.barrier(sh)

			h.inject(key(tc.enter), key('l')) // select something
			h.barrier(sh)

			h.inject(click(1, 1))
			h.barrier(sh)

			_, mode, _, _ := edState(h, ed)
			if mode != widget.ModeNormal {
				t.Errorf("mode = %v after a press, want Normal (the press must exit %s)",
					mode, tc.name)
			}
			before, _, _, _ := edState(h, ed)
			h.inject(key('l'), key('d'))
			h.barrier(sh)
			if after, _, _, _ := edState(h, ed); after != before {
				t.Errorf("a stale visual anchor survived the press: `d` after the click "+
					"deleted a selection\n before: %q\n after:  %q", before, after)
			}
		})
	}
}

func TestEditorPressWideGraphemeAndScroll(t *testing.T) {
	h, ed, sh := focusedEditor(t, 20, 3) // 3 visible rows
	h.onLoop(func() { ed.SetValue("a漢b\nsecond\nthird\nfourth\nfifth") })
	h.barrier(sh)

	h.inject(click(2, 0))
	h.barrier(sh)
	if _, _, ln, col := edState(h, ed); ln != 0 || col != 1 {
		t.Errorf("click on the trailing half of a wide grapheme -> (%d,%d), want (0,1)", ln, col)
	}

	h.inject(
		tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 1, Y: 1},
		tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 1, Y: 1},
	)
	h.barrier(sh)
	h.inject(click(0, 0))
	h.barrier(sh)
	if _, _, ln, _ := edState(h, ed); ln != 2 {
		t.Errorf("after scrolling 2 lines, a click on the top row -> line %d, want 2", ln)
	}
}

func TestEditorPressClamps(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 8)
	h.onLoop(func() { ed.SetValue("ab\nlonger line") })
	h.barrier(sh)

	h.inject(click(25, 0)) // far past the end of "ab"
	h.barrier(sh)
	if _, _, ln, col := edState(h, ed); ln != 0 || col != 1 {
		t.Errorf("click past EOL -> (%d,%d), want (0,1) — the last column of \"ab\"", ln, col)
	}

	h.inject(click(0, 6)) // below the last line
	h.barrier(sh)
	if _, _, ln, _ := edState(h, ed); ln != 1 {
		t.Errorf("click below the last line -> line %d, want 1 (the last buffer line)", ln)
	}
}

func TestEditorWheelScrollsWithoutMovingCaret(t *testing.T) {
	h, ed, sh := focusedEditor(t, 20, 3)
	h.onLoop(func() { ed.SetValue("one\ntwo\nthree\nfour\nfive") })
	h.barrier(sh)

	_, _, ln0, col0 := edState(h, ed)

	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 1, Y: 1})
	h.barrier(sh)

	_, _, ln1, col1 := edState(h, ed)
	if ln1 != ln0 || col1 != col0 {
		t.Errorf("the wheel moved the caret (%d,%d) -> (%d,%d); it must only scroll",
			ln0, col0, ln1, col1)
	}

	h.inject(click(0, 0))
	h.barrier(sh)
	if _, _, ln, _ := edState(h, ed); ln == 0 {
		t.Error("the wheel did not scroll: the top row is still line 0")
	}
}

func TestEditorWrapSoftClickOnLaterRow(t *testing.T) {
	h, ed, sh := focusedEditor(t, 6, 4, widget.WithEditorWrap(widget.WrapSoft))
	h.onLoop(func() { ed.SetValue("abcdefghij\nzzz\nyyy\nxxx\nwww") })
	h.barrier(sh)

	h.inject(click(2, 1)) // third cell of the SECOND visual row -> 'h' (col 7)
	h.barrier(sh)
	if _, _, ln, col := edState(h, ed); ln != 0 || col != 7 {
		t.Errorf("click on wrapped row 1 -> (%d,%d), want (0,7)", ln, col)
	}
}

func TestEditorClickAtHorizontalScroll(t *testing.T) {
	h, ed, sh := focusedEditor(t, 8, 3)
	h.onLoop(func() { ed.SetValue("0123456789abcdefghij") })
	h.barrier(sh)

	h.inject(key('$'))
	h.barrier(sh)
	_, _, _, endCol := edState(h, ed)
	if endCol != 19 {
		t.Fatalf("precondition: `$` -> col %d, want 19", endCol)
	}

	wantCol := endCol - (8 - 1)
	h.inject(click(0, 0))
	h.barrier(sh)
	if _, _, _, col := edState(h, ed); col != wantCol {
		t.Errorf("click at x=0 -> column %d, want exactly %d (left + 0)", col, wantCol)
	}
}

func TestEditorClickOnIndicatorColumnIsInert(t *testing.T) {
	h, ed, sh := focusedEditor(t, 8, 3)
	h.onLoop(func() { ed.SetValue("aaa\nbbb\nccc\nddd\neee\nfff") })
	h.barrier(sh)

	h.inject(click(1, 1))
	h.barrier(sh)
	_, _, ln0, col0 := edState(h, ed)

	h.inject(click(7, 2))
	h.barrier(sh)
	if _, _, ln, col := edState(h, ed); ln != ln0 || col != col0 {
		t.Errorf("a press on the indicator column moved the caret (%d,%d) -> (%d,%d); "+
			"that column is not text", ln0, col0, ln, col)
	}
}

func TestEditorWheelStepsAndClamps(t *testing.T) {
	for _, wrap := range []struct {
		name string
		mode widget.WrapMode
	}{{"wrapnone", widget.WrapNone}, {"wrapsoft", widget.WrapSoft}} {
		t.Run(wrap.name, func(t *testing.T) {
			h, ed, sh := focusedEditor(t, 8, 3, widget.WithEditorWrap(wrap.mode))
			h.onLoop(func() { ed.SetValue("l0\nl1\nl2\nl3\nl4\nl5") })
			h.barrier(sh)

			topLine := func() int {
				h.inject(click(0, 0))
				h.barrier(sh)
				_, _, ln, _ := edState(h, ed)
				return ln
			}
			if got := topLine(); got != 0 {
				t.Fatalf("precondition: top line = %d, want 0", got)
			}

			h.inject(wheel(false), wheel(false))
			h.barrier(sh)
			if got := topLine(); got != 2 {
				t.Errorf("2 wheel events -> top line %d, want 2 (one step per event)", got)
			}

			for i := 0; i < 20; i++ {
				h.inject(wheel(false))
			}
			h.barrier(sh)
			bottom := topLine()
			h.inject(wheel(false))
			h.barrier(sh)
			if got := topLine(); got != bottom {
				t.Errorf("wheel past the end kept scrolling: %d -> %d", bottom, got)
			}

			for i := 0; i < 40; i++ {
				h.inject(wheel(true))
			}
			h.barrier(sh)
			if got := topLine(); got != 0 {
				t.Errorf("wheel up did not clamp at the first line: top = %d", got)
			}
		})
	}
}

func wheel(up bool) tui.MouseEvent {
	b := tui.WheelDown
	if up {
		b = tui.WheelUp
	}
	return tui.MouseEvent{Kind: tui.MouseWheel, Button: b, X: 1, Y: 1}
}

func TestEditorAltKeysBubbleRatherThanActingAsMotions(t *testing.T) {
	h, ed, sh := focusedEditor(t, 20, 4)
	h.onLoop(func() { ed.SetValue("abcdef") })
	h.barrier(sh)

	h.inject(key('$'))
	h.barrier(sh)
	_, _, _, before := edState(h, ed)
	if before == 0 {
		t.Fatal("precondition: caret did not move to end-of-line")
	}

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 'h', Mods: tui.ModAlt})
	h.barrier(sh)
	if _, _, _, col := edState(h, ed); col != before {
		t.Errorf("Alt+h moved the caret %d -> %d: an Alt chord was consumed as the plain "+
			"`h` motion, so the host can never bind Alt", before, col)
	}

	h.inject(key('h'))
	h.barrier(sh)
	if _, _, _, col := edState(h, ed); col != before-1 {
		t.Errorf("plain h no longer moves left (%d -> %d); the Alt guard is too broad",
			before, col)
	}
}

func TestEditorWrapSoftClickStaysOnItsRow(t *testing.T) {
	h, ed, sh := focusedEditor(t, 6, 4, widget.WithEditorWrap(widget.WrapSoft))
	h.onLoop(func() { ed.SetValue("a bbbbb") })
	h.barrier(sh)

	h.inject(click(4, 0))
	h.barrier(sh)

	_, _, ln, col := edState(h, ed)
	if ln != 0 || col != 0 {
		t.Errorf("click in row 0's blank tail -> (%d,%d); want (0,0), the last column "+
			"painted on that row.", ln, col)
	}
}

func TestEditorNanoKeymap(t *testing.T) {
	h, ed, sh := focusedEditor(t, 40, 10,
		widget.WithNanoKeymap(),
		widget.WithInitialText("first line\nsecond line"),
	)

	// Nano starts in insert mode immediately (modeless)
	if mode := ed.Mode(); mode != widget.ModeInsert {
		t.Fatalf("expected initial mode ModeInsert for Nano, got %v", mode)
	}

	// Directly typing text does not require 'i'
	h.inject(typeString("hello ")...)
	h.barrier(sh)

	val, _, _, _ := edState(h, ed)
	if !strings.HasPrefix(val, "hello first line") {
		t.Fatalf("after direct typing in Nano: got %q", val)
	}

	// Ctrl+K cuts line
	h.inject(keyMod('k', tui.ModCtrl))
	h.barrier(sh)

	val, _, _, _ = edState(h, ed)
	if val != "second line" {
		t.Fatalf("after Ctrl+K: got %q, want \"second line\"", val)
	}

	// Ctrl+U uncuts / pastes line
	h.inject(keyMod('u', tui.ModCtrl))
	h.barrier(sh)

	val, _, _, _ = edState(h, ed)
	if !strings.Contains(val, "hello first line") {
		t.Fatalf("after Ctrl+U: got %q", val)
	}
}

func TestEditorStandardKeymap(t *testing.T) {
	h, ed, sh := focusedEditor(t, 40, 10,
		widget.WithStandardKeymap(),
		widget.WithInitialText("sample text"),
	)

	if mode := ed.Mode(); mode != widget.ModeInsert {
		t.Fatalf("expected initial mode ModeInsert for Standard, got %v", mode)
	}

	// Ctrl+A selects all text
	h.inject(keyMod('a', tui.ModCtrl))
	h.barrier(sh)

	// Ctrl+C copies selection
	h.inject(keyMod('c', tui.ModCtrl))
	h.barrier(sh)

	reg, _ := ed.Register()
	if reg != "sample text" {
		t.Fatalf("after Ctrl+C: register has %q, want \"sample text\"", reg)
	}

	// Re-select all for cut
	h.inject(keyMod('a', tui.ModCtrl))
	h.barrier(sh)

	// Ctrl+X cuts selection
	h.inject(keyMod('x', tui.ModCtrl))
	h.barrier(sh)

	val, _, _, _ := edState(h, ed)
	if val != "" {
		t.Fatalf("after Ctrl+X: got %q, want empty", val)
	}

	// Ctrl+V pastes selection
	h.inject(keyMod('v', tui.ModCtrl))
	h.barrier(sh)

	val, _, _, _ = edState(h, ed)
	if val != "sample text" {
		t.Fatalf("after Ctrl+V: got %q, want \"sample text\"", val)
	}
}

func TestEditorVimKeysetEscapeChord(t *testing.T) {
	// Confirm that 'jk' functions as escape for Vim keyset by default
	h, ed, sh := focusedEditor(t, 40, 10, widget.WithVimKeymap(), widget.WithInitialText("start"))

	if chord := ed.EscapeChord(); chord != "jk" {
		t.Fatalf("ed.EscapeChord() = %q, want \"jk\"", chord)
	}

	// Enter insert mode and type text
	h.inject(key('A'))
	h.inject(typeString(" more text")...)
	h.barrier(sh)

	if mode := ed.Mode(); mode != widget.ModeInsert {
		t.Fatalf("expected ModeInsert, got %v", mode)
	}

	// Type fast escape chord 'jk'
	h.inject(key('j'), key('k'))
	h.barrier(sh)

	// Mode must return to Normal and "jk" must NOT be in the buffer
	wantState(t, h, ed, "start more text", widget.ModeNormal, 0, 14)
}

func TestEditorRuntimeKeymapReflection(t *testing.T) {
	ed := widget.NewEditor(widget.WithVimKeymap())

	if ed.Keyset() != widget.KeysetVim {
		t.Errorf("ed.Keyset() = %v, want KeysetVim", ed.Keyset())
	}
	if ed.EscapeChord() != "jk" {
		t.Errorf("ed.EscapeChord() = %q, want \"jk\"", ed.EscapeChord())
	}

	snap := ed.SnapshotKeymap()
	if snap.KeysetName != "Vim" || !snap.Modal || snap.EscapeChord != "jk" {
		t.Errorf("unexpected snapshot metadata: %+v", snap)
	}
	if len(snap.Bindings) == 0 {
		t.Fatal("expected bindings in snapshot")
	}

	// All discrete bindings in the snapshot must have valid non-zero chords
	for _, b := range snap.Bindings {
		if b.Chord.Code == 0 {
			t.Errorf("snap.Bindings contains zero-valued chord: %+v", b)
		}
	}

	// ActionForChord lookup
	act, ok := ed.ActionForChord(widget.KeyChord{Mode: widget.ModeNormal, Code: 'j'})
	if !ok || act != widget.ActDown {
		t.Errorf("ActionForChord(Normal, 'j') = (%v, %v), want (ActDown, true)", act, ok)
	}

	// ChordsForAction reverse lookup
	chords := ed.ChordsForAction(widget.ActDown)
	foundJ := false
	for _, c := range chords {
		if c.Mode == widget.ModeNormal && c.Code == 'j' {
			foundJ = true
			break
		}
	}
	if !foundJ {
		t.Errorf("ChordsForAction(ActDown) = %+v, expected to include Normal 'j'", chords)
	}

	// Keymap defensive copy
	km1 := ed.Keymap()
	km1[widget.KeyChord{Mode: widget.ModeNormal, Code: 'j'}] = widget.ActUp
	km2 := ed.Keymap()
	if km2[widget.KeyChord{Mode: widget.ModeNormal, Code: 'j'}] != widget.ActDown {
		t.Errorf("mutating Keymap copy affected internal keymap")
	}
}

func TestEditorMousePressModelessVisualExit(t *testing.T) {
	ed := widget.NewEditor(widget.WithStandardKeymap(), widget.WithInitialText("hello\nworld"))
	_ = ed.Layout(tui.Tight(tui.Size{W: 20, H: 10}))

	// Select all with Ctrl+A -> ModeVisual
	ed.HandleEvent(tui.KeyEvent{Code: 'a', Mods: tui.ModCtrl})
	if ed.Mode() != widget.ModeVisual {
		t.Fatalf("expected ModeVisual after Ctrl+A, got %v", ed.Mode())
	}

	// Mouse press at (1, 1) -> must exit visual mode and return to ModeInsert (modeless)
	ed.HandleEvent(tui.MouseEvent{
		Kind:   tui.MousePress,
		Button: tui.MouseLeft,
		X:      1,
		Y:      1,
	})

	if ed.Mode() != widget.ModeInsert {
		t.Errorf("expected ModeInsert after mouse press in modeless editor, got %v", ed.Mode())
	}
	row, col := ed.Line()
	if row != 1 || col != 1 {
		t.Errorf("expected cursor at (1, 1), got (%d, %d)", row, col)
	}
}
