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
// topmost as AutoDoc attaches it, still opens a menu on a click on its title — the empty layer
// contains no point, and a card that only tells passes the pointer through too.
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

// TestAToastWithOnClickTakesAClickAndOthersPassItThrough: a card whose toast only tells passes a
// click through to the button beneath it; a card whose toast has OnClick contains its points, and a
// press and release on it runs OnClick once, the button beneath untouched.
func TestAToastWithOnClickTakesAClickAndOthersPassItThrough(t *testing.T) {
	var under, opened atomic.Int64
	button := widget.NewButton(strings.Repeat("U", 36), widget.WithOnActivate(func() { under.Add(1) }))
	root := tui.NewFlex(tui.Vertical)
	root.AddWeighted(widget.NewText("page"), 1)
	root.Add(button) // the bottom row, under the card in the corner
	host := widget.NewOverlayHost(root)
	toasts := widget.NewToasts(widget.WithToastCorner(widget.BottomRight), widget.WithToastWidth(20))
	host.AttachTopmost(toasts.Float())
	h := startApp(t, host, 40, 10)
	defer h.stop()
	const x, y = 30, 9 // the card's bottom border, over the button's right end

	h.onLoop(func() { toasts.Post(widget.Toast{Text: "only tells"}) })
	h.waitFor("the toast shown", func() bool { return strings.Contains(h.grid(), "only tells") })
	if c := h.tb.Snapshot()[y][x].Content; c != "─" {
		t.Fatalf("the setup: (%d, %d) is %q, not the card's border:\n%s", x, y, c, h.grid())
	}
	pointerClick(h, x, y)
	h.waitFor("the button under a card that only tells clicked", func() bool { return under.Load() == 1 })

	h.onLoop(func() { toasts.Post(widget.Toast{Text: "click me", OnClick: func() { opened.Add(1) }}) })
	h.waitFor("the clickable toast in the corner", func() bool { return strings.Contains(h.row(8), "click me") })
	pointerClick(h, x, y)
	h.waitFor("its OnClick ran", func() bool { return opened.Load() == 1 })
	h.settle()
	if opened.Load() != 1 || under.Load() != 1 {
		t.Fatalf("OnClick ran %d times, the button beneath %d (want 1 and 1)", opened.Load(), under.Load())
	}
}
