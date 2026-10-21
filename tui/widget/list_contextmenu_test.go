package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// list_contextmenu_test.go: a row's context menu (List.SetContextMenu).

func rightPress(x, y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight, X: x, Y: y}
}

// shellMice is how many mouse events reached the shell around the list: what the list left
// unhandled.
func shellMice(h *harness, sh *shell) int {
	var n int
	h.onLoop(func() {
		sh.mu.Lock()
		n = len(sh.mice)
		sh.mu.Unlock()
	})
	return n
}

// TestListContextMenuIsOffByDefault: without SetContextMenu a right press is not the list's: the
// cursor stays, no menu is requested, and the press goes on to the list's parent, as before.
func TestListContextMenuIsOffByDefault(t *testing.T) {
	src := widget.SliceSource([]string{"a", "b", "c", "d"})
	h, l, sh := focusedList(t, src, 10, 4)
	menus := record[widget.ContextMenuEvent](h)
	h.inject(rightPress(2, 2))
	h.barrier(sh)
	var idx int
	h.onLoop(func() { idx, _ = l.Selected() })
	if idx != 0 || len(menus.events()) != 0 || shellMice(h, sh) != 1 {
		t.Fatalf("off: cursor %d, menus %+v, the parent saw %d presses", idx, menus.events(), shellMice(h, sh))
	}
}

// TestListContextMenuRequestsTheRowPressed: on, a right press on a row moves the cursor there
// first (SelectionChangedEvent, as a left press) and then requests that row's menu at the pointer,
// in the list's cells, at any scroll; below the rows it requests no row's (-1), the cursor left
// where it is; the press is the list's.
func TestListContextMenuRequestsTheRowPressed(t *testing.T) {
	src := widget.SliceSource([]string{"a", "b", "c", "d", "e", "f"})
	h, l, sh := focusedList(t, src, 10, 4)
	h.onLoop(func() { l.SetContextMenu(true) })
	moves := record[widget.SelectionChangedEvent](h)
	menus := record[widget.ContextMenuEvent](h)

	h.inject(rightPress(3, 2))
	h.barrier(sh)
	var idx int
	h.onLoop(func() { idx, _ = l.Selected() })
	if idx != 2 {
		t.Fatalf("a right press on row 2: the cursor is on %d", idx)
	}
	if m, ok := moves.last(); !ok || m.Index != 2 {
		t.Fatalf("no move to row 2 before the menu: %+v", moves.events())
	}
	if got := menus.events(); len(got) != 1 || got[0].Index != 2 || got[0].At != (tui.Point{X: 3, Y: 2}) {
		t.Fatalf("the menu asked: %+v", got)
	}
	if shellMice(h, sh) != 0 {
		t.Fatal("a right press the list took reached its parent")
	}

	// scrolled down two rows: the third row on screen is row 4
	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 1},
		tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 1})
	h.barrier(sh)
	h.inject(rightPress(0, 2))
	h.barrier(sh)
	h.onLoop(func() { idx, _ = l.Selected() })
	if got := menus.events(); idx != 4 || len(got) != 2 || got[1].Index != 4 || got[1].At != (tui.Point{X: 0, Y: 2}) {
		t.Fatalf("scrolled: cursor %d, menus %+v", idx, got)
	}

	// below the rows: no row's menu, the cursor where it was
	few := widget.SliceSource([]string{"a", "b"})
	h2, l2, sh2 := focusedList(t, few, 10, 4)
	h2.onLoop(func() { l2.SetContextMenu(true) })
	menus2 := record[widget.ContextMenuEvent](h2)
	h2.inject(rightPress(1, 3))
	h2.barrier(sh2)
	h2.onLoop(func() { idx, _ = l2.Selected() })
	if got := menus2.events(); idx != 0 || len(got) != 1 || got[0].Index != -1 || got[0].At != (tui.Point{X: 1, Y: 3}) {
		t.Fatalf("below the rows: cursor %d, menus %+v", idx, got)
	}
}

// TestListContextMenuKeys: on, Shift+F10 and the Menu key request the cursor row's menu, at its
// first cell on screen; an empty list's, no row's. Off, the keys are not the list's.
func TestListContextMenuKeys(t *testing.T) {
	src := widget.SliceSource([]string{"a", "b", "c", "d", "e", "f"})
	h, l, sh := focusedList(t, src, 10, 3)
	menus := record[widget.ContextMenuEvent](h)
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF10, Mods: tui.ModShift})
	h.barrier(sh)
	if len(menus.events()) != 0 {
		t.Fatalf("off: Shift+F10 asked a menu: %+v", menus.events())
	}
	h.onLoop(func() { l.SetContextMenu(true); l.SetCursor(4) }) // scrolled: row 4 on the screen's last row
	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyF10, Mods: tui.ModShift}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyMenu})
	h.barrier(sh)
	got := menus.events()
	if len(got) != 2 || got[0].Index != 4 || got[0].At != (tui.Point{Y: 2}) || got[1] != got[0] {
		t.Fatalf("the keys asked: %+v", got)
	}

	empty := widget.SliceSource([]string{})
	h2, l2, sh2 := focusedList(t, empty, 10, 3)
	h2.onLoop(func() { l2.SetContextMenu(true) })
	menus2 := record[widget.ContextMenuEvent](h2)
	h2.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyMenu})
	h2.barrier(sh2)
	if got := menus2.events(); len(got) != 1 || got[0].Index != -1 {
		t.Fatalf("an empty list's menu: %+v", got)
	}
}

// TestAListTellsItsOwnerOfAMenuRequest: WithListOnContextMenu hears a right press's request, with
// the row and the point, as ContextMenuEvent carries them (data-I4).
func TestAListTellsItsOwnerOfAMenuRequest(t *testing.T) {
	type ask struct {
		i  int
		at tui.Point
	}
	var asks []ask
	src := widget.SliceSource([]string{"a", "b", "c"})
	h, l, sh := focusedList(t, src, 10, 4,
		widget.WithListOnContextMenu[string](func(i int, at tui.Point) { asks = append(asks, ask{i, at}) }))
	h.onLoop(func() { l.SetContextMenu(true) })
	h.inject(rightPress(3, 2))
	h.barrier(sh)
	var got []ask
	h.onLoop(func() { got = asks })
	if len(got) != 1 || got[0] != (ask{2, tui.Point{X: 3, Y: 2}}) {
		t.Fatalf("menu requests %+v, want row 2 at (3,2)", got)
	}
}
