package widget_test

// SetKeyset contract, through the public surface: a profile switch on a live
// editor keeps the document and its undo history, lands in the new profile's
// starting mode and says so on the bus, clears the visual selection, and
// replays host key bindings. The pending-input half — a held chord rune, a
// count, an operator prefix, an open undo group — is in
// editor_keyset_internal_test.go, for the reason recorded there.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// setKeyset switches the profile on the loop goroutine, where every other
// editor mutation happens.
func setKeyset(h *harness, ed *widget.Editor, ks widget.Keyset) {
	h.onLoop(func() { ed.SetKeyset(ks) })
}

func keysetOf(h *harness, ed *widget.Editor) widget.Keyset {
	var ks widget.Keyset
	h.onLoop(func() { ks = ed.Keyset() })
	return ks
}

// A preference change must not cost the operator their work: the text, the
// cursor and the undo history all outlive the switch, and the new profile's
// undo chord reaches that same history.
func TestEditorSetKeysetKeepsDocumentAndUndoHistory(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6)

	h.inject(key('i'))
	h.inject(typeString("hello")...)
	h.inject(key(tui.KeyEscape))
	h.barrier(sh)
	wantState(t, h, ed, "hello", widget.ModeNormal, 0, 4)

	setKeyset(h, ed, widget.KeysetStandard)
	h.barrier(sh)

	if got := keysetOf(h, ed); got != widget.KeysetStandard {
		t.Fatalf("Keyset() = %v, want Standard", got)
	}
	val, mode, ln, col := edState(h, ed)
	if val != "hello" {
		t.Fatalf("value = %q, want %q — the switch lost the buffer", val, "hello")
	}
	if mode != widget.ModeInsert {
		t.Fatalf("mode = %v, want Insert (Standard is modeless)", mode)
	}
	if ln != 0 || col != 4 {
		t.Fatalf("cursor = %d,%d, want 0,4", ln, col)
	}

	// Ctrl+Z is Standard's undo. It must reach the history recorded while the
	// editor was in Vim: the switch swaps tables, not the document.
	h.inject(keyMod('z', tui.ModCtrl))
	h.barrier(sh)
	if val, _, _, _ := edState(h, ed); val != "" {
		t.Fatalf("after Standard Ctrl+Z value = %q, want %q — undo history did not survive the switch", val, "")
	}
}

// Each switch moves the mode exactly once, and publishes it, so a status bar
// showing NORMAL/INSERT updates with no special case for the switch itself.
func TestEditorSetKeysetPublishesOneModeChangePerSwitch(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6)
	modes := record[widget.ModeChangedEvent](h)

	setKeyset(h, ed, widget.KeysetStandard) // Normal -> Insert
	h.barrier(sh)
	if n := modes.count(); n != 1 {
		t.Fatalf("mode events after Vim->Standard = %d, want 1", n)
	}
	setKeyset(h, ed, widget.KeysetVim) // Insert -> Normal
	h.barrier(sh)
	if n := modes.count(); n != 2 {
		t.Fatalf("mode events after Standard->Vim = %d, want 2", n)
	}
	evs := modes.events()
	if evs[0].Mode != widget.ModeInsert || evs[1].Mode != widget.ModeNormal {
		t.Fatalf("modes = %v, %v, want Insert, Normal", evs[0].Mode, evs[1].Mode)
	}
	if _, mode, _, _ := edState(h, ed); mode != widget.ModeNormal {
		t.Fatalf("mode = %v, want Normal", mode)
	}
}

// The app-loop harness cannot hold pending input across a barrier: the barrier
// INJECTS a sentinel key, and the editor settles a held chord rune and cancels
// a pending count on any key. The pending-input half of the contract is
// therefore in editor_keyset_internal_test.go, driving HandleEvent directly.

// Leaving a visual selection behind would let a later yank or delete act on a
// span the operator can no longer see.
func TestEditorSetKeysetClearsVisualSelection(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithInitialText("hello"))

	h.inject(key('v'), key('l'))
	h.barrier(sh)
	var sel string
	h.onLoop(func() { sel = ed.SelectedText() })
	if sel != "he" {
		t.Fatalf("SelectedText() = %q, want %q before the switch", sel, "he")
	}

	setKeyset(h, ed, widget.KeysetStandard)
	h.barrier(sh)
	h.onLoop(func() { sel = ed.SelectedText() })
	if sel != "" {
		t.Fatalf("SelectedText() = %q, want empty — the selection outlived the switch", sel)
	}
}

// A host that rebinds a key means it for the editor, not for one profile of
// it. The binding survives a switch, and it survives the construction option
// order that used to decide whether it applied at all.
func TestEditorSetKeysetReplaysHostKeymap(t *testing.T) {
	home := widget.KeyChord{Mode: widget.ModeInsert, Code: 'q', Ctrl: true}
	overlay := widget.Keymap{home: widget.ActLineStart}

	// The overlay is declared BEFORE the profile option: the profile installs
	// a fresh base table, and the host binding still has to be there.
	h, ed, sh := focusedEditor(t, 30, 6,
		widget.WithKeymap(overlay), widget.WithStandardKeymap())

	bound := func(when string) {
		t.Helper()
		var (
			act widget.Action
			ok  bool
		)
		h.onLoop(func() { act, ok = ed.ActionForChord(home) })
		if !ok || act != widget.ActLineStart {
			t.Fatalf("%s: Ctrl+Q -> (%v, %v), want ActLineStart, true", when, act, ok)
		}
	}
	bound("after construction")

	setKeyset(h, ed, widget.KeysetVim)
	h.barrier(sh)
	bound("after switching to Vim")

	setKeyset(h, ed, widget.KeysetStandard)
	h.barrier(sh)
	bound("after switching back to Standard")

	// And it still does its job, rather than merely appearing in the table.
	h.inject(typeString("abc")...)
	h.inject(keyMod('q', tui.ModCtrl))
	h.barrier(sh)
	if val, _, ln, col := edState(h, ed); val != "abc" || ln != 0 || col != 0 {
		t.Fatalf("state = (%q, %d,%d), want (%q, 0,0) — the replayed binding did not act", val, ln, col, "abc")
	}
}

// An out-of-range profile is a caller bug, and the editor must not be left
// with no keymap at all: it lands on Vim, the same normalization WithKeyset
// has always applied at construction.
func TestEditorSetKeysetNormalizesUnknownProfile(t *testing.T) {
	h, ed, sh := focusedEditor(t, 30, 6, widget.WithStandardKeymap())

	setKeyset(h, ed, widget.Keyset(99))
	h.barrier(sh)

	if got := keysetOf(h, ed); got != widget.KeysetVim {
		t.Fatalf("Keyset() = %v, want Vim", got)
	}
	if _, mode, _, _ := edState(h, ed); mode != widget.ModeNormal {
		t.Fatalf("mode = %v, want Normal", mode)
	}
	// The Vim table is live: `i` enters Insert rather than typing an "i".
	h.inject(key('i'))
	h.barrier(sh)
	if val, mode, _, _ := edState(h, ed); val != "" || mode != widget.ModeInsert {
		t.Fatalf("state = (%q, %v), want (\"\", Insert)", val, mode)
	}
}
