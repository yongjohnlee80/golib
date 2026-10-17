package widget

import (
	"fmt"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
)

// oldInputKey is TextInput.handleKey at golib 6a0b271, verbatim but for its receiver: the oracle of
// the no-map differential, which compares effects (value, cursor, selection, completion, submits and
// the answer) from several starting states.
func oldInputKey(t *TextInput, e tui.KeyEvent) bool {
	if e.Kind == tui.KeyRelease {
		return false
	}
	shift := e.Mods&tui.ModShift != 0
	word := e.Mods&(tui.ModCtrl|tui.ModAlt) != 0
	if e.Code == tui.KeyTab && e.Mods&^tui.ModShift == 0 && t.completer != nil {
		return t.complete(shift)
	}

	switch e.Code {
	case tui.KeyEnter:
		if t.validate != nil {
			if err := t.validate(t.Value()); err != nil {
				t.verr = err
				t.MarkDirty()
				return true
			}
		}
		t.verr = nil
		t.MarkDirty()
		// The hook runs HERE, on the input lane, before this handler returns
		// and therefore before any further keystroke is dispatched. The event
		// follows and is delivered on the program lane as it always was.
		if t.onSubmit != nil {
			t.onSubmit(t.Value())
		}
		t.publish(SubmitEvent{Owner: t.NodeID(), Value: t.Value()})
		// NOT CONSUMED, as QLineEdit ignores Return after emitting accepted:
		// Enter goes on to the parent, so a field in a dialog lets the dialog
		// answer with its default button. A value the validator refuses (above)
		// does hold Enter — a dialog must not close over a field showing its
		// error.
		return false
	case tui.KeyBackspace:
		switch lo, hi, ok := t.selection(); {
		case ok:
			t.deleteRange(lo, hi)
		case word && t.wordLeft() < t.cur:
			t.deleteRange(t.wordLeft(), t.cur)
		case !word && t.cur > 0:
			t.deleteRange(t.cur-1, t.cur)
		default:
			return true // consumed; nothing to delete
		}
		t.edited()
		return true
	case tui.KeyDelete:
		switch lo, hi, ok := t.selection(); {
		case ok:
			t.deleteRange(lo, hi)
		case t.cur < len(t.cs):
			t.deleteRange(t.cur, t.cur+1)
		default:
			return true
		}
		t.edited()
		return true
	case tui.KeyLeft:
		if word {
			t.moveTo(t.wordLeft(), shift)
		} else {
			t.moveTo(t.cur-1, shift)
		}
		return true
	case tui.KeyRight:
		if word {
			t.moveTo(t.wordRight(), shift)
		} else {
			t.moveTo(t.cur+1, shift)
		}
		return true
	case tui.KeyHome:
		t.moveTo(0, shift)
		return true
	case tui.KeyEnd:
		t.moveTo(len(t.cs), shift)
		return true
	}

	// Readline subset (Ctrl+A/E/U/W).
	if e.Mods&tui.ModCtrl != 0 {
		switch e.Code {
		case 'a':
			t.moveTo(0, false)
			return true
		case 'e':
			t.moveTo(len(t.cs), false)
			return true
		case 'u':
			if t.cur > 0 {
				t.deleteRange(0, t.cur)
				t.edited()
			}
			return true
		case 'w':
			if w := t.wordLeft(); w < t.cur {
				t.deleteRange(w, t.cur)
				t.edited()
			}
			return true
		}
		return false
	}

	// Printable text (never Tab — focus traversal is framework-owned).
	if e.Text != "" && e.Mods&nonTextMods == 0 && e.Code != tui.KeyTab {
		t.insert(e.Text)
		return true
	}
	return false
}

// hyperOrMeta is the deviation key.Of makes on purpose: a key with Hyper or Meta held
// matches no command (the old TextInput moved the cursor under them, as though they were not held).
//
// And one the old field got wrong: its completion compared the raw Mods (e.Mods&^ModShift == 0), so
// a lock bit a kitty terminal reports (Num Lock, Caps Lock) stopped Tab completing. Every binding now
// drops lock bits, as Mods.Chord's contract says it must.
func hyperOrMeta(e tui.KeyEvent) string {
	switch {
	case e.Mods&(tui.ModHyper|tui.ModMeta) != 0:
		return "Hyper or Meta held matches no command"
	case e.Code == tui.KeyTab && e.Mods&tui.LockMods != 0:
		return "a lock bit no longer disables Tab completion"
	}
	return noTextShiftedPrintable(e)
}

func TestTextInputKeysMatchTheOldHandlerWithNoMap(t *testing.T) {
	completer := func(text string, cursor int) (int, []string) { return 0, []string{"alpha", "beta"} }
	for _, st := range []struct {
		name string
		opts []TextInputOption
		prep func(*TextInput)
	}{
		{"empty", nil, func(*TextInput) {}},
		{"words, cursor at the end", []TextInputOption{WithInitialValue("hello big world")}, func(*TextInput) {}},
		{"words, cursor mid", []TextInputOption{WithInitialValue("hello big world")}, func(t *TextInput) { t.cur = 7 }},
		{"a selection", []TextInputOption{WithInitialValue("hello big world")}, func(t *TextInput) { t.cur, t.anchor = 9, 6 }},
		{"a completer", []TextInputOption{WithInitialValue("al"), WithCompleter(completer)}, func(*TextInput) {}},
		{"a refusing validator", []TextInputOption{WithInitialValue("x"), WithValidate(func(string) error { return fmt.Errorf("no") })}, func(*TextInput) {}},
	} {
		run := func(handle func(*TextInput, tui.KeyEvent) bool) func(tui.KeyEvent) string {
			return func(e tui.KeyEvent) string {
				var submits []string
				ti := NewTextInput(append(append([]TextInputOption(nil), st.opts...), WithOnSubmit(func(v string) { submits = append(submits, v) }))...)
				st.prep(ti)
				ret := handle(ti, e)
				lo, hi, sel := ti.selection()
				return fmt.Sprintf("ret=%v value=%q cur=%d sel=%v[%d,%d] verr=%v submits=%q", ret, ti.Value(), ti.cur, sel, lo, hi, ti.verr != nil, submits)
			}
		}
		keyGridDiff(t, "TextInput ("+st.name+")", run(oldInputKey), run((*TextInput).handleKey), hyperOrMeta)
	}
}
