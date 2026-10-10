package widget

import (
	"image/color"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// The editor reports the colours of the text at its caret, for the text an input method is
// composing there: the page's background and the text's colour, or a code block's background
// when the caret is in one. Before, the backend drew the composition in the window theme's
// colours, a dark box on a light page.
func TestTheEditorReportsItsCaretsColours(t *testing.T) {
	h := startEditor(t, 60, 8, WithRenderer(NewMarkdownRenderer()),
		WithCore(tuiwidget.CoreInitialText("prose\n\n```\ncode\n```")))
	ink, page := color.NRGBA{R: 0x30, G: 0x28, B: 0x20, A: 0xff}, color.NRGBA{R: 0xe8, G: 0xdc, B: 0xc0, A: 0xff}
	// paint on a canvas whose cells are a sepia page, as a light theme's window is
	paint := func() (fg, bg color.NRGBA, th Theme) {
		h.onLoop(func() {
			rc := gui.NewRecordingCanvas(gui.Size{W: float32(h.e.body.w) * cellW, H: float32(h.e.body.h) * cellH}, h.cell)
			rc.Colors = func(int, int) (color.NRGBA, color.NRGBA) { return ink, page }
			v, _ := h.e.body.NativeView()
			v.(gui.View).Paint(rc)
			fg, bg = bodyView{b: h.e.body}.CaretColors()
			th = h.e.layout.th
		})
		return fg, bg, th
	}
	fg, bg, th := paint()
	if bg != page || fg != th.Text || fg.A == 0 {
		t.Fatalf("on the prose line: %v on %v, want the text %v on the page %v", fg, bg, th.Text, page)
	}

	h.onLoop(func() { h.e.SetMode(Rendered); h.e.core.SetLine(3, 0) })
	_, bg, th = paint()
	if th.CodeBackground.A == 0 || th.CodeBackground == page {
		t.Fatalf("the theme's code background %v must be its own for this cell to observe anything", th.CodeBackground)
	}
	if bg != th.CodeBackground {
		t.Errorf("in the rendered code block: background %v, want the block's %v (page %v)", bg, th.CodeBackground, page)
	}
}

var _ gui.CaretColorsView = bodyView{}
