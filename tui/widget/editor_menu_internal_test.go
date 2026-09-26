package widget

import "testing"

func TestMenuActionsPreserveRegisterSelectionAndUndoAcrossKeysets(t *testing.T) {
	for _, keyset := range []Keyset{KeysetVim, KeysetNano, KeysetStandard} {
		t.Run(keyset.String(), func(t *testing.T) {
			edits := 0
			e := NewEditor(WithInitialText("one\ntwo"), WithOnChange(func() { edits++ }))
			e.SetKeyset(keyset)
			e.Copy()
			if got, linewise := e.Register(); got != "one" || !linewise || edits != 0 {
				t.Fatalf("Copy register = (%q, %t), edits %d", got, linewise, edits)
			}
			e.Cut()
			if got, linewise := e.Register(); e.Value() != "two" || got != "one" || !linewise || edits != 1 {
				t.Fatalf("Cut value %q, register (%q, %t), edits %d", e.Value(), got, linewise, edits)
			}
			e.Paste()
			if e.Value() != "one\ntwo" || edits != 2 {
				t.Fatalf("Paste value %q, edits %d", e.Value(), edits)
			}
			e.doUndo()
			if e.Value() != "two" {
				t.Fatalf("Paste was not its own undo group: %q", e.Value())
			}
			e.doUndo()
			if e.Value() != "one\ntwo" {
				t.Fatalf("Cut did not undo: %q", e.Value())
			}
		})
	}
}

func TestMenuActionsUseVisualSelectionAndRespectReadOnlyAndYankPolicy(t *testing.T) {
	e := NewEditor(WithInitialText("abcd"))
	pressRune(e, 'v')
	pressRune(e, 'l')
	e.Copy()
	if got, linewise := e.Register(); got != "ab" || linewise || e.Mode() != ModeNormal {
		t.Fatalf("visual Copy register (%q, %t), mode %v", got, linewise, e.Mode())
	}
	pressRune(e, 'v')
	pressRune(e, 'l')
	e.Cut()
	if e.Value() != "cd" {
		t.Fatalf("visual Cut removed %q instead of the selected two characters", e.Value())
	}
	e.SetReadOnly(true)
	e.Copy()
	if got, _ := e.Register(); got != "cd" {
		t.Fatalf("read-only Copy refused unexpectedly: %q", got)
	}
	e.Cut()
	e.Paste()
	if e.Value() != "cd" {
		t.Fatalf("read-only Cut/Paste changed text: %q", e.Value())
	}

	blocked := NewEditor(WithInitialText("private"), WithYank(false))
	blocked.SetRegister("old", false)
	blocked.Copy()
	if got, _ := blocked.Register(); got != "old" {
		t.Fatalf("disabled yank copied into the register: %q", got)
	}
	blocked.Cut() // deletion still populates only the internal register
	if got, linewise := blocked.Register(); got != "private" || !linewise {
		t.Fatalf("Cut with disabled yank lost its internal register: (%q, %t)", got, linewise)
	}
}

func TestMenuActionsSettleHeldTextAndCancelPendingCountAndOperator(t *testing.T) {
	e := NewEditor()
	pressRune(e, 'i')
	pressRune(e, 'a')
	pressRune(e, 'j') // held as the first rune of the Vim escape chord
	if e.pendingRune != 'j' || !e.groupOpen {
		t.Fatal("fixture did not hold an insert rune in an open undo group")
	}
	e.Copy()
	if e.Value() != "aj" || e.pendingRune != 0 || e.groupOpen {
		t.Fatalf("menu action failed to settle held text and end its edit group: %q", e.Value())
	}
	e.Cut()
	if e.Value() != "" {
		t.Fatalf("Cut after held text left %q", e.Value())
	}
	e.doUndo()
	if e.Value() != "aj" {
		t.Fatalf("undo merged typing and menu Cut: %q", e.Value())
	}
	e.doUndo()
	if e.Value() != "" {
		t.Fatalf("undo did not restore pre-insert content: %q", e.Value())
	}

	e = NewEditor(WithInitialText("one\ntwo"))
	pressRune(e, '2')
	pressRune(e, 'd')
	if e.pendingAct != ActDeletePrefix || e.pendingCount != 2 {
		t.Fatal("fixture did not arm counted delete prefix")
	}
	e.Copy()
	if e.pendingAct != ActUnbound || e.pendingCount != 0 || e.pendingChord != (KeyChord{}) || e.count != 0 {
		t.Fatal("a menu action left part of the counted operator armed")
	}
	pressRune(e, 'd')
	if e.Value() != "one\ntwo" || e.pendingAct != ActDeletePrefix {
		t.Fatalf("next d completed a stale prefix: %q", e.Value())
	}
	pressRune(e, 'd')
	if e.Value() != "two" {
		t.Fatalf("fresh dd inherited old count: %q", e.Value())
	}
}
