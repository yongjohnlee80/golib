package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestEditorHistoryUndoRedo(t *testing.T) {
	h, ed, sh := focusedEditor(t, 40, 10, widget.WithInitialText("initial"))

	// Mutate via insert: type " line"
	h.inject(key('A'))
	h.inject(typeString(" line")...)
	h.inject(key(tui.KeyEscape))
	h.barrier(sh)

	if got := ed.Value(); got != "initial line" {
		t.Fatalf("after edit: got %q, want %q", got, "initial line")
	}

	// Undo back to "initial"
	h.inject(key('u'))
	h.barrier(sh)
	if got := ed.Value(); got != "initial" {
		t.Fatalf("after undo: got %q, want %q", got, "initial")
	}

	// Redo back to "initial line"
	h.inject(keyMod('r', tui.ModCtrl))
	h.barrier(sh)
	if got := ed.Value(); got != "initial line" {
		t.Fatalf("after redo: got %q, want %q", got, "initial line")
	}
}

func TestEditorHistoryDisabled(t *testing.T) {
	h, ed, sh := focusedEditor(t, 40, 10,
		widget.WithInitialText("initial"),
		widget.WithUndo(false),
	)

	h.inject(key('A'))
	h.inject(typeString(" line")...)
	h.inject(key(tui.KeyEscape))
	h.barrier(sh)

	if got := ed.Value(); got != "initial line" {
		t.Fatalf("after edit: got %q, want %q", got, "initial line")
	}

	// Undo should do nothing because undo is disabled
	h.inject(key('u'))
	h.barrier(sh)
	if got := ed.Value(); got != "initial line" {
		t.Fatalf("after undo with undo disabled: got %q, want %q", got, "initial line")
	}
}

func TestEditorHistoryRingCapacity(t *testing.T) {
	h, ed, sh := focusedEditor(t, 40, 10, widget.WithInitialText("start"))

	// Create 70 separate normal-mode edits (exceeding editorUndoCap = 64)
	for i := 0; i < 70; i++ {
		h.inject(key('o'))
		h.inject(key(tui.KeyEscape))
	}
	h.barrier(sh)

	// Attempt to undo 70 times; because history is capped at 64, the earliest 6 edits cannot be undone.
	for i := 0; i < 70; i++ {
		h.inject(key('u'))
	}
	h.barrier(sh)

	var numLines int
	h.onLoop(func() {
		numLines = len(ed.Lines())
	})

	// 1 initial line + 70 created = 71 lines.
	// Capped at 64 undos => 71 - 64 = 7 lines remain.
	if numLines != 7 {
		t.Fatalf("expected ring buffer cap to leave 7 lines, got %d", numLines)
	}
}
