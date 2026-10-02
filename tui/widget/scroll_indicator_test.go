package widget_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// THE SCROLL INDICATOR'S THUMB spans the scroll range: on the top row when the
// view shows the first line, on the bottom row when it shows the last one. The
// range is the tops the view can take with the last line still on screen, not
// the line count: a document twice the screen's height, scrolled to its end,
// used to put the thumb halfway down (Johno, 2026-10-02).

// thumbRow is the row the indicator's thumb is painted on, -1 for none.
func thumbRow(h *harness) int {
	for y, row := range strings.Split(h.grid(), "\n") {
		if strings.Contains(row, "█") {
			return y
		}
	}
	return -1
}

func numberedLines(n int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	return strings.Join(lines, "\n")
}

func TestEditorThumbSpansTheScrollRange(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
		opts []widget.EditorOption
	}{
		{"unwrapped", numberedLines(10), nil},
		// each line wraps to two rows at this width, so the range is in lines whose rows fill the
		// screen, not in lines
		{"wrapped", strings.Repeat("aaaa bbbb cccc dddd eeee\n", 9) + "aaaa bbbb cccc dddd eeee", []widget.EditorOption{widget.WithEditorWrap(widget.WrapSoft)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, ed, sh := focusedEditor(t, 20, 5, tc.opts...)
			h.onLoop(func() { ed.SetValue(tc.text); ed.SetLine(0, 0) })
			h.barrier(sh)
			if got := thumbRow(h); got != 0 {
				t.Fatalf("at the first line the thumb is on row %d, want 0\n%s", got, h.grid())
			}
			h.onLoop(func() { ed.SetLine(9, 0) })
			h.barrier(sh)
			if !strings.Contains(h.grid(), "line 9") && tc.name == "unwrapped" {
				t.Fatalf("the last line is not on screen\n%s", h.grid())
			}
			if got := thumbRow(h); got != 4 {
				t.Fatalf("at the last line the thumb is on row %d, want 4 (the bottom)\n%s", got, h.grid())
			}
		})
	}
}

func TestTextAreaThumbSpansTheScrollRange(t *testing.T) {
	h, _, sh := focusedArea(t, 20, 5)
	h.inject(tui.PasteEvent{Text: numberedLines(10)})
	h.barrier(sh)
	h.wantContains("line 9")
	if got := thumbRow(h); got != 4 {
		t.Fatalf("at the last line the thumb is on row %d, want 4 (the bottom)\n%s", got, h.grid())
	}
}

func TestTreeThumbSpansTheScrollRange(t *testing.T) {
	var roots []*widget.TreeNode
	for i := range 10 {
		roots = append(roots, widget.NewTreeNode(fmt.Sprint(i), fmt.Sprintf("node %d", i)))
	}
	h, _, sh := focusedTree(t, 20, 5, widget.WithRoots(roots...))
	if got := thumbRow(h); got != 0 {
		t.Fatalf("at the first row the thumb is on row %d, want 0\n%s", got, h.grid())
	}
	h.inject(key(tui.KeyEnd))
	h.barrier(sh)
	h.wantContains("node 9")
	if got := thumbRow(h); got != 4 {
		t.Fatalf("at the last row the thumb is on row %d, want 4 (the bottom)\n%s", got, h.grid())
	}
}
