package widget_test

import (
	"testing"

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
		{what: "the chord itself plays back as Esc", text: "x\ny", seq: "qaA!jkjq@a", want: "x!\ny!"},
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
