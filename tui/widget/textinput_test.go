package widget_test

// TextInput contract (ADR-0007 §2.4, §5.3, §5.5).

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// focusedInput mounts a TextInput inside a recording shell and focuses it.
func focusedInput(t *testing.T, opts ...widget.TextInputOption) (*harness, *widget.TextInput, *shell) {
	t.Helper()
	in := widget.NewTextInput(opts...)
	sh := newShell(in)
	h := startApp(t, sh, 20, 1)
	h.inject(tab())
	h.barrier(sh)
	return h, in, sh
}

func TestTextInputTypingAndEvents(t *testing.T) {
	h, in, sh := focusedInput(t)
	changes := record[widget.ChangeEvent](h)
	submits := record[widget.SubmitEvent](h)

	h.inject(typeString("hi")...)
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)

	var val string
	var id tui.NodeID
	h.onLoop(func() { val = in.Value(); id = in.NodeID() })
	if val != "hi" {
		t.Fatalf("Value = %q, want hi", val)
	}
	if got := changes.count(); got != 2 {
		t.Fatalf("ChangeEvents = %d, want 2 (one per keystroke)", got)
	}
	if ev, ok := changes.last(); !ok || ev.Owner != id || ev.Value != "hi" {
		t.Fatalf("last ChangeEvent = %+v (owner %d)", ev, id)
	}
	if ev, ok := submits.last(); !ok || ev.Owner != id || ev.Value != "hi" {
		t.Fatalf("SubmitEvent = %+v, want owner %d value hi", ev, id)
	}
	h.wantContains("hi")
}

// TestTextInputKeysConsumedVsBubbled walks the §2.4 key inventory
// (table-driven): editing keys are consumed; Tab and unknown chords bubble.
func TestTextInputKeysConsumedVsBubbled(t *testing.T) {
	cases := []struct {
		name     string
		ev       tui.KeyEvent
		consumed bool
	}{
		{"printable", key('x'), true},
		{"backspace", key(tui.KeyBackspace), true},
		{"delete", key(tui.KeyDelete), true},
		{"left", key(tui.KeyLeft), true},
		{"right", key(tui.KeyRight), true},
		{"shift-left", keyShift(tui.KeyLeft), true},
		{"ctrl-left-word", keyMod(tui.KeyLeft, tui.ModCtrl), true},
		{"alt-right-word", keyMod(tui.KeyRight, tui.ModAlt), true},
		{"home", key(tui.KeyHome), true},
		{"end", key(tui.KeyEnd), true},
		{"enter", key(tui.KeyEnter), true},
		{"ctrl-a", keyMod('a', tui.ModCtrl), true},
		{"ctrl-e", keyMod('e', tui.ModCtrl), true},
		{"ctrl-u", keyMod('u', tui.ModCtrl), true},
		{"ctrl-w", keyMod('w', tui.ModCtrl), true},
		{"tab-bubbles", tab(), false},
		{"f5-bubbles", key(tui.KeyF5), false},
		{"ctrl-q-bubbles", keyMod('q', tui.ModCtrl), false},
		{"up-bubbles", key(tui.KeyUp), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _, sh := focusedInput(t, widget.WithInitialValue("word one"))
			h.inject(tc.ev)
			h.barrier(sh)
			bubbled := false
			for _, k := range sh.bubbledKeys() {
				if k.Code == tc.ev.Code && k.Mods == tc.ev.Mods {
					bubbled = true
				}
			}
			if bubbled == tc.consumed {
				t.Fatalf("key %v: consumed=%v, want %v", tc.ev, !bubbled, tc.consumed)
			}
		})
	}
}

// TestTextInputPasteAtomic asserts §5.5: bracketed paste inserts atomically
// — one ChangeEvent, no SubmitEvent from a pasted newline.
func TestTextInputPasteAtomic(t *testing.T) {
	h, in, sh := focusedInput(t)
	changes := record[widget.ChangeEvent](h)
	submits := record[widget.SubmitEvent](h)

	h.inject(tui.PasteEvent{Text: "line1\nline2"})
	h.barrier(sh)

	var val string
	h.onLoop(func() { val = in.Value() })
	if val != "line1 line2" {
		t.Fatalf("pasted value = %q, want newline flattened to space", val)
	}
	if changes.count() != 1 {
		t.Fatalf("paste emitted %d ChangeEvents, want 1 (atomic)", changes.count())
	}
	if submits.count() != 0 {
		t.Fatalf("multi-line paste faked a SubmitEvent")
	}
}

// TestTextInputMask asserts §5.5: mask runes paint; Value() returns raw.
func TestTextInputMask(t *testing.T) {
	h, in, sh := focusedInput(t, widget.WithMask('•'))
	h.inject(typeString("secret")...)
	h.barrier(sh)
	var val string
	h.onLoop(func() { val = in.Value() })
	if val != "secret" {
		t.Fatalf("masked Value = %q, want secret", val)
	}
	h.wantContains("••••••")
	h.wantNotContains("secret")
}

// TestTextInputIMECursor asserts §5.5: the hardware cursor parks at the
// insertion point while focused (ADR-0003 rule), via TestBackend cursor
// state.
func TestTextInputIMECursor(t *testing.T) {
	h, _, sh := focusedInput(t)
	h.inject(typeString("abc")...)
	h.barrier(sh)
	x, y, visible := h.tb.CursorPos()
	if !visible || x != 3 || y != 0 {
		t.Fatalf("cursor = (%d,%d,%v), want (3,0,true)", x, y, visible)
	}
	h.inject(key(tui.KeyLeft))
	h.barrier(sh)
	if x, _, _ := h.tb.CursorPos(); x != 2 {
		t.Fatalf("cursor after Left = %d, want 2", x)
	}
	// Wide grapheme: the insertion point advances two cells.
	h.inject(key(tui.KeyEnd), tui.PasteEvent{Text: "界"})
	h.barrier(sh)
	if x, _, _ := h.tb.CursorPos(); x != 5 {
		t.Fatalf("cursor after wide cluster = %d, want 5", x)
	}
}

// TestTextInputCursorAmbiguousWidePolicy asserts finding 1: under
// WithWidthPolicy(WidthPolicyAmbiguousWide) the widget's OWN cursor/layout
// math (cellAt via Context.StringWidth) agrees with the width-2 paint for an
// East-Asian-ambiguous cluster — "§" is width 1 under the default policy and
// width 2 under AmbiguousWide. Before the fix, cellAt used tui.StringWidth
// (default policy) and the cursor parked at column 1 while Render painted the
// glyph two cells wide.
func TestTextInputCursorAmbiguousWidePolicy(t *testing.T) {
	in := widget.NewTextInput()
	sh := newShell(in)
	h := startAppOpts(t, sh, 20, 1, tui.WithWidthPolicy(tui.WidthPolicyAmbiguousWide))
	h.inject(tab())
	h.barrier(sh)

	// One ambiguous-width cluster: paint width 2, so the insertion point must
	// sit at column 2 (not 1, the default-policy answer).
	h.inject(tui.PasteEvent{Text: "§"})
	h.barrier(sh)
	if x, y, visible := h.tb.CursorPos(); !visible || x != 2 || y != 0 {
		t.Fatalf("cursor after ambiguous-wide cluster = (%d,%d,%v), want (2,0,true)", x, y, visible)
	}
}

// TestTextInputValidation: failing validation consumes Enter, sets the
// error state, and suppresses SubmitEvent; the next edit clears it.
func TestTextInputValidation(t *testing.T) {
	vErr := errors.New("too short")
	validate := func(s string) error {
		if len(s) < 3 {
			return vErr
		}
		return nil
	}
	h, in, sh := focusedInput(t, widget.WithValidate(validate))
	submits := record[widget.SubmitEvent](h)

	h.inject(typeString("ab")...)
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	var got error
	h.onLoop(func() { got = in.Err() })
	if !errors.Is(got, vErr) {
		t.Fatalf("Err = %v, want validation error", got)
	}
	if submits.count() != 0 {
		t.Fatalf("failed validation emitted SubmitEvent")
	}

	h.inject(typeString("c")...)
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	h.onLoop(func() { got = in.Err() })
	if got != nil {
		t.Fatalf("Err after valid submit = %v, want nil", got)
	}
	if ev, ok := submits.last(); !ok || ev.Value != "abc" {
		t.Fatalf("SubmitEvent = %+v, want abc", ev)
	}
}

// TestTextInputSelectionAndReadline: Shift+arrows select (replaced by
// typing); Ctrl+A/E/U/W behave readline-style.
func TestTextInputSelectionAndReadline(t *testing.T) {
	h, in, sh := focusedInput(t, widget.WithInitialValue("hello world"))
	get := func() string {
		var v string
		h.onLoop(func() { v = in.Value() })
		return v
	}

	// Ctrl+W deletes the word before the cursor (cursor starts at end).
	h.inject(keyMod('w', tui.ModCtrl))
	h.barrier(sh)
	if got := get(); got != "hello " {
		t.Fatalf("after Ctrl+W: %q, want %q", got, "hello ")
	}
	// Shift+Left twice selects "o "; typing replaces the selection.
	h.inject(keyShift(tui.KeyLeft), keyShift(tui.KeyLeft))
	h.inject(typeString("p!")...)
	h.barrier(sh)
	if got := get(); got != "hellp!" {
		t.Fatalf("after selection replace: %q, want %q", got, "hellp!")
	}
	// Ctrl+A home + Delete kills the first cluster.
	h.inject(keyMod('a', tui.ModCtrl), key(tui.KeyDelete))
	h.barrier(sh)
	if got := get(); got != "ellp!" {
		t.Fatalf("after Ctrl+A Delete: %q, want %q", got, "ellp!")
	}
	// Ctrl+E end + Ctrl+U kills to start.
	h.inject(keyMod('e', tui.ModCtrl), keyMod('u', tui.ModCtrl))
	h.barrier(sh)
	if got := get(); got != "" {
		t.Fatalf("after Ctrl+U: %q, want empty", got)
	}
}

// TestTextInputPlaceholderAndScroll: placeholder paints while empty;
// overflow keeps the cursor visible by scrolling horizontally.
func TestTextInputPlaceholder(t *testing.T) {
	h, _, sh := focusedInput(t, widget.WithPlaceholder("type here"))
	h.settle()
	h.wantContains("type here")
	h.inject(key('x'))
	h.barrier(sh)
	h.wantNotContains("type here")
}

func TestTextInputHorizontalScroll(t *testing.T) {
	h, _, sh := focusedInput(t)
	// 25 chars into a 20-cell viewport: the head scrolls off; the cursor
	// stays visible at the right edge.
	h.inject(typeString("abcdefghijklmnopqrstuvwxy")...)
	h.barrier(sh)
	if strings.Contains(h.row(0), "abc") {
		t.Fatalf("head did not scroll off: %q", h.row(0))
	}
	h.wantContains("y")
	x, _, _ := h.tb.CursorPos()
	if x != 19 {
		t.Fatalf("cursor pinned at %d, want right edge 19", x)
	}
}
