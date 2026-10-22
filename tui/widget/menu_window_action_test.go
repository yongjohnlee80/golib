package widget_test

import (
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A MENU ROW THAT ACTS ON THE WINDOW AROUND ITS OWNER.
//
// A right-click menu is a popup layer that traps the input scope while it is open, and a window's
// operation asked by input (WindowMod.InvokeInput) is refused outside that scope. A row that
// maximizes the window its owner sits in must therefore run once the menu has gone, or it is
// refused. These cells click such a row in each menu that opens as a popup layer: an Editor's
// right-click menu and a TerminalTabs' strip menu.

// windowed puts content in a maximizable window in a Float over a base, as a panel sits in a
// Drawer.
func windowed(content tui.Component) (*widget.WindowMod, *widget.Float, *widget.OverlayHost) {
	w := widget.NewWindowMod(content, widget.WithWindowMaximize())
	f := widget.NewFloat(w, widget.WithAnchor(widget.AtRect(tui.Rect{X: 2, Y: 2, W: 30, H: 8})))
	host := widget.NewOverlayHost(widget.NewButton("base"))
	host.Attach(f)
	return w, f, host
}

// maximizeRow is a row whose action maximizes w by input, as a Drawer's toggleMaximize does,
// counting what the window answered.
type maximizeRow struct {
	w             *widget.WindowMod
	took, refused atomic.Int64
}

func (m *maximizeRow) run() {
	if m.w.InvokeInput(widget.WindowMaximizeAction{}) {
		m.took.Add(1)
	} else {
		m.refused.Add(1)
	}
}

func (m *maximizeRow) answered() bool { return m.took.Load()+m.refused.Load() > 0 }

// TestAnEditorMenuRowMaximizesTheWindowAroundIt: a click on the right-click menu's Maximize runs
// it once the menu has closed, and the window around the editor maximizes.
func TestAnEditorMenuRowMaximizesTheWindowAroundIt(t *testing.T) {
	row := &maximizeRow{}
	ed := widget.NewEditor(widget.WithCore(widget.CoreInitialText("hello")), widget.WithContextMenu(func(*widget.Editor) []widget.MenuItemModel {
		return []widget.MenuItemModel{widget.NewCommand("max", "Maximize", widget.EditorMenuAction{ID: "max",
			Run: func(*widget.Editor) { row.run() }})}
	}))
	w, f, host := windowed(ed)
	row.w = w
	h := startApp(t, host, 50, 20)
	defer h.stop()
	h.onLoop(f.Show)
	h.settle()
	x, y := cellOfRun(t, h, "hello")
	leftClick(h, x, y) // the editor's keyboard
	h.settle()
	h.inject(rightClick(x, y))
	h.waitFor("the menu opened", func() bool { return menuOpen(h, ed) })
	h.settle()
	mx, my := cellOfRun(t, h, "Maximize")
	leftClick(h, mx, my)
	h.waitFor("the row ran", row.answered)
	if row.refused.Load() != 0 || row.took.Load() != 1 {
		t.Fatalf("the window refused the menu's maximize (took %d, refused %d): the row ran inside the menu's trap",
			row.took.Load(), row.refused.Load())
	}
	var maximized, open bool
	h.onLoop(func() { maximized, open = w.Core().State().Maximized, ed.ContextMenu().IsOpen() })
	if !maximized || open {
		t.Errorf("maximized %v, the menu open %v", maximized, open)
	}
}

// TestATerminalTabsMenuRowMaximizesTheWindowAroundIt: the strip's own menu (SetMenu), opened by a
// right press on a body no terminal fills, runs a clicked Maximize once it has closed, and the
// window around the strip maximizes.
func TestATerminalTabsMenuRowMaximizesTheWindowAroundIt(t *testing.T) {
	row := &maximizeRow{}
	tabs := widget.NewTerminalTabs()
	tabs.SetMenu(func(widget.TerminalTabID) []widget.MenuItemModel {
		return []widget.MenuItemModel{widget.NewCommand("max", "Maximize", widget.TerminalTabsMenuAction{ID: "max",
			Run: func(widget.TerminalTabID) { row.run() }})}
	})
	w, f, host := windowed(tabs)
	row.w = w
	h := startApp(t, host, 50, 20)
	defer h.stop()
	h.onLoop(f.Show)
	h.settle()
	h.inject(rightClick(10, 6)) // the empty body
	h.waitFor("the menu opened", func() bool {
		var open bool
		h.onLoop(func() { open = tabs.MenuOpen() })
		return open
	})
	h.settle()
	mx, my := cellOfRun(t, h, "Maximize")
	leftClick(h, mx, my)
	h.waitFor("the row ran", row.answered)
	if row.refused.Load() != 0 || row.took.Load() != 1 {
		t.Fatalf("the window refused the menu's maximize (took %d, refused %d): the row ran inside the menu's trap",
			row.took.Load(), row.refused.Load())
	}
	var maximized bool
	h.onLoop(func() { maximized = w.Core().State().Maximized })
	if !maximized {
		t.Error("the window around the strip did not maximize")
	}
}
