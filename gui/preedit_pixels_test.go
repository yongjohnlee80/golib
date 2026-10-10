package gui

import (
	"image"
	"image/color"
	"testing"

	"gioui.org/gpu/headless"
	"gioui.org/io/key"
	"gioui.org/op"
	"gioui.org/unit"
)

// The text being composed is drawn on the text's own page, not the window theme's: with a light
// page at the caret, the preedit's box is that page, where it used to be the theme's dark box.
func TestThePreeditIsDrawnInTheTextsColours(t *testing.T) {
	b := NewBackend()
	b.gio.ime.edit(key.EditEvent{Text: "한"})
	b.gio.ime.compose(key.CompositionEvent{Start: 0, End: 1})
	fs := b.font.Load()
	m := measure(fs.fm, fs.size, 0, image.Pt(200, 60), unit.Metric{PxPerDp: 1, PxPerSp: 1})
	hw, err := headless.NewWindow(m.window.X, m.window.Y)
	if err != nil {
		t.Skipf("no headless GPU here: %v", err)
	}
	defer hw.Release()
	page := color.NRGBA{R: 0xe8, G: 0xdc, B: 0xc0, A: 0xff}
	ink := color.NRGBA{R: 0x30, G: 0x28, B: 0x20, A: 0xff}
	if page == b.cfg.theme.BG {
		t.Fatal("the page must differ from the theme's background for this cell to observe anything")
	}
	caret := image.Rect(10, 10, 10+m.cell.X, 10+m.cell.Y)
	ops := new(op.Ops)
	b.drawPreedit(ops, &frame{caret: caret, base: m.baseline, fg: ink, bg: page, tinted: true}, m, fs)
	if err := hw.Frame(ops); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rectangle{Max: m.window})
	if err := hw.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	// the box's top-left corner is background: above the glyph, left of its ink
	got := img.RGBAAt(caret.Min.X, caret.Min.Y)
	if got.R != page.R || got.G != page.G || got.B != page.B {
		t.Fatalf("the preedit's box at its corner is %v, want the page %v (theme background %v)", got, page, b.cfg.theme.BG)
	}
}
