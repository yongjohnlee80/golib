package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func listCore(t *testing.T, text string, ln, col int) *widget.EditorCore {
	t.Helper()
	c, _ := coreWith(t, text, widget.CoreKeyset(widget.KeysetStandard), widget.CoreListEditing(true))
	c.SetLine(ln, col)
	return c
}

func listPress(c *widget.EditorCore, code rune, mods tui.Mods) {
	c.HandleKey(tui.KeyEvent{Code: code, Mods: mods})
}

func wantText(t *testing.T, c *widget.EditorCore, value string, ln, col int) {
	t.Helper()
	if got := c.Value(); got != value {
		t.Errorf("Value = %q, want %q", got, value)
	}
	if l, cc := c.Line(); l != ln || cc != col {
		t.Errorf("cursor (%d, %d), want (%d, %d)", l, cc, ln, col)
	}
}

// Enter on an item starts the next: the indentation and marker kept, a number one up, a task's
// box unchecked. Text after the cursor moves into the new item.
func TestListEnterContinuesTheList(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"- one", "- one\n- "},
		{"* one", "* one\n* "},
		{"  + nested", "  + nested\n  + "},
		{"9. nine", "9. nine\n10. "},
		{"1) one", "1) one\n2) "},
		{"- [x] done", "- [x] done\n- [ ] "},
	} {
		c := listCore(t, tc.in, 0, len(tc.in))
		listPress(c, tui.KeyEnter, 0)
		ln := len(tc.want) - len(tc.in) - 1
		wantText(t, c, tc.want, 1, ln)
	}
	c := listCore(t, "- onetwo", 0, len("- one"))
	listPress(c, tui.KeyEnter, 0)
	wantText(t, c, "- one\n- two", 1, 2)
}

// The list goes on until the writer ends it: Enter on an empty item, or Backspace on its marker.
func TestListEndsOnAnEmptyItemOrBackspace(t *testing.T) {
	c := listCore(t, "- one\n- ", 1, 2)
	listPress(c, tui.KeyEnter, 0)
	wantText(t, c, "- one\n", 1, 0)

	c = listCore(t, "- one\n  - ", 1, 4)
	listPress(c, tui.KeyBackspace, 0)
	wantText(t, c, "- one\n  ", 1, 2)

	c = listCore(t, "- one", 0, 3) // inside the text: an ordinary Backspace
	listPress(c, tui.KeyBackspace, 0)
	wantText(t, c, "- ne", 0, 2)
}

// Tab nests an item by its marker's width, from anywhere on it; Shift+Tab un-nests it.
func TestListTabNests(t *testing.T) {
	c := listCore(t, "- one\n- two", 1, 4)
	listPress(c, tui.KeyTab, 0)
	wantText(t, c, "- one\n  - two", 1, 6)
	listPress(c, tui.KeyTab, tui.ModShift)
	wantText(t, c, "- one\n- two", 1, 4)
	listPress(c, tui.KeyTab, tui.ModShift) // at the top level: nothing, and no tab typed
	wantText(t, c, "- one\n- two", 1, 4)

	c = listCore(t, "10. ten", 0, 0)
	listPress(c, tui.KeyTab, 0)
	wantText(t, c, "    10. ten", 0, 4)
}

// Off a list item, with list editing off, or on a thematic break, the keys are as ever.
func TestListEditingLeavesOtherLinesAlone(t *testing.T) {
	c := listCore(t, "plain", 0, 5)
	listPress(c, tui.KeyEnter, 0)
	wantText(t, c, "plain\n", 1, 0)

	c = listCore(t, "* * *", 0, 5)
	listPress(c, tui.KeyEnter, 0)
	wantText(t, c, "* * *\n", 1, 0)

	c, _ = coreWith(t, "- one", widget.CoreKeyset(widget.KeysetStandard))
	c.SetLine(0, 5)
	listPress(c, tui.KeyEnter, 0)
	wantText(t, c, "- one\n", 1, 0)
}

// SetListEditingWhere leaves out the lines it reports false for: Enter and Tab there are as ever.
func TestListEditingWhereLeavesLinesOut(t *testing.T) {
	c := listCore(t, "- code", 0, 6)
	c.SetListEditingWhere(func(ln int) bool { return false })
	listPress(c, tui.KeyEnter, 0)
	wantText(t, c, "- code\n", 1, 0)
	c.SetListEditingWhere(func(ln int) bool { return true })
	c.SetValue("- item")
	c.SetLine(0, 6)
	listPress(c, tui.KeyEnter, 0)
	wantText(t, c, "- item\n- ", 1, 2)
}

// One undo takes a continuation back whole.
func TestListContinuationUndoesInOne(t *testing.T) {
	c := listCore(t, "- one", 0, 5)
	listPress(c, tui.KeyEnter, 0)
	c.Undo()
	if got := c.Value(); got != "- one" {
		t.Errorf("after undo %q, want %q", got, "- one")
	}
}
