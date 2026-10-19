package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// menubar_corner_test.go: the MenuBar's corner widget (Qt's QMenuBar::setCornerWidget).

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
