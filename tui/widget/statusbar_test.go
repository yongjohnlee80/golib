package widget_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestStatusBarSegments(t *testing.T) {
	bar := widget.NewStatusBar()
	h := startApp(t, bar, 40, 1)
	h.onLoop(func() {
		bar.SetLeft("LEFT")
		bar.SetCenter("CENTER")
		bar.SetRight("RIGHT", style.New().Bold(true))
	})
	h.settle()
	row := h.row(0)
	if !strings.HasPrefix(row, "LEFT") || !strings.HasSuffix(row, "RIGHT") {
		t.Fatalf("segment placement: %q", row)
	}
	if got := strings.Index(row, "CENTER"); got != 17 {
		t.Fatalf("center segment at %d, want 17: %q", got, row)
	}
	// Truncation priority (center first, right survives): narrow to 14.
	h.tb.InjectResize(14, 1)
	h.waitFor("narrow bar", func() bool { return strings.HasSuffix(h.row(0), "RIGHT") && !strings.Contains(h.row(0), "CENTER") })
}

// TestStatusBarOptions: the construction option sets the bar's look, a nil
// option is skipped, and a segment takes one style at most.
func TestStatusBarOptions(t *testing.T) {
	red := style.New().Background(style.ANSI(1))
	s := widget.NewStatusBar(nil, widget.WithBarStyle(red))
	h := startApp(t, s, 10, 1)
	h.waitFor("the bar in red", func() bool {
		var bg tui.CellColor
		h.onLoop(func() { bg = h.tb.Snapshot()[0][0].Attrs.BG })
		return bg == tui.CellColor{Kind: tui.CellColorANSI, Index: 1}
	})
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "at most 1") {
			t.Fatalf("two styles: recovered %v", r)
		}
	}()
	s.SetLeft("x", red, red)
}

// TestStatusBarPermanentWidgets: children added after mount sit at the right end in order, the
// right segment a column before them; Move reorders them, Remove drops one; one wider than the row
// is truncated to what is free, and one offered no columns takes none.
func TestStatusBarPermanentWidgets(t *testing.T) {
	bar := widget.NewStatusBar()
	a, b := widget.NewText("AA"), widget.NewText("BBB")
	h := startApp(t, bar, 20, 1)
	h.onLoop(func() {
		bar.SetLeft("L")
		bar.SetRight("R")
		bar.Add(a, b)
	})
	h.waitFor("added after mount", func() bool { return strings.HasSuffix(h.row(0), "R AA BBB") })
	var kids []tui.Component
	h.onLoop(func() {
		for c := range bar.Children() {
			kids = append(kids, c)
		}
	})
	if len(kids) != 2 || kids[0] != a || kids[1] != b {
		t.Fatalf("Children: %v", kids)
	}
	h.onLoop(func() { bar.Move(b, 0) })
	h.waitFor("moved", func() bool { return strings.HasSuffix(h.row(0), "R BBB AA") })
	h.onLoop(func() {
		bar.Move(widget.NewText("stranger"), 0) // not its own: nothing happens
		bar.Remove(b)
		bar.Remove(nil)
	})
	h.waitFor("removed", func() bool { return strings.HasSuffix(h.row(0), "R AA") && !strings.Contains(h.row(0), "BBB") })
	// too wide for the row: the last added keeps the end, truncated to the columns free, and the
	// one before it, offered none, takes none
	wide := widget.NewText(strings.Repeat("W", 30))
	h.onLoop(func() { bar.Add(wide) })
	h.waitFor("the wide one truncated, AA left out", func() bool {
		return h.row(0) == strings.Repeat("W", 19)+"…"
	})
	h.onLoop(func() { bar.Remove(wide) })
	h.waitFor("the row back", func() bool { return strings.HasPrefix(h.row(0), "L") && strings.HasSuffix(h.row(0), "R AA") })
}

// TestStatusBarMoveRefusesAnIndexOutside: a move past the permanent widgets is the caller's
// structure gone wrong.
func TestStatusBarMoveRefusesAnIndexOutside(t *testing.T) {
	bar := widget.NewStatusBar()
	a := widget.NewText("A")
	bar.Add(a, widget.NewText("B"))
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "outside") {
			t.Fatalf("recovered %v", r)
		}
	}()
	bar.Move(a, 2)
}

// TestStatusBarAddRefusesNil: a nil permanent widget is a construction error.
func TestStatusBarAddRefusesNil(t *testing.T) {
	defer func() {
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), "nil child") {
			t.Fatalf("recovered %v", r)
		}
	}()
	widget.NewStatusBar().Add(nil)
}

// TestStatusBarNormalWidgets: AddWidget's widgets sit at the left end in order, a column apart,
// before the left segment; Children and Move keep one order across both kinds; a normal widget
// wider than what the permanent ones left is truncated to it.
func TestStatusBarNormalWidgets(t *testing.T) {
	bar := widget.NewStatusBar()
	n1, n2, p := widget.NewText("N1"), widget.NewText("N2"), widget.NewText("P")
	h := startApp(t, bar, 20, 1)
	h.onLoop(func() {
		bar.SetLeft("L")
		bar.SetRight("R")
		bar.AddWidget(n1)
		bar.Add(p)
		bar.AddWidget(n2)
	})
	h.waitFor("both ends", func() bool {
		row := h.row(0)
		return strings.HasPrefix(row, "N1 N2 L") && strings.HasSuffix(row, "R P")
	})
	var kids []tui.Component
	h.onLoop(func() {
		for c := range bar.Children() {
			kids = append(kids, c)
		}
	})
	if len(kids) != 3 || kids[0] != n1 || kids[1] != p || kids[2] != n2 {
		t.Fatalf("Children in the order added: %v", kids)
	}
	h.onLoop(func() { bar.Move(n2, 0) })
	h.waitFor("moved", func() bool { return strings.HasPrefix(h.row(0), "N2 N1 L") })
	wide := widget.NewText(strings.Repeat("W", 30))
	h.onLoop(func() {
		bar.Remove(n1)
		bar.Remove(n2)
		bar.AddWidget(wide)
	})
	h.waitFor("the wide one truncated to what is left", func() bool {
		row := h.row(0)
		return strings.HasPrefix(row, "WWW") && strings.HasSuffix(row, "… P") && !strings.Contains(row, "L")
	})
}
