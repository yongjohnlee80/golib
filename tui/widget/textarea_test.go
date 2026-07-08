package widget_test

// TextArea contract (ADR-0007 §2.4; Q5: no submit key).

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func focusedArea(t *testing.T, w, h int, opts ...widget.TextAreaOption) (*harness, *widget.TextArea, *shell) {
	t.Helper()
	ta := widget.NewTextArea(opts...)
	sh := newShell(ta)
	hh := startApp(t, sh, w, h)
	hh.inject(tab())
	hh.barrier(sh)
	return hh, ta, sh
}

// TestTextAreaEnterIsNewline: Enter inserts a newline and emits NO submit
// event (ADR-0007 Q5) — only ChangeEvent.
func TestTextAreaEnterIsNewline(t *testing.T) {
	h, ta, sh := focusedArea(t, 20, 5)
	submits := record[widget.SubmitEvent](h)
	changes := record[widget.ChangeEvent](h)

	h.inject(typeString("ab")...)
	h.inject(key(tui.KeyEnter))
	h.inject(typeString("cd")...)
	h.barrier(sh)

	var val string
	h.onLoop(func() { val = ta.Value() })
	if val != "ab\ncd" {
		t.Fatalf("Value = %q, want ab\\ncd", val)
	}
	if submits.count() != 0 {
		t.Fatalf("TextArea emitted SubmitEvent — Q5 forbids a submit key")
	}
	if changes.count() != 5 {
		t.Fatalf("ChangeEvents = %d, want 5 (per input event)", changes.count())
	}
	// Both lines painted; cursor after "cd".
	h.wantContains("ab")
	h.wantContains("cd")
	if x, y, _ := h.tb.CursorPos(); x != 2 || y != 1 {
		t.Fatalf("cursor = (%d,%d), want (2,1)", x, y)
	}
}

// TestTextAreaPasteMultiline: one paste, one ChangeEvent, lines split.
func TestTextAreaPasteMultiline(t *testing.T) {
	h, ta, sh := focusedArea(t, 20, 5)
	changes := record[widget.ChangeEvent](h)
	h.inject(tui.PasteEvent{Text: "one\ntwo\nthree"})
	h.barrier(sh)
	var val string
	h.onLoop(func() { val = ta.Value() })
	if val != "one\ntwo\nthree" {
		t.Fatalf("Value = %q", val)
	}
	if changes.count() != 1 {
		t.Fatalf("paste emitted %d ChangeEvents, want 1", changes.count())
	}
}

// TestTextAreaNavigation: vertical movement with sticky column, Backspace
// join, Ctrl+Home/End.
func TestTextAreaNavigation(t *testing.T) {
	h, ta, sh := focusedArea(t, 20, 5)
	h.inject(tui.PasteEvent{Text: "long line\nab\nlonger line"})
	h.barrier(sh)

	// Ctrl+Home → buffer start.
	h.inject(keyMod(tui.KeyHome, tui.ModCtrl))
	h.barrier(sh)
	if x, y, _ := h.tb.CursorPos(); x != 0 || y != 0 {
		t.Fatalf("Ctrl+Home cursor = (%d,%d), want (0,0)", x, y)
	}
	// End (line) → col 9; Down twice keeps the sticky column where it can.
	h.inject(key(tui.KeyEnd), key(tui.KeyDown), key(tui.KeyDown))
	h.barrier(sh)
	if x, y, _ := h.tb.CursorPos(); x != 9 || y != 2 {
		t.Fatalf("sticky-column cursor = (%d,%d), want (9,2)", x, y)
	}
	// Backspace at column 0 joins lines.
	h.inject(key(tui.KeyHome), key(tui.KeyBackspace))
	h.barrier(sh)
	var val string
	h.onLoop(func() { val = ta.Value() })
	if val != "long line\nablonger line" {
		t.Fatalf("join = %q", val)
	}
}

// TestTextAreaWrapSoft: WrapSoft wraps at the viewport width; WrapNone
// scrolls horizontally instead.
func TestTextAreaWrapModes(t *testing.T) {
	h, _, sh := focusedArea(t, 8, 4, widget.WithWrap(widget.WrapSoft))
	h.inject(tui.PasteEvent{Text: "alpha beta gamma"})
	h.barrier(sh)
	// Wrapped: "alpha" endures on row 0, "beta" on a later row.
	if got := h.row(0); got != "alpha   " {
		t.Fatalf("WrapSoft row 0 = %q, want %q", got, "alpha   ")
	}
	h.wantContains("beta")

	h2, _, sh2 := focusedArea(t, 8, 4, widget.WithWrap(widget.WrapNone))
	h2.inject(tui.PasteEvent{Text: "alpha beta gamma"})
	h2.barrier(sh2)
	// No wrap: one line, scrolled so the cursor (end) is visible.
	h2.wantContains("gamma")
	h2.wantNotContains("alpha")
}

// TestTextAreaScrollIndicator: content beyond the viewport paints the
// indicator column and scrolls to keep the cursor visible.
func TestTextAreaViewportScroll(t *testing.T) {
	h, _, sh := focusedArea(t, 10, 3)
	h.inject(tui.PasteEvent{Text: "l1\nl2\nl3\nl4\nl5"})
	h.barrier(sh)
	// Cursor on l5: the viewport shows the tail.
	h.wantContains("l5")
	h.wantNotContains("l1")
	// Scroll indicator column present.
	h.wantContains("█")
	// Ctrl+Home scrolls back to the top.
	h.inject(keyMod(tui.KeyHome, tui.ModCtrl))
	h.barrier(sh)
	h.wantContains("l1")
	h.wantNotContains("l5")
}

// TestTextAreaTabBubbles: Tab is not consumed (focus traversal).
func TestTextAreaTabBubbles(t *testing.T) {
	h, _, sh := focusedArea(t, 20, 5)
	h.inject(tab())
	h.barrier(sh)
	found := false
	for _, k := range sh.bubbledKeys() {
		if k.Code == tui.KeyTab {
			found = true
		}
	}
	if !found {
		t.Fatalf("Tab did not bubble out of TextArea")
	}
	_ = h
}
