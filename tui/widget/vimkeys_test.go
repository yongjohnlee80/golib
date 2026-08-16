package widget_test

// Vim-key contract shared by the list-shaped widgets, and the rule that
// application chords (Ctrl/Alt/Super) are NOT widget input: they bubble
// so the host can bind pane motion, quit, and friends.

import (
	"iter"
	"strings"
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

// Hosts drive the editor cursor for search / jump-to-line.
func TestEditorSetLineAndLines(t *testing.T) {
	e := widget.NewEditor()
	sh := newShell(e)
	h := startApp(t, sh, 40, 8)
	h.inject(tab())
	h.barrier(sh)

	h.onLoop(func() { e.SetValue("alpha\nbeta\ngamma") })
	h.barrier(sh)

	var lines []string
	var row, col int
	h.onLoop(func() {
		lines = e.Lines()
		e.SetLine(2, 3)
		row, col = e.Line()
	})
	h.barrier(sh)
	if len(lines) != 3 || lines[1] != "beta" {
		t.Fatalf("Lines: %v", lines)
	}
	if row != 2 || col != 3 {
		t.Fatalf("SetLine: got %d,%d want 2,3", row, col)
	}
	// Out-of-range targets clamp instead of panicking.
	h.onLoop(func() {
		e.SetLine(99, 99)
		row, col = e.Line()
	})
	h.barrier(sh)
	if row != 2 || col > 4 {
		t.Fatalf("SetLine clamp: got %d,%d", row, col)
	}
}

// A read-only Editor is a VIEWER: motions, visual selection, and yank
// work; nothing mutates the document.
func TestEditorReadOnlyViewer(t *testing.T) {
	e := widget.NewEditor()
	sh := newShell(e)
	h := startApp(t, sh, 40, 8)
	h.inject(tab())
	h.barrier(sh)

	const doc = "alpha\nbeta\ngamma"
	h.onLoop(func() {
		e.SetValue(doc)
		e.SetReadOnly(true)
	})
	h.barrier(sh)

	// Insert entry, typed text, delete, paste, undo — all refused.
	h.inject(key('i'), key('X'), key('x'), key('d'), key('d'), key('p'), key('u'))
	h.barrier(sh)
	var got string
	var mode widget.EditorMode
	h.onLoop(func() { got, mode = e.Value(), e.Mode() })
	if got != doc {
		t.Fatalf("read-only document changed: %q", got)
	}
	if mode != widget.ModeNormal {
		t.Fatalf("read-only editor entered mode %v", mode)
	}

	// Motions and visual yank still work.
	h.inject(key('j'), key('V'), key('y'))
	h.barrier(sh)
	var reg string
	var row int
	h.onLoop(func() {
		reg, _ = e.Register()
		row, _ = e.Line()
	})
	if row != 1 {
		t.Fatalf("j should move in read-only mode: row = %d", row)
	}
	if !strings.Contains(reg, "beta") {
		t.Fatalf("visual yank should work in read-only mode: register = %q", reg)
	}
	// Paste events cannot sneak text in either.
	h.inject(tui.PasteEvent{Text: "nope"})
	h.barrier(sh)
	h.onLoop(func() { got = e.Value() })
	if got != doc {
		t.Fatalf("paste mutated a read-only document: %q", got)
	}
}

// Flex columns share the remaining width EVENLY: a table of all-flex
// columns renders equal columns instead of the first one swallowing the
// row (autodb M6: a uuid id column took the whole results pane).
func TestTableFlexColumnsShareWidth(t *testing.T) {
	type row struct{ a, b, c string }
	cols := []widget.TableColumn[row]{
		{Title: "A", Cell: func(r row) string { return r.a }},
		{Title: "B", Cell: func(r row) string { return r.b }},
		{Title: "C", Cell: func(r row) string { return r.c }},
	}
	tbl := widget.NewTable(cols, widget.WithItems([]row{{
		a: strings.Repeat("x", 40), b: "short", c: "tiny",
	}}, func(r row) string { return r.a }))
	sh := newShell(tbl)
	h := startApp(t, sh, 62, 6)
	h.inject(tab())
	h.barrier(sh)

	// The header row shows each title at its column origin; with even
	// sharing the three origins are evenly spaced.
	line := h.row(0)
	ia := strings.Index(line, "A")
	ib := strings.Index(line, "B")
	ic := strings.Index(line, "C")
	if ia < 0 || ib < 0 || ic < 0 {
		t.Fatalf("headers missing: %q", line)
	}
	// Even sharing, up to the one-column division remainder that the
	// leftmost flex columns absorb.
	gap1, gap2 := ib-ia, ic-ib
	if d := gap1 - gap2; d < 0 || d > 1 {
		t.Fatalf("columns not evenly shared: origins %d/%d/%d (%q)", ia, ib, ic, line)
	}
	// A fixed column keeps its width; the rest still share.
	if gap1 < 8 {
		t.Fatalf("flex share below the minimum: %d", gap1)
	}
}

// Tree.Reload refreshes a subtree in place: an expanded node re-requests
// its children under a new generation, the cursor stays put, and a
// collapsed node simply drops what it cached.
func TestTreeReloadSubtree(t *testing.T) {
	root := widget.NewTreeNode("ws", "workspace")
	notes := widget.NewTreeNode("notes", "notes")
	root.SetChildren(0, []*widget.TreeNode{notes})
	h, tr, sh := focusedTree(t, 40, 10, widget.WithRoots(root))
	reqs := record[widget.ExpandRequestEvent](h)

	h.inject(key('l'), key('j'), key('l')) // expand ws, onto notes, expand it
	h.barrier(sh)
	if reqs.count() != 1 {
		t.Fatalf("expected one expand request, got %d", reqs.count())
	}
	ev, _ := reqs.last()
	h.onLoop(func() {
		ev.Node.SetChildren(ev.Gen, []*widget.TreeNode{
			widget.NewTreeNode("a", "a.sql", widget.WithLeaf()),
		})
	})
	h.barrier(sh)
	h.wantContains("a.sql")

	before := selectedID(h, tr)
	var ok bool
	h.onLoop(func() { ok = tr.Reload("notes") })
	h.barrier(sh)
	if !ok {
		t.Fatal("Reload reported no such node")
	}
	if reqs.count() != 2 {
		t.Fatalf("an expanded node should re-request: %d requests", reqs.count())
	}
	if after := selectedID(h, tr); after != before {
		t.Fatalf("Reload moved the cursor: %q → %q", before, after)
	}
	// The new generation differs, so the stale load cannot install.
	ev2, _ := reqs.last()
	if ev2.Gen == ev.Gen {
		t.Fatalf("Reload reused generation %d", ev.Gen)
	}
	h.onLoop(func() {
		ev.Node.SetChildren(ev.Gen, []*widget.TreeNode{
			widget.NewTreeNode("stale", "stale.sql", widget.WithLeaf()),
		})
	})
	h.barrier(sh)
	h.wantNotContains("stale.sql")
}

// lateContent builds its focusable child during Init — the common shape
// for content whose data arrives with the float. Focus seeding must
// still reach it: focusFirst at Show() time cannot focus a component
// that is not mounted yet, and treating that as "no focus stop" left
// the modal trapping every key but Esc.
type lateContent struct {
	widget.Base
	ctx  *tui.Context
	list *widget.List[string]
}

func (c *lateContent) Init(ctx *tui.Context) {
	c.Base.Init(ctx)
	c.ctx = ctx
	c.list = widget.NewList(widget.WithItems([]string{"one", "two"},
		func(s string) string { return s }))
	ctx.Mount(c.list)
}
func (c *lateContent) Layout(cs tui.Constraints) tui.Size {
	sz := c.ctx.LayoutChild(c.list, cs)
	c.ctx.PlaceChild(c.list, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
	return cs.Constrain(sz)
}
func (c *lateContent) Render(tui.Surface)            {}
func (c *lateContent) HandleEvent(ev tui.Event) bool { return false }
func (c *lateContent) Add(...tui.Component)          {}
func (c *lateContent) Remove(tui.Component)          {}
func (c *lateContent) Children() iter.Seq[tui.Component] {
	return func(yield func(tui.Component) bool) {
		if c.list != nil {
			yield(c.list)
		}
	}
}

func TestModalFloatSeedsFocusIntoLateMountedContent(t *testing.T) {
	body := &lateContent{}
	f := widget.NewFloat(body, widget.WithModal(true))
	host := widget.NewOverlayHost(widget.NewText("背景"))
	sh := newShell(host)
	h := startApp(t, sh, 40, 10)
	h.onLoop(func() {
		host.Attach(f)
		f.Show()
	})
	h.barrier(sh)
	h.settle()

	// The list must own focus, so its keys work: j moves the cursor.
	h.inject(key('j'))
	h.barrier(sh)
	var idx int
	var ok bool
	h.onLoop(func() { idx, ok = body.list.Selected() })
	if !ok || idx != 1 {
		t.Fatalf("modal focus never reached the late-mounted list: cursor=%d ok=%v", idx, ok)
	}
}
