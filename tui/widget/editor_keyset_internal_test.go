package widget

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// The pending-input half of the SetKeyset contract lives here rather than in
// editor_keyset_test.go because the app-loop harness cannot observe it: its
// barrier INJECTS a sentinel key (F12), and the editor treats that like any
// other key — it settles a held chord rune and cancels a pending count. A test
// that barriers between arming and switching therefore measures the barrier.
// Driving HandleEvent directly keeps the sequence exact, and an unmounted
// Editor is safe: MarkDirty, publish and NodeID are all nil-context tolerant.

func pressRune(e *Editor, r rune) {
	e.HandleEvent(tui.KeyEvent{Kind: tui.KeyPress, Code: r, Text: string(r)})
}

// A held escape-chord rune is the operator's text. The switch settles it into
// the buffer the way any other keystroke would, rather than dropping it with
// the profile that was waiting on it.
func TestSetKeysetSettlesHeldChordRune(t *testing.T) {
	e := NewEditor()
	pressRune(e, 'i')
	pressRune(e, 'a')
	pressRune(e, 'b')
	pressRune(e, 'j') // chord[0]: held, waiting for 'k' or the tick

	if e.pendingRune != 'j' {
		t.Fatalf("pendingRune = %q, want 'j' — the fixture never armed the chord", e.pendingRune)
	}
	if got := e.value(); got != "ab" {
		t.Fatalf("value = %q, want %q while the rune is held", got, "ab")
	}

	e.SetKeyset(KeysetStandard)

	if e.pendingRune != 0 {
		t.Errorf("pendingRune = %q, want 0 — the chord outlived its profile", e.pendingRune)
	}
	if got := e.value(); got != "abj" {
		t.Errorf("value = %q, want %q — the held rune was dropped instead of settled", got, "abj")
	}
}

// A count belongs to the profile that was reading it: it goes when the profile
// goes, and it stays when nothing goes.
func TestSetKeysetCountDroppedOnSwitchKeptOnNoOp(t *testing.T) {
	e := NewEditor(WithInitialText("abcdef"))
	pressRune(e, '3')
	if e.count != 3 {
		t.Fatalf("count = %d, want 3 — the fixture never armed a count", e.count)
	}

	e.SetKeyset(KeysetVim) // already Vim: nothing happens, pending input included
	if e.count != 3 {
		t.Fatalf("count = %d after a no-op switch, want 3", e.count)
	}

	e.SetKeyset(KeysetNano)
	if e.count != 0 {
		t.Fatalf("count = %d after Vim->Nano, want 0 — a stale count survived", e.count)
	}

	// And the next Vim command counts once, not three times.
	e.SetKeyset(KeysetVim)
	pressRune(e, 'x')
	if got := e.value(); got != "bcdef" {
		t.Errorf("value = %q, want %q — `x` acted on a count nobody typed", got, "bcdef")
	}
}

// An operator prefix is half a command. Carrying `d` across a switch would let
// the next `d` complete a `dd` the operator started under another profile.
func TestSetKeysetDropsOperatorPrefix(t *testing.T) {
	e := NewEditor(WithInitialText("one\ntwo"))
	pressRune(e, 'd')
	if e.pendingAct != ActDeletePrefix {
		t.Fatalf("pendingAct = %v, want ActDeletePrefix — the fixture never armed a prefix", e.pendingAct)
	}

	e.SetKeyset(KeysetStandard)
	if e.pendingAct != ActUnbound {
		t.Fatalf("pendingAct = %v, want ActUnbound", e.pendingAct)
	}

	e.SetKeyset(KeysetVim)
	pressRune(e, 'd') // a FRESH prefix, not the completion of the old one
	if got := e.value(); got != "one\ntwo" {
		t.Errorf("value = %q, want both lines — the stale prefix completed a dd", got)
	}
	pressRune(e, 'd')
	if got := e.value(); got != "two" {
		t.Errorf("value = %q, want %q — a fresh dd did not delete the line", got, "two")
	}
}

// An open undo group spans the edits that should undo together. A profile
// switch is a boundary between two ways of editing, so the next edit starts
// its own group rather than joining one recorded under the old keyset.
func TestSetKeysetClosesOpenUndoGroup(t *testing.T) {
	e := NewEditor()
	pressRune(e, 'i')
	pressRune(e, 'a')
	if !e.groupOpen {
		t.Fatalf("groupOpen = false, want true — the fixture never opened a group")
	}

	e.SetKeyset(KeysetStandard)
	if e.groupOpen {
		t.Errorf("groupOpen = true, want false — the group outlived the switch")
	}
}
