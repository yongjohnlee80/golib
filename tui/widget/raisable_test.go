package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// pageOf is a Raisable around a titled box holding one button: the page.
func pageOf(maxW int) (*widget.Raisable, *widget.Button) {
	btn := widget.NewButton("page-btn")
	r := widget.NewRaisable(widget.NewBox(btn, widget.WithTitle("page")))
	r.SetMaximumWidth(maxW)
	return r, btn
}

// TestTheKeyboardRaisesARaisable: a page capped to 20 of 40 columns, centred, and a panel over its
// right edge. Each one taking the keyboard is drawn above the other, as two panels are; a click in
// the page's margin reaches neither.
func TestTheKeyboardRaisesARaisable(t *testing.T) {
	page, pageBtn := pageOf(20)
	host := widget.NewOverlayHost(page)
	panel, panelBtn := panelFloat("P", tui.Rect{X: 24, Y: 2, W: 16, H: 6})
	host.Attach(panel)
	h := startApp(t, host, 40, 12)
	defer h.stop()
	h.onLoop(func() { panel.Show() })
	h.settle()
	var content tui.Rect
	h.onLoop(func() { content = page.Float().ContentRect() })
	if content != (tui.Rect{X: 10, Y: 0, W: 20, H: 12}) {
		t.Fatalf("the page's layer covers %+v; want its capped place, centred", content)
	}

	h.inject(clickUp(36, 4)...) // the panel's part outside the page
	h.settle()
	if !focusedOn(t, h, panelBtn) {
		t.Fatal("the click did not focus the panel")
	}
	if o := layerOrder(h, host, page.Float(), panel); o[1] < o[0] {
		t.Fatalf("layers %v (page, panel); the focused panel is beneath the page", o)
	}

	h.onLoop(func() { pageBtn.Context().RequestFocus() })
	h.settle()
	if o := layerOrder(h, host, page.Float(), panel); o[0] < o[1] {
		t.Fatalf("layers %v (page, panel); the focused page is beneath the panel", o)
	}
	h.wantContains("page-btn")

	h.inject(clickUp(3, 6)...) // the page's margin: no part of its layer, and no panel there
	h.settle()
	if !focusedOn(t, h, pageBtn) {
		t.Fatal("a click in the page's margin moved the keyboard")
	}
}

// TestARaisableStartsBeneathAndAPanelShownRises: a page mounted while a panel is open arrives
// beneath it (a main window under its panels), without raising itself. Once the page has the
// keyboard, AttachBeneath again leaves it where it stands, and the panel shown again comes above it,
// as a shown window does, keyboard or not. A dialog open then stays above that panel.
func TestARaisableStartsBeneathAndAPanelShownRises(t *testing.T) {
	page, pageBtn := pageOf(20)
	root := tui.NewFlex(tui.Vertical)
	host := widget.NewOverlayHost(root)
	panel, panelBtn := panelFloat("P", tui.Rect{X: 24, Y: 2, W: 16, H: 6})
	host.Attach(panel)
	h := startApp(t, host, 40, 12)
	defer h.stop()
	h.onLoop(func() { panel.Show() })
	h.settle()
	h.onLoop(func() { root.AddWeighted(page, 1) })
	h.settle()
	h.wantContains("P-btn")
	if o := layerOrder(h, host, page.Float(), panel); o[0] < 1 || o[1] < o[0] {
		t.Fatalf("layers %v (page, panel); want the page above the base, beneath the panel open before it", o)
	}
	if shown := func() (on bool) { h.onLoop(func() { on = page.Float().Shown() }); return on }(); !shown {
		t.Fatal("the page mounted late is not shown")
	}

	h.onLoop(func() { pageBtn.Context().RequestFocus() })
	h.settle()
	if o := layerOrder(h, host, page.Float(), panel); o[0] < o[1] {
		t.Fatalf("layers %v (page, panel); the focused page is beneath the panel", o)
	}
	h.onLoop(func() { host.AttachBeneath(page.Float()) })
	if o := layerOrder(h, host, page.Float(), panel); o[0] < o[1] {
		t.Fatalf("layers %v (page, panel); AttachBeneath again moved the raised page", o)
	}
	h.onLoop(func() { panel.Hide(); panel.Show() })
	h.settle()
	if o := layerOrder(h, host, page.Float(), panel); o[1] < o[0] {
		t.Fatalf("layers %v (page, panel); the panel shown again is beneath the page", o)
	}
	if focusedOn(t, h, panelBtn) || !focusedOn(t, h, pageBtn) {
		t.Fatal("showing the panel moved the keyboard")
	}

	m := widget.NewModal(widget.NewText("dialog"), widget.WithModalTitle("D"))
	openOn(t, h, m, host)
	h.onLoop(func() { panel.Hide(); panel.Show() })
	h.settle()
	if o := layerOrder(h, host, panel, m); o[1] < o[0] {
		t.Fatalf("layers %v (panel, dialog); the panel shown went above the open dialog", o)
	}
	h.onLoop(func() { m.Dismiss(widget.DismissProgrammatic) })
}

// TestARaisableFollowsItsPlace: inside a centred, capped box, the Raisable's place moves when the
// window widens, and its layer moves with it in the same frame (a node's rect is the previous
// frame's while the tree is laid out; the commit lays out again).
func TestARaisableFollowsItsPlace(t *testing.T) {
	page, _ := pageOf(0)
	outer := widget.NewBox(page, widget.WithTitle("outer"))
	outer.SetMaximumWidth(30)
	h := startApp(t, widget.NewOverlayHost(outer), 40, 12)
	defer h.stop()
	h.settle()
	at := func() (r tui.Rect) { h.onLoop(func() { r = page.Float().ContentRect() }); return r }
	if got := at(); got != (tui.Rect{X: 6, Y: 1, W: 28, H: 10}) {
		t.Fatalf("at 40 columns the page's layer covers %+v", got)
	}
	h.tb.InjectResize(60, 12)
	h.settle()
	if got := at(); got != (tui.Rect{X: 16, Y: 1, W: 28, H: 10}) {
		t.Fatalf("at 60 columns the page's layer covers %+v; want its place moved", got)
	}
}

// TestTheMenuBarsCorner: the corner sits at the bar's right end, a PegRight menu just before it; a
// bar too narrow for both keeps the menus whole and cuts the corner.
func TestTheMenuBarsCorner(t *testing.T) {
	m := widget.NewMenu()
	help := widget.NewCommand("help", "Help", nil)
	help.PegRight = true
	if err := m.SetModel([]widget.MenuItemModel{widget.NewCommand("file", "File", nil), help}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	bar := widget.NewMenuBar(m, widget.WithCornerWidget(widget.NewText("[dock]")))
	h := startApp(t, widget.NewOverlayHost(bar), 40, 3)
	defer h.stop()
	h.settle()
	row := strings.TrimRight(h.row(0), " ")
	if !strings.HasSuffix(row, "[dock]") || !strings.Contains(row, "File") ||
		strings.Index(row, "Help") > strings.Index(row, "[dock]") || !strings.HasSuffix(strings.TrimSpace(strings.TrimSuffix(row, "[dock]")), "Help") {
		t.Fatalf("the bar: %q; want File, then Help just before the corner at the right end", row)
	}
	h.tb.InjectResize(13, 3)
	h.settle()
	if row := h.row(0); !strings.Contains(row, "File") || !strings.Contains(row, "Help") {
		t.Fatalf("a narrow bar: %q; want its menus whole", row)
	}
}

// TestARaisableHiddenOrRemovedIsGone: a Raisable hidden (Qt's visible: false) hides its layer, so
// nothing of the page is drawn or clicked, while the panel beside it, the control, still is; shown
// again, the page is back. Removed from the tree, its layer leaves the host, once; put back, it
// returns.
func TestARaisableHiddenOrRemovedIsGone(t *testing.T) {
	page, pageBtn := pageOf(20)
	root := tui.NewFlex(tui.Vertical)
	root.AddWeighted(page, 1)
	host := widget.NewOverlayHost(root)
	panel, _ := panelFloat("P", tui.Rect{X: 24, Y: 2, W: 16, H: 6})
	host.Attach(panel)
	h := startApp(t, host, 40, 12)
	defer h.stop()
	h.onLoop(func() { panel.Show() })
	h.settle()
	h.wantContains("page-btn")
	shown := func() (on bool) { h.onLoop(func() { on = page.Float().Shown() }); return on }

	h.onLoop(func() { page.SetVisible(false) })
	h.settle()
	if shown() {
		t.Fatal("a hidden Raisable's layer is still shown")
	}
	h.wantNotContains("page-btn")
	h.wantContains("P-btn") // the control: the panel is drawn in the same frame
	h.inject(clickUp(12, 5)...)
	h.settle()
	if focusedOn(t, h, pageBtn) {
		t.Fatal("a click where the hidden page was reached it")
	}

	h.onLoop(func() { page.SetVisible(true) })
	h.settle()
	if !shown() {
		t.Fatal("the Raisable shown again left its layer hidden")
	}
	h.wantContains("page-btn")

	h.onLoop(func() { root.Remove(page) })
	h.settle()
	if o := layerOrder(h, host, page.Float()); o[0] != -1 {
		t.Fatalf("a removed Raisable's layer is still the host's, at %d", o[0])
	}
	h.wantNotContains("page-btn")
	h.wantContains("P-btn")
	h.onLoop(func() { root.AddWeighted(page, 1) })
	h.settle()
	if o := layerOrder(h, host, page.Float()); o[0] < 0 || !shown() {
		t.Fatalf("the Raisable put back: layer at %d, shown %v", o[0], shown())
	}
	h.wantContains("page-btn")
}

// TestARaisedPageStaysBeneathADialog: with a dialog open over the page and a panel, raising the
// page (as the keyboard entering it would) leaves the dialog, and a topmost notification, above it.
func TestARaisedPageStaysBeneathADialog(t *testing.T) {
	page, _ := pageOf(20)
	host := widget.NewOverlayHost(page)
	panel, _ := panelFloat("P", tui.Rect{X: 24, Y: 2, W: 16, H: 6})
	host.Attach(panel)
	h := startApp(t, host, 40, 12)
	defer h.stop()
	toasts := widget.NewToasts(widget.WithToastWidth(12))
	h.onLoop(func() {
		panel.Show()
		host.AttachTopmost(toasts.Float())
		toasts.Post(widget.Toast{Text: "note", Ongoing: true})
	})
	h.settle()
	m := widget.NewModal(widget.NewText("dialog"), widget.WithModalTitle("D"), widget.WithScrim(true))
	openOn(t, h, m, host)
	h.onLoop(func() { host.Raise(panel); host.Raise(page.Float()) })
	h.settle()
	o := layerOrder(h, host, panel, page.Float(), m, toasts.Float())
	if o[1] < o[0] || o[2] < o[1] || o[3] < o[2] {
		t.Fatalf("layers %v (panel, page, dialog, toasts); want the page above the panel, beneath the dialog and the notification", o)
	}
	if host.TopModal() != m {
		t.Fatal("the dialog is no longer the top modal")
	}
	h.onLoop(func() { m.Dismiss(widget.DismissProgrammatic) })
}

// TestTheMenuBarsCornerIsReplacedAndRemoved: SetCorner swaps the corner of a mounted bar (the old
// one unmounted, the new one drawn), keeps one set twice, and takes it away with nil; a vertical
// bar puts the corner at its foot.
func TestTheMenuBarsCornerIsReplacedAndRemoved(t *testing.T) {
	m := widget.NewMenu()
	if err := m.SetModel([]widget.MenuItemModel{widget.NewCommand("file", "File", nil)}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	first, second := widget.NewText("[one]"), widget.NewText("[two]")
	bar := widget.NewMenuBar(m, widget.WithCornerWidget(first))
	h := startApp(t, widget.NewOverlayHost(bar), 40, 3)
	defer h.stop()
	h.settle()
	h.wantContains("[one]")
	h.onLoop(func() {
		bar.SetCorner(second)
		bar.SetCorner(second) // the same one again: nothing to do
	})
	h.settle()
	h.wantContains("[two]")
	h.wantNotContains("[one]")
	var mounted bool
	h.onLoop(func() { mounted = first.Context() != nil && first.Context().Mounted() })
	if mounted || bar.Corner() != second {
		t.Fatalf("the replaced corner is still mounted (%v), or the bar's corner is not the new one", mounted)
	}
	h.onLoop(func() { bar.SetCorner(nil) })
	h.settle()
	h.wantNotContains("[two]")
	if bar.Corner() != nil {
		t.Fatal("SetCorner(nil) left a corner")
	}

	side := widget.NewMenu()
	if err := side.SetModel([]widget.MenuItemModel{widget.NewCommand("a", "A", nil)}); err != nil {
		t.Fatalf("SetModel: %v", err)
	}
	vbar := widget.NewMenuBar(side, widget.WithBarPlacement(widget.BarPlacementLeft), widget.WithCornerWidget(widget.NewText("Z")))
	h2 := startApp(t, widget.NewOverlayHost(vbar), 10, 8)
	defer h2.stop()
	h2.settle()
	if !strings.Contains(h2.row(7), "Z") || strings.Contains(h2.row(0), "Z") {
		t.Fatalf("a vertical bar's corner is not at its foot:\n%s", h2.grid())
	}
}
