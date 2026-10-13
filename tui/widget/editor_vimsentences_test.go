package widget_test

import "testing"

// ( and ), as Vim's findsent: every expectation is headless Neovim's on the same text and keys.
func TestVimSentenceMotions(t *testing.T) {
	runEdits(t, []editCase{
		{what: ")", text: "one. two. three", seq: ")", want: "one. two. three", cursor: true, wantCol: 5},
		{what: "$(", text: "one. two. three", seq: "$(", want: "one. two. three", cursor: true, wantCol: 10},
		{what: ") past the last: the line's last character", text: "one.  two", seq: "))D", want: "one.  tw"},
		{what: "an empty line is a sentence", text: "a\n\nb. c", seq: ")))D", want: "a\n\nb. "},
		{what: "a blank-only line counts as empty (golib's { } rule)", text: "a\n  \nb. c", seq: ")))D", want: "a\n  \nb. "},
		{what: ") stops on a blank-only line (Vim, for a line of nothing, would pass it)", text: "a\n  \nb", seq: ")", want: "a\n  \nb", cursor: true, wantLn: 1},
		{what: "! and a closing )", text: "Hi! (Yes.) No", seq: ")", want: "Hi! (Yes.) No", cursor: true, wantCol: 4},
		{what: "twice", text: "Hi! (Yes.) No", seq: "))", want: "Hi! (Yes.) No", cursor: true, wantCol: 11},
		{what: "( from inside a sentence", text: "Hi! (Yes.) No", col: 12, seq: "(", want: "Hi! (Yes.) No", cursor: true, wantCol: 11},
		{what: "2(", text: "Hi! (Yes.) No", col: 12, seq: "2(", want: "Hi! (Yes.) No", cursor: true, wantCol: 4},
		{what: "a closing quote", text: "He said \"go.\" Then", seq: ")", want: "He said \"go.\" Then", cursor: true, wantCol: 14},
		{what: "3)", text: "a. b. c. d.", seq: "3)", want: "a. b. c. d.", cursor: true, wantCol: 9},
		{what: "( at a start: the previous", text: "a. b. c. d.", col: 9, seq: "(", want: "a. b. c. d.", cursor: true, wantCol: 6},
		{what: "( on a sentence's dot", text: "a. b. c. d.", col: 7, seq: "(", want: "a. b. c. d.", cursor: true, wantCol: 6},
		{what: "across a line", text: "one\ntwo. three", seq: ")", want: "one\ntwo. three", cursor: true, wantLn: 1, wantCol: 5},
		{what: "( to an empty line", text: "one\n\ntwo", ln: 2, seq: "(", want: "one\n\ntwo", cursor: true, wantLn: 1},
		{what: "(( to the text's start", text: "one\n\ntwo", ln: 2, seq: "((", want: "one\n\ntwo", cursor: true},
		{what: "a dot inside a word ends nothing", text: "x.y z. w", seq: ")", want: "x.y z. w", cursor: true, wantCol: 7},
		{what: "d)", text: "a. b. c.", col: 3, seq: "d)", want: "a. c.", cursor: true, wantCol: 3},
		{what: "d(", text: "a. b. c.", col: 3, seq: "d(", want: "b. c."},
		{what: "c)", text: "a. b. c.", col: 3, seq: "c)X<esc>", want: "a. Xc."},
		{what: "y) then P", text: "a. b. c.", seq: "y)P", want: "a. a. b. c."},
		{what: "v) is inclusive", text: "a. b. c.", seq: "v)d", want: ". c."},
		{what: ") is a jump: Ctrl-o goes back", text: "a. b. c.", seq: "2)<cto>", want: "a. b. c.", cursor: true},
		{what: "( is a jump: two of them, Ctrl-o back one", text: "a. b. c.", col: 6, seq: "((<cto>", want: "a. b. c.", cursor: true, wantCol: 3},
	})
}

// d) within a line goes into "1, as Vim's use_reg_one has it for ( and ).
func TestVimSentenceDeleteRegister(t *testing.T) {
	c, _ := coreWith(t, "a. b. c.")
	vimSeq(c, "d)")
	if one, _, _ := c.NamedRegister('1'); one != "a. " {
		t.Fatalf(`d) left "1 = %q`, one)
	}
}

// is and as, as Vim's current_sent: Neovim's results again.
func TestVimSentenceObjects(t *testing.T) {
	const t3 = "One two. Three four. Five six."
	runEdits(t, []editCase{
		{what: "dis", text: t3, col: 10, seq: "dis", want: "One two.  Five six."},
		{what: "das", text: t3, col: 10, seq: "das", want: "One two. Five six."},
		{what: "das on the first", text: t3, seq: "das", want: "Three four. Five six."},
		{what: "das on the last takes the blank before", text: t3, col: 25, seq: "das", want: "One two. Three four."},
		{what: "dis on the blank between", text: t3, col: 8, seq: "dis", want: "One two.Three four. Five six."},
		{what: "das on the blank between", text: t3, col: 8, seq: "das", want: "One two. Five six."},
		{what: "d2is", text: t3, col: 10, seq: "d2is", want: "One two. Five six."},
		{what: "d2as", text: t3, col: 10, seq: "d2as", want: "One two."},
		{what: "cis", text: t3, col: 10, seq: "cisX<esc>", want: "One two. X Five six."},
		{what: "vis", text: t3, col: 10, seq: "visd", want: "One two.  Five six."},
		{what: "vas", text: t3, col: 10, seq: "vasd", want: "One two. Five six."},
		{what: "vis then is extends", text: t3, col: 10, seq: "visisd", want: "One two. Five six."},
		{what: "vas as extends", text: "a. b. c.", col: 3, seq: "vasasd", want: "a. "},
		{what: "vis o then extends back", text: "a. b. c.", col: 3, seq: "visod", want: "a.  c."},
		{what: "das keeps the line break", text: "a. b.\n\nc. d.", col: 3, seq: "das", want: "a.\n\nc. d."},
		{what: "das at a line's end", text: "a. b.\nc. d.", col: 3, seq: "das", want: "a.\nc. d."},
		{what: "yis then P", text: "a. b. c.", col: 3, seq: "yisP", want: "a. b.b. c."},
		{what: "gUis", text: "a. b. c.", col: 3, seq: "gUis", want: "a. B. c."},
		{what: "dis on a dot", text: "a. b. c.", col: 2, seq: "dis", want: "a.b. c."},
		{what: "das on a blank", text: "a. b. c.", col: 2, seq: "das", want: "a. c."},
		{what: "das on two blanks", text: "a.  b. c.", col: 2, seq: "das", want: "a. c."},
	})
}
