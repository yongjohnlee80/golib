package widget_test

// WrapSoft click inversion must not cross visual rows.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// A word-wrapped short row has a BLANK TAIL. A click in that tail must clamp to
// the row's own last column, never scan on into content painted on the next row.
func TestEditorWrapSoftClickStaysOnItsRow(t *testing.T) {
	h, ed, sh := focusedEditor(t, 6, 4, widget.WithEditorWrap(widget.WrapSoft))
	// Width 6, word wrap: "a" alone on row 0, "bbbbb" on row 1.
	h.onLoop(func() { ed.SetValue("a bbbbb") })
	h.barrier(sh)

	// x=4 is inside row 0's blank tail — past "a", still row 0.
	h.inject(click(4, 0))
	h.barrier(sh)

	_, _, ln, col := edState(h, ed)
	if ln != 0 || col != 0 {
		t.Errorf("click in row 0's blank tail -> (%d,%d); want (0,0), the last column "+
			"painted on that row. A column scan that runs to the end of the logical "+
			"line selects text drawn on the NEXT visual row.", ln, col)
	}
}
