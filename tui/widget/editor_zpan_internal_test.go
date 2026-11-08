package widget

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// widePicture is a mermaid fence whose picture is 60 cells wide, its rows "0123456789" six times,
// and a line after it: in a 40-cell view it is laid at its width, its content 38 cells between its
// two mark columns, so it pans 0 to 22.
func widePicture(t *testing.T) (*Editor, *internalHarness) {
	t.Helper()
	lay := newHeldLay()
	e, ih := pictureEditor(t, "```mermaid\nW\n```\nafter", lay, 40, 8)
	lay.await(t, lay.started, "W", "request")
	row := strings.Repeat("0123456789", 6)
	lay.answer("W", rowsPicture{row, row})
	waitGrid(t, ih, "the wide picture", func(g string) bool { return strings.Contains(g, "012345678901") })
	return e, ih
}

// shownFrom is the picture's first row from its content's first cell (after the left mark column).
func shownFrom(ih *internalHarness) string {
	r := []rune(strings.Split(ih.tb.String(), "\n")[0])
	if len(r) < 3 {
		return ""
	}
	return string(r[1:3])
}

// zKeys types keys into e on the loop, one KeyEvent each.
func zKeys(ih *internalHarness, e *Editor, keys string) {
	ih.onLoopInternal(func() {
		for _, k := range keys {
			e.HandleEvent(tui.KeyEvent{Code: k, Text: string(k)})
		}
	})
}

// waitShown waits for the picture's first row to start at want (two digits: the pan's).
func waitShown(t *testing.T, ih *internalHarness, what, want string) {
	t.Helper()
	waitGrid(t, ih, what, func(string) bool { return shownFrom(ih) == want })
}

// A wide picture pans by hand, as it has no source for the cursor to follow: zl a cell right, a
// count before it that many, zh back, clamped to what it has to show (0 to 22); the cursor moving
// off the picture and back keeps the pan (reveal pans only a block with source, a table).
func TestAWidePicturePansWithZhAndZl(t *testing.T) {
	e, ih := widePicture(t)
	if got := shownFrom(ih); got != "01" {
		t.Fatalf("unpanned, the picture starts %q, want 01:\n%s", got, ih.tb.String())
	}
	zKeys(ih, e, "zl")
	waitShown(t, ih, "zl: a cell", "12")
	zKeys(ih, e, "3zl")
	waitShown(t, ih, "3zl: three more", "45")
	zKeys(ih, e, "zh")
	waitShown(t, ih, "zh: one back", "34")
	zKeys(ih, e, "50zl")
	waitShown(t, ih, "50zl: clamped at 22", "23")
	zKeys(ih, e, "jk")
	ih.syncInternal()
	if got := shownFrom(ih); got != "23" {
		t.Errorf("the cursor off the picture and back: it starts %q, want the pan kept (23):\n%s", got, ih.tb.String())
	}
	zKeys(ih, e, "99zh")
	waitShown(t, ih, "99zh: clamped at 0", "01")
}

// Shift with the wheel, or a sideways wheel, pans the wide block under the pointer three cells
// (another picture than the cursor's), and with the pointer on a row of no wide block, the
// cursor's.
func TestAWidePicturePansWithShiftAndTheWheel(t *testing.T) {
	lay := newHeldLay()
	e, ih := pictureEditor(t, "```mermaid\nW\n```\n```mermaid\nV\n```\nafter", lay, 40, 8)
	digits, letters := strings.Repeat("0123456789", 6), strings.Repeat("abcdefghij", 6)
	lay.answer("W", rowsPicture{digits})
	lay.answer("V", rowsPicture{letters})
	waitGrid(t, ih, "both pictures", func(g string) bool { return strings.Contains(g, "01234567") && strings.Contains(g, "abcdefgh") })
	from := func(y int) string { // row y from its content's first cell
		r := []rune(strings.Split(ih.tb.String(), "\n")[y])
		if len(r) < 3 {
			return ""
		}
		return string(r[1:3])
	}
	wheel := func(b tui.MouseButton, mods tui.Mods, y int) {
		ih.onLoopInternal(func() { e.HandleEvent(tui.MouseEvent{Kind: tui.MouseWheel, Button: b, Mods: mods, X: 5, Y: y}) })
	}
	shown := func(what, w, v string) {
		t.Helper()
		waitGrid(t, ih, what, func(string) bool { return from(0) == w && from(1) == v })
	}
	wheel(tui.WheelDown, tui.ModShift, 1) // over V, the cursor on W
	shown("Shift+wheel down over the second picture: it pans, the cursor's does not", "01", "de")
	wheel(tui.WheelRight, 0, 0)
	shown("a sideways wheel over the first picture", "34", "de")
	wheel(tui.WheelLeft, 0, 2) // over "after": no wide block, so the cursor's (W)
	shown("a sideways wheel off any wide block: the cursor's picture", "01", "de")
	wheel(tui.WheelUp, tui.ModShift, 1)
	shown("Shift+wheel up over the second picture", "01", "ab")
}
