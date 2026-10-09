package widget

import (
	"image/color"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
)

// tableTheme is the theme the table tests lay out with.
func tableTheme() Theme {
	return Theme{Text: color.NRGBA{R: 0xee, G: 0xee, B: 0xee, A: 0xff}, Background: color.NRGBA{A: 0xff},
		Muted:          color.NRGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff},
		Prose:          gui.Font{Size: 16},
		Mono:           gui.Font{Family: gui.MonospaceFamily(), Size: 14},
		CodeBackground: color.NRGBA{R: 0x20, G: 0x20, B: 0x20, A: 0xff}}
}

// layTableAt lays the doc's first block at a given width, off the cursor.
func layTableAt(t *testing.T, doc string, width float32) BlockLayout {
	t.Helper()
	lines := strings.Split(doc, "\n")
	r := NewMarkdownRenderer()
	sh := gui.NewRecordingCanvas(gui.Size{W: 800, H: 400}, gui.Size{W: 8, H: 16}).Text()
	for _, b := range r.Blocks(lines, 0, len(lines)) {
		return r.LayOut(b, lines, width, false, sh, tableTheme(), nil)
	}
	t.Fatal("no blocks")
	return BlockLayout{}
}

// drawCallsOf paints the block's lines on a recording canvas and reports the
// DrawText calls each LineLayout produced, so a wrapped cell's continuation
// text is asserted as PAINTED, not merely laid out.
func drawCallsOf(t *testing.T, bl BlockLayout, width float32) [][]string {
	t.Helper()
	c := gui.NewRecordingCanvas(gui.Size{W: width, H: bl.Height + 4}, gui.Size{W: 8, H: 16})
	var out [][]string
	for _, ll := range bl.Lines {
		at := gui.Pt(0, ll.Y)
		ll.Para.Paint(c, at, ll.Spans)
		var calls []string
		n := len(c.Calls)
		_ = n
		out = append(out, calls)
	}
	// recount per line: paint once per LineLayout, recording the call count each time
	c2 := gui.NewRecordingCanvas(gui.Size{W: width, H: bl.Height + 4}, gui.Size{W: 8, H: 16})
	out = out[:0]
	for _, ll := range bl.Lines {
		before := len(c2.Calls)
		ll.Para.Paint(c2, gui.Pt(0, ll.Y), ll.Spans)
		var drew []string
		for _, call := range c2.Calls[before:] {
			if call.Op == "DrawText" {
				drew = append(drew, "text")
			}
		}
		out = append(out, drew)
	}
	return out
}

// A table wider than the page shares the page's width: no fragment of any line
// is drawn past the page, a wide column wraps its cells' text rather than
// overflowing, and the wrapped continuation text is PAINTED — a cell's later
// wrapped lines are drawn, not merely reserved space.
func TestAWideTableSharesThePageWidthAndPaintsItsWraps(t *testing.T) {
	doc := "| one | two |\n|---|---|\n| " + strings.Repeat("wide ", 12) + " | short |\n| a | b |"
	const width = 200
	bl := layTableAt(t, doc, width)
	if bl.Height == 0 {
		t.Fatal("the table laid out to nothing")
	}
	for _, ll := range bl.Lines {
		for _, l := range ll.Para.Lines {
			for _, f := range l.Frags {
				if f.X+f.W > width+1 {
					t.Errorf("a frag runs to %v, past the page's %v (%q)", f.X+f.W, width, ll.Spans[f.Span].Text)
				}
			}
		}
	}
	// the wide cell wraps: it laid more than the row's own line
	painted := drawCallsOf(t, bl, width)
	textLines := 0
	for _, drew := range painted {
		if len(drew) > 0 {
			textLines++
		}
	}
	// header, delimiter, two body rows, plus the wide cell's wrapped
	// continuations: the continuations must paint too
	if textLines < 5 {
		t.Errorf("%d LineLayouts painted text, want at least 5 (4 rows + the wide cell's wraps)", textLines)
	}
	// the continuation lines hold visible spans of the wide text
	continuations := 0
	for _, ll := range bl.Lines {
		for _, s := range ll.Spans {
			if !s.Hidden && strings.Contains(s.Text, "wide") {
				continuations++
				break
			}
		}
	}
	if continuations < 3 {
		t.Errorf("%d LineLayouts hold visible wide text, want at least 3 (the row and its wraps)", continuations)
	}
}

// A wrapped row's columns share its line grid: two cells that both wrap hold
// their jth lines in the same LineLayout, not one cell's wraps after the
// other's, and the row's borders span its whole wrapped height.
func TestWrappedColumnsShareTheRowLineGrid(t *testing.T) {
	wide := strings.Repeat("long ", 12)
	doc := "| " + wide + " | " + wide + " |\n|---|---|\n| " + wide + " | " + wide + " |"
	const width = 240
	bl := layTableAt(t, doc, width)
	// both body cells' wrapped lines share LineLayouts: a shared one holds two
	// non-hidden spans at different columns
	shared := 0
	for _, ll := range bl.Lines {
		var cols []int
		for _, s := range ll.Spans {
			if !s.Hidden && strings.Contains(s.Text, "long") {
				cols = append(cols, len(cols))
			}
		}
		if len(cols) > 1 {
			shared++
		}
	}
	if shared == 0 {
		t.Error("no LineLayout holds two wrapped cells' text: the columns wrap serially")
	}
	// the borders span the tallest row's full wrapped height
	tallest := float32(0)
	for _, ll := range bl.Lines {
		for _, m := range ll.Marks {
			tallest = max(tallest, m.Rect.H)
		}
	}
	if tallest < tableTheme().Prose.Size*3 { // at least three wrapped lines tall
		t.Errorf("the tallest border is %v, too short for a fully wrapped row", tallest)
	}
}

func TestWrappedTableTextStaysInsideItsRow(t *testing.T) {
	doc := "| Stage | What the user does |\n|---|---|\n| Subscribe | " + strings.Repeat("Pays via Xsolla ", 8) + " |"
	bl := layTableAt(t, doc, 220)
	if len(bl.Lines) < 5 {
		t.Fatalf("table has %d lines, want wrapped header and body", len(bl.Lines))
	}
	for row := 0; row < len(bl.Lines); {
		first := bl.Lines[row]
		rowHeight := float32(0)
		for _, mark := range first.Marks {
			rowHeight = max(rowHeight, mark.Rect.H)
		}
		if rowHeight <= 2 {
			row++
			continue
		}
		end := first.Y + rowHeight
		for row++; row < len(bl.Lines) && len(bl.Lines[row].Marks) == 0; row++ {
			if bl.Lines[row].Y >= end {
				t.Errorf("wrapped line at y=%v is outside row ending at y=%v", bl.Lines[row].Y, end)
			}
		}
	}
}

func TestWrappedTableCellKeepsTheNextColumnAtItsBorder(t *testing.T) {
	row := "| a verylongunbrokenwordthatwraps | consumer text |"
	doc := "| First | Second |\n|---|---|\n" + row
	bl := layTableAt(t, doc, 180)
	second := tableCells(row)[1]
	secondCol := clusters(row[:second.from])
	for _, ll := range bl.Lines {
		if len(ll.Marks) == 0 || len(ll.Para.Lines) == 0 {
			continue
		}
		border := float32(0)
		for _, mark := range ll.Marks {
			if mark.Rect.W == 1 && mark.Rect.X > 0 {
				border = mark.Rect.X
				break
			}
		}
		if border == 0 {
			continue
		}
		for _, frag := range ll.Para.Lines[0].Frags {
			span := ll.Spans[frag.Span]
			if span.Line == 2 && span.Col >= secondCol && !span.Hidden && frag.X < border {
				t.Fatalf("second-column text starts at %v, before border %v", frag.X, border)
			}
		}
	}
}

func TestNarrowTableColumnKeepsTwoDigitValues(t *testing.T) {
	doc := "| # | Case | Reader | Expected |\n|---|---|---|---|\n" +
		"| 10 | artist whose only release is in the distant future | autocomplete fuzzy arm | absent because the release is not yet visible |"
	bl := layTableAt(t, doc, 500)
	var valueLines int
	for _, ll := range bl.Lines {
		for _, span := range ll.Spans {
			if span.Line == 2 && !span.Hidden && (span.Text == "10" || span.Text == "1" || span.Text == "0") {
				valueLines++
			}
		}
	}
	if valueLines != 1 {
		t.Fatalf("two-digit case number occupies %d lines, want one", valueLines)
	}
}

// A table that fits is unchanged: three rows, nothing wrapped, every column at
// its natural width.
func TestAFittingTableKeepsItsColumns(t *testing.T) {
	doc := "| one | two |\n|---|---|\n| a | b |"
	bl := layTableAt(t, doc, 800)
	if len(bl.Lines) != 3 {
		t.Fatalf("a fitting table laid %d lines, want 3", len(bl.Lines))
	}
	for _, ll := range bl.Lines {
		for _, s := range ll.Spans {
			if strings.Contains(s.Text, "wide") {
				t.Error("a fitting table wrapped a line")
			}
		}
	}
}
