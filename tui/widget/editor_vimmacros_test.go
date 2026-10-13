package widget_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// q{a–z} records, q stops, @{a–z} plays, N@a N times, @@ the last played; each change a macro
// makes is its own undo step.
func TestVimMacrosRecordAndPlay(t *testing.T) {
	runEdits(t, []editCase{
		{what: "record, then @a", text: "a\nb\nc", seq: "qaA!<esc>jq@a", want: "a!\nb!\nc"},
		{what: "N@a", text: "a\nb\nc\nd", seq: "qaA!<esc>jq2@a", want: "a!\nb!\nc!\nd"},
		{what: "@@", text: "a\nb\nc\nd", seq: "qaA!<esc>jq@a@@", want: "a!\nb!\nc!\nd"},
		{what: "an empty register plays nothing", text: "a", seq: "@b", want: "a"},
		{what: "@@ before any play: nothing", text: "a", seq: "@@", want: "a"},
		{what: "u undoes one change of a macro", text: "a\nb\nc", seq: "qaA!<esc>jq2@au", want: "a!\nb!\nc"},
		{what: "the chord's tick plays back as text", text: "x\ny", seq: "qaAj<tick>k<esc>jq@a", want: "xjk\nyjk"},
		{what: "the chord itself plays back as Esc: x after @a is Normal's", text: "x\ny\nz", seq: "qaA!jkjq@ax", want: "x!\ny!\n"},
		{what: "a paste plays back", text: "x\ny", seq: "qaA<paste:p\\nq><esc>jq@a", want: "xp\nq\nyp\nq"},
		{what: ". inside a macro repeats its last change", text: "a\nb\nc\nd\ne", seq: "qaA!<esc>j.qj@a",
			want: "a!\nb!\nc!\nd!\ne"},
		{what: "a macro playing itself stops", text: "ab\ncd\nef", seq: "qaxj@aq@a", want: "b\nd\n"},
	})
}

// The register holds a macro's text, Vim's way (Esc as \x1b, a key with no character by its name),
// and the text is what counts: written over by a yank, @a plays the yanked text as keys. qA appends,
// to the recording or to the text.
func TestVimMacrosRegisterText(t *testing.T) {
	runRegs(t, []regCase{
		{what: "the macro's text", text: "a\nb", seq: "qaA!<esc>jq", regs: map[rune]string{'a': "A!\x1bj", '"': "A!\x1bj"}},
		{what: "a key with no character by its name", text: "ab", seq: "qai<left>x<esc>q", regs: map[rune]string{'a': "i<Left>x\x1b"}},
		{what: "qA onto a recording", text: "a\nb", seq: "qaA!<esc>qqAjq", regs: map[rune]string{'a': "A!\x1bj"}},
		{what: "qA onto a yanked text", text: "x\ny", seq: `"aylqAA!<esc>q`, regs: map[rune]string{'a': "xA!\x1b"}},
	})
	runEdits(t, []editCase{
		{what: `qa…q, "ay over it, @a plays the text`, text: "x\nA?\nz", seq: `qaA!<esc>qj0"ay$j@a`, want: "x!\nA?\nz?"},
		{what: `"ap puts the macro's text`, text: "", seq: `qaix<esc>q"ap`, want: "xix\x1b"},
	})
}

// On a viewer q and @ bubble to the host (a macro could change nothing there).
func TestVimMacrosOnAViewer(t *testing.T) {
	c, _ := coreWith(t, "a", widget.CoreReadOnly(true))
	if c.HandleKey(key('q')) || c.HandleKey(key('@')) {
		t.Fatal("a viewer consumed q or @")
	}
}

// A counted macro that plays itself fans out at every level: "a holding 2@a would play some 2^99
// times. One @ may play only so much (its macros and count inside it), and is stopped there, the
// keys still queued dropped. The cell has its own deadline, so a regression fails, not hangs.
func TestVimMacrosBoundedWork(t *testing.T) {
	c, _ := coreWith(t, "ab")
	c.SetNamedRegister('a', "2@a", false)
	done := make(chan struct{})
	go func() { vimSeq(c, "@a"); close(done) }()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal(`2@a in "a still playing after 20s: one @'s work is unbounded`)
	}
	if c.Value() != "ab" {
		t.Fatalf("the stopped macro left %q", c.Value())
	}

	// @a in "a stops at the depth, as before
	c.SetNamedRegister('a', "@a", false)
	vimSeq(c, "@a")

	// a long legitimate replay is not cut off: 3000 plays of a 4-key macro
	long, _ := coreWith(t, strings.Repeat("x\n", 3000)+"x")
	long.SetLine(0, 0)
	long.SetNamedRegister('b', "A!\x1bj", false)
	vimSeq(long, "3000@b")
	if lines := strings.Split(long.Value(), "\n"); lines[2999] != "x!" || lines[3000] != "x" {
		t.Fatalf("3000@b was cut short: line 2999 %q, line 3000 %q", lines[2999], lines[3000])
	}
}

// A stopped macro says so: MacroStoppedEvent, once, naming the register and ErrMacroTooLong. The
// next @ has a budget of its own.
func TestVimMacrosStoppedEvent(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 4)
	rec := record[widget.MacroStoppedEvent](h)
	h.onLoop(func() {
		ed.SetValue("ab")
		ed.SetLine(0, 0)
		ed.SetNamedRegister('a', "2@a", false)
		ed.SetNamedRegister('b', "A!\x1b", false)
	})
	h.inject(key('@'), key('a'))
	h.barrier(sh)
	evs := rec.events()
	if len(evs) != 1 || evs[0].Register != 'a' || !errors.Is(evs[0].Err, widget.ErrMacroTooLong) {
		t.Fatalf("events %+v, want one for a, ErrMacroTooLong", evs)
	}
	h.inject(key('@'), key('b'))
	h.barrier(sh)
	if v := edValue(h, ed); v != "ab!" || len(rec.events()) != 1 {
		t.Fatalf("the next @ after a stop: %q, events %d", v, len(rec.events()))
	}
}
