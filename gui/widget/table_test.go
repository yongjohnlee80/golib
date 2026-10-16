package widget

import (
	"image/color"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui/style"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// tableRows is an in-memory TableSource; with more set it is a TablePager with rows still to load.
type tableRows struct {
	fields []tuiwidget.TableField
	rows   [][]string
}

func (s *tableRows) Fields() []tuiwidget.TableField { return s.fields }
func (s *tableRows) Len() int                       { return len(s.rows) }
func (s *tableRows) Cell(r, c int) string           { return s.rows[r][c] }

type moreRows struct{ *tableRows }

func (moreRows) More() bool    { return true }
func (moreRows) FetchMore(int) {}

// cities is n rows of an id, a name, a city and a right-aligned number.
func cities(n int) *tableRows {
	s := &tableRows{fields: []tuiwidget.TableField{{Title: "id"}, {Title: "name"}, {Title: "city"},
		{Title: "n", Align: style.AlignRight}}}
	for i := range n {
		s.rows = append(s.rows, []string{strconv.Itoa(i), "row" + strconv.Itoa(i), "Seoul", strconv.Itoa(i * 10)})
	}
	return s
}

// pixelTable is a DataTable over src bound to its pixel layout.
func pixelTable(t *testing.T, src tuiwidget.TableSource, opts ...tuiwidget.TableOption) (*tuiwidget.DataTable, *tableLayout) {
	t.Helper()
	dt := tuiwidget.NewDataTable(tuiwidget.NewTableCore(src, opts...))
	BindTable(dt)
	BindTable(nil) // nothing to bind
	l, ok := dt.BoundLayout().(*tableLayout)
	if !ok {
		t.Fatalf("BindTable bound a %T", dt.BoundLayout())
	}
	return dt, l
}

// paintTable paints l once on a recording canvas of w × h at 14px text, light on dark.
func paintTable(l *tableLayout, w, h float32) *gui.RecordingCanvas {
	c := gui.NewRecordingCanvas(gui.Size{W: w, H: h}, gui.Size{W: 8, H: 16})
	c.TextPx = 14
	c.Colors = func(int, int) (color.NRGBA, color.NRGBA) {
		return color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}, color.NRGBA{R: 0x11, G: 0x11, B: 0x11, A: 0xff}
	}
	l.Paint(c)
	return c
}

// calls are the recording's calls of op.
func calls(c *gui.RecordingCanvas, op string) []gui.PaintCall {
	var out []gui.PaintCall
	for _, call := range c.Calls {
		if call.Op == op {
			out = append(out, call)
		}
	}
	return out
}

// fillsIn are the recording's fills in col, in order.
func fillsIn(c *gui.RecordingCanvas, col color.NRGBA) []gui.Rect {
	var out []gui.Rect
	for _, f := range calls(c, "FillRect") {
		if f.Brush.Color == col {
			out = append(out, f.Rect)
		}
	}
	return out
}

// runTexts are a band's runs' texts.
func runTexts(b *tableBand) []string {
	var out []string
	for _, r := range b.runs {
		out = append(out, r.Text)
	}
	return out
}

// Each row band is one draw: its cells shaped together into one layout, drawn by one DrawText,
// with a fill each for the page, the header's tint and rule and the cursor; nothing per cell. A
// second paint shapes nothing, and a changed cell shapes its row's band alone.
func TestATableRowBandIsOneDraw(t *testing.T) {
	src := cities(30)
	dt, l := pixelTable(t, src)
	c := paintTable(l, 600, 200)
	body := l.bodyRows()
	if l.rowH != 24 || body != 7 {
		t.Fatalf("rows %v high, %d shown whole; want 24 and 7 under a 24 header in 200", l.rowH, body)
	}
	perBand := map[int]int{}
	for _, call := range calls(c, "DrawText") {
		perBand[int(call.Rect.Y/l.rowH)]++
	}
	for band := 0; band <= body+1; band++ {
		if perBand[band] != 1 {
			t.Errorf("row band %d drew %d times, want once (bands %v)", band, perBand[band], perBand)
		}
	}
	if n := len(calls(c, "DrawText")); n != body+2 {
		t.Errorf("%d DrawText calls, want %d: the header, %d rows and the one cut by the edge", n, body+2, body)
	}
	if n := len(calls(c, "FillRect")); n != 4 {
		t.Errorf("%d fills, want 4: the page, the header's tint and rule, the cursor", n)
	}
	if n := len(c.Calls) - len(calls(c, "DrawText")) - len(calls(c, "FillRect")); n != 0 {
		t.Errorf("%d other calls: %+v", n, c.Calls)
	}
	if got := runTexts(l.bands[0]); strings.Join(got, "|") != "0|row0|Seoul|0" {
		t.Errorf("the first row's runs %q, want its four cells", got)
	}

	head, rows := l.header.text, map[int]*gui.TextLayout{}
	for r, b := range l.bands {
		rows[r] = b.text
	}
	paintTable(l, 600, 200)
	if l.header.text != head {
		t.Error("an unchanged header was shaped again")
	}
	for r, b := range l.bands {
		if b.text != rows[r] {
			t.Errorf("unchanged row %d was shaped again", r)
		}
	}
	src.rows[2][1] = "wor2" // no wider: the widths stay
	dt.Core().Changed()
	paintTable(l, 600, 200)
	if l.bands[2].text == rows[2] || runTexts(l.bands[2])[1] != "wor2" {
		t.Errorf("the changed row was not shaped again: %q", runTexts(l.bands[2]))
	}
	if l.bands[3].text != rows[3] {
		t.Error("a row that did not change was shaped again")
	}
}

// The cursor's cell and a selection are filled behind the text, and the sorted field's title
// carries its mark: ▲ or ▼, and "(loaded rows)" while rows remain to load and there is room.
func TestATableDrawsTheCursorTheSelectionAndTheSortMark(t *testing.T) {
	dt, l := pixelTable(t, cities(30))
	core := dt.Core()
	paintTable(l, 600, 200)
	core.SetCursor(tuiwidget.TablePos{Row: 2, Col: 1})
	c := paintTable(l, 600, 200)
	cols := l.columns(600)
	want := gui.Rect{X: cols[1].x - l.pad, Y: 3 * l.rowH, W: cols[1].w + 2*l.pad, H: l.rowH}
	var cursor bool
	for _, f := range calls(c, "FillRect") {
		if f.Brush.Color == l.blurred { // no App gives the table the keys
			cursor = true
			if f.Rect != want {
				t.Errorf("the cursor filled %+v, want row 2 field 1's %+v", f.Rect, want)
			}
		}
	}
	if !cursor {
		t.Error("no fill in the cursor's colour")
	}
	core.Select(tuiwidget.TableSelectBlock, tuiwidget.TablePos{Row: 1, Col: 0})
	c = paintTable(l, 600, 200)
	sel := gui.Rect{Y: 2 * l.rowH, W: cols[1].x + cols[1].w + l.pad, H: 2 * l.rowH}
	if got := fillsIn(c, l.selection); len(got) != 1 || got[0] != sel {
		t.Errorf("the block of rows 1-2, fields 0-1 filled %+v, want one fill at %+v", got, sel)
	}
	core.SetAnchor(nil)

	core.SortBy(3, true)
	core.SetCursor(tuiwidget.TablePos{})
	paintTable(l, 600, 200)
	cols = l.columns(600)
	if got := runTexts(l.header); strings.Join(got, "|") != "id|name|city|n|▼" {
		t.Errorf("header %q: want every title and the n field's mark", got)
	}
	if mark := l.header.runs[4]; mark.X < cols[3].x+cols[3].w || mark.X+l.width(mark.Text, l.head) > cols[3].x+cols[3].w+2*l.pad+0.5 {
		t.Errorf("the mark at %v, want in the gap after the n field (%v to %v)", mark.X, cols[3].x+cols[3].w, cols[3].x+cols[3].w+2*l.pad)
	}
	if got := runTexts(l.bands[0]); got[0] != "29" {
		t.Errorf("sorted by n descending, the first row is %q", got)
	}

	wide := cities(3)
	wide.fields[1].Width = 40 // room for the note
	dt, l = pixelTable(t, moreRows{wide})
	dt.Core().SortBy(1, false)
	paintTable(l, 900, 200)
	if got := runTexts(l.header); strings.Join(got, "|") != "id|name (loaded rows)|city|n|▲" {
		t.Errorf("a sort of a partial result: header %q, want the note in the title", got)
	}
	dt.Core().SortBy(0, false) // a field too narrow for the note: its title and the mark
	paintTable(l, 900, 200)
	if got := runTexts(l.header); strings.Join(got, "|") != "id|name|city|n|▲" {
		t.Errorf("a narrow field's header %q, want its title and the mark", got)
	}
}

// The core's moves reveal through the layout: down past the rows shown scrolls them, right past
// the width scrolls the fields, and the page key moves the rows shown. Before the first paint
// the layout has no size and reveals at the paint.
func TestATableRevealsTheCursor(t *testing.T) {
	dt, l := pixelTable(t, cities(30))
	core := dt.Core()
	core.SetCursor(tuiwidget.TablePos{Row: 20})
	if l.top != 0 {
		t.Errorf("a reveal before the first paint scrolled to %d", l.top)
	}
	paintTable(l, 600, 200)
	if l.top != 14 {
		t.Errorf("the first paint shows rows from %d, want 14: row 20 at the bottom of 7", l.top)
	}
	if got := runTexts(l.bands[14])[0]; got != "14" {
		t.Errorf("the first row band shows %q", got)
	}
	if l.PageRows() != 6 {
		t.Errorf("a page is %d rows, want 6", l.PageRows())
	}
	core.SetCursor(tuiwidget.TablePos{Row: 2})
	if l.top != 2 {
		t.Errorf("up to row 2: rows from %d", l.top)
	}

	many := &tableRows{}
	for i := range 8 {
		many.fields = append(many.fields, tuiwidget.TableField{Title: "field" + strconv.Itoa(i), Width: 10})
	}
	many.rows = [][]string{{"a", "b", "c", "d", "e", "f", "g", "h"}}
	dt, l = pixelTable(t, many)
	paintTable(l, 300, 120)
	dt.Core().Move(0, 7)
	if l.left == 0 || !l.fits(l.left, 7) || l.fits(l.left-1, 7) {
		t.Errorf("the last field: first shown %d, want the first that keeps field 7 in 300px", l.left)
	}
	paintTable(l, 300, 120)
	if got := runTexts(l.header)[0]; got != "field"+strconv.Itoa(l.left) {
		t.Errorf("the header starts at %q, want field %d", got, l.left)
	}
	dt.Core().Move(0, -7)
	if l.left != 0 {
		t.Errorf("back to the first field: first shown %d", l.left)
	}
}

// Rows that go from under a scrolled view (a filter, a new result) leave as many as fit in sight:
// the cursor's row is not left alone at the top with the rows before it scrolled off.
func TestATableScrollsBackWhenRowsGo(t *testing.T) {
	src := cities(30)
	dt, l := pixelTable(t, src)
	core := dt.Core()
	paintTable(l, 600, 200) // 7 rows
	core.SetCursor(tuiwidget.TablePos{Row: 29})
	if l.top != 23 {
		t.Fatalf("at the last row: rows from %d, want 23", l.top)
	}
	src.rows = src.rows[26:] // four rows left
	core.Changed()
	core.SetCursor(tuiwidget.TablePos{Row: 3})
	paintTable(l, 600, 200)
	if l.top != 0 {
		t.Errorf("after rows went: rows from %d, want 0, all four in sight", l.top)
	}
	if got := runTexts(l.bands[0])[0]; got != "26" {
		t.Errorf("the first row band shows %q, want 26", got)
	}
}

// Widths are the layout's pixels: a measured field is as wide as its widest text, a declared one
// as its cells of digits, a long cell is cut with an ellipsis, a right-aligned cell ends at its
// field's edge, and a larger text size measures them again.
func TestATableMeasuresItsWidthsInPixels(t *testing.T) {
	src := cities(3)
	src.fields = append(src.fields, tuiwidget.TableField{Title: "d", Width: 6})
	src.fields[2].Title = "city"
	src.rows[1][2] = strings.Repeat("Gyeongsangbuk-do ", 6)
	for i := range src.rows {
		src.rows[i] = append(src.rows[i], "x")
	}
	dt, l := pixelTable(t, src)
	core := dt.Core()
	paintTable(l, 1600, 200)
	if w, want := core.Width(1), max(l.Measure("name"), l.Measure("row0"), l.Measure("row1"), l.Measure("row2")); w != want || w < 20 {
		t.Errorf("the name field is %d wide, want its widest text's %d pixels", w, want)
	}
	if w, want := core.Width(4), l.Measure("000000"); w != want {
		t.Errorf("a field declared 6 wide is %d, want six digits' %d", w, want)
	}
	cols := l.columns(1600)
	if x := l.bands[0].runs[1].X; x != cols[1].x || cols[1].x != 3*l.pad+float32(core.Width(0)) {
		t.Errorf("the name field's text at %v, want after the id field and its padding (%v)", x, cols[1].x)
	}
	n := l.bands[2].runs[3]
	if end := n.X + l.width(n.Text, l.body); abs32(end-(cols[3].x+cols[3].w)) > 0.5 {
		t.Errorf("a right-aligned cell ends at %v, want its field's edge %v", end, cols[3].x+cols[3].w)
	}
	long := l.bands[1].runs[2]
	if !strings.HasSuffix(long.Text, "…") || l.width(long.Text, l.body) > cols[2].w {
		t.Errorf("a long cell drew %q, %v wide in a field %v wide", long.Text, l.width(long.Text, l.body), cols[2].w)
	}
	before := core.Width(1)
	c := gui.NewRecordingCanvas(gui.Size{W: 1600, H: 200}, gui.Size{W: 8, H: 16})
	c.TextPx = 28
	l.Paint(c)
	if core.Width(1) <= before {
		t.Errorf("at twice the text size the name field is %d wide, was %d: not measured again", core.Width(1), before)
	}
	if l.rowH != 48 {
		t.Errorf("rows %v high at 28px, want 48", l.rowH)
	}
}

// The pointer is placed in pixels: the header, a cell under its row band and field, nothing past
// the last field; the wheel's rows scroll the view and stop at the last row.
func TestATablePlacesThePointer(t *testing.T) {
	_, l := pixelTable(t, cities(30))
	paintTable(l, 600, 200)
	if _, header, ok := l.CellAt(10, 5); !header || !ok {
		t.Error("a point on the header is not the header")
	}
	cols := l.columns(600)
	if p, header, ok := l.CellAt(cols[1].x+2, 3.5*l.rowH); header || !ok || p != (tuiwidget.TablePos{Row: 2, Col: 1}) {
		t.Errorf("a point in row band 3, field 1: %+v header %v ok %v; want row 2 field 1", p, header, ok)
	}
	if _, _, ok := l.CellAt(cols[3].x+cols[3].w+l.pad+1, 3*l.rowH); ok {
		t.Error("a point past the last field is a cell")
	}
	l.ScrollRows(5)
	if p, _, _ := l.CellAt(cols[0].x, 1.5*l.rowH); l.top != 5 || p.Row != 5 {
		t.Errorf("scrolled 5: rows from %d, the first band's row %d", l.top, p.Row)
	}
	l.ScrollRows(100)
	if l.top != 30-l.bodyRows() {
		t.Errorf("scrolled past the end: rows from %d, want %d", l.top, 30-l.bodyRows())
	}
	l.ScrollRows(-100)
	if l.top != 0 {
		t.Errorf("scrolled back past the top: rows from %d", l.top)
	}
}

// An empty table draws its header and its empty text, in two draws.
func TestAnEmptyTableSaysSo(t *testing.T) {
	dt, l := pixelTable(t, &tableRows{fields: []tuiwidget.TableField{{Title: "x"}}})
	c := paintTable(l, 400, 100)
	if n := len(calls(c, "DrawText")); n != 2 || strings.Join(runTexts(l.empty), "") != "No rows" {
		t.Errorf("%d draws, empty text %q; want the header and No rows", n, runTexts(l.empty))
	}
	dt.SetEmptyText("Nothing here")
	paintTable(l, 400, 100)
	if got := strings.Join(runTexts(l.empty), ""); got != "Nothing here" {
		t.Errorf("empty text %q", got)
	}
}

// HitAt tells every part of the table apart: the gutter's numbers, each field's title, a cell,
// and nothing at the gutter's corner of the header, past the last field, below the last row or
// off the view. CellAt keeps its answers, and a row's number is no cell to it.
func TestATableHitsTheGutterTheHeadersAndTheCells(t *testing.T) {
	_, l := pixelTable(t, cities(5), tuiwidget.TableRowNumbers(true))
	paintTable(l, 600, 200)
	g := l.gutter()
	cols := l.columns(600)
	if g <= 0 || cols[0].x != g+l.pad {
		t.Fatalf("a gutter %v wide, the first field at %v: want a gutter, and the field after it", g, cols[0].x)
	}
	hit := func(x, y float32) (tuiwidget.TablePos, tuiwidget.TableHit) { return l.HitAt(x, y) }
	if p, h := hit(g/2, 3.5*l.rowH); h != tuiwidget.TableHitGutter || p.Row != 2 {
		t.Errorf("over row 2's number: %+v %v, want the gutter's row 2", p, h)
	}
	for _, c := range cols {
		for _, x := range []float32{c.x - l.pad + 1, c.x + c.w/2, c.x + c.w + l.pad - 1} {
			if p, h := hit(x, l.rowH/2); h != tuiwidget.TableHitHeader || p.Col != c.col {
				t.Errorf("over field %d's title at x %v: %+v %v, want its header", c.col, x, p, h)
			}
		}
	}
	if p, h := hit(cols[2].x+2, 3.5*l.rowH); h != tuiwidget.TableHitCell || p != (tuiwidget.TablePos{Row: 2, Col: 2}) {
		t.Errorf("over row 2 field 2: %+v %v, want that cell", p, h)
	}
	end := cols[3].x + cols[3].w + l.pad
	for _, pt := range [][2]float32{{g / 2, l.rowH / 2}, {end + 1, 2.5 * l.rowH}, {end + 1, l.rowH / 2},
		{cols[1].x, 6.5 * l.rowH}, {-1, 2.5 * l.rowH}, {cols[1].x, -1}} {
		if p, h := hit(pt[0], pt[1]); h != tuiwidget.TableHitNone {
			t.Errorf("at %v: %+v %v, want nothing", pt, p, h)
		}
	}
	// past the painted view: nothing, and no field beyond the ones painted
	_, ln := pixelTable(t, cities(5), tuiwidget.TableRowNumbers(true))
	paintTable(ln, 100, 100)
	shown := ln.columns(100)
	for _, pt := range [][2]float32{{120, ln.rowH / 2}, {120, 2.5 * ln.rowH}, {50, 120}, {120, 120}, {100, ln.rowH / 2}} {
		if p, h := ln.HitAt(pt[0], pt[1]); h != tuiwidget.TableHitNone {
			t.Errorf("a 100x100 view at %v: %+v %v, want nothing past what is painted", pt, p, h)
		}
	}
	if last := shown[len(shown)-1]; last.col >= 3 {
		t.Fatalf("a 100-pixel view paints field %d: the case below proves nothing", last.col)
	}
	// CellAt, which is not bounded by the view, still names only a painted field: just past the last
	// painted field's end is where the first unpainted field would begin
	last := shown[len(shown)-1]
	past := last.x + last.w + ln.pad + 1
	if p, header, ok := ln.CellAt(past, 2.5*ln.rowH); ok || header {
		t.Errorf("CellAt at x %v, past field %d (the last painted) in a 100-pixel view: %+v %v %v, want no field", past, last.col, p, header, ok)
	}
	if _, header, ok := l.CellAt(cols[2].x, l.rowH/2); !header || !ok {
		t.Error("CellAt over the header is not the header")
	}
	if p, header, ok := l.CellAt(cols[1].x, 2.5*l.rowH); header || !ok || p != (tuiwidget.TablePos{Row: 1, Col: 1}) {
		t.Errorf("CellAt over row 1 field 1: %+v %v %v", p, header, ok)
	}
	if _, _, ok := l.CellAt(g/2, 2.5*l.rowH); ok {
		t.Error("CellAt answers a cell over a row's number")
	}
}

// The gutter draws each row's number from 1 in the header's font and colour, right-aligned, on its
// own tint, the header's corner blank; every field moves right by it; and it widens as the rows
// reach a digit more, 9 to 10, moving the fields with it.
func TestATableGutterNumbersTheRowsAndWidens(t *testing.T) {
	src := cities(9)
	dt, l := pixelTable(t, src, tuiwidget.TableRowNumbers(true))
	c := paintTable(l, 600, 400)
	g9 := l.gutter()
	if want := float32(l.Measure("0")) + 2*l.pad; g9 != want {
		t.Fatalf("9 rows: a gutter %v wide, want one digit and its padding, %v", g9, want)
	}
	numbers := func(c *gui.RecordingCanvas, g float32) {
		t.Helper()
		for r := range dt.Core().Len() {
			b := l.nums[r]
			if b == nil || strings.Join(runTexts(b), "") != strconv.Itoa(r+1) {
				t.Fatalf("row %d's number band %v, want %d", r, b, r+1)
			}
			if end := b.runs[0].X + l.width(b.runs[0].Text, l.head); abs32(end-(g-l.pad)) > 0.5 {
				t.Errorf("row %d's number ends at %v, want a pad before the gutter's edge %v", r, end, g-l.pad)
			}
			drawn := false
			for _, d := range calls(c, "DrawText") {
				if d.Text == b.text {
					drawn = true
					if d.Brush.Color != l.muted || int(d.Rect.Y/l.rowH) != r+1 {
						t.Errorf("row %d's number drawn in %v in band %d, want the header's %v in band %d", r, d.Brush.Color, int(d.Rect.Y/l.rowH), l.muted, r+1)
					}
				}
			}
			if !drawn {
				t.Errorf("row %d's number was not drawn", r)
			}
		}
		if x := l.header.runs[0].X; x < g {
			t.Errorf("the header's first run at %v, inside the gutter %v: its corner is blank", x, g)
		}
		if cols := l.columns(600); cols[0].x != g+l.pad || l.bands[0].runs[0].X != cols[0].x {
			t.Errorf("the first field at %v, its text at %v; want both after the gutter, %v", cols[0].x, l.bands[0].runs[0].X, g+l.pad)
		}
		if f := fillsIn(c, l.tint); len(f) != 2 || f[1] != (gui.Rect{Y: l.rowH, W: g, H: l.rowH * float32(dt.Core().Len())}) {
			t.Errorf("the tints %+v, want the header's and the gutter's under its rows", f)
		}
	}
	numbers(c, g9)

	src.rows = append(src.rows, []string{"9", "row9", "Seoul", "90"})
	dt.Core().Changed()
	c = paintTable(l, 600, 400)
	g10 := l.gutter()
	if want := float32(l.Measure("00")) + 2*l.pad; g10 != want || g10 <= g9 {
		t.Fatalf("10 rows: a gutter %v wide, want two digits' %v, wider than 9 rows' %v", g10, want, g9)
	}
	numbers(c, g10)

	dt.Core().SetRowNumbers(false)
	c = paintTable(l, 600, 400)
	if l.gutter() != 0 || l.nums != nil || l.columns(600)[0].x != l.pad || len(fillsIn(c, l.tint)) != 1 {
		t.Errorf("numbers off: a gutter %v, numbers %v; want none", l.gutter(), l.nums)
	}
}

// The selection is painted cell by cell, as InSelection decides: a Cells selection over two rows
// is the anchor's row from its field and the cursor's row up to its field, not the rectangle
// between them; Rows is the whole rows; a Block the rectangle. Each is a fill per run, a run the
// row above has too grown into one fill.
func TestATablePaintsEachSelectionKind(t *testing.T) {
	dt, l := pixelTable(t, cities(30))
	core := dt.Core()
	paintTable(l, 600, 200)
	cols := l.columns(600)
	left, right := func(i int) float32 { return cols[i].x - l.pad }, func(i int) float32 { return cols[i].x + cols[i].w + l.pad }
	band := func(row, rows int, from, to int) gui.Rect {
		return gui.Rect{X: left(from), Y: l.rowH * float32(row+1), W: right(to) - left(from), H: l.rowH * float32(rows)}
	}
	cases := []struct {
		name   string
		kind   tuiwidget.TableSelectKind
		anchor tuiwidget.TablePos
		cursor tuiwidget.TablePos
		want   []gui.Rect
	}{
		{"Cells over two rows", tuiwidget.TableSelectCells, tuiwidget.TablePos{Row: 1, Col: 2}, tuiwidget.TablePos{Row: 2, Col: 1},
			[]gui.Rect{band(1, 1, 2, 3), band(2, 1, 0, 1)}},
		{"Cells over four rows", tuiwidget.TableSelectCells, tuiwidget.TablePos{Row: 1, Col: 2}, tuiwidget.TablePos{Row: 4, Col: 1},
			[]gui.Rect{band(1, 1, 2, 3), band(2, 2, 0, 3), band(4, 1, 0, 1)}},
		{"Cells in one row", tuiwidget.TableSelectCells, tuiwidget.TablePos{Row: 3, Col: 2}, tuiwidget.TablePos{Row: 3, Col: 1},
			[]gui.Rect{band(3, 1, 1, 2)}},
		{"Rows", tuiwidget.TableSelectRows, tuiwidget.TablePos{Row: 3, Col: 2}, tuiwidget.TablePos{Row: 1, Col: 2},
			[]gui.Rect{band(1, 3, 0, 3)}},
		{"Block", tuiwidget.TableSelectBlock, tuiwidget.TablePos{Row: 1, Col: 2}, tuiwidget.TablePos{Row: 4, Col: 1},
			[]gui.Rect{band(1, 4, 1, 2)}},
	}
	for _, tc := range cases {
		core.SetAnchor(nil)
		core.SetCursor(tc.cursor)
		core.Select(tc.kind, tc.anchor)
		c := paintTable(l, 600, 200)
		got := fillsIn(c, l.selection)
		slices.SortFunc(got, func(a, b gui.Rect) int { return int(a.Y - b.Y) })
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: the selection filled %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// Every sort key marks its field, in key order, as the terminal's DataTable does; with more than
// one key each mark carries its key's place, and only the first key's title takes the note.
func TestATableMarksEverySortKey(t *testing.T) {
	dt, l := pixelTable(t, cities(30))
	dt.Core().SortKeys([]tuiwidget.TableSortKey{{Col: 3, Desc: true}, {Col: 1}})
	paintTable(l, 600, 200)
	cols := l.columns(600)
	if got := runTexts(l.header); strings.Join(got, "|") != "id|name|city|n|▼¹|▲²" {
		t.Fatalf("two keys: header %q, want each key's field marked, in key order", got)
	}
	for i, col := range []int{3, 1} {
		c, m := cols[col], l.header.runs[4+i]
		if m.X < c.x+c.w || m.X > c.x+c.w+2*l.pad {
			t.Errorf("key %d's mark at %v, want in the gap after field %d (%v to %v)", i+1, m.X, col, c.x+c.w, c.x+c.w+2*l.pad)
		}
	}
	dt.Core().SortKeys([]tuiwidget.TableSortKey{{Col: 2}, {Col: 0, Desc: true}, {Col: 2, Desc: true}})
	paintTable(l, 600, 200)
	if got := runTexts(l.header); strings.Join(got, "|") != "id|name|city|n|▲¹|▼²" {
		t.Errorf("a field keyed twice: header %q, want its first key's mark alone", got)
	}
	dt.Core().SortKeys([]tuiwidget.TableSortKey{{Col: 1}, {Col: 3}})
	paintTable(l, 600, 200)
	dt.Core().SortKeys([]tuiwidget.TableSortKey{{Col: 3}, {Col: 1}}) // the same marks' texts, the places swapped
	paintTable(l, 600, 200)
	for i, col := range []int{3, 1} {
		c, m := cols[col], l.header.runs[4+i]
		if m.X < c.x+c.w || m.X > c.x+c.w+2*l.pad {
			t.Errorf("places swapped: key %d's mark at %v, want after field %d (%v to %v): the old marks reused", i+1, m.X, col, c.x+c.w, c.x+c.w+2*l.pad)
		}
	}
	dt.Core().SortKeys([]tuiwidget.TableSortKey{{Col: 0}})
	paintTable(l, 600, 200)
	if got := runTexts(l.header); strings.Join(got, "|") != "id|name|city|n|▲" {
		t.Errorf("one key: header %q, want its mark with no place", got)
	}

	wide := cities(3)
	wide.fields[1].Width, wide.fields[3].Width = 40, 40
	dt, l = pixelTable(t, moreRows{wide})
	dt.Core().SortKeys([]tuiwidget.TableSortKey{{Col: 3}, {Col: 1}})
	paintTable(l, 1200, 200)
	if got := runTexts(l.header); strings.Join(got, "|") != "id|name|city|n (loaded rows)|▲¹|▲²" {
		t.Errorf("two keys over a partial result: header %q, want the note on the first key's title", got)
	}
}

// A row band stays one layout per text style: the cells' line, and its number's when the gutter
// is on; the header one. The fills are the page, the header's tint and rule, the gutter's tint,
// a fill per selected run and the cursor's: none per cell.
func TestATableRowBandStaysBatchedWithTheGutterAndASelection(t *testing.T) {
	dt, l := pixelTable(t, cities(30), tuiwidget.TableRowNumbers(true))
	core := dt.Core()
	core.SetCursor(tuiwidget.TablePos{Row: 4, Col: 1})
	core.Select(tuiwidget.TableSelectCells, tuiwidget.TablePos{Row: 1, Col: 2})
	c := paintTable(l, 600, 200)
	body := l.bodyRows()
	perBand := map[int]int{}
	for _, call := range calls(c, "DrawText") {
		perBand[int(call.Rect.Y/l.rowH)]++
	}
	for band := 0; band <= body+1; band++ {
		want := 2 // the cells and the number
		if band == 0 {
			want = 1 // the titles; the gutter's corner is blank
		}
		if perBand[band] != want {
			t.Errorf("row band %d drew %d times, want %d (bands %v)", band, perBand[band], want, perBand)
		}
	}
	// the page, the header's tint and rule, the gutter, three runs (row 1 from field 2, rows 2-3
	// whole, row 4 to field 1) and the cursor
	if n := len(calls(c, "FillRect")); n != 8 {
		t.Errorf("%d fills, want 8: %+v", n, calls(c, "FillRect"))
	}
	if n := len(c.Calls) - len(calls(c, "DrawText")) - len(calls(c, "FillRect")); n != 0 {
		t.Errorf("%d other calls: %+v", n, c.Calls)
	}
}
