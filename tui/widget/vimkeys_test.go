package widget_test

// Vim-key contract shared by the list-shaped widgets, and the rule that
// application chords (Ctrl/Alt/Super) are NOT widget input: they bubble
// so the host can bind pane motion, quit, and friends.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestListHonorsVimMotions(t *testing.T) {
	l := widget.NewList(widget.WithItems([]string{"a", "b", "c", "d"}, func(s string) string { return s }))
	sh := newShell(l)
	h := startApp(t, sh, 30, 6)
	h.inject(tab())
	h.barrier(sh)

	at := func() int {
		var i int
		h.onLoop(func() { i, _ = l.Selected() })
		return i
	}
	h.inject(key('j'))
	h.barrier(sh)
	if got := at(); got != 1 {
		t.Fatalf("j: cursor = %d, want 1", got)
	}
	h.inject(key('j'), key('k'))
	h.barrier(sh)
	if got := at(); got != 1 {
		t.Fatalf("jk: cursor = %d, want 1", got)
	}
	h.inject(key('G'))
	h.barrier(sh)
	if got := at(); got != 3 {
		t.Fatalf("G: cursor = %d, want 3", got)
	}
	h.inject(key('g'))
	h.barrier(sh)
	if got := at(); got != 0 {
		t.Fatalf("g: cursor = %d, want 0", got)
	}
}

func TestListIgnoresApplicationChords(t *testing.T) {
	l := widget.NewList(widget.WithItems([]string{"a", "b", "c"}, func(s string) string { return s }))
	sh := newShell(l)
	h := startApp(t, sh, 30, 6)
	h.inject(tab())
	h.barrier(sh)

	h.inject(keyMod('j', tui.ModCtrl))
	h.barrier(sh)
	var idx int
	h.onLoop(func() { idx, _ = l.Selected() })
	if idx != 0 {
		t.Fatalf("Ctrl-j must bubble, not move the cursor: got %d", idx)
	}
}

// Ctrl-l/Ctrl-h are pane motion in vim-keyed hosts; the tree must not
// read them as its own expand/collapse letters.
func TestTreeIgnoresApplicationChords(t *testing.T) {
	root := widget.NewTreeNode("conns", "connections")
	h, _, sh := focusedTree(t, 40, 10, widget.WithRoots(root))
	reqs := record[widget.ExpandRequestEvent](h)

	h.inject(keyMod('l', tui.ModCtrl))
	h.barrier(sh)
	if got := reqs.count(); got != 0 {
		t.Fatalf("Ctrl-l must bubble, not expand: %d expand request(s)", got)
	}
	// The bare letter still expands.
	h.inject(key('l'))
	h.barrier(sh)
	if got := reqs.count(); got != 1 {
		t.Fatalf("bare l should expand: %d expand request(s)", got)
	}
}

// Hosts drive cursor and styling programmatically: focus that rests on a
// delegating wrapper is invisible to the widget, so the host supplies the
// focused/blurred styles itself.
func TestListAndTreeHostControls(t *testing.T) {
	l := widget.NewList(widget.WithItems([]string{"a", "b", "c"}, func(s string) string { return s }))
	sh := newShell(l)
	h := startApp(t, sh, 30, 6)
	h.inject(tab())
	h.barrier(sh)

	h.onLoop(func() {
		l.SetCursor(2)
		l.SetStyles(widget.ListStyles{CursorRow: style.New().Background(style.ANSI(8))})
	})
	h.barrier(sh)
	var idx, n int
	h.onLoop(func() {
		idx, _ = l.Selected()
		n = l.Len()
	})
	if idx != 2 || n != 3 {
		t.Fatalf("SetCursor/Len: cursor=%d len=%d, want 2/3", idx, n)
	}

	root := widget.NewTreeNode("ws", "workspace")
	root.SetChildren(0, []*widget.TreeNode{
		widget.NewTreeNode("a", "alpha", widget.WithLeaf()),
		widget.NewTreeNode("b", "beta", widget.WithLeaf()),
	})
	th, tr, tsh := focusedTree(t, 40, 10, widget.WithRoots(root))
	th.inject(key('l')) // expand the pre-assembled root
	th.barrier(tsh)

	var labels []string
	var cur int
	th.onLoop(func() {
		tr.SetStyles(widget.ListStyles{CursorRow: style.New().Background(style.ANSI(8))})
		for _, n := range tr.VisibleRows() {
			labels = append(labels, n.Label())
		}
		tr.SetCursor(2)
		cur = tr.Cursor()
	})
	th.barrier(tsh)
	if len(labels) != 3 || labels[0] != "workspace" || labels[2] != "beta" {
		t.Fatalf("VisibleRows/Label: %v", labels)
	}
	if cur != 2 {
		t.Fatalf("Tree.SetCursor: cursor = %d, want 2", cur)
	}
	if id := selectedID(th, tr); id != "b" {
		t.Fatalf("cursor should sit on beta, got %q", id)
	}
}
