package widget_test

import (
	"slices"
	"strconv"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestTextInputGetters(t *testing.T) {
	h, in, sh := focusedInput(t, widget.WithInitialValue("hello world"))
	h.inject(keyShift(tui.KeyLeft), keyShift(tui.KeyLeft))
	h.barrier(sh)
	var cur, start, end int
	var ok bool
	h.onLoop(func() { cur = in.CursorIndex(); start, end, ok = in.Selection() })
	if cur != 9 || !ok || start != 9 || end != 11 {
		t.Fatalf("cursor %d, selection [%d,%d) %v; want 9, [9,11) true", cur, start, end, ok)
	}

	h, in, sh = focusedInput(t)
	h.inject(typeString("abcdefghijklmnopqrstuvwxy")...)
	h.barrier(sh)
	var scroll int
	h.onLoop(func() { scroll = in.Scroll() })
	if scroll != 6 { // 25 clusters in 20 cells, the cursor pinned at the last cell
		t.Fatalf("Scroll() = %d; want 6", scroll)
	}

	p := widget.NewTextInput(widget.WithPlaceholder("type"), widget.WithMask('*'))
	if p.Placeholder() != "type" || !p.Masked() {
		t.Fatalf("Placeholder %q, Masked %v", p.Placeholder(), p.Masked())
	}
	if widget.NewTextInput().Masked() {
		t.Fatal("an unmasked input reports Masked")
	}
}

func TestTextAreaGetters(t *testing.T) {
	h, ta, sh := focusedArea(t, 10, 2)
	h.inject(typeString("one")...)
	h.inject(key(tui.KeyEnter))
	h.inject(typeString("two")...)
	h.inject(key(tui.KeyEnter))
	h.inject(typeString("three")...)
	h.inject(keyShift(tui.KeyLeft), keyShift(tui.KeyLeft))
	h.barrier(sh)
	var ln, col, fl, fc, tl, tc, top, left int
	var ok bool
	h.onLoop(func() {
		ln, col = ta.CursorPos()
		fl, fc, tl, tc, ok = ta.Selection()
		top, left = ta.Scroll()
	})
	if ln != 2 || col != 3 {
		t.Fatalf("CursorPos = %d,%d; want 2,3", ln, col)
	}
	if !ok || fl != 2 || fc != 3 || tl != 2 || tc != 5 {
		t.Fatalf("Selection = %d,%d..%d,%d %v; want 2,3..2,5 true", fl, fc, tl, tc, ok)
	}
	if top != 1 || left != 0 { // three lines in two rows: the first scrolled off
		t.Fatalf("Scroll = %d,%d; want 1,0", top, left)
	}
}

func TestSelectGetters(t *testing.T) {
	h, sel, sh := selectFixture(t, widget.WithOptions(selectItems("alpha", "beta")),
		widget.WithSelectPlaceholder[string]("pick"))
	var label string
	var open bool
	read := func() { h.onLoop(func() { label, open = sel.Label(), sel.Open() }) }
	read()
	if label != "pick" || open {
		t.Fatalf("Label %q, Open %v; want the placeholder, closed", label, open)
	}
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	read()
	if !open {
		t.Fatal("Open() is false with the popup up")
	}
	h.onLoop(func() { sel.SetSelectedIndex(1) })
	read()
	if label != "beta" {
		t.Fatalf("Label %q; want beta", label)
	}
}

func TestSetterMirrors(t *testing.T) {
	p := widget.NewProgressBar()
	p.SetProgress(0.25)
	if f, det := p.Progress(); f != 0.25 || !det {
		t.Fatalf("Progress = %v, %v", f, det)
	}

	tabs := widget.NewTabs(widget.WithTab("a", widget.NewText("")), widget.WithTab("b", widget.NewText("")))
	if got := tabs.Titles(); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("Titles = %q", got)
	}

	sb := widget.NewStatusBar()
	sb.SetLeft("l")
	sb.SetCenter("c")
	sb.SetRight("r")
	if sb.Left()+sb.Center()+sb.Right() != "lcr" {
		t.Fatalf("segments %q %q %q", sb.Left(), sb.Center(), sb.Right())
	}

	b := widget.NewBox(nil, widget.WithTitle("T"), widget.WithStatus("S"))
	if b.Title() != "T" || b.Status() != "S" || b.FocusWithin() {
		t.Fatalf("Box %q %q %v", b.Title(), b.Status(), b.FocusWithin())
	}

	m := widget.NewModal(widget.NewText(""), widget.WithModalTitle("Save?"))
	if m.Title() != "Save?" {
		t.Fatalf("Modal title %q", m.Title())
	}
	m.SetTitle("Quit?")
	if m.Title() != "Quit?" {
		t.Fatalf("Modal title %q after SetTitle", m.Title())
	}
}

func TestProgressIndeterminate(t *testing.T) {
	p := widget.NewProgressBar()
	sh := newShell(p)
	h := startApp(t, sh, 10, 1)
	h.onLoop(func() { p.SetIndeterminate() })
	var det bool
	h.onLoop(func() { _, det = p.Progress() })
	if det {
		t.Fatal("determinate after SetIndeterminate")
	}
}

func TestListScrollState(t *testing.T) {
	items := make([]string, 10)
	for i := range items {
		items[i] = strconv.Itoa(i)
	}
	h, l, sh := focusedList(t, widget.SliceSource(items), 10, 3)
	for range 5 {
		h.inject(key(tui.KeyDown))
	}
	h.barrier(sh)
	var off, view, content int
	h.onLoop(func() { off, view, content = l.ScrollState() })
	if off != 3 || view != 3 || content != 10 { // the cursor on row 5, the last of three shown
		t.Fatalf("ScrollState = %d,%d,%d; want 3,3,10", off, view, content)
	}
}

func TestButtonHovered(t *testing.T) {
	b := widget.NewButton("OK")
	sh := newShell(b)
	h := startApp(t, sh, 10, 1)
	hovered := func() (v bool) { h.onLoop(func() { v = b.Hovered() }); return v }
	h.inject(tui.MouseEvent{Kind: tui.MouseMotion, X: 1, Y: 0})
	h.waitFor("hovered", hovered)
	h.inject(tui.PointerLeaveEvent{})
	h.waitFor("un-hovered", func() bool { return !hovered() })
}

func TestToastsItems(t *testing.T) {
	ts := widget.NewToasts()
	sh := newShell(ts)
	h := startApp(t, sh, 30, 6)
	h.onLoop(func() { ts.Post(widget.Toast{Text: "saved"}) })
	var items []widget.Toast
	h.onLoop(func() { items = ts.Items() })
	if len(items) != 1 || items[0].Text != "saved" {
		t.Fatalf("Items = %+v", items)
	}
}
