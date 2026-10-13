package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// Ctrl-o and Ctrl-i walk the editor's jumps: G and gg, {, }, %, a mark's jump; with a count.
func TestVimJumpsBackAndForth(t *testing.T) {
	runEdits(t, []editCase{
		{what: "G then Ctrl-o", text: "0\n1\n2\n3\n4", ln: 1, seq: "G<cto>", want: "0\n1\n2\n3\n4", cursor: true, wantLn: 1},
		{what: "Ctrl-o then Ctrl-i", text: "0\n1\n2\n3\n4", ln: 1, seq: "G<cto><cti>", want: "0\n1\n2\n3\n4", cursor: true, wantLn: 4},
		{what: "two jumps, 2 Ctrl-o", text: "0\n1\n2\n3\n4", ln: 1, seq: "Ggg2<cto>", want: "0\n1\n2\n3\n4", cursor: true, wantLn: 1},
		{what: "} is a jump", text: "a\n\nb", seq: "}<cto>", want: "a\n\nb", cursor: true},
		{what: "% is a jump", text: "(ab)", col: 1, seq: "l%<cto>", want: "(ab)", cursor: true, wantCol: 2},
		{what: "a mark's jump", text: "a\nb\nc", seq: "majj'a<cto>", want: "a\nb\nc", cursor: true, wantLn: 2},
		{what: "nothing back: no move", text: "a\nb", ln: 1, seq: "<cto>", want: "a\nb", cursor: true, wantLn: 1},
		{what: "an entry follows a line deleted above it", text: "0\n1\n2\n3\n4", ln: 3, seq: "ggdd<cto>", want: "1\n2\n3\n4", cursor: true, wantLn: 2},
		{what: "an entry on a deleted line stays at the removal", text: "0\n1\n2\n3", ln: 2, seq: "ggjjdd<cto>", want: "0\n1\n3", cursor: true, wantLn: 2},
	})
}

// With a host's list, a step to another document (or view) goes to the host, which owns it: the
// list is unchanged until the host commits. A step in the editor's own document is the editor's.
func TestVimJumpsWithAHostList(t *testing.T) {
	c, _ := coreWith(t, "a\nb\nc")
	list := widget.NewJumpList(0)
	list.Push(widget.JumpPos{Doc: "B", Line: 3})
	var asked []widget.JumpStep
	c.SetJumps(list, func(s widget.JumpStep) { asked = append(asked, s) })
	c.SetJumpDoc("A")
	c.SetLine(1, 0)
	vimSeq(c, "<cto>")
	if len(asked) != 1 || asked[0].To != (widget.JumpPos{Doc: "B", Line: 3}) {
		t.Fatalf("the host was asked %+v", asked)
	}
	if es, idx := list.Entries(); len(es) != 1 || idx != 1 {
		t.Fatalf("the list changed before the host committed: %+v at %d", es, idx)
	}
	if !list.Commit(asked[0]) {
		t.Fatal("the host's commit was refused")
	}
	if es, idx := list.Entries(); len(es) != 2 || es[1] != (widget.JumpPos{Doc: "A", Line: 1}) || idx != 0 {
		t.Fatalf("after the host's commit: %+v at %d, want A's place recorded and the index at B", es, idx)
	}

	// a jump in A is recorded with A's name; back to it is the editor's own
	vimSeq(c, "<cti>")
	c.SetLine(0, 0)
	vimSeq(c, "G")
	if es, _ := list.Entries(); es[len(es)-1] != (widget.JumpPos{Doc: "A", Line: 0}) {
		t.Fatalf("G recorded %+v", es[len(es)-1])
	}
	asked = nil
	vimSeq(c, "<cto>")
	if ln, _ := c.Line(); ln != 0 || len(asked) != 0 {
		t.Fatalf("a step in A: line %d, host asked %d times", ln, len(asked))
	}

	// another view of the same document is the host's too
	c.SetJumpView(func() string { return "rendered" })
	list.Push(widget.JumpPos{Doc: "A", Line: 2})
	vimSeq(c, "<cto>")
	if len(asked) != 1 || asked[0].To.View != "" {
		t.Fatalf("a step to A's source from its rendered view: host asked %+v", asked)
	}
}

// A viewer jumps (jumping isn't editing). In Insert, Ctrl-o is Insert's one Normal-mode command,
// not a jump; a second Ctrl-o is that command, a jump back, and Insert resumes there.
func TestVimJumpsOnAViewerAndInInsert(t *testing.T) {
	c, _ := coreWith(t, "a\nb\nc", widget.CoreReadOnly(true))
	vimSeq(c, "G<cto>")
	if ln, _ := c.Line(); ln != 0 {
		t.Fatalf("a viewer's G Ctrl-o: line %d", ln)
	}
	ins, _ := coreWith(t, "a\nb\nc")
	vimSeq(ins, "Gi<cto>")
	if ln, _ := ins.Line(); ln != 2 || !ins.InsertOneShot() {
		t.Fatalf("Insert's Ctrl-o: line %d, one-shot %v; want no jump, the one-shot", ln, ins.InsertOneShot())
	}
	vimSeq(ins, "<cto>")
	if ln, _ := ins.Line(); ln != 0 || ins.Mode() != widget.ModeInsert {
		t.Fatalf("Ctrl-o's command Ctrl-o: line %d, mode %v; want back at line 0, in Insert", ln, ins.Mode())
	}
}
