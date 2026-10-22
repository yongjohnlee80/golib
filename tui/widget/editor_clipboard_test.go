package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

func TestVisualModesKeepTheNormalBlockCursor(t *testing.T) {
	h, ed, sh := focusedEditor(t, 40, 10, widget.WithCore(widget.CoreInitialText("hello world")))
	for _, step := range []struct {
		key  rune
		mode widget.EditorMode
	}{
		{'v', widget.ModeVisual},
		{'v', widget.ModeNormal},
		{'V', widget.ModeVisualLine},
	} {
		h.inject(key(step.key))
		h.barrier(sh)
		if ed.Core().Mode() != step.mode || ed.CursorShape() != tui.CursorShapeBlock {
			t.Errorf("after %q: mode=%v, cursor=%v; want %v and block", step.key, ed.Core().Mode(), ed.CursorShape(), step.mode)
		}
	}
}

func TestEditorClipboardRegisters(t *testing.T) {
	h, ed, sh := focusedEditor(t, 40, 10, widget.WithCore(widget.CoreInitialText("line 1\nline 2")))

	// Manual register set and inspection
	ed.Core().SetRegister("custom content", false)
	text, linewise := ed.Core().Register()
	if text != "custom content" || linewise {
		t.Fatalf("Register(): got (%q, %v), want (%q, false)", text, linewise, "custom content")
	}

	// Paste register after current character (cursor was on 'l' in "line 1")
	h.inject(key('p'))
	h.barrier(sh)

	var val string
	h.onLoop(func() {
		val = ed.Core().Value()
	})
	if val != "lcustom contentine 1\nline 2" {
		t.Errorf("after paste: got %q, want %q", val, "lcustom contentine 1\nline 2")
	}
}

func TestEditorClipboardSelectionAndYank(t *testing.T) {
	h, ed, sh := focusedEditor(t, 40, 10, widget.WithCore(widget.CoreInitialText("hello world")))

	// Visual mode 'v', move 5 characters right, yank 'y'
	h.inject(key('v'))
	for i := 0; i < 4; i++ {
		h.inject(key('l'))
	}
	h.barrier(sh)

	if got := ed.Core().SelectedText(); got != "hello" {
		t.Errorf("SelectedText() during visual: got %q, want %q", got, "hello")
	}

	h.inject(key('y'))
	h.barrier(sh)

	if ed.Core().Mode() != widget.ModeNormal {
		t.Errorf("after yank: mode is %v, want ModeNormal", ed.Core().Mode())
	}

	text, linewise := ed.Core().Register()
	if text != "hello" || linewise {
		t.Errorf("yanked register: got (%q, %v), want (\"hello\", false)", text, linewise)
	}
}

func TestEditorClipboardDisabledFeatures(t *testing.T) {
	// Editor with selection disabled
	h, ed, sh := focusedEditor(t, 40, 10,
		widget.WithCore(widget.CoreInitialText("hello world")),
		widget.WithCore(widget.CoreSelection(false)),
	)

	h.inject(key('v'))
	h.barrier(sh)

	// Mode should not transition to ModeVisual when selection is disabled
	if ed.Core().SelectedText() != "" {
		t.Errorf("SelectedText() with selection disabled: got %q, want empty", ed.Core().SelectedText())
	}
}

func TestEditorClipboardDisabledYank(t *testing.T) {
	h, ed, sh := focusedEditor(t, 40, 10,
		widget.WithCore(widget.CoreInitialText("ab")),
		widget.WithCore(widget.CoreYank(false)),
	)

	h.inject(key('v'))
	h.inject(key('l'))
	h.barrier(sh)

	if got := ed.Core().SelectedText(); got != "ab" {
		t.Fatalf("SelectedText() during visual: got %q, want %q", got, "ab")
	}

	h.inject(key('y'))
	h.barrier(sh)

	if ed.Core().Mode() != widget.ModeNormal {
		t.Errorf("after yank: mode is %v, want ModeNormal", ed.Core().Mode())
	}

	text, linewise := ed.Core().Register()
	if text != "" || linewise {
		t.Errorf("register after yank with WithYank(false): got (%q, %v), want (\"\", false)", text, linewise)
	}

	// Also verify normal-mode yy
	h.inject(key('y'), key('y'))
	h.barrier(sh)
	text, linewise = ed.Core().Register()
	if text != "" || linewise {
		t.Errorf("register after yy with WithYank(false): got (%q, %v), want (\"\", false)", text, linewise)
	}
}
