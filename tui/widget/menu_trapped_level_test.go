package widget_test

import (
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A SUBMENU OPENED INSIDE A FOCUS TRAP.
//
// A cascade's level is a layer of the overlay host, a sibling of whatever opened the menu. When
// the menu sits inside a trapping scope — a right-click menu's popup layer, a modal dialog — the
// level lies outside that scope's subtree, and the pointer is confined to the scope. These tests
// click a level's row in each of those places and require the row to run.

// cellOfRun is the screen column and row where label starts, counted in cells: the frames around
// the menus are multi-byte runes, so a byte offset into a row's text names the wrong column.
func cellOfRun(t *testing.T, h *harness, label string) (x, y int) {
	t.Helper()
	want := []rune(label)
	for row, cells := range h.tb.Snapshot() {
		for col := range cells {
			if col+len(want) > len(cells) {
				break
			}
			match := true
			for i, r := range want {
				if cells[col+i].Content != string(r) {
					match = false
					break
				}
			}
			if match {
				return col, row
			}
		}
	}
	t.Fatalf("label %q is not on screen:\n%s", label, h.grid())
	return 0, 0
}

// leftClick is a primary press and its release at (x, y).
func leftClick(h *harness, x, y int) {
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
}

// zoomRows is the editor's stock rows and a Zoom submenu, AutoDoc's right-click menu in
// miniature: each step records its percent in chose.
func zoomRows(chose *atomic.Int64) func(e *widget.Editor) []widget.MenuItemModel {
	return func(e *widget.Editor) []widget.MenuItemModel {
		step := func(id widget.ItemID, label string, key rune, pct int64) widget.MenuItemModel {
			r := widget.NewCommand(id, label, widget.EditorMenuAction{ID: tui.ActionID(id),
				Run: func(*widget.Editor) { chose.Store(pct) }})
			r.Hotkey, r.HotkeyIdx = key, 0
			return r
		}
		return append(widget.EditorContextItems(e), widget.NewSeparator("sep.zoom"),
			widget.NewSubmenu("zoom", "Zoom", []widget.MenuItemModel{
				step("z100", "1. 100%", '1', 100),
				step("z125", "2. 125%", '2', 125),
				step("z150", "3. 150%", '3', 150),
			}))
	}
}

// openZoom right-clicks the editor and clicks Zoom, leaving its level on screen.
func openZoom(t *testing.T, h *harness, ed *widget.Editor) {
	t.Helper()
	h.inject(rightClick(1, 0))
	h.waitFor("the menu opened", func() bool { return menuOpen(h, ed) })
	h.settle()
	zx, zy := cellOfRun(t, h, "Zoom")
	leftClick(h, zx, zy)
	h.settle()
	h.settle()
	h.wantContains("2. 125%")
}

// TestAContextMenuSubmenuRowRunsOnAClick: the right-click menu's popup layer traps focus, and the
// Zoom level is a separate layer of the same host. A click on one of the level's rows reaches the
// row, runs it and closes the menu.
func TestAContextMenuSubmenuRowRunsOnAClick(t *testing.T) {
	var chose atomic.Int64
	h, ed, _ := ctxFixture(t, 60, 16, widget.WithContextMenu(zoomRows(&chose)))
	defer h.stop()
	openZoom(t, h, ed)

	x, y := cellOfRun(t, h, "2. 125%")
	leftClick(h, x, y)
	h.waitFor("the menu closed", func() bool { return !menuOpen(h, ed) })
	if got := chose.Load(); got != 125 {
		t.Errorf("chose %d%%, want 125%%: the click on the level's row did not run it", got)
	}
}

// TestAContextMenuSubmenuRowRunsFromItsMnemonic: the keyboard half of the same menu, which the
// trap never blocked — the menu keeps the focus while its level is open.
func TestAContextMenuSubmenuRowRunsFromItsMnemonic(t *testing.T) {
	var chose atomic.Int64
	h, ed, _ := ctxFixture(t, 60, 16, widget.WithContextMenu(zoomRows(&chose)))
	defer h.stop()
	openZoom(t, h, ed)

	h.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: '3'})
	h.waitFor("the menu closed", func() bool { return !menuOpen(h, ed) })
	if got := chose.Load(); got != 150 {
		t.Errorf("chose %d%%, want 150%%", got)
	}
}

// TestAContextMenuLevelFramePressKeepsTheMenu: a press on the level's frame, off every row, lands
// on the level, not on the popup layer behind it — which would read it as a press outside the menu
// and close everything.
func TestAContextMenuLevelFramePressKeepsTheMenu(t *testing.T) {
	var chose atomic.Int64
	h, ed, _ := ctxFixture(t, 60, 16, widget.WithContextMenu(zoomRows(&chose)))
	defer h.stop()
	openZoom(t, h, ed)

	x, y := cellOfRun(t, h, "1. 100%")
	leftClick(h, x, y-1) // the level's top border, above its first row
	h.settle()
	if !menuOpen(h, ed) {
		t.Error("a press on the level's frame closed the menu")
	}
	if got := chose.Load(); got != 0 {
		t.Errorf("a press on the frame chose %d%%", got)
	}
}

// TestAMenuInAModalDialogOpensAClickableLevel: a menu inside a modal Float — a trapping scope of
// its own — opens its level on the host, outside the dialog's subtree. The level's rows answer the
// pointer as the dialog's own controls do.
func TestAMenuInAModalDialogOpensAClickableLevel(t *testing.T) {
	var ran atomic.Int64
	m := widget.NewMenu(widget.WithActionExecutor(func(inv tui.ActionInvocation) bool {
		if inv.Action.ActionID() == "open" {
			ran.Add(1)
		}
		return true
	}))
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{
			widget.NewCommand("new", "New", saveAction{}),
			widget.NewCommand("open", "Open", testAction{id: "open"}),
		}),
		widget.NewCommand("quit", "Quit", saveAction{}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	base := widget.NewButton("base")
	host := widget.NewOverlayHost(base)
	dialog := widget.NewFloat(widget.NewBox(m), widget.WithModal(true),
		widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 1, W: 20, H: 6})))
	host.Attach(dialog)
	h := startApp(t, host, 60, 16)
	defer h.stop()
	h.onLoop(func() {
		dialog.Show()
		m.Context().RequestFocus()
	})
	h.settle()

	fx, fy := cellOfRun(t, h, "File")
	leftClick(h, fx, fy)
	h.settle()
	h.settle()
	if n := openLevelsOn(t, h, m); n != 1 {
		t.Fatalf("clicking File opened %d levels, want 1:\n%s", n, h.grid())
	}
	ox, oy := cellOfRun(t, h, "Open")
	leftClick(h, ox, oy)
	h.waitFor("the level's row ran", func() bool { return ran.Load() == 1 })
	h.settle()
	if n := openLevelsOn(t, h, m); n != 0 {
		t.Errorf("%d levels still open after the row ran", n)
	}
}

// testAction is an action with an id of the test's choosing.
type testAction struct{ id tui.ActionID }

func (a testAction) ActionID() tui.ActionID { return a.id }
