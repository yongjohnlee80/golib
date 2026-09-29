package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TestAnEditorWearsItsBaseOverItsWholeArea: the cells past each line's text,
// and the rows past the document, wear the text's background as the text does
// — a preview in a dialog is one colour, not text on bands of the dialog's.
func TestAnEditorWearsItsBaseOverItsWholeArea(t *testing.T) {
	base := style.New().Background(style.ANSI(4)).Foreground(style.ANSI(7))
	ed := widget.NewEditor(widget.WithEditorStyles(widget.TextInputStyles{Text: base}))
	ed.SetValue("ab\nlonger line")
	h := startApp(t, ed, 20, 4)
	defer h.stop()
	h.settle()
	var snap [][]tui.Cell
	h.onLoop(func() { snap = h.tb.Snapshot() })
	want := snap[0][0].Attrs.BG // under the text
	if want.Kind == tui.CellColorDefault {
		t.Fatalf("fixture: the text's own background is the terminal's (%+v)", want)
	}
	for _, at := range [][2]int{{5, 0}, {18, 1}, {0, 3}, {15, 3}} { // past "ab", past "longer line", rows past the document
		if got := snap[at[1]][at[0]].Attrs.BG; got != want {
			t.Errorf("cell %d,%d wears %+v, want the text's %+v", at[0], at[1], got, want)
		}
	}
}
