package widget_test

import (
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

func jp(line int) widget.JumpPos { return widget.JumpPos{Line: line} }

func lines(l *widget.JumpList) ([]int, int) {
	es, idx := l.Entries()
	var out []int
	for _, e := range es {
		out = append(out, e.Line)
	}
	return out, idx
}

// Vim's rules (mark.c): a jump appends where it left; the first step back from the end records the
// place now, so Forward returns to it; nothing past either end; an older duplicate of a line goes.
func TestJumpListVimRules(t *testing.T) {
	l := widget.NewJumpList(0)
	l.Push(jp(1))
	l.Push(jp(5))
	if _, ok := l.Forward(jp(9), 1); ok {
		t.Fatal("forward from the end went somewhere")
	}
	step, ok := l.Back(jp(9), 1)
	if !ok || step.To != jp(5) {
		t.Fatalf("back: %+v %v, want line 5", step.To, ok)
	}
	if got, idx := lines(l); !slices.Equal(got, []int{1, 5}) || idx != 2 {
		t.Fatalf("a proposed step changed the list: %v at %d", got, idx)
	}
	if !l.Commit(step) {
		t.Fatal("a fresh step was refused")
	}
	if got, idx := lines(l); !slices.Equal(got, []int{1, 5, 9}) || idx != 1 {
		t.Fatalf("after back: %v at %d, want [1 5 9] at 1", got, idx)
	}
	if step, ok := l.Forward(jp(5), 1); !ok || step.To != jp(9) || !l.Commit(step) {
		t.Fatalf("forward: %+v %v, want line 9", step.To, ok)
	}
	step, _ = l.Back(jp(9), 2)
	l.Commit(step)
	if _, ok := l.Back(jp(1), 1); ok {
		t.Fatal("back from the oldest went somewhere")
	}

	dup := widget.NewJumpList(0)
	dup.Push(jp(3))
	dup.Push(jp(7))
	dup.Push(jp(3))
	if step, _ := dup.Back(jp(9), 1); step.To != jp(3) {
		t.Fatalf("back over a duplicate: %+v, want line 3", step.To)
	}
	if step, _ := dup.Back(jp(9), 2); step.To != jp(7) {
		t.Fatalf("2 back over a duplicate: %+v, want line 7 (the older 3 dropped)", step.To)
	}
	if _, ok := dup.Back(jp(9), 3); ok {
		t.Fatal("3 back reached the dropped duplicate")
	}

	docs := widget.NewJumpList(0)
	docs.Push(widget.JumpPos{Doc: "a", Line: 1})
	docs.Push(widget.JumpPos{Doc: "b", Line: 1})
	if step, _ := docs.Back(widget.JumpPos{Doc: "b", Line: 4}, 2); step.To.Doc != "a" {
		t.Fatalf("the same line of another document is no duplicate: %+v", step.To)
	}

	small := widget.NewJumpList(3)
	for i := 1; i <= 4; i++ {
		small.Push(jp(i))
	}
	if got, _ := lines(small); !slices.Equal(got, []int{2, 3, 4}) {
		t.Fatalf("past the limit: %v, want the oldest gone", got)
	}
}

// A step is committed only while the list is as it was proposed from: a jump, or another commit,
// makes it stale. Drop removes the step's entry for a place gone for good.
func TestJumpListProposeCommitDrop(t *testing.T) {
	l := widget.NewJumpList(0)
	l.Push(jp(1))
	l.Push(jp(5))
	step, _ := l.Back(jp(9), 1)
	l.Push(jp(7))
	if l.Commit(step) {
		t.Fatal("a step from before a jump was committed")
	}
	step, _ = l.Back(jp(9), 1)
	other, _ := l.Back(jp(9), 2)
	if !l.Commit(step) || l.Commit(step) || l.Commit(other) {
		t.Fatal("a step committed twice, or one from before another's commit")
	}
	if widget.NewJumpList(0).Commit(step) {
		t.Fatal("another list took the step")
	}

	d := widget.NewJumpList(0)
	d.Push(jp(1))
	d.Push(jp(5))
	gone, _ := d.Back(jp(9), 1)
	if !d.Drop(gone) || d.Drop(gone) {
		t.Fatal("Drop refused a fresh step, or took a stale one")
	}
	if got, idx := lines(d); !slices.Equal(got, []int{1}) || idx != 1 {
		t.Fatalf("after Drop: %v at %d, want [1] past the end", got, idx)
	}
	if step, _ := d.Back(jp(9), 1); step.To != jp(1) {
		t.Fatalf("back after a drop: %+v, want line 1", step.To)
	}

	// a counted step over an entry that is gone: dropped, the same count lands on the next one back
	c := widget.NewJumpList(0)
	for _, ln := range []int{1, 2, 3} {
		c.Push(jp(ln))
	}
	gone2, _ := c.Back(jp(9), 2)
	if gone2.To != jp(2) || !c.Drop(gone2) {
		t.Fatalf("2 back: %+v", gone2.To)
	}
	if step, _ := c.Back(jp(9), 2); step.To != jp(1) {
		t.Fatalf("2 back after the drop: %+v, want line 1", step.To)
	}
}
