package widget_test

import (
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/pty"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// column is a vertical Flex of fixed-height children: a Text of n rows for an int, the component
// itself, one row high, otherwise.
func column(items ...any) *tui.Flex {
	f := tui.NewFlex(tui.Vertical)
	for i := 0; i < len(items); i++ {
		switch it := items[i].(type) {
		case int:
			f.AddSized(widget.NewText(""), it)
		case sized:
			f.AddSized(it.c, it.n)
		case tui.Component:
			f.AddSized(it, 1)
		}
	}
	return f
}

// sized is a column item n rows high.
type sized struct {
	c tui.Component
	n int
}

func scrollState(h *harness, v *widget.ScrollView) (off, view, content int) {
	h.onLoop(func() { off, view, content = v.ScrollState() })
	return
}

// Two 10-row viewports, the inner one at row 20 of the outer's
// child, the focused leaf at row 50 of the inner's. Tab to the leaf scrolls the inner one first
// (41: the leaf on its last row), then the outer from where that left it (20), so the leaf shows
// on the screen's last row; neither moved on the other's old geometry.
func TestNestedScrollViewsRevealTheFocusedLeafInnermostFirst(t *testing.T) {
	first := widget.NewButton("first")
	leaf := widget.NewButton("leaf")
	inner := widget.NewScrollView(column(50, leaf, 20))
	outer := widget.NewScrollView(column(first, 19, sized{inner, 10}, 20))
	h := startApp(t, outer, 30, 10)
	h.onLoop(func() { first.Context().RequestFocus() })
	h.settle()
	if off, _, _ := scrollState(h, outer); off != 0 {
		t.Fatalf("the outer view starts at %d, want 0", off)
	}

	h.inject(tab())
	h.waitFor("the leaf on the last row", func() bool { return rowOfLabel(h, "leaf") == 9 })
	if off, view, content := scrollState(h, inner); off != 41 || view != 10 || content != 71 {
		t.Fatalf("inner %d/%d/%d, want offset 41 of 71 in a 10-row view", off, view, content)
	}
	if off, view, content := scrollState(h, outer); off != 20 || view != 10 || content != 50 {
		t.Fatalf("outer %d/%d/%d, want offset 20 of 50 in a 10-row view", off, view, content)
	}
}

// Nine nested views each have to move to show the leaf: more than one frame's reveal steps. The
// rest is served on the frames after it, and every view ends up moved.
func TestADeepNestingIsRevealedOverMoreThanOneFrame(t *testing.T) {
	const depth = 9
	first := widget.NewButton("first")
	leaf := widget.NewButton("leaf")
	var c tui.Component = widget.NewScrollView(column(15, leaf))
	views := []*widget.ScrollView{c.(*widget.ScrollView)}
	for range depth - 1 {
		v := widget.NewScrollView(column(15, sized{c, 10}))
		views = append(views, v)
		c = v
	}
	root := column(first, sized{c, 10})
	h := startApp(t, root, 40, 11)
	h.onLoop(func() { first.Context().RequestFocus() })
	h.settle()
	h.inject(tab())
	h.waitFor("the leaf on screen", func() bool { return rowOfLabel(h, "leaf") > 0 })
	h.settle()
	for i, v := range views {
		if off, _, _ := scrollState(h, v); off == 0 {
			t.Errorf("view %d (0 innermost) never moved", i)
		}
	}
	if r := h.rowWith("leaf"); r < 1 || r > 10 {
		t.Fatalf("the leaf is on row %d, outside the views:\n%s", r, h.grid())
	}
}

// Once the focused leaf is shown, the request is over: the wheel scrolls it out again and nothing
// pulls it back. The page keys, which the focused button declines, scroll too.
func TestAWheelAfterARevealIsNotPulledBack(t *testing.T) {
	leaf := widget.NewButton("leaf")
	v := widget.NewScrollView(column(30, leaf, 10))
	h := startApp(t, v, 20, 6)
	h.onLoop(func() { leaf.Context().RequestFocus() })
	h.waitFor("the leaf revealed", func() bool { return rowOfLabel(h, "leaf") == 5 })
	if off, _, _ := scrollState(h, v); off != 25 {
		t.Fatalf("revealed at %d, want 25", off)
	}

	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelUp, X: 2, Y: 2})
	h.waitFor("the wheel scrolled", func() bool { off, _, _ := scrollState(h, v); return off == 22 })
	h.settle()
	if off, _, _ := scrollState(h, v); off != 22 {
		t.Fatalf("the wheel's scroll was pulled back to %d", off)
	}

	steps := []struct {
		k    tui.KeyEvent
		want int
	}{
		{key(tui.KeyHome), 0},
		{key(tui.KeyPageDown), 5},
		{key(tui.KeyEnd), 35},
		{key(tui.KeyPageUp), 30},
	}
	for _, s := range steps {
		h.inject(s.k)
		h.waitFor(fmt.Sprintf("offset %d", s.want), func() bool { off, _, _ := scrollState(h, v); return off == s.want })
	}
}

// Content that fits does not scroll: the wheel bubbles past it, and the indicator's column stays
// blank (its width reserved all the same).
func TestContentThatFitsDoesNotScroll(t *testing.T) {
	v := widget.NewScrollView(column(widget.NewText("only"), 2))
	sh := newShell(v)
	h := startApp(t, sh, 10, 6)
	h.settle()
	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 1})
	h.waitFor("the wheel bubbled", func() bool { return len(sh.bubbledMice()) == 1 })
	if off, view, content := scrollState(h, v); off != 0 || view != 6 || content != 3 {
		t.Fatalf("%d/%d/%d, want 0 of 3 in 6", off, view, content)
	}
}

// A Terminal in a ScrollView is laid out at one width, the view's less the indicator's column,
// whether its content overflows or not: its program is started at it and never resized as the view
// scrolls (a second measurement would resize the program, and leave a child's commit stale).
func TestATerminalInAScrollViewIsLaidOutAtOneWidth(t *testing.T) {
	var procs []*fakeProc
	var cmds []pty.Cmd
	term := widget.NewTerminal(widget.WithStarter(func(c pty.Cmd) (widget.TerminalProcess, error) {
		p := newFakeProc()
		procs, cmds = append(procs, p), append(cmds, c)
		return p, nil
	}))
	last := widget.NewButton("last")
	v := widget.NewScrollView(column(sized{term, 6}, 20, last))
	h := startApp(t, v, 30, 8)
	h.onLoop(func() {
		term.Context().RequestFocus()
		if err := term.Start(); err != nil {
			t.Errorf("Start: %v", err)
		}
	})
	h.settle()
	h.onLoop(func() { last.Context().RequestFocus() })
	h.waitFor("the last button revealed", func() bool { return rowOfLabel(h, "last") == 7 })
	h.settle()
	h.onLoop(func() {
		if len(cmds) != 1 || cmds[0].Rows != 6 || cmds[0].Cols != 29 {
			t.Errorf("started %+v, want one program at 6x29", cmds)
		}
		for _, p := range procs {
			p.mu.Lock()
			for _, s := range p.sizes {
				if s != [2]int{6, 29} {
					t.Errorf("resized to %v, want only 6x29", s)
				}
			}
			p.mu.Unlock()
		}
	})
}

// A ScrollView in a Modal keeps its offset when the dialog lays out again: the dialog sizes its
// card with an unbounded measure, which a ScrollView answers without storing anything. Its Layout
// reset the offset to 0 on that measure, so every relayout scrolled the dialog back to the top.
func TestAScrollViewInAModalKeepsItsOffset(t *testing.T) {
	leaf := widget.NewButton("leaf")
	sv := widget.NewScrollView(column(widget.NewButton("first"), 40, leaf))
	modal := widget.NewModal(sv)
	host := widget.NewOverlayHost(widget.NewText("base"))
	sh := newShell(host)
	h := startApp(t, sh, 40, 14)
	h.onLoop(func() {
		if err := modal.Open(host); err != nil {
			t.Fatalf("Open: %v", err)
		}
	})
	h.settle()
	h.onLoop(func() { leaf.Context().RequestFocus() })
	h.settle()
	off, _, _ := scrollState(h, sv)
	if off == 0 {
		t.Fatal("focusing the last row did not scroll the dialog's ScrollView")
	}
	h.onLoop(func() { sv.Context().RequestLayout() })
	h.settle()
	if again, _, _ := scrollState(h, sv); again != off {
		t.Errorf("after a relayout the offset is %d, want %d kept", again, off)
	}
}
