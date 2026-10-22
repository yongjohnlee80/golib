package widget

import "testing"

func TestMenuActionsPreserveRegisterSelectionAndUndoAcrossKeysets(t *testing.T) {
	for _, keyset := range []Keyset{KeysetVim, KeysetNano, KeysetStandard} {
		t.Run(keyset.String(), func(t *testing.T) {
			edits := 0
			e := NewEditor(WithCore(CoreInitialText("one\ntwo")), WithCore(CoreOnChange(func() { edits++ })))
			e.Core().SetKeyset(keyset)
			e.Core().Copy()
			if got, linewise := e.Core().Register(); got != "one" || !linewise || edits != 0 {
				t.Fatalf("Copy register = (%q, %t), edits %d", got, linewise, edits)
			}
			e.Core().Cut()
			if got, linewise := e.Core().Register(); e.Core().Value() != "two" || got != "one" || !linewise || edits != 1 {
				t.Fatalf("Cut value %q, register (%q, %t), edits %d", e.Core().Value(), got, linewise, edits)
			}
			e.Core().Paste()
			if e.Core().Value() != "one\ntwo" || edits != 2 {
				t.Fatalf("Paste value %q, edits %d", e.Core().Value(), edits)
			}
			e.core.doUndo()
			if e.Core().Value() != "two" {
				t.Fatalf("Paste was not its own undo group: %q", e.Core().Value())
			}
			e.core.doUndo()
			if e.Core().Value() != "one\ntwo" {
				t.Fatalf("Cut did not undo: %q", e.Core().Value())
			}
		})
	}
}

func TestMenuActionsUseVisualSelectionAndRespectReadOnlyAndYankPolicy(t *testing.T) {
	e := NewEditor(WithCore(CoreInitialText("abcd")))
	pressRune(e, 'v')
	pressRune(e, 'l')
	e.Core().Copy()
	if got, linewise := e.Core().Register(); got != "ab" || linewise || e.Core().Mode() != ModeNormal {
		t.Fatalf("visual Copy register (%q, %t), mode %v", got, linewise, e.Core().Mode())
	}
	pressRune(e, 'v')
	pressRune(e, 'l')
	e.Core().Cut()
	if e.Core().Value() != "cd" {
		t.Fatalf("visual Cut removed %q instead of the selected two characters", e.Core().Value())
	}
	e.Core().SetReadOnly(true)
	e.Core().Copy()
	if got, _ := e.Core().Register(); got != "cd" {
		t.Fatalf("read-only Copy refused unexpectedly: %q", got)
	}
	e.Core().Cut()
	e.Core().Paste()
	if e.Core().Value() != "cd" {
		t.Fatalf("read-only Cut/Paste changed text: %q", e.Core().Value())
	}

	blocked := NewEditor(WithCore(CoreInitialText("private")), WithCore(CoreYank(false)))
	blocked.Core().SetRegister("old", false)
	blocked.Core().Copy()
	if got, _ := blocked.Core().Register(); got != "old" {
		t.Fatalf("disabled yank copied into the register: %q", got)
	}
	blocked.Core().Cut() // deletion still populates only the internal register
	if got, linewise := blocked.Core().Register(); got != "private" || !linewise {
		t.Fatalf("Cut with disabled yank lost its internal register: (%q, %t)", got, linewise)
	}
}

func TestMenuActionsSettleHeldTextAndCancelPendingCountAndOperator(t *testing.T) {
	e := NewEditor()
	pressRune(e, 'i')
	pressRune(e, 'a')
	pressRune(e, 'j') // held as the first rune of the Vim escape chord
	if e.core.keys.pendingRune != 'j' || !e.core.hist.open {
		t.Fatal("fixture did not hold an insert rune in an open undo group")
	}
	e.Core().Copy()
	if e.Core().Value() != "aj" || e.core.keys.pendingRune != 0 || e.core.hist.open {
		t.Fatalf("menu action failed to settle held text and end its edit group: %q", e.Core().Value())
	}
	e.Core().Cut()
	if e.Core().Value() != "" {
		t.Fatalf("Cut after held text left %q", e.Core().Value())
	}
	e.core.doUndo()
	if e.Core().Value() != "aj" {
		t.Fatalf("undo merged typing and menu Cut: %q", e.Core().Value())
	}
	e.core.doUndo()
	if e.Core().Value() != "" {
		t.Fatalf("undo did not restore pre-insert content: %q", e.Core().Value())
	}

	e = NewEditor(WithCore(CoreInitialText("one\ntwo")))
	pressRune(e, '2')
	pressRune(e, 'd')
	if e.core.keys.op != ActDeletePrefix || e.core.keys.opCount != 2 {
		t.Fatal("fixture did not arm counted delete operator")
	}
	e.Core().Copy()
	if e.core.keys.op != ActUnbound || e.core.keys.opCount != 0 || e.core.keys.opChord != (KeyChord{}) || e.core.keys.count != 0 {
		t.Fatal("a menu action left part of the counted operator armed")
	}
	pressRune(e, 'd')
	if e.Core().Value() != "one\ntwo" || e.core.keys.op != ActDeletePrefix {
		t.Fatalf("next d completed a stale prefix: %q", e.Core().Value())
	}
	pressRune(e, 'd')
	if e.Core().Value() != "two" {
		t.Fatalf("fresh dd inherited old count: %q", e.Core().Value())
	}
}
