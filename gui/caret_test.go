package gui

import (
	"image"
	"image/color"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

type caretView struct {
	r    Rect
	base float32
	ok   bool
}

func (caretView) Paint(Canvas)                   {}
func (v caretView) Caret() (Rect, float32, bool) { return v.r, v.base, v.ok }

// colourCaretView also reports the colours of the text at its caret.
type colourCaretView struct {
	caretView
	fg, bg color.NRGBA
}

func (v colourCaretView) CaretColors() (color.NRGBA, color.NRGBA) { return v.fg, v.bg }

func TestViewCaretInDevicePixels(t *testing.T) {
	m := metrics{origin: image.Pt(4, 6), scale: 2, cell: image.Pt(16, 32)}
	natives := []tui.NativePlacement{
		{View: "plain", X: 0, Y: 0, Cols: 3, Rows: 1},
		{View: caretView{ok: false}, X: 1, Y: 1, Cols: 3, Rows: 1},
		{View: caretView{r: Rect{X: 10, Y: 3, W: 1, H: 12}, base: 9, ok: true}, X: 2, Y: 1, Cols: 10, Rows: 2},
	}
	c, ok := viewCaret(m, natives)
	if !ok {
		t.Fatal("no caret found")
	}
	// cell (2, 1) is at 4+2*16, 6+1*32 = (36, 38); the view's (10, 3) is 2× that in device pixels
	if want := image.Rect(56, 44, 58, 68); c.rect != want || c.base != 18 {
		t.Fatalf("caret %v base %d, want %v base 18", c.rect, c.base, want)
	}
	if c.tinted {
		t.Error("a view without CaretColors reported colours")
	}
	if _, ok := viewCaret(m, natives[:2]); ok {
		t.Error("a view whose caret is off reported one")
	}
}

// A view that reports its text's colours at the caret hands them to the frame, for the text
// being composed there; the winning caret's colours, not another view's.
func TestViewCaretCarriesTheTextsColours(t *testing.T) {
	m := metrics{scale: 1, cell: image.Pt(8, 16)}
	page, ink := color.NRGBA{R: 0xe8, G: 0xdc, B: 0xc0, A: 0xff}, color.NRGBA{R: 0x30, G: 0x28, B: 0x20, A: 0xff}
	natives := []tui.NativePlacement{
		{View: colourCaretView{caretView: caretView{r: Rect{W: 1, H: 12}, ok: true}, fg: color.NRGBA{A: 0xff}, bg: color.NRGBA{B: 0xff, A: 0xff}}},
		{View: colourCaretView{caretView: caretView{r: Rect{X: 4, W: 1, H: 12}, ok: true}, fg: ink, bg: page}, X: 1},
	}
	c, ok := viewCaret(m, natives)
	if !ok || !c.tinted || c.fg != ink || c.bg != page {
		t.Fatalf("caret colours %v/%v tinted %v, want the winning view's %v/%v", c.fg, c.bg, c.tinted, ink, page)
	}
}

// The preedit takes the text's colours when they are known and opaque, else the window theme's.
func TestPreeditColorsPreferTheTextsAtTheCaret(t *testing.T) {
	themeFG, themeBG := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, color.NRGBA{B: 0x40, A: 0xff}
	page, ink := color.NRGBA{R: 0xe8, G: 0xdc, B: 0xc0, A: 0xff}, color.NRGBA{R: 0x30, A: 0xff}
	cases := []struct {
		name   string
		f      frame
		fg, bg color.NRGBA
	}{
		{"the text's", frame{fg: ink, bg: page, tinted: true}, ink, page},
		{"unknown: the theme's", frame{fg: ink, bg: page}, themeFG, themeBG},
		{"no background of its own: the theme's", frame{fg: ink, bg: color.NRGBA{}, tinted: true}, themeFG, themeBG},
		{"no ink: the theme's ink on the text's page", frame{bg: page, tinted: true}, themeFG, page},
	}
	for _, tc := range cases {
		if fg, bg := preeditColors(&tc.f, themeFG, themeBG); fg != tc.fg || bg != tc.bg {
			t.Errorf("%s: %v on %v, want %v on %v", tc.name, fg, bg, tc.fg, tc.bg)
		}
	}
}

// With the cell cursor showing, the frame's caret takes the colours its cell is drawn in: a text
// field's composition sits on the field's background, not the window theme's.
func TestTheCellCursorsCaretCarriesItsCellsColours(t *testing.T) {
	b := NewBackend()
	b.grid.resize(4, 2)
	page := tui.CellColor{Kind: tui.CellColorRGB, R: 0xe8, G: 0xdc, B: 0xc0}
	ink := tui.CellColor{Kind: tui.CellColorRGB, R: 0x30, G: 0x28, B: 0x20}
	b.grid.cells[1*4+2] = tui.Cell{Content: " ", Width: 1, Attrs: tui.CellAttrs{FG: ink, BG: page}}
	b.cursor = cursorState{visible: true, x: 2, y: 1}
	m := metrics{scale: 1, cell: image.Pt(8, 16), baseline: 12}
	var f frame
	b.frameCaret(&f, m)
	wantFG, wantBG := b.render.colors(b.grid.at(2, 1).Attrs)
	if !f.tinted || f.fg != wantFG || f.bg != wantBG {
		t.Fatalf("caret colours %v/%v tinted %v, want the cell's %v/%v", f.fg, f.bg, f.tinted, wantFG, wantBG)
	}
	if wantBG == b.cfg.theme.BG {
		t.Fatal("the cell's background must differ from the theme's for this cell to observe anything")
	}
}
