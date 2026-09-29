package widget_test

// Tree selection: SelectionChangedEvent follows the NODE under the cursor,
// and a structural change publishes from the commit phase, never from the
// Layout or Render that caused it.

import (
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// layoutHook is a shell that runs a one-shot hook at the start of its Layout,
// before laying its child out: a stand-in for a delegate that changes the
// tree's structure while the frame is being laid out.
type layoutHook struct {
	*shell
	mu   sync.Mutex
	hook func()
}

func (l *layoutHook) Layout(c tui.Constraints) tui.Size {
	l.mu.Lock()
	fn := l.hook
	l.hook = nil
	l.mu.Unlock()
	if fn != nil {
		fn()
	}
	return l.shell.Layout(c)
}

func (l *layoutHook) arm(fn func()) {
	l.mu.Lock()
	l.hook = fn
	l.mu.Unlock()
}

func threeKids() (*widget.TreeNode, []*widget.TreeNode) {
	return widget.NewTreeNode("r", "root"), []*widget.TreeNode{
		widget.NewTreeNode("k1", "kid-one", widget.WithLeaf()),
		widget.NewTreeNode("k2", "kid-two", widget.WithLeaf()),
		widget.NewTreeNode("k3", "kid-three", widget.WithLeaf()),
	}
}

func lastLabel(r *recorder[widget.SelectionChangedEvent]) string {
	evs := r.events()
	if len(evs) == 0 {
		return ""
	}
	return evs[len(evs)-1].Label
}

// Each key that brings a different node under the cursor raises one event
// naming it; a key that leaves the same node there raises none.
func TestTreeSelectionEventFollowsTheNode(t *testing.T) {
	root, kids := threeKids()
	h, _, sh := focusedTree(t, 40, 10, widget.WithRoots(root))
	reqs := record[widget.ExpandRequestEvent](h)
	sel := record[widget.SelectionChangedEvent](h)
	h.inject(key('l'))
	h.barrier(sh)
	h.onLoop(func() { root.SetChildren(reqs.events()[0].Gen, kids) })
	h.barrier(sh)

	h.inject(key('j'))
	h.barrier(sh)
	if got := lastLabel(sel); got != "kid-one" {
		t.Fatalf("after j: last selection %q, want kid-one", got)
	}
	h.inject(key(tui.KeyEnd))
	h.barrier(sh)
	if got := lastLabel(sel); got != "kid-three" {
		t.Fatalf("after End: last selection %q, want kid-three", got)
	}
	n := len(sel.events())
	h.inject(key(tui.KeyEnd))
	h.barrier(sh)
	if got := len(sel.events()); got != n {
		t.Fatalf("End on the last row raised %d more events, want 0", got-n)
	}
	if ev := sel.events()[n-1]; ev.Index != 3 {
		t.Fatalf("kid-three reported at row %d, want 3", ev.Index)
	}
}

// A structural change that moves the cursor to another node (a Reset shrinks
// the tree under it) is reported once, after the frame commits.
func TestTreeStructuralChangeReportsTheNewSelectionOnce(t *testing.T) {
	root, kids := threeKids()
	h, _, sh := focusedTree(t, 40, 10, widget.WithRoots(root))
	reqs := record[widget.ExpandRequestEvent](h)
	h.inject(key('l'))
	h.barrier(sh)
	h.onLoop(func() { root.SetChildren(reqs.events()[0].Gen, kids) })
	h.barrier(sh)
	h.inject(key(tui.KeyEnd))
	h.barrier(sh)

	sel := record[widget.SelectionChangedEvent](h)
	h.onLoop(func() { root.Reset() })
	h.waitFor("the reconciled row reported", func() bool { return lastLabel(sel) == "root" })
	h.barrier(sh)
	if got := len(sel.events()); got != 1 {
		t.Fatalf("a Reset raised %d selection events, want 1", got)
	}
}

// The same change made DURING a Layout: the Tree defers the event to the
// commit phase, so it still arrives, once, and nothing panics on the way
// (AfterLayout may only be called from the registering node's own Layout).
func TestTreeStructuralChangeDuringLayoutPublishesFromCommit(t *testing.T) {
	root, kids := threeKids()
	tr := widget.NewTree(widget.WithRoots(root))
	lh := &layoutHook{shell: newShell(tr)}
	h := startApp(t, lh, 40, 10)
	h.inject(tab())
	h.barrier(lh.shell)
	reqs := record[widget.ExpandRequestEvent](h)
	h.inject(key('l'))
	h.barrier(lh.shell)
	h.onLoop(func() { root.SetChildren(reqs.events()[0].Gen, kids) })
	h.barrier(lh.shell)
	h.inject(key(tui.KeyEnd))
	h.barrier(lh.shell)

	sel := record[widget.SelectionChangedEvent](h)
	h.onLoop(func() {
		lh.arm(func() { root.Reset() })
		lh.shell.ctx.RequestLayout()
	})
	h.waitFor("the reconciled row reported", func() bool { return lastLabel(sel) == "root" })
	h.barrier(lh.shell)
	if got := len(sel.events()); got != 1 {
		t.Fatalf("a Reset during Layout raised %d selection events, want 1", got)
	}
}
