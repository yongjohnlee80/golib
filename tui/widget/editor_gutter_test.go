package widget_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// TestTheEditorsLineNumbers: the gutter shows each line's number right-aligned, and the text starts
// after it. A wrapped line numbers only its first row. The cursor and a click are in the text's
// columns. Hidden, the text takes the whole width again. A count that gains a digit widens the
// gutter.
func TestTheEditorsLineNumbers(t *testing.T) {
	h, ed, sh := focusedEditor(t, 20, 6, widget.WithInitialText("alpha\nbravo charlie delta echo\ngolf"),
		widget.WithEditorLineNumbers(true), widget.WithEditorWrap(widget.WrapSoft))
	h.settle()
	for y, want := range []string{"  1 alpha", "  2 bravo charlie", "    delta echo", "  3 golf"} {
		if got := strings.TrimRight(h.row(y), " "); got != want {
			t.Fatalf("row %d %q, want %q:\n%s", y, got, want, h.grid())
		}
	}
	// dimmed, the cursor's line too: faint, in the muted colour
	if c := h.tb.Snapshot()[0][2]; c.Content != "1" || c.Attrs.Mask&tui.AttrFaint == 0 {
		t.Fatalf("line 1's number %q is not faint: %+v", c.Content, c.Attrs)
	}
	var cx int
	h.onLoop(func() { cx, _, _ = ed.Cursor() })
	if cx != 4 {
		t.Fatalf("the cursor at column %d, want 4 (after the gutter)", cx)
	}
	// a click on "golf"'s l: its text column, not the screen's
	h.inject(tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 6, Y: 3},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 6, Y: 3})
	h.barrier(sh)
	if _, _, ln, col := edState(h, ed); ln != 2 || col != 2 {
		t.Fatalf("the click put the cursor at %d:%d, want 2:2", ln, col)
	}
	// unwrapped: one row a line, the long one cut at the width
	h.onLoop(func() { ed.SetWrap(widget.WrapNone) })
	h.waitFor("unwrapped", func() bool { return strings.TrimRight(h.row(2), " ") == "  3 golf" })
	h.onLoop(func() { ed.SetLineNumbers(false) })
	h.waitFor("no gutter", func() bool { return strings.HasPrefix(h.row(0), "alpha") })
	h.onLoop(func() { cx, _, _ = ed.Cursor() })
	if cx != 2 {
		t.Fatalf("without the gutter the cursor is at column %d, want 2", cx)
	}
	// 999 lines, three digits; an edit that makes a thousand widens the gutter to four
	h.onLoop(func() {
		ed.SetLineNumbers(true)
		ed.SetValue(strings.Repeat("x\n", 998) + "last")
	})
	h.waitFor("three digits", func() bool { return strings.HasPrefix(h.row(0), "  1 x") })
	h.inject(key('G'), key('o'))
	h.barrier(sh)
	h.waitFor("four digits after the edit", func() bool { return strings.Contains(h.grid(), "\n1000 ") })
	if !strings.Contains(h.grid(), " 999 last") {
		t.Fatalf("the line above, in the wider gutter:\n%s", h.grid())
	}
}

// TestSetWrapRefusesAnotherMode: SetWrap takes WrapNone or WrapSoft.
func TestSetWrapRefusesAnotherMode(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("SetWrap(Truncate) was taken")
		}
	}()
	widget.NewEditor().SetWrap(widget.Truncate)
}
