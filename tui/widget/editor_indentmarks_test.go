package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// IndentMarked counts only a marked line's leading run of spaces and tabs. Cursor-line mode
// marks the cursor's line, and only for a focused widget.
func TestIndentMarkedCountsTheMarkedLinesLeadingRun(t *testing.T) {
	c := widget.NewEditorCore(widget.CoreInitialText("  \tx y\nz\n    w"), widget.CoreIndentMarks(widget.IndentMarksCursorLine))
	c.SetLine(0, 0)
	cases := []struct {
		ln      int
		focused bool
		want    int
		why     string
	}{
		{0, true, 3, "the cursor's line: two spaces and a tab, not the space between words"},
		{0, false, 0, "an unfocused editor is not being edited"},
		{2, true, 0, "not the cursor's line"},
	}
	for _, tc := range cases {
		if got := c.IndentMarked(tc.ln, tc.focused); got != tc.want {
			t.Errorf("IndentMarked(%d, %v) = %d, want %d: %s", tc.ln, tc.focused, got, tc.want, tc.why)
		}
	}
	c.SetLine(2, 0)
	if got := c.IndentMarked(2, true); got != 4 {
		t.Errorf("after moving to line 2, IndentMarked(2) = %d, want 4", got)
	}
	c.SetIndentMarks(widget.IndentMarksAll)
	if got, got2 := c.IndentMarked(0, false), c.IndentMarked(1, false); got != 3 || got2 != 0 {
		t.Errorf("all lines: IndentMarked(0) = %d (want 3), IndentMarked(1) = %d (want 0, no indentation)", got, got2)
	}
	c.SetIndentMarks(widget.IndentMarksOff)
	if got := c.IndentMarked(2, true); got != 0 {
		t.Errorf("off: IndentMarked(2) = %d, want 0", got)
	}
}

// The terminal editor draws the cursor line's leading spaces as a dimmed "·": the text after
// them, and the other lines, are untouched. "All" marks every line.
func TestTheEditorMarksTheCursorLinesIndentation(t *testing.T) {
	h, ed, _ := focusedEditor(t, 20, 4, widget.WithInitialText("  a b\n    c"),
		widget.WithCore(widget.CoreIndentMarks(widget.IndentMarksCursorLine)))
	h.settle()
	if got := strings.TrimRight(h.row(0), " "); got != "··a b" {
		t.Fatalf("row 0 %q, want %q:\n%s", got, "··a b", h.grid())
	}
	if got := strings.TrimRight(h.row(1), " "); got != "    c" {
		t.Fatalf("row 1 %q, want it unmarked (not the cursor's line):\n%s", got, h.grid())
	}
	snap := h.tb.Snapshot()
	if c := snap[0][0]; c.Attrs.Mask&tui.AttrFaint == 0 {
		t.Errorf("the mark is not dimmed: %+v", c.Attrs)
	}
	if c := snap[0][2]; c.Content != "a" || c.Attrs.Mask&tui.AttrFaint != 0 {
		t.Errorf("the text after the indentation is %q, dimmed=%v", c.Content, c.Attrs.Mask&tui.AttrFaint != 0)
	}
	h.onLoop(func() { ed.Core().SetIndentMarks(widget.IndentMarksAll) })
	h.settle()
	if got := strings.TrimRight(h.row(1), " "); got != "····c" {
		t.Fatalf("with all lines marked, row 1 %q, want %q:\n%s", got, "····c", h.grid())
	}
}

// A leading space that carries a combining mark is one grapheme cluster, and still a space of
// indentation, as indent.Leading (the rule auto-indent uses) counts it: it is marked, and the
// terminal editor keeps the combining mark on the "·".
func TestALeadingSpaceWithACombiningMarkIsIndentation(t *testing.T) {
	c := widget.NewEditorCore(widget.CoreInitialText(" \u0301x\n\u0301x"), widget.CoreIndentMarks(widget.IndentMarksAll))
	if got := c.IndentMarked(0, true); got != 1 {
		t.Errorf("IndentMarked(\" \\u0301x\") = %d, want 1 (the space is indentation)", got)
	}
	if got := c.IndentMarked(1, true); got != 0 {
		t.Errorf("IndentMarked(\"\\u0301x\") = %d, want 0 (no leading space)", got)
	}

	h, _, _ := focusedEditor(t, 20, 4, widget.WithInitialText(" \u0301x"),
		widget.WithCore(widget.CoreIndentMarks(widget.IndentMarksCursorLine)))
	h.settle()
	cell := h.tb.Snapshot()[0][0]
	if cell.Content != "·\u0301" || cell.Attrs.Mask&tui.AttrFaint == 0 {
		t.Fatalf("the marked cell is %q (faint %v), want \"·\\u0301\" dimmed:\n%s", cell.Content, cell.Attrs.Mask&tui.AttrFaint != 0, h.grid())
	}
}
