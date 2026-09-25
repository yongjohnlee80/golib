package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// visible_test.go holds Context.SetVisible — Qt's Item.visible — to what it
// promises: a hidden node takes no space, is not painted, is not a tab stop,
// hides its subtree, and comes back as it was.

func TestAHiddenNodeTakesNoSpaceNoPaintAndNoFocus(t *testing.T) {
	top := widget.NewText("TOP")
	field := widget.NewTextInput(widget.WithInitialValue("FIELD"))
	bottom := widget.NewText("BOTTOM")
	col := tui.NewFlex(tui.Vertical)
	col.Add(top)
	col.Add(field)
	col.Add(bottom)
	h := startApp(t, col, 20, 5)
	defer h.stop()
	h.onLoop(func() { field.Context().RequestFocus() })
	h.settle()
	if h.rowWith("BOTTOM") != 2 {
		t.Fatalf("precondition: BOTTOM on row %d, want 2:\n%s", h.rowWith("BOTTOM"), h.grid())
	}

	h.onLoop(func() { field.SetVisible(false) })
	h.settle()
	if strings.Contains(h.grid(), "FIELD") {
		t.Errorf("a hidden field is still painted:\n%s", h.grid())
	}
	if y := h.rowWith("BOTTOM"); y != 1 {
		t.Errorf("BOTTOM on row %d, want 1 — the hidden field still takes space:\n%s", y, h.grid())
	}
	var focused, visible bool
	h.onLoop(func() { focused, visible = field.Context().Focused(), field.Visible() })
	if focused {
		t.Error("a hidden field kept the keyboard")
	}
	if visible {
		t.Error("Visible() is true after SetVisible(false)")
	}

	h.onLoop(func() { field.SetVisible(true) })
	h.settle()
	if h.rowWith("FIELD") != 1 || h.rowWith("BOTTOM") != 2 {
		t.Errorf("shown again, the layout did not come back:\n%s", h.grid())
	}
}

func TestHidingAContainerHidesEverythingUnderIt(t *testing.T) {
	inner := tui.NewFlex(tui.Vertical)
	field := widget.NewTextInput(widget.WithInitialValue("INNER"))
	inner.Add(field)
	col := tui.NewFlex(tui.Vertical)
	col.Add(inner)
	col.Add(widget.NewText("AFTER"))
	h := startApp(t, col, 20, 5)
	defer h.stop()
	h.onLoop(func() { field.Context().RequestFocus() })
	h.settle()
	h.onLoop(func() { inner.SetVisible(false) })
	h.settle()
	var focused bool
	h.onLoop(func() { focused = field.Context().Focused() })
	if strings.Contains(h.grid(), "INNER") || focused {
		t.Errorf("a hidden container's child is painted (%v) or focused (%v):\n%s",
			strings.Contains(h.grid(), "INNER"), focused, h.grid())
	}
	if h.rowWith("AFTER") != 0 {
		t.Errorf("AFTER on row %d, want 0:\n%s", h.rowWith("AFTER"), h.grid())
	}
}

// A container that gives a child a rect whatever its size — a Split pane —
// still does not paint or focus a hidden one.
func TestAHiddenSplitPaneIsNeitherPaintedNorFocused(t *testing.T) {
	left := widget.NewTextInput(widget.WithInitialValue("LEFTPANE"))
	right := widget.NewText("RIGHT")
	split := widget.NewSplit(widget.Horizontal, left, right)
	h := startApp(t, split, 40, 3)
	defer h.stop()
	h.onLoop(func() { left.Context().RequestFocus() })
	h.settle()
	h.onLoop(func() { left.SetVisible(false) })
	h.settle()
	var focused bool
	h.onLoop(func() { focused = left.Context().Focused() })
	if strings.Contains(h.grid(), "LEFTPANE") || focused {
		t.Errorf("a hidden pane is painted (%v) or focused (%v):\n%s",
			strings.Contains(h.grid(), "LEFTPANE"), focused, h.grid())
	}
}

// TestAHiddenSplitPaneGivesItsSpaceToTheOther: no blank area and no divider —
// the other pane takes the whole split — and showing it again restores the
// division it had.
func TestAHiddenSplitPaneGivesItsSpaceToTheOther(t *testing.T) {
	left := widget.NewText("LEFT")
	right := widget.NewText("RIGHT")
	split := widget.NewSplit(widget.Horizontal, left, right)
	h := startApp(t, split, 40, 3)
	defer h.stop()
	h.settle()
	col := func(label string) int { return strings.Index(h.row(0), label) }
	before := col("RIGHT")
	if before < 10 {
		t.Fatalf("precondition: RIGHT at column %d:\n%s", before, h.grid())
	}
	h.onLoop(func() { left.SetVisible(false) })
	h.settle()
	if c := col("RIGHT"); c != 0 {
		t.Errorf("with LEFT hidden, RIGHT starts at column %d, want 0 — the pane kept its space:\n%s", c, h.grid())
	}
	h.onLoop(func() { left.SetVisible(true); right.SetVisible(false) })
	h.settle()
	if strings.Contains(h.grid(), "RIGHT") || strings.ContainsAny(h.row(0), "│") {
		t.Errorf("with RIGHT hidden, it or the divider is still painted:\n%s", h.grid())
	}
	h.onLoop(func() { right.SetVisible(true) })
	h.settle()
	if c := col("RIGHT"); c != before {
		t.Errorf("shown again, RIGHT at column %d, want %d as before:\n%s", c, before, h.grid())
	}
}
