package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// A DataTable reports its scroll position as a backend's scrollbar needs it: the first row shown,
// the rows its body holds (the titles and their rule not counted), and the rows there are. A wheel
// notch moves the first row by the table's own step.
func TestADataTableReportsWhereItIsScrolled(t *testing.T) {
	h, _, dt := dataTable(t, numbered(20), 30, 8) // 8 lines: 2 for the titles and rule, 6 for rows
	state := func() (s [3]int) {
		h.onLoop(func() { s[0], s[1], s[2] = dt.ScrollState() })
		return s
	}
	if got := state(); got != [3]int{0, 6, 20} {
		t.Errorf("unscrolled: ScrollState %v, want [0 6 20]", got)
	}
	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 5, Y: 4})
	h.settle()
	if got := state(); got != [3]int{3, 6, 20} {
		t.Errorf("a notch down: ScrollState %v, want [3 6 20]", got)
	}
}

// scrollingLayout is a bound pixel layout that scrolls itself and says where.
type scrollingLayout struct{ hittingTable }

func (*scrollingLayout) ScrollState() (int, int, int) { return 7, 5, 99 }

// A bound layout that reports its own scroll position answers for the table.
func TestADataTableBoundToAScrollingLayoutReportsTheLayouts(t *testing.T) {
	h, dt := boundTableApp(t, &scrollingLayout{})
	var got [3]int
	h.onLoop(func() { got[0], got[1], got[2] = dt.ScrollState() })
	if got != [3]int{7, 5, 99} {
		t.Errorf("ScrollState %v, want the layout's [7 5 99]", got)
	}
}
