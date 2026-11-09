package editor

import (
	"runtime"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/core"
)

// largeMarkdown is about n bytes of plain Markdown prose, 79 characters a line.
func largeMarkdown(n int) string {
	return strings.Repeat(strings.Repeat("x", 39)+" "+strings.Repeat("y", 39)+"\n", n/80+1)
}

// allocatedBy is the bytes fn allocates.
func allocatedBy(fn func()) uint64 {
	var a, b runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&a)
	fn()
	runtime.ReadMemStats(&b)
	return b.TotalAlloc - a.TotalAlloc
}

// largeSetBudget is the most a Rendered Editor may allocate per byte of a long text it is given: a
// cell a character, laid once, each row made its size. Laying the text whole at two widths, and
// growing each row by appends, cost about 2,400 bytes a byte (plain build); once, sized, about 380,
// and about 730 under -race, which allocates more for the same work.
func largeSetBudget() int {
	if raceBuild {
		return 1100
	}
	return 600
}

// A 1 MiB document set into a Rendered Editor showing 100x30 is laid once, at the width it shows
// at, its rows made their size: its allocations stay within largeSetBudget a byte of text.
func TestSettingALargeDocumentLaysItOutWithinItsBudget(t *testing.T) {
	text := largeMarkdown(1 << 20)
	e := NewEditor()
	e.SetRenderer(&MarkdownCellRenderer{MarkdownBlocks: MarkdownBlocks{HideMetadata: true}})
	e.SetView(ViewRendered)
	e.cells.w, e.cells.h, e.cells.wrap = 100, 30, core.WrapSoft
	got := allocatedBy(func() { e.SetValue(text) })
	perByte := float64(got) / float64(len(text))
	t.Logf("SetValue of %d bytes allocated %d MiB (%.0f bytes a byte of text)", len(text), got>>20, perByte)
	if perByte > float64(largeSetBudget()) {
		t.Errorf("setting %d bytes allocated %.0f bytes a byte of text, past the budget of %d: the text is laid more than once, or its rows grow by copying", len(text), perByte, largeSetBudget())
	}
	if r := e.rowMap().r; r == nil || len(r.rows) <= e.cells.h || r.key.width != e.cells.w-1 {
		t.Errorf("the long text's layout is not the scrolling one, one narrower than the view (%d): %+v", e.cells.w-1, r != nil)
	}
}

// tallerThan answers as the whole layout does, for texts shorter, as tall as and taller than the
// view, at widths that wrap them differently: the width a text is laid at never changes.
func TestTallerThanAgreesWithTheWholeLayout(t *testing.T) {
	for _, n := range []int{0, 1, 5, 9, 10, 11, 30, 200} {
		text := strings.Repeat("a few words that wrap at some widths and not at others\n\n", n)
		e := NewEditor()
		e.SetRenderer(&MarkdownCellRenderer{MarkdownBlocks: MarkdownBlocks{HideMetadata: true}})
		e.SetView(ViewRendered)
		e.SetValue(text)
		for _, w := range []int{20, 40, 57, 80} {
			rows := len(e.renderedRows(w).rows)
			for _, h := range []int{1, rows - 1, rows, rows + 1, 10} {
				if h < 1 {
					continue
				}
				e.laid = [2]*renderedLayout{} // asked fresh, blocks laid one by one
				if got, want := e.tallerThan(w, h), rows > h; got != want {
					t.Errorf("%d paragraphs at width %d, %d rows: tallerThan(%d) = %v, want %v", n, w, rows, h, got, want)
				}
				e.renderedRows(w) // and from the kept layout
				if got, want := e.tallerThan(w, h), rows > h; got != want {
					t.Errorf("%d paragraphs at width %d, kept: tallerThan(%d) = %v, want %v", n, w, h, got, want)
				}
			}
		}
	}
}
