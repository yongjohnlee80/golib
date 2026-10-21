package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A Table restyled as a RowStyler restyles its rows: the cursor row (row 0, under the header) wears
// the look given, and ResetStyles without it drops it again.
func TestATableRestylesItsRowsAsARowStyler(t *testing.T) {
	h, tbl := tableWithRows(t, 3, 20, 6)
	var rs widget.RowStyler = tbl
	h.onLoop(func() { rs.ResetStyles(widget.ListStyles{CursorRow: style.New().Bold(true)}) })
	h.settle()
	if a := cellAttrs(h, 1, 1); a.Mask&tui.AttrBold == 0 {
		t.Fatalf("the cursor row drawn %+v, want bold through Table.ResetStyles", a)
	}
	h.onLoop(func() { rs.ResetStyles(widget.ListStyles{}) })
	h.settle()
	if a := cellAttrs(h, 1, 1); a.Mask&tui.AttrBold != 0 {
		t.Errorf("after a reset without it the cursor row is still bold: %+v", a)
	}
}
