package widget_test

// Editor contract (ADR-0008 §2.1): modal state machine, counts, the
// double-key pending buffer, the escape chord's dispatch-order semantics,
// inclusive visual ranges, register linewise-ness, undo groups, and the
// bubble rule for unbound keys.

import (
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
	h.inject(key('j'), key('x')) // j held, x settles: commit j then insert x
	h.inject(key('j'))           // held again
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

	step("l", 0, 1)     // right
	step("3l", 0, 4)    // count applies
	step("h", 0, 3)     // left
	step("w", 0, 6)     // word forward → "beta"
	step("e", 0, 9)     // word end → "beta"'s a
	step("b", 0, 6)     // word back
	step("$", 0, 15)    // line end (last grapheme)
	step("0", 0, 0)     // line start
	step("10l", 0, 10)  // multi-digit count (0 extends the count)
	step("j", 1, 10)    // down, sticky column clamps to line length later
	step("G", 3, 0)     // bottom
	step("{", 2, 0)     // paragraph back → blank line
	step("}", 3, 0)     // paragraph forward
	step("[", 2, 0)     // v1 alias of {
	step("]", 3, 0)     // v1 alias of }
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
	// ADR-0008 §2.1: `d` followed by anything but `d` cancels the pending
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
		{{Mode: widget.ModeInsert, Code: 'x'}: widget.ActLeft},         // no Insert bindings
		{{Mode: widget.ModeNormal, Code: 'z'}: widget.Action(200)},     // unknown action
		{{Mode: widget.ModeVisual, Code: 'z'}: widget.ActInsert},       // Normal-only action in Visual
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
