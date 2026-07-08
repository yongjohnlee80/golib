package widget_test

// Base embedding contract + Box chrome (ADR-0007 §5.2, §5.4).

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// contexter is the Base plumbing surface every widget promotes.
type contexter interface {
	Context() *tui.Context
	NodeID() tui.NodeID
}

// TestBaseInitPlumbing mounts every v1 widget and asserts the Base Init
// chain ran: non-nil Context, valid NodeID (ADR-0007 §5.2 — catches missed
// Base.Init chaining).
func TestBaseInitPlumbing(t *testing.T) {
	widgets := map[string]tui.Component{
		"TextInput":   widget.NewTextInput(),
		"TextArea":    widget.NewTextArea(),
		"Select":      widget.NewSelect[string](),
		"List":        widget.NewList(widget.WithItems([]string{"a"}, func(s string) string { return s })),
		"BufferView":  widget.NewBufferView(),
		"Tabs":        widget.NewTabs(widget.WithTab("t", widget.NewText("x"))),
		"Split":       widget.NewSplit(widget.Horizontal, widget.NewText("a"), widget.NewText("b")),
		"Float":       widget.NewFloat(widget.NewText("f")),
		"StatusBar":   widget.NewStatusBar(),
		"ProgressBar": widget.NewProgressBar(),
		"Text":        widget.NewText("hello"),
		"Box":         widget.NewBox(widget.NewText("c")),
	}
	for name, w := range widgets {
		t.Run(name, func(t *testing.T) {
			h := startApp(t, w, 40, 12)
			h.onLoop(func() {
				c := w.(contexter)
				if c.Context() == nil {
					t.Errorf("%s: nil Context after mount — Base.Init not chained", name)
				}
				if c.NodeID() == 0 {
					t.Errorf("%s: zero NodeID after mount", name)
				}
			})
		})
	}
}

// TestBoxChrome asserts §5.4: title and status render inside the border
// rows (outer height = child + 2), truncate with ellipsis, and the
// focused-style merge activates when a descendant gains focus.
func TestBoxChrome(t *testing.T) {
	input := widget.NewTextInput(widget.WithInitialValue("v"))
	box := widget.NewBox(input, widget.WithTitle("Query"), widget.WithStatus("F5 run"))
	h := startApp(t, box, 30, 5)
	h.settle()

	top := h.row(0)
	if !strings.Contains(top, "Query") {
		t.Fatalf("title not in the top border row: %q", top)
	}
	if !strings.HasPrefix(top, "┌") || !strings.HasSuffix(top, "┐") {
		t.Fatalf("top border corners missing: %q", top)
	}
	bottom := h.row(4)
	if !strings.Contains(bottom, "F5 run") {
		t.Fatalf("status not in the bottom border row: %q", bottom)
	}
	// The child (height 1) renders inside a 3-row-high box when parent
	// gives more: with tight 5 rows the box stretches, but title/status
	// still cost zero content rows — the value row is inside.
	if !strings.Contains(h.grid(), "v") {
		t.Fatalf("child content missing:\n%s", h.grid())
	}

	// Focused border merge: before focus, border resolves TokenBorder
	// (default theme: terminal default fg); after Tab focuses the input,
	// TokenBorderFocused (accent = ANSI 4).
	before := cellAttrs(h, 0, 0)
	h.inject(tab())
	h.waitFor("focus visual", func() bool { return cellAttrs(h, 0, 0) != before })
	after := cellAttrs(h, 0, 0)
	if after.FG.Kind != tui.CellColorANSI || after.FG.Index != 4 {
		t.Fatalf("focused border is not TokenBorderFocused (ANSI 4): %+v", after.FG)
	}
}

// TestBoxTitleTruncation narrows the box until the title truncates with an
// ellipsis.
func TestBoxTitleTruncation(t *testing.T) {
	box := widget.NewBox(widget.NewText("x"), widget.WithTitle("A very long panel title"))
	h := startApp(t, box, 12, 3)
	h.settle()
	if !strings.Contains(h.row(0), "…") {
		t.Fatalf("narrow title did not truncate with ellipsis: %q", h.row(0))
	}
}

// TestBoxOuterIsChildPlusFrame asserts the frame math: a Box around a
// height-1 widget reports child + 2 border rows when constraints are loose.
func TestBoxOuterIsChildPlusFrame(t *testing.T) {
	// Dock gives the bottom slot loose height: the Box answers 1+2 rows.
	box := widget.NewBox(widget.NewTextInput())
	dock := tui.NewDock()
	dock.Pin(tui.DockBottom, box)
	fill := widget.NewText("")
	dock.Add(fill)
	h := startApp(t, dock, 20, 8)
	h.settle()
	// Rows 5..7 are the box (┌ on row 5, value row 6, └ on row 7).
	if !strings.HasPrefix(h.row(5), "┌") {
		t.Fatalf("expected box top border on row 5:\n%s", h.grid())
	}
	if !strings.HasPrefix(h.row(7), "└") {
		t.Fatalf("expected box bottom border on row 7:\n%s", h.grid())
	}
}

// TestBoxFocusableChildless: WithFocusable(true) puts a child-less Box in
// the focus chain.
func TestBoxFocusableChildless(t *testing.T) {
	box := widget.NewBox(nil, widget.WithFocusable(true), widget.WithTitle("Pane"))
	h := startApp(t, box, 20, 4)
	h.inject(tab())
	h.waitFor("box focused", func() bool {
		a := cellAttrs(h, 0, 0)
		return a.FG.Kind == tui.CellColorANSI && a.FG.Index == 4
	})
}

// TestBoxConsumesNothing: keys bubble through a Box (contract: HandleEvent
// returns false).
func TestBoxConsumesNothing(t *testing.T) {
	box := widget.NewBox(nil, widget.WithFocusable(true))
	sh := newShell(box)
	h := startApp(t, sh, 20, 4)
	h.inject(tab()) // focus the box itself
	h.inject(key('x'), key(tui.KeyEnter))
	h.barrier(sh)
	keys := sh.bubbledKeys()
	if len(keys) < 2 {
		t.Fatalf("expected x and Enter to bubble through Box, got %v", keys)
	}
}

// TestBoxStyleOverrides: WithBorder and WithFocusedStyle replace the
// defaults.
func TestBoxStyleOverrides(t *testing.T) {
	box := widget.NewBox(widget.NewText("x"),
		widget.WithBorder(style.BorderDouble),
		widget.WithFocusedStyle(style.New().BorderForeground(style.TokenError)))
	h := startApp(t, box, 10, 3)
	h.settle()
	if !strings.HasPrefix(h.row(0), "╔") {
		t.Fatalf("WithBorder(BorderDouble) not honored: %q", h.row(0))
	}
}
