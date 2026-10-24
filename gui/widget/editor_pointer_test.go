package widget

import (
	"math"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// The native editor body is an I-beam from the first whole cell past its gutter, as the paint
// that frame measured it: gui asks a view's regions after it paints, so a frame paints once and
// is asked once. With the line numbers off, past the page's padding alone; turned on, past the
// numbers on that same frame, not the next.
func TestTheNativeEditorBodyIsAnIBeamPastItsGutter(t *testing.T) {
	h := startEditor(t, 60, 8, WithCore(tuiwidget.CoreInitialText("one\ntwo")))
	frame := func(numbers bool) (got []tui.PointerRegion, want int) {
		h.onLoop(func() {
			h.e.SetLineNumbers(numbers)
			rc := gui.NewRecordingCanvas(gui.Size{W: float32(h.e.body.w) * cellW, H: float32(h.e.body.h) * cellH}, h.cell)
			v, _ := h.e.body.NativeView()
			v.(gui.View).Paint(rc) // the frame's one paint
			got = v.(gui.PointerRegionsView).PointerRegions(nil)
			want = int(math.Ceil(float64((padX + h.e.layout.gutter) / h.e.layout.cell.W)))
		})
		return got, want
	}
	plain, at := frame(false)
	if len(plain) != 1 || plain[0].Shape != tui.PointerText || plain[0].Rect.X != at {
		t.Fatalf("without numbers: %+v, want the I-beam from column %d", plain, at)
	}
	numbered, past := frame(true)
	if past <= at {
		t.Fatalf("the numbers' gutter moved the text to column %d from %d: the cell observes nothing", past, at)
	}
	if len(numbered) != 1 || numbered[0].Shape != tui.PointerText || numbered[0].Rect.X != past {
		t.Fatalf("the frame the numbers turn on: %+v, want the I-beam from column %d", numbered, past)
	}
}

// A bound HTML page's links are a hand on the first frame they are drawn: the layout gives them
// after the paint that laid them out. The cell view gives none of its own for it.
func TestABoundHTMLPagesLinksAreAHandOnTheirFirstFrame(t *testing.T) {
	v := tuiwidget.NewHTMLView()
	BindHTML(v)
	v.SetHTML([]byte(`<p>see <a href="b.md">the link</a> here</p>`))
	view := v.BoundLayout().(gui.View)
	rc := gui.NewRecordingCanvas(gui.Size{W: 40 * cellW, H: 4 * cellH}, gui.Size{W: cellW, H: cellH})
	view.Paint(rc)
	got := view.(gui.PointerRegionsView).PointerRegions(nil)
	if len(got) == 0 || got[0].Shape != tui.PointerHand || got[0].Rect.Y != 0 || got[0].Rect.X <= 0 {
		t.Fatalf("the first frame: %+v, want a hand over the link, past \"see \"", got)
	}
	if own := v.PointerShapes(nil); len(own) != 0 {
		t.Errorf("the cell view gives shapes of its own for a bound page: %+v", own)
	}
}
