package widget

import (
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui"
	tuiwidget "github.com/yongjohnlee80/golib/tui/widget"
)

// csvPixelTable is a pixel table over a CSV document's core, with the edges and the row numbers.
func csvPixelTable(t *testing.T, text string) (*tuiwidget.DataTable, *tableLayout, *tuiwidget.EditorCore) {
	t.Helper()
	ed := tuiwidget.NewEditorCore(tuiwidget.CoreInitialText(text))
	dt, l := pixelTable(t, tuiwidget.NewTableCSV(ed, ','), tuiwidget.TableAppendEdges(), tuiwidget.TableRowNumbers(true))
	return dt, l, ed
}

func keyRune(r rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: r, Text: string(r)} }

// A cell's value is drawn on one line as TableCellLine writes it; the edge field's + heads the
// field after the last, and the edge row's + is in its gutter.
func TestATableDrawsTheValuesAndTheEdges(t *testing.T) {
	_, l, _ := csvPixelTable(t, "h,v\n\"x\ny\",1\nb,2\n")
	c := paintTable(l, 600, 300)
	if got := runTexts(l.bands[0]); !slices.Contains(got, "x⏎y") {
		t.Fatalf("a two-line value drew as %q, want x⏎y", got)
	}
	if got := runTexts(l.plus); !slices.Equal(got, []string{"+"}) {
		t.Fatalf("the edge's mark: %q", got)
	}
	n := 0
	for _, call := range calls(c, "DrawText") {
		if call.Text == l.plus.text {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("the + drew %d times, want 2: over the edge field and in the edge row's gutter", n)
	}
}

// The pointer finds the cells as the core draws them: the edge row is the row after the last and
// the edge field the field after the last; with o open, its new row is no cell and the rows after
// it are one row down.
func TestATableHitsTheEdgesAndOsRow(t *testing.T) {
	dt, l, _ := csvPixelTable(t, "h,v\na,1\nb,2\n")
	paintTable(l, 600, 300)
	at := func(drawn int) float32 { return l.rowH*float32(drawn+1) + l.rowH/2 }
	cols := l.columns(l.size.W)
	first := cols[0].x + 1
	edge, ok := l.edgeColumn(cols)
	if !ok {
		t.Fatal("the edge field is not shown at 600 px")
	}
	if p, hit := l.HitAt(first, at(2)); hit != tuiwidget.TableHitCell || p != (tuiwidget.TablePos{Row: 2, Col: 0}) {
		t.Fatalf("the edge row: %+v %v, want row 2 (Len) field 0", p, hit)
	}
	if p, hit := l.HitAt(edge.x+1, at(0)); hit != tuiwidget.TableHitCell || p != (tuiwidget.TablePos{Row: 0, Col: 2}) {
		t.Fatalf("the edge field: %+v %v, want field 2", p, hit)
	}
	if _, hit := l.HitAt(1, at(2)); hit != tuiwidget.TableHitNone {
		t.Fatalf("the edge row's gutter is %v, want nothing", hit)
	}
	dt.Core().HandleKey(keyRune('o'))
	paintTable(l, 600, 300)
	if _, hit := l.HitAt(first, at(1)); hit != tuiwidget.TableHitNone {
		t.Fatalf("o's new row is %v, want no cell", hit)
	}
	if p, hit := l.HitAt(first, at(2)); hit != tuiwidget.TableHitCell || p.Row != 1 {
		t.Fatalf("the row after o's: %+v %v, want display row 1", p, hit)
	}
}

// The open field is drawn over its cell, at least fieldMinChars wide, with its caret after the
// text typed; the edge field's cursor is filled where it is drawn.
func TestATablePaintsTheOpenFieldAndItsCaret(t *testing.T) {
	dt, l, _ := csvPixelTable(t, "h,v\na,1\nb,2\n")
	paintTable(l, 600, 300)
	core := dt.Core()
	if !core.EditCell("abc") {
		t.Fatal("the field did not open")
	}
	c := paintTable(l, 600, 300)
	col := l.columns(l.size.W)[0]
	fill := l.cursorFill()
	fills := fillsIn(c, fill)
	minW := float32(l.Measure(strings.Repeat("0", fieldMinChars)))
	var field bool
	for _, r := range fills {
		if r.X == col.x-l.pad && r.Y == l.rowH && r.W >= minW && r.H == l.rowH {
			field = true
		}
	}
	if !field {
		t.Fatalf("no field fill over row 0's first cell, at least %v wide: %+v", minW, fills)
	}
	want := col.x + l.shaper.Measure("abc", l.body).X[3]
	var caret bool
	for _, r := range fillsIn(c, l.cursorText(fill)) {
		if r.X == want && r.W >= 1 && r.H < l.rowH {
			caret = true
		}
	}
	if !caret {
		t.Fatalf("no caret at x %v after abc", want)
	}

	core.DropEdit()
	core.SetCursor(tuiwidget.TablePos{Row: 0, Col: 2}) // the edge field
	c = paintTable(l, 600, 300)
	edge, _ := l.edgeColumn(l.columns(l.size.W))
	var cursor bool
	for _, r := range fillsIn(c, l.cursorFill()) {
		if r.X == edge.x-l.pad && r.Y == l.rowH {
			cursor = true
		}
	}
	if !cursor {
		t.Fatal("the cursor on the edge field is not filled")
	}
}

// Shift+wheel's ScrollFields moves the first field shown, clamped so the edge field still shows.
func TestATableScrollsItsFieldsSideways(t *testing.T) {
	_, l, _ := csvPixelTable(t, "a,b,c\n1,2,3\n")
	paintTable(l, 600, 300)
	l.ScrollFields(1)
	if l.left != 1 {
		t.Fatalf("one right: left %d", l.left)
	}
	l.ScrollFields(10)
	if l.left != 3 {
		t.Fatalf("far right: left %d, want 3 (the edge field)", l.left)
	}
	l.ScrollFields(-10)
	if l.left != 0 {
		t.Fatalf("far left: left %d", l.left)
	}
}

// The gui's table document is the terminal's, its table drawn in pixels: an Editor takes it as
// its document view.
func TestTheGuiTableDocumentIsTheSharedOneInPixels(t *testing.T) {
	ed := tuiwidget.NewEditorCore(tuiwidget.CoreInitialText("h,v\na,1\n"))
	d := NewTableDocument(ed, ',')
	if _, ok := d.Table().BoundLayout().(*tableLayout); !ok {
		t.Fatalf("its table is laid out by %T, want the pixel layout", d.Table().BoundLayout())
	}
	var _ DocumentView = d
}

// fieldShown reports whether the open field's fill, at least fieldMinChars wide (or the room the
// width leaves), lies within the view's rows.
func fieldShown(c *gui.RecordingCanvas, l *tableLayout) bool {
	minW := min(float32(l.Measure(strings.Repeat("0", fieldMinChars))), l.size.W/2)
	for _, r := range fillsIn(c, l.cursorFill()) {
		if r.W >= minW && r.Y >= l.rowH && r.Y+r.H <= l.size.H+0.01 {
			return true
		}
	}
	return false
}

// o's field is scrolled into sight as it opens, even on the last row shown, and so is the caret's
// line of a value taller than the view; the field's first row stays shown. With room below, nothing
// scrolls.
func TestTheOpenFieldIsRevealedAsItOpens(t *testing.T) {
	for _, c := range []struct {
		name    string
		bottom  bool
		wantTop int
	}{{"room below", false, 0}, {"the last row shown", true, 1}} {
		t.Run(c.name, func(t *testing.T) {
			dt, l, _ := csvPixelTable(t, "h,v\na,1\nb,2\nc,3\nd,4\ne,5\n")
			height := l.rowH * 4 // the header and three rows
			paintTable(l, 600, height)
			row := 0
			if c.bottom {
				row = l.bodyRows() - 1
			}
			dt.Core().SetCursor(tuiwidget.TablePos{Row: row})
			paintTable(l, 600, height)
			if !dt.HandleEvent(keyRune('o')) || dt.Core().Editing() == nil {
				t.Fatal("o opened no row")
			}
			rec := paintTable(l, 600, height)
			if !fieldShown(rec, l) || l.top != c.wantTop {
				t.Fatalf("o's field: shown %v, top %d (want %d)", fieldShown(rec, l), l.top, c.wantTop)
			}
		})
	}

	dt, l, _ := csvPixelTable(t, "h,v\na,1\nb,2\nc,3\nd,4\ne,5\n")
	height := l.rowH * 4
	paintTable(l, 600, height)
	dt.Core().SetCursor(tuiwidget.TablePos{Row: 1})
	paintTable(l, 600, height)
	dt.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	for _, line := range []string{"x", "y", "z"} {
		for _, r := range line {
			dt.HandleEvent(keyRune(r))
		}
		dt.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter, Mods: tui.ModAlt}) // a newline in the value
	}
	ln, _ := dt.Core().Editing().Field.Caret()
	paintTable(l, 600, height)
	v := dt.Core().DrawnOf(1)
	if ln != 3 || l.top > v || v+ln < l.top || v+ln > l.top+l.bodyRows()-1 && l.top != v {
		t.Fatalf("a field of 4 lines from drawn row %d: top %d, caret line %d", v, l.top, ln)
	}

	// the wheel's scroll stays while the field is unchanged; a key typed reveals it again
	l.ScrollRows(-10)
	paintTable(l, 600, height)
	if l.top != 0 {
		t.Fatalf("the wheel's scroll was undone with nothing typed: top %d", l.top)
	}
	dt.HandleEvent(keyRune('w'))
	paintTable(l, 600, height)
	if l.top == 0 {
		t.Fatal("a key typed did not reveal the field again")
	}
}

// Reaching the edge field scrolls the fields so it shows, the edge's own width counted: at the
// width where the fields fit but the edge does not, at a roomy width, and at one where the edge
// shows alone; its cursor and its open field are drawn.
func TestTheEdgeFieldIsRevealedWhereItIsReached(t *testing.T) {
	for _, c := range []struct {
		name  string
		width func(l *tableLayout, dt *tuiwidget.DataTable) float32
		left  func(n int) int
	}{
		{"roomy", func(*tableLayout, *tuiwidget.DataTable) float32 { return 600 }, func(int) int { return 0 }},
		{"the fields fit, the edge not", func(l *tableLayout, dt *tuiwidget.DataTable) float32 {
			w := l.gutter() + l.pad + l.edgeWidth()/2
			for col := range len(dt.Core().Fields()) {
				w += float32(dt.Core().Width(col)) + 2*l.pad
			}
			return w
		}, func(int) int { return 1 }},
		{"the edge alone", func(l *tableLayout, _ *tuiwidget.DataTable) float32 {
			return l.gutter() + 2*l.pad + l.edgeWidth() + 1
		}, func(n int) int { return n }},
	} {
		t.Run(c.name, func(t *testing.T) {
			dt, l, _ := csvPixelTable(t, "h,v\na,1\nb,2\n")
			paintTable(l, 600, 300)
			n := len(dt.Core().Fields())
			width := c.width(l, dt)
			paintTable(l, width, 300)
			dt.Core().SetCursor(tuiwidget.TablePos{Row: 0, Col: n - 1})
			dt.HandleEvent(keyRune('l'))
			if dt.Core().Cursor().Col != n {
				t.Fatal("l did not reach the edge field")
			}
			rec := paintTable(l, width, 300)
			if _, shown := l.edgeColumn(l.columns(l.size.W)); !shown || l.left < c.left(n) || len(fillsIn(rec, l.cursorFill())) == 0 {
				t.Fatalf("the edge field's cursor at %v px: shown %v, left %d (want ≥ %d)", width, shown, l.left, c.left(n))
			}
			dt.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
			for _, r := range "abc" {
				dt.HandleEvent(keyRune(r))
			}
			rec = paintTable(l, width, 300)
			if len(fillsIn(rec, l.cursorText(l.cursorFill()))) == 0 { // the caret: an open field's alone
				t.Fatalf("the edge field's open field at %v px is not drawn", width)
			}
		})
	}
}
