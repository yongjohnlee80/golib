package widget_test

// Vim-key contract shared by the list-shaped widgets, and the rule that
// application chords (Ctrl/Alt/Super) are NOT widget input: they bubble
// so the host can bind pane motion, quit, and friends.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
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
