package widget

import (
	"image/color"
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// A covered-cell test's colours: the view's own sepia, and a dark terminal panel raised over its
// first cell.
var (
	ownFG   = color.NRGBA{R: 0x5b, G: 0x46, B: 0x36, A: 0xff}
	ownBG   = color.NRGBA{R: 0xf4, G: 0xec, B: 0xd8, A: 0xff}
	panelFG = color.NRGBA{R: 0xd0, G: 0xd0, B: 0xd0, A: 0xff}
	panelBG = color.NRGBA{R: 0x1c, G: 0x1c, B: 0x1c, A: 0xff}
)

// underPanel makes c's cells the view's own, but those covered answers for, which are the panel's
// and covered.
func underPanel(c *gui.RecordingCanvas, covered func(col, row int) bool) *gui.RecordingCanvas {
	c.Covers = covered
	c.Colors = func(col, row int) (color.NRGBA, color.NRGBA) {
		if covered(col, row) {
			return panelFG, panelBG
		}
		return ownFG, ownBG
	}
	return c
}

func firstCell(col, row int) bool { return col == 0 && row == 0 }
func everyCell(int, int) bool     { return true }

// A panel's bar takes its colours from a bar cell nothing covers, and keeps those of its last paint
// while the whole bar is covered.
func TestThePanelBarIsNotThePanelOverItsColour(t *testing.T) {
	p := NewPanel(tuiwidget.NewText("x"), WithTitle("Notes"))
	v, _ := p.NativeView()
	v.(gui.View).Paint(underPanel(gui.NewRecordingCanvas(gui.Size{W: 240, H: 80}, gui.Size{W: 8, H: 16}), firstCell))
	text, fill, _ := p.BarColors()
	if (text != ownFG && text != ownBG) || fill == mix(panelFG, panelBG, 0.06) || fill == mix(panelBG, panelFG, 0.06) {
		t.Fatalf("bar text %v, fill %v: want the bar's own %v / %v, not the panel's", text, fill, ownFG, ownBG)
	}
	rc := underPanel(gui.NewRecordingCanvas(gui.Size{W: 240, H: 80}, gui.Size{W: 8, H: 16}), everyCell)
	v.(gui.View).Paint(rc)
	if len(rc.Calls) != 0 {
		t.Fatalf("a wholly covered bar drew %v", rc.Ops())
	}
	if t2, f2, _ := p.BarColors(); t2 != text || f2 != fill {
		t.Fatalf("a wholly covered bar changed BarColors to %v %v from %v %v", t2, f2, text, fill)
	}
}

// An HTML view takes the page's text and ground from a cell nothing covers, and keeps its look
// while it is wholly covered.
func TestTheHTMLPageIsNotThePanelOverItsColour(t *testing.T) {
	_, l, _ := pixelView(t, "<p>hi</p>", 160, 80)
	l.Paint(underPanel(gui.NewRecordingCanvas(gui.Size{W: 160, H: 80}, gui.Size{W: 8, H: 16}), firstCell))
	if l.fg != ownFG || l.bg != ownBG {
		t.Fatalf("page %v on %v; want its own %v on %v", l.fg, l.bg, ownFG, ownBG)
	}
	l.Paint(underPanel(gui.NewRecordingCanvas(gui.Size{W: 160, H: 80}, gui.Size{W: 8, H: 16}), everyCell))
	if l.fg != ownFG || l.bg != ownBG {
		t.Fatalf("wholly covered, the page became %v on %v; want the look it had", l.fg, l.bg)
	}
}

// With no page style, the editor's page takes its colours from a body cell nothing covers, and
// keeps them while the body is wholly covered.
func TestTheEditorPageIsNotThePanelOverItsColour(t *testing.T) {
	h := startEditor(t, 60, 8, WithCore(tuiwidget.CoreInitialText("body")))
	paint := func(covered func(col, row int) bool) (fg, bg color.NRGBA) {
		h.onLoop(func() {
			rc := gui.NewRecordingCanvas(gui.Size{W: float32(h.e.body.w) * cellW, H: float32(h.e.body.h) * cellH}, h.cell)
			v, _ := h.e.body.NativeView()
			v.(gui.View).Paint(underPanel(rc, covered))
			fg, bg = h.e.layout.th.Text, h.e.layout.th.Background
		})
		return fg, bg
	}
	if fg, bg := paint(firstCell); fg != ownFG || bg != ownBG {
		t.Fatalf("page %v on %v; want the body's own %v on %v", fg, bg, ownFG, ownBG)
	}
	if fg, bg := paint(everyCell); fg != ownFG || bg != ownBG {
		t.Fatalf("wholly covered, the page became %v on %v; want the colours it had", fg, bg)
	}
}

// Before its panel's bar has painted, the editor's mode switch takes its text from a cell of its
// own nothing covers: it draws as it would with nothing over it.
func TestTheModeSwitchIsNotThePanelOverItsColour(t *testing.T) {
	e := NewEditor(WithPanel(WithTitle("Notes")))
	colours := func(rc *gui.RecordingCanvas) []color.NRGBA {
		var out []color.NRGBA
		for _, c := range rc.Calls {
			out = append(out, c.Brush.Color)
		}
		return out
	}
	open := gui.NewRecordingCanvas(gui.Size{W: 120, H: 16}, gui.Size{W: 8, H: 16})
	open.Colors = func(int, int) (color.NRGBA, color.NRGBA) { return ownFG, ownBG }
	e.sw.paint(open)
	under := underPanel(gui.NewRecordingCanvas(gui.Size{W: 120, H: 16}, gui.Size{W: 8, H: 16}), firstCell)
	e.sw.paint(under)
	if a, b := colours(open), colours(under); len(a) == 0 || !slices.Equal(a, b) {
		t.Fatalf("under a panel the switch drew %v; want as with nothing over it, %v", b, a)
	}
}
