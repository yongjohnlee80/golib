package widget_test

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func pointerClick(h *harness, x, y int) {
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y})
	h.inject(tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y})
}

// TestTheApplicationUnderANonModalFloatTakesItsClicks: a shown non-modal float — the toasts, a
// card in a corner — lets the pointer through wherever its content is not, so a button beneath it
// is clicked; its own content still takes a click on it. A modal float's backdrop still catches
// the pointer. (The toasts, always shown, once took every click on an application's screen.)
func TestTheApplicationUnderANonModalFloatTakesItsClicks(t *testing.T) {
	var under, card atomic.Int64
	button := widget.NewButton("Under", widget.WithOnActivate(func() { under.Add(1) }))
	host := widget.NewOverlayHost(button)
	cardButton := widget.NewButton("Card", widget.WithOnActivate(func() { card.Add(1) }))
	float := widget.NewFloat(cardButton, widget.WithAnchor(widget.BottomRight))
	host.AttachTopmost(float)
	h := startApp(t, host, 40, 8)
	defer h.stop()
	h.onLoop(float.Show)
	h.settle()

	x, y := cellOfLabel(t, h, "Under")
	pointerClick(h, x, y)
	h.waitFor("the button under the float clicked", func() bool { return under.Load() == 1 })

	cx, cy := cellOfLabel(t, h, "Card")
	pointerClick(h, cx, cy)
	h.waitFor("the float's own content clicked", func() bool { return card.Load() == 1 })

	modal := widget.NewFloat(widget.NewText("a dialog"), widget.WithModal(true))
	h.onLoop(func() { host.Attach(modal); modal.Show() })
	h.settle()
	pointerClick(h, x, y)
	h.settle()
	h.settle()
	if under.Load() != 1 {
		t.Fatalf("a click on a modal float's backdrop reached the application (%d)", under.Load())
	}
}

// TestAMenuTitleUnderTheToastsOpensItsMenu: a menu bar under an always-shown toasts layer, attached
// topmost as AutoDoc attaches it, still opens a menu on a click on its title.
func TestAMenuTitleUnderTheToastsOpensItsMenu(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{
		widget.NewSubmenu("file", "File", []widget.MenuItemModel{widget.NewCommand("new", "New note", nil)}),
	}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	bar := widget.NewMenuBar(m)
	root := tui.NewFlex(tui.Vertical)
	root.Add(bar, widget.NewText("page"))
	host := widget.NewOverlayHost(root)
	toasts := widget.NewToasts()
	host.AttachTopmost(toasts.Float())
	h := startApp(t, host, 40, 8)
	defer h.stop()
	h.onLoop(func() { toasts.Post(widget.Toast{Text: "connected"}) })
	h.waitFor("the toast shown", func() bool { return strings.Contains(h.grid(), "connected") })

	x, y := cellOfLabel(t, h, "File")
	pointerClick(h, x, y)
	h.waitFor("the File menu open", func() bool { return strings.Contains(h.grid(), "New note") })
}
