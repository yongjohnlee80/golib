package widget_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// relayout asks the App for a layout pass and waits for it: what a status line's update or a toast
// does, unasked by the widget.
func relayout(h *harness, sh *shell, c interface{ Context() *tui.Context }) {
	h.onLoop(func() { c.Context().RequestLayout() })
	h.barrier(sh)
}

// The wheel's scroll holds through a later layout pass, which used to snap it back to the cursor;
// a cursor that moves is still brought into view.
func TestWheelScrollHoldsThroughALayout(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		var items []string
		for i := range 30 {
			items = append(items, fmt.Sprintf("row%02d", i))
		}
		h, l, sh := focusedList(t, widget.SliceSource(items), 20, 4)
		for range 5 {
			h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 1})
		}
		h.barrier(sh)
		relayout(h, sh, l)
		if s := h.grid(); !strings.Contains(s, "row05") || strings.Contains(s, "row00") {
			t.Fatalf("after a layout the list is back at the cursor:\n%s", s)
		}
		h.inject(key('j')) // the cursor moves: it is revealed again
		h.barrier(sh)
		relayout(h, sh, l)
		h.wantContains("row01")
	})
	t.Run("tree", func(t *testing.T) {
		var roots []*widget.TreeNode
		for i := range 30 {
			roots = append(roots, widget.NewTreeNode(fmt.Sprintf("n%02d", i), fmt.Sprintf("node%02d", i)))
		}
		h, tr, sh := focusedTree(t, 30, 4, widget.WithRoots(roots...))
		for range 5 {
			h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 2, Y: 1})
		}
		h.barrier(sh)
		relayout(h, sh, tr)
		if s := h.grid(); !strings.Contains(s, "node05") || strings.Contains(s, "node00") {
			t.Fatalf("the tree did not scroll, or snapped back:\n%s", s)
		}
	})
	t.Run("editor", func(t *testing.T) {
		h, ed, sh := focusedEditor(t, 20, 3)
		var lines []string
		for i := range 30 {
			lines = append(lines, fmt.Sprintf("line%02d", i))
		}
		h.onLoop(func() { ed.SetValue(strings.Join(lines, "\n")) })
		h.barrier(sh)
		for range 5 {
			h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 1, Y: 1})
		}
		h.barrier(sh)
		relayout(h, sh, ed)
		if s := h.grid(); !strings.Contains(s, "line05") || strings.Contains(s, "line00") {
			t.Fatalf("after a layout the editor is back at the cursor:\n%s", s)
		}
	})
}
