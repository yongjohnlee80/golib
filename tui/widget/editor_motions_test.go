package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestEditorMotionsWordsAndParagraphs(t *testing.T) {
	text := "first word\n\nsecond paragraph word\nthird"
	h, ed, sh := focusedEditor(t, 40, 10, widget.WithInitialText(text))

	// Initial cursor at (0, 0)
	row, col := ed.Line()
	if row != 0 || col != 0 {
		t.Fatalf("initial pos: got (%d, %d), want (0, 0)", row, col)
	}

	// 'w' moves to start of "word"
	h.inject(key('w'))
	h.barrier(sh)
	row, col = ed.Line()
	if row != 0 || col != 6 {
		t.Errorf("after 'w': got (%d, %d), want (0, 6)", row, col)
	}

	// 'w' crosses line ends to start of "second"
	h.inject(key('w'))
	h.barrier(sh)
	row, col = ed.Line()
	if row != 2 || col != 0 {
		t.Errorf("after second 'w': got (%d, %d), want (2, 0)", row, col)
	}

	// 'b' moves back to "word" on line 0
	h.inject(key('b'))
	h.barrier(sh)
	row, col = ed.Line()
	if row != 0 || col != 6 {
		t.Errorf("after 'b': got (%d, %d), want (0, 6)", row, col)
	}

	// 'G' moves to bottom line
	h.inject(key('G'))
	h.barrier(sh)
	row, _ = ed.Line()
	if row != 3 {
		t.Errorf("after 'G': got row %d, want 3", row)
	}

	// 'g' 'g' moves to top line
	h.inject(key('g'), key('g'))
	h.barrier(sh)
	row, _ = ed.Line()
	if row != 0 {
		t.Errorf("after 'gg': got row %d, want 0", row)
	}

	// '}' moves forward to blank line boundary
	h.inject(key('}'))
	h.barrier(sh)
	row, _ = ed.Line()
	if row != 1 {
		t.Errorf("after '}': got row %d, want 1", row)
	}

	// '{' moves back to beginning
	h.inject(key('{'))
	h.barrier(sh)
	row, _ = ed.Line()
	if row != 0 {
		t.Errorf("after '{': got row %d, want 0", row)
	}
}

func TestEditorMotionsNanoLineEnd(t *testing.T) {
	h, ed, sh := focusedEditor(t, 40, 10,
		widget.WithInitialText("abc"),
		widget.WithKeymap(widget.NanoKeymap()),
		widget.WithModalEditing(false),
	)

	// Ctrl+E in Nano moves to insertion boundary at end of line (col 3)
	h.inject(keyMod('e', tui.ModCtrl))
	h.barrier(sh)

	row, col := ed.Line()
	if row != 0 || col != 3 {
		t.Fatalf("after Ctrl+E in Nano: got (%d, %d), want (0, 3)", row, col)
	}

	// Type 'X' - should append after 'c', producing "abcX"
	h.inject(key('X'))
	h.barrier(sh)

	if got := ed.Value(); got != "abcX" {
		t.Errorf("value after typing 'X' at end of line: got %q, want %q", got, "abcX")
	}
}
