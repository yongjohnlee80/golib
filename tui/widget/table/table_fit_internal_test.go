package table

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/core"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TestACellFitsItsColumnInCells: a cell is fitted to its column's width in CELLS, as the screen
// measures it, so a CJK or emoji cell leaves the next column where the header put it; elided past
// the width, and placed as the column aligns (tui-widget-data-D8).
func TestACellFitsItsColumnInCells(t *testing.T) {
	m := tui.StringWidth
	for _, c := range []struct {
		s    string
		w    int
		mode core.Elide
		a    style.Align
		want string
	}{
		{"漢字", 6, core.ElideRight, style.AlignLeft, "漢字  "},
		{"ab", 4, core.ElideRight, style.AlignRight, "  ab"},
		{"ab", 5, core.ElideRight, style.AlignCenter, " ab  "},
		{"漢字漢字", 5, core.ElideRight, style.AlignLeft, "漢字…"},
	} {
		got := fitCell(c.s, c.w, c.mode, c.a, m)
		if got != c.want || m(got) != c.w {
			t.Errorf("fitCell(%q, %d) = %q (%d cells), want %q (%d cells)", c.s, c.w, got, m(got), c.want, c.w)
		}
	}
}
