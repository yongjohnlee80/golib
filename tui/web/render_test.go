package web

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// A cell's colours reach the client as CSS: each resolved kind in its form, the default as
// nothing (the stylesheet's own). Reverse is an fg/bg exchange, not a filter, and a default
// swapped to the other side is the theme's custom property, so default-on-default reverse is still
// seen. The reverse bit is cleared from the mask and the others are kept, so a client cannot apply
// the swap twice.
func TestWireColors(t *testing.T) {
	t.Parallel()
	red := tui.CellColor{Kind: tui.CellColorRGB, R: 0xff}
	cases := []struct {
		name   string
		attrs  tui.CellAttrs
		fg, bg string
		mask   uint16
	}{
		{"default", tui.CellAttrs{}, "", "", 0},
		{"rgb fg", tui.CellAttrs{FG: tui.CellColor{Kind: tui.CellColorRGB, R: 0x0a, G: 0xff}}, "#0aff00", "", 0},
		{"ansi bg", tui.CellAttrs{BG: tui.CellColor{Kind: tui.CellColorANSI, Index: 4}}, "", "var(--a4)", 0},
		{"ansi256 fg", tui.CellAttrs{FG: tui.CellColor{Kind: tui.CellColorANSI256, Index: 200}}, "var(--a200)", "", 0},
		{"reverse swaps", tui.CellAttrs{FG: red, BG: tui.CellColor{Kind: tui.CellColorANSI, Index: 4},
			Mask: tui.AttrReverse | tui.AttrBold}, "var(--a4)", "#ff0000", uint16(tui.AttrBold)},
		{"default-on-default reverse", tui.CellAttrs{Mask: tui.AttrReverse}, "var(--bg)", "var(--fg)", 0},
	}
	for _, c := range cases {
		fg, bg, mask := wireColors(c.attrs)
		if fg != c.fg || bg != c.bg || mask != c.mask {
			t.Errorf("%s: fg %q bg %q mask %b, want %q %q %b", c.name, fg, bg, mask, c.fg, c.bg, c.mask)
		}
	}
}

func TestMetrics_Validity(t *testing.T) {
	t.Parallel()
	for _, bad := range []Metrics{{}, {CellW: 0, CellH: 10}, {CellW: 8, CellH: 0}, {CellW: -8, CellH: 10}} {
		if bad.valid() {
			t.Errorf("%+v must be refused: a zero cell size collapses the grid, and a "+
				"client that could not measure must retry rather than be handed a guess", bad)
		}
	}
	if !(Metrics{CellW: 8.4, CellH: 17}).valid() {
		t.Error("measured metrics were refused")
	}
}
