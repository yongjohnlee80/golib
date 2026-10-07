package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func drag(x, y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: tui.MouseMotion, Button: tui.MouseLeft, X: x, Y: y}
}

func editorSelection(h *harness, ed *widget.Editor) (text string) {
	h.onLoop(func() { text = ed.SelectedText() })
	return text
}

// A drag selects the portion in visual mode, and y copies it, as v then y would.
func TestEditorDragSelectsInVisualModeAndYCopies(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 4)
	h.onLoop(func() { ed.SetValue("hello world\nsecond line") })
	h.barrier(sh)

	h.inject(click(0, 0), drag(2, 0), drag(4, 0), release(4, 0))
	h.barrier(sh)
	if _, mode, _, _ := edState(h, ed); mode != widget.ModeVisual {
		t.Fatalf("mode after a drag = %v; want visual", mode)
	}
	if got := editorSelection(h, ed); got != "hello" {
		t.Fatalf("selected %q; want \"hello\"", got)
	}
	h.inject(key('y'))
	h.waitFor("the yank on the clipboard", func() bool { return string(h.tb.Clipboard()) == "hello" })
}

// Across lines, and backwards: the selection runs between the press and the pointer.
func TestEditorDragSelectsAcrossLinesBothWays(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 4)
	h.onLoop(func() { ed.SetValue("hello world\nsecond line") })
	h.barrier(sh)

	h.inject(click(6, 0), drag(5, 1), release(5, 1))
	h.barrier(sh)
	if got := editorSelection(h, ed); got != "world\nsecond" {
		t.Fatalf("forward drag selected %q; want \"world\\nsecond\"", got)
	}
	h.inject(click(5, 1), drag(6, 0), release(6, 0))
	h.barrier(sh)
	if got := editorSelection(h, ed); got != "world\nsecond" {
		t.Fatalf("backward drag selected %q; want \"world\\nsecond\"", got)
	}
}

// A press and release on one spot is a click: the caret moves, nothing is selected.
func TestEditorClickWithoutDragSelectsNothing(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 4)
	h.onLoop(func() { ed.SetValue("hello world") })
	h.barrier(sh)

	h.inject(click(3, 0), drag(3, 0), release(3, 0))
	h.barrier(sh)
	if _, mode, ln, col := edState(h, ed); mode != widget.ModeNormal || ln != 0 || col != 3 {
		t.Fatalf("after a click: mode %v at %d,%d; want normal at 0,3", mode, ln, col)
	}
	if got := editorSelection(h, ed); got != "" {
		t.Fatalf("a click selected %q", got)
	}
}

// In Text (modeless) mode, a drag selects and Ctrl+C copies.
func TestEditorDragThenCtrlCInTextMode(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 4, widget.WithStandardKeymap())
	h.onLoop(func() { ed.SetValue("copy this text") })
	h.barrier(sh)

	h.inject(click(5, 0), drag(8, 0), release(8, 0))
	h.barrier(sh)
	if got := editorSelection(h, ed); got != "this" {
		t.Fatalf("selected %q; want \"this\"", got)
	}
	h.inject(tui.KeyEvent{Code: 'c', Mods: tui.ModCtrl})
	h.waitFor("Ctrl+C on the clipboard", func() bool { return string(h.tb.Clipboard()) == "this" })
}

// Dragging past the bottom edge scrolls the view and extends the selection with it.
func TestEditorDragPastTheEdgeScrolls(t *testing.T) {
	h, ed, sh := focusedEditor(t, 20, 2)
	h.onLoop(func() { ed.SetValue("l0\nl1\nl2\nl3\nl4") })
	h.barrier(sh)

	// Held below the view without moving: the auto-scroll carries the selection to the end.
	h.inject(click(0, 0), drag(1, 1), drag(1, 5))
	h.waitFor("the auto-scroll to reach the last line", func() bool {
		_, _, ln, _ := edState(h, ed)
		return ln == 4
	})
	h.inject(release(1, 5))
	h.barrier(sh)
	if got := editorSelection(h, ed); len(got) < len("l0\nl1\nl2\nl") {
		t.Fatalf("selected %q; want it to run from l0 past the old bottom", got)
	}
}

// A plain click after a drag-selection ends the selection, as it ends v's.
func TestEditorClickAfterDragClearsTheSelection(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 4)
	h.onLoop(func() { ed.SetValue("hello world") })
	h.barrier(sh)
	h.inject(click(0, 0), drag(4, 0), release(4, 0))
	h.barrier(sh)
	h.inject(click(8, 0), release(8, 0))
	h.barrier(sh)
	if _, mode, _, _ := edState(h, ed); mode != widget.ModeNormal {
		t.Fatalf("mode after a click = %v; want normal", mode)
	}
}
