package decl

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// syncDock makes the dock hold exactly the items it is given: two children dropped together, one
// after the other, both go, and what stays keeps its place.
func TestSyncDockDropsNeighbouringChildren(t *testing.T) {
	one, two, body := widget.NewText("alpha"), widget.NewText("bravo"), widget.NewText("charlie")
	w := &windowNode{}
	w.syncDock([]docked{{one, tui.DockTop}, {two, tui.DockTop}, {body, tui.DockCenter}})
	w.syncDock([]docked{{body, tui.DockCenter}})
	var got []tui.Component
	for _, c := range w.dock.All() {
		got = append(got, c)
	}
	if len(got) != 1 || got[0] != body {
		t.Fatalf("the dock holds %d children, want the content alone", len(got))
	}
}
