package widget

import (
	"math"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// The native editor body is an I-beam from the first whole cell past its gutter, as its paint
// measured it: with the line numbers off, past the page's padding alone; with them on, past the
// numbers too.
func TestTheNativeEditorBodyIsAnIBeamPastItsGutter(t *testing.T) {
	h := startEditor(t, 60, 8, WithCore(tuiwidget.CoreInitialText("one\ntwo")))
	shape := func(numbers bool) (got []tui.PointerRegion, want int) {
		h.onLoop(func() {
			h.e.SetLineNumbers(numbers)
			rc := gui.NewRecordingCanvas(gui.Size{W: float32(h.e.body.w) * cellW, H: float32(h.e.body.h) * cellH}, h.cell)
			v, _ := h.e.body.NativeView()
			v.(gui.View).Paint(rc)
			got = h.e.body.PointerShapes(nil)
			want = int(math.Ceil(float64((padX + h.e.layout.gutter) / h.e.layout.cell.W)))
		})
		return got, want
	}
	plain, at := shape(false)
	if len(plain) != 1 || plain[0].Shape != tui.PointerText || plain[0].Rect.X != at {
		t.Fatalf("without numbers: %+v, want the I-beam from column %d", plain, at)
	}
	numbered, past := shape(true)
	if past <= at {
		t.Fatalf("the numbers' gutter moved the text to column %d from %d: the cell observes nothing", past, at)
	}
	if len(numbered) != 1 || numbered[0].Shape != tui.PointerText || numbered[0].Rect.X != past {
		t.Fatalf("with numbers: %+v, want the I-beam from column %d", numbered, past)
	}
}
