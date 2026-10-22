package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// zLayout is a consumer's own cell layout: one row of Z, the width it was told.
type zLayout struct{ w int }

func (l *zLayout) SetSource([]byte)                                   {}
func (l *zLayout) Height() float32                                    { return 1 }
func (l *zLayout) At(float32, float32) widget.DocPos                  { return widget.DocPos{} }
func (l *zLayout) Rects(widget.DocPos, widget.DocPos) []widget.Rect32 { return nil }
func (l *zLayout) Text(widget.DocPos, widget.DocPos) string           { return "" }
func (l *zLayout) LinkAt(float32, float32) string                     { return "" }
func (l *zLayout) BlockAt(float32) (int, float32)                     { return -1, 0 }
func (l *zLayout) BlockTop(int) (float32, bool)                       { return 0, false }
func (l *zLayout) SetWidth(w float32)                                 { l.w = int(w) }
func (l *zLayout) PaintCells(s tui.Surface, top, w int, _, _ widget.DocPos) {
	if top == 0 {
		s.Fill(tui.Rect{W: min(w, 5), H: 1}, "Z", style.New())
	}
}

// An HTMLView paints any cell layout bound to it (HTMLLayoutCells), not only its built-in one:
// the view lays it out at its width and paints it in its cells. Before, the view drew only its own
// cell layout and filled the cells under any other with the background, for a backend to cover.
func TestAnHTMLViewPaintsABoundCellLayout(t *testing.T) {
	v := widget.NewHTMLView()
	l := &zLayout{}
	v.BindLayout(l)
	h := startApp(t, v, 20, 3)
	h.waitFor("the bound layout painted", func() bool { return strings.HasPrefix(h.row(0), "ZZZZZ") })
	var w int
	h.onLoop(func() { w = l.w })
	if w != 20 {
		t.Errorf("the layout was told width %d, want the view's 20", w)
	}
}
