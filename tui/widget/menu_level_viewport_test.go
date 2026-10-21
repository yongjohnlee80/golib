package widget_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// rowsNamed are n commands labelled "<prefix> 00", "<prefix> 01", …
func rowsNamed(prefix string, n int) []widget.MenuItemModel {
	rows := make([]widget.MenuItemModel, n)
	for i := range rows {
		label := fmt.Sprintf("%s %02d", prefix, i)
		rows[i] = widget.NewCommand(widget.ItemID(label), label, nil)
	}
	return rows
}

func pressKey(h *harness, code rune, times int) {
	for range times {
		h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: code})
		h.settle()
	}
}

// A column of rows taller than the screen scrolls to the row the keys select: it is painted, and
// what is painted is what can be pointed at. The first rows show before any move, the control.
func TestAMenuColumnShowsTheRowItsKeysSelect(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel(rowsNamed("Item", 12)); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 5)
	defer h.stop()

	if !strings.Contains(h.grid(), "Item 00") {
		t.Fatalf("the column's first row is not on screen before any move:\n%s", h.grid())
	}
	pressKey(h, tui.KeyDown, 8)
	if id := selectedOn(t, h, m); id != "Item 08" {
		t.Fatalf("Down ×8 selected %q, want Item 08", id)
	}
	if !strings.Contains(h.grid(), "Item 08") {
		t.Errorf("the selected row is off screen; the column did not scroll to it:\n%s", h.grid())
	}
}

// A dropdown level taller than the screen scrolls to the row the keys select, and a submenu on that
// row opens: its row is on screen, so it is an anchor.
func TestAMenuLevelOpensTheSubmenuOfARowItsKeysScrolledTo(t *testing.T) {
	children := rowsNamed("Child", 12)
	children[8] = widget.NewSubmenu("deep", "Deeper", []widget.MenuItemModel{widget.NewCommand("leaf", "Leaf", nil)})
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{widget.NewSubmenu("more", "More", children)}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 40, 8)
	defer h.stop()

	h.onLoop(func() {
		if err := m.Open("more"); err != nil {
			t.Errorf("Open(more): %v", err)
		}
	})
	h.settle()
	pressKey(h, tui.KeyDown, 8)
	if id := selectedOn(t, h, m); id != "deep" {
		t.Fatalf("Down ×8 in the level selected %q, want deep", id)
	}
	if !strings.Contains(h.grid(), "Deeper") {
		t.Errorf("the selected row is off screen; the level did not scroll to it:\n%s", h.grid())
	}
	pressKey(h, tui.KeyRight, 1)
	if n := openLevelsOn(t, h, m); n != 2 {
		t.Errorf("Right on the selected submenu row left %d levels open, want 2:\n%s", n, h.grid())
	}
}

// A wheel notch scrolls the column and leaves the selection where it was, and a later layout does
// not pull the window back to it: the selection moved nowhere, so there is nothing to reveal. The
// rows overflowing the column show the scroll indicator.
func TestAMenuColumnScrollsOnTheWheelAndStaysThere(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel(rowsNamed("Item", 12)); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	h, _ := menuFixture(t, m, 30, 5)
	defer h.stop()

	if !strings.Contains(h.grid(), "█") {
		t.Errorf("rows overflowing the column show no scroll indicator:\n%s", h.grid())
	}
	for range 3 {
		h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 1})
		h.settle()
	}
	h.onLoop(func() { m.Context().RequestLayout() }) // a later pass, the selection unmoved
	h.settle()
	if got := h.grid(); strings.Contains(got, "Item 00") || !strings.Contains(got, "Item 03") {
		t.Errorf("three notches down: want Item 03 on top and Item 00 scrolled away:\n%s", got)
	}
	if id := selectedOn(t, h, m); id != "Item 00" {
		t.Errorf("the wheel moved the selection to %q; it scrolls the rows only", id)
	}
}

// A new selection is revealed though it sits where the old one did: after the wheel scrolled away
// from row 0, a model with new ids selects its own row 0, which is a different item and comes into
// view. TestAMenuColumnScrollsOnTheWheelAndStaysThere is the control: an
// unchanged selection leaves the wheel's scroll alone.
func TestAMenuColumnRevealsANewSelectionAtTheOldOnesPlace(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel(rowsNamed("Original", 12)); err != nil {
		t.Fatal(err)
	}
	h, _ := menuFixture(t, m, 30, 5)
	defer h.stop()
	for range 3 {
		h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 1})
		h.settle()
	}
	if strings.Contains(h.grid(), "Original 00") {
		t.Fatal("precondition: the wheel never scrolled away from the selection")
	}
	h.onLoop(func() {
		if err := m.SetModel(rowsNamed("Replacement", 12)); err != nil {
			t.Error(err)
		}
	})
	h.settle()
	if id := selectedOn(t, h, m); id != "Replacement 00" {
		t.Fatalf("precondition: selected %q, want the new first row", id)
	}
	if got := h.grid(); !strings.Contains(got, "Replacement 00") {
		t.Errorf("the new selection, at the old one's place, is scrolled off:\n%s", got)
	}
}
