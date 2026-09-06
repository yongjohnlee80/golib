package web

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// rowHTML renders one row to a string.
func rowHTML(cells ...tui.Cell) string {
	var b strings.Builder
	renderRow(&b, 0, cells)
	return b.String()
}

func wide(s string) tui.Cell { return tui.Cell{Content: s, Width: 2} }
func cont() tui.Cell         { return tui.Cell{Content: "", Width: 0} }

// A wide grapheme occupies exactly two columns, a Width-0
// continuation emits no glyph, and a mismatched font cannot shift the rest of
// the row.
func TestRender_WideGraphemeOccupiesTwoColumns(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		cells []tui.Cell
		want  []string // substrings that must appear, in order
	}{
		"CJK": {
			cells: []tui.Cell{cell("a"), wide("漢"), cont(), cell("b")},
			want: []string{
				`grid-column:1"`,
				`grid-column:2 / span 2"`,
				`grid-column:4"`,
			},
		},
		"emoji": {
			cells: []tui.Cell{wide("🙂"), cont(), cell("x")},
			want: []string{
				`grid-column:1 / span 2"`,
				`grid-column:3"`,
			},
		},
		"two wides in a row": {
			cells: []tui.Cell{wide("漢"), cont(), wide("字"), cont(), cell("!")},
			want: []string{
				`grid-column:1 / span 2"`,
				`grid-column:3 / span 2"`,
				`grid-column:5"`,
			},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := rowHTML(c.cells...)
			at := 0
			for _, want := range c.want {
				i := strings.Index(got[at:], want)
				if i < 0 {
					t.Fatalf("missing %q in order; got %q", want, got)
				}
				at += i + len(want)
			}
			// A continuation must emit NO element: an empty box would occupy a
			// track and shift the rest of the row by one column per wide
			// grapheme.
			if n := strings.Count(got, "<i "); n != len(c.want) {
				t.Errorf("%d cell elements, want %d — a continuation emitted a box", n, len(c.want))
			}
		})
	}
}

// The column arithmetic is what keeps a row aligned, so it is checked
// independently of the markup: the last cell of a row containing wide graphemes
// must land on the column the server intends.
func TestRender_ColumnArithmeticSurvivesWideCells(t *testing.T) {
	t.Parallel()
	// 10 columns: a, 漢(2), b, 🙂(2), c, d, e, f  ->  1,2-3,4,5-6,7,8,9,10
	cells := []tui.Cell{
		cell("a"), wide("漢"), cont(), cell("b"), wide("🙂"), cont(),
		cell("c"), cell("d"), cell("e"), cell("f"),
	}
	got := rowHTML(cells...)
	for _, want := range []string{
		`grid-column:1"`, `grid-column:2 / span 2"`, `grid-column:4"`,
		`grid-column:5 / span 2"`, `grid-column:7"`, `grid-column:8"`,
		`grid-column:9"`, `grid-column:10"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if strings.Contains(got, "grid-column:11") {
		t.Error("the row overflowed its 10 columns — Cell.Width was not honored exactly")
	}
}

// Cell content is application data. An App that renders a filename is not
// thinking about markup.
func TestRender_ContentIsEscaped(t *testing.T) {
	t.Parallel()
	got := rowHTML(cell("<"), cell("&"), cell(">"), cell(`"`))
	for _, raw := range []string{"<i class", "</i>"} {
		if !strings.Contains(got, raw) {
			t.Fatalf("expected markup missing: %q", got)
		}
	}
	// The content characters must be escaped, so no cell can open an element.
	inner := got
	for _, tag := range []string{`<i class="c" style="grid-column:1">`, `<i class="c" style="grid-column:2">`,
		`<i class="c" style="grid-column:3">`, `<i class="c" style="grid-column:4">`, `</i>`} {
		inner = strings.ReplaceAll(inner, tag, "")
	}
	if strings.ContainsAny(inner, "<>") {
		t.Errorf("unescaped content survived: %q", inner)
	}
	if !strings.Contains(got, "&lt;") || !strings.Contains(got, "&amp;") || !strings.Contains(got, "&gt;") {
		t.Errorf("content was not HTML-escaped: %q", got)
	}
}

// A script-shaped payload in cell content must not become a script.
func TestRender_ScriptContentCannotEscape(t *testing.T) {
	t.Parallel()
	got := rowHTML(cell("<script>alert(1)</script>"))
	if strings.Contains(got, "<script") {
		t.Fatalf("cell content produced a script element: %q", got)
	}
	if !strings.Contains(got, "&lt;script&gt;") {
		t.Errorf("payload was not escaped: %q", got)
	}
}

func TestRender_Attributes(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		attrs  tui.CellAttrs
		want   []string
		absent []string
	}{
		"plain": {absent: []string{"color:", "background:", "font-weight"}},
		"bold": {
			attrs: tui.CellAttrs{Mask: tui.AttrBold},
			want:  []string{"font-weight:700"},
		},
		"italic": {
			attrs: tui.CellAttrs{Mask: tui.AttrItalic},
			want:  []string{"font-style:italic"},
		},
		"faint": {
			attrs: tui.CellAttrs{Mask: tui.AttrFaint},
			want:  []string{"opacity:.6"},
		},
		"rgb fg": {
			attrs: tui.CellAttrs{FG: tui.CellColor{Kind: tui.CellColorRGB, R: 0x0a, G: 0xff, B: 0x00}},
			want:  []string{"color:#0aff00"},
		},
		"ansi bg": {
			attrs: tui.CellAttrs{BG: tui.CellColor{Kind: tui.CellColorANSI, Index: 4}},
			want:  []string{"background:var(--a4)"},
		},
		"ansi256": {
			attrs: tui.CellAttrs{FG: tui.CellColor{Kind: tui.CellColorANSI256, Index: 200}},
			want:  []string{"color:var(--a200)"},
		},
		// Underline and strikethrough share one CSS property, so emitting two
		// declarations would silently drop the first.
		"underline and strikethrough": {
			attrs: tui.CellAttrs{Mask: tui.AttrUnderline | tui.AttrStrikethrough},
			want:  []string{"text-decoration:underline line-through"},
		},
		// Blink is an accessibility hazard browsers dropped on purpose: the cell
		// is marked, nothing animates.
		"blink does not animate": {
			attrs:  tui.CellAttrs{Mask: tui.AttrBlink},
			want:   []string{"--blink:1"},
			absent: []string{"animation"},
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := rowHTML(tui.Cell{Content: "x", Width: 1, Attrs: c.attrs})
			for _, want := range c.want {
				if !strings.Contains(got, want) {
					t.Errorf("missing %q in %q", want, got)
				}
			}
			for _, absent := range c.absent {
				if strings.Contains(got, absent) {
					t.Errorf("unexpected %q in %q", absent, got)
				}
			}
		})
	}
}

// Reverse swaps the emitted colors rather than applying a CSS filter: a filter
// would invert whatever the theme painted behind the cell, and the terminal
// semantics are specifically an fg/bg exchange.
func TestRender_ReverseSwapsColors(t *testing.T) {
	t.Parallel()
	red := tui.CellColor{Kind: tui.CellColorRGB, R: 0xff}
	blue := tui.CellColor{Kind: tui.CellColorRGB, B: 0xff}

	got := rowHTML(tui.Cell{Content: "x", Width: 1,
		Attrs: tui.CellAttrs{FG: red, BG: blue, Mask: tui.AttrReverse}})
	if !strings.Contains(got, "color:#0000ff") || !strings.Contains(got, "background:#ff0000") {
		t.Errorf("reverse did not swap the colors: %q", got)
	}
	if strings.Contains(got, "filter") {
		t.Error("reverse used a filter, which would also invert the theme behind the cell")
	}

	// Default-on-default reverse must still be visible, so both sides resolve to
	// explicit theme tokens.
	got = rowHTML(tui.Cell{Content: "x", Width: 1, Attrs: tui.CellAttrs{Mask: tui.AttrReverse}})
	if !strings.Contains(got, "color:var(--bg)") || !strings.Contains(got, "background:var(--fg)") {
		t.Errorf("default-on-default reverse produced %q, which renders invisibly", got)
	}
}

// Identical input renders to byte-identical HTML across runs. Map
// iteration or time-dependent output would break this.
func TestRender_IsDeterministic(t *testing.T) {
	t.Parallel()
	cells := []tui.Cell{
		{Content: "a", Width: 1, Attrs: tui.CellAttrs{
			FG:   tui.CellColor{Kind: tui.CellColorRGB, R: 1, G: 2, B: 3},
			BG:   tui.CellColor{Kind: tui.CellColorANSI, Index: 7},
			Mask: tui.AttrBold | tui.AttrItalic | tui.AttrUnderline | tui.AttrStrikethrough | tui.AttrFaint,
		}},
		wide("漢"), cont(), cell("z"),
	}
	first := rowHTML(cells...)
	for range 50 {
		if got := rowHTML(cells...); got != first {
			t.Fatalf("render is not deterministic:\n%q\n%q", first, got)
		}
	}
	// Pinned golden, so a change to the emitted markup is a deliberate decision
	// rather than an accident noticed later.
	const golden = `<i class="c" style="grid-column:1;color:#010203;background:var(--a7);` +
		`font-weight:700;opacity:.6;font-style:italic;text-decoration:underline line-through;">a</i>` +
		`<i class="c" style="grid-column:2 / span 2">漢</i>` +
		`<i class="c" style="grid-column:4">z</i>`
	if first != golden {
		t.Errorf("golden mismatch\n got: %s\nwant: %s", first, golden)
	}
}

// A Width value the tui package does not produce must not corrupt the row.
func TestRender_UnexpectedWidthIsContained(t *testing.T) {
	t.Parallel()
	// Width 3 is not something tui emits, but the renderer must stay coherent:
	// the span is honored and the next cell starts after it, so the row cannot
	// silently overlap.
	got := rowHTML(tui.Cell{Content: "?", Width: 3}, cell("x"))
	if !strings.Contains(got, `grid-column:1 / span 3`) {
		t.Errorf("width was not honored: %q", got)
	}
	if !strings.Contains(got, `grid-column:4"`) {
		t.Errorf("the following cell did not start after the span: %q", got)
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
