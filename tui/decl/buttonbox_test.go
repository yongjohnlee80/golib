package decl_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// buttonbox_test.go: DialogButtonBox — a dialog's own answers, by Qt's roles.

const unsavedDoc = `import tui 1.0
import demo 1.0
Window {
 Text { text: "under" }
 Dialog { id: d; title: "unsaved"
  Text { text: "save it?" }
  DialogButtonBox {
   Button { text: "S&tay";    DialogButtonBox.buttonRole: DialogButtonBox.RejectRole; onClicked: App.log("stay") }
   Button { text: "&Discard"; DialogButtonBox.buttonRole: DialogButtonBox.DestructiveRole; onClicked: App.log("discard") }
   Button { text: "&Save";    DialogButtonBox.buttonRole: DialogButtonBox.AcceptRole; onClicked: App.log("save") }
  }
  Shortcut { sequence: "Ctrl+K"; onActivated: App.log("mark") }
  onAccepted: App.log("accepted")
  onRejected: App.log("rejected")
 }
}`

func runUnsaved(t *testing.T) (*decltest.Screen, *recorder) {
	t.Helper()
	return runUnsavedDoc(t, unsavedDoc)
}

func runUnsavedDoc(t *testing.T, doc string) (*decltest.Screen, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := decltest.Run(t, 50, 12,
		tuidecl.LayoutSource("main.qml", []byte(doc)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "under")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "┌ unsaved ")
	return s, rec
}

func logged(rec *recorder) string {
	var out []string
	for _, v := range rec.all() {
		out = append(out, v.Raw)
	}
	return strings.Join(out, ",")
}

// AcceptRole: its own onClicked, then accepted, and the dialog closes.
func TestAnAcceptRoleButtonAccepts(t *testing.T) {
	s, rec := runUnsaved(t)
	s.Keys(t, decltest.Rune('s')) // &Save
	s.WaitFor(t, "closed", func(sc string) bool { return !strings.Contains(sc, "┌ unsaved ") })
	if got := logged(rec); got != "save,accepted" {
		t.Errorf("logged %q, want save,accepted", got)
	}
}

// DestructiveRole: its button's clicked only — no accepted, no rejected — and
// the dialog closes without an answer.
func TestADestructiveRoleButtonIsItsClickedOnly(t *testing.T) {
	s, rec := runUnsaved(t)
	s.Keys(t, decltest.Rune('d'))
	s.WaitFor(t, "closed", func(sc string) bool { return !strings.Contains(sc, "┌ unsaved ") })
	onScreenLoop(t, s, func() {})
	if got := logged(rec); got != "discard" {
		t.Errorf("logged %q, want discard alone", got)
	}
}

// Escape PRESSES the RejectRole button — its own onClicked runs, then
// rejected, and the dialog closes — so the way out by key is the way out by
// button, never a second, quieter one.
func TestEscapeRejects(t *testing.T) {
	s, rec := runUnsaved(t)
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	s.WaitFor(t, "closed", func(sc string) bool { return !strings.Contains(sc, "┌ unsaved ") })
	if got := logged(rec); got != "stay,rejected" {
		t.Errorf("logged %q, want stay,rejected", got)
	}
}

// No implicit answer: Save listed FIRST, and a stray Enter or Space on
// opening answers nothing — focus is on no button; a choice — a step to a
// button, or Tab — then presses the chosen one.
func TestNoKeyAnswersBeforeAButtonIsChosen(t *testing.T) {
	saveFirst := strings.Replace(unsavedDoc, `  DialogButtonBox {
   Button { text: "S&tay";    DialogButtonBox.buttonRole: DialogButtonBox.RejectRole; onClicked: App.log("stay") }
   Button { text: "&Discard"; DialogButtonBox.buttonRole: DialogButtonBox.DestructiveRole; onClicked: App.log("discard") }
   Button { text: "&Save";    DialogButtonBox.buttonRole: DialogButtonBox.AcceptRole; onClicked: App.log("save") }
`, `  DialogButtonBox {
   Button { text: "&Save";    DialogButtonBox.buttonRole: DialogButtonBox.AcceptRole; onClicked: App.log("save") }
   Button { text: "&Discard"; DialogButtonBox.buttonRole: DialogButtonBox.DestructiveRole; onClicked: App.log("discard") }
   Button { text: "S&tay";    DialogButtonBox.buttonRole: DialogButtonBox.RejectRole; onClicked: App.log("stay") }
`, 1)
	if saveFirst == unsavedDoc {
		t.Fatal("fixture: the Save-first reorder did not apply")
	}
	enter := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}
	space := tui.KeyEvent{Kind: tui.KeyPress, Code: ' '}
	right := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight}
	left := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyLeft}
	tab := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}
	for _, c := range []struct {
		name   string
		choose []tui.Event
		want   string
	}{
		{"a step right, to the first: Save", []tui.Event{right}, "save,accepted"},
		{"two steps right, to Discard", []tui.Event{right, right}, "discard"},
		{"a step left, to the last: Stay", []tui.Event{left}, "stay,rejected"},
		{"Tab, to the first: Save", []tui.Event{tab}, "save,accepted"},
	} {
		s, rec := runUnsavedDoc(t, saveFirst)
		// The mark is the barrier: keys are handled in order, so once it is
		// logged Enter and Space have been handled too — and answered nothing.
		s.Keys(t, enter, space, decltest.Ctrl('k'))
		s.WaitFor(t, "the mark", func(string) bool { return len(rec.all()) > 0 })
		if got := logged(rec); got != "mark" || !strings.Contains(s.String(), "┌ unsaved ") {
			t.Fatalf("%s: Enter and Space on opening answered — logged %q, want the mark alone", c.name, got)
		}
		s.Keys(t, append(c.choose, enter)...)
		// Every answer here closes the dialog, after its handlers have run.
		s.WaitFor(t, "the dialog answered and closed", func(sc string) bool { return !strings.Contains(sc, "┌ unsaved ") })
		if got := strings.TrimPrefix(logged(rec), "mark,"); got != c.want {
			t.Errorf("%s, then Enter: logged %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWhatADialogButtonBoxRefuses(t *testing.T) {
	for doc, want := range map[string]string{
		"Window { Text { }\n Dialog { Text { }\n DialogButtonBox { Text { } } } }":               "holds Buttons only",
		"Window { Text { }\n Dialog { Text { }\n DialogButtonBox { Button { text: \"x\" } } } }": "needs its DialogButtonBox.buttonRole",
		"Window { Text { }\n Dialog { Text { }\n DialogButtonBox { Button { text: \"x\"; DialogButtonBox.buttonRole: DialogButtonBox.AcceptRole } }\n DialogButtonBox { Button { text: \"y\"; DialogButtonBox.buttonRole: DialogButtonBox.RejectRole } } } }": "one DialogButtonBox",
		"Window { Text { }\n Dialog { Text { }\n DialogButtonBox { } } }": "at least one Button",
		"Window { Text { }\n Dialog { standardButtons: Dialog.Ok\n Text { }\n DialogButtonBox { Button { text: \"x\"; DialogButtonBox.buttonRole: DialogButtonBox.AcceptRole } } } }": "not both",
	} {
		_, err := mountDoc(t, "import tui 1.0\n"+doc)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s\n err = %v\nwant %q", doc, err, want)
		}
	}
}

// A Button's `&` marks its mnemonic, as Qt's text does: the marker is not
// drawn, and the marked letter — mid-word, S&tay's t — presses the button.
func TestAButtonsAmpersandMarksItsMnemonic(t *testing.T) {
	s, rec := runUnsaved(t)
	if strings.Contains(s.String(), "&") {
		t.Errorf("the & marker was drawn:\n%s", s.String())
	}
	s.Keys(t, decltest.Rune('t'))
	s.WaitFor(t, "closed", func(sc string) bool { return !strings.Contains(sc, "┌ unsaved ") })
	if got := logged(rec); got != "stay,rejected" {
		t.Errorf("t logged %q, want Stay's stay,rejected", got)
	}
}

// Shift+Tab from the first button returns focus to the dialog — the
// unanswered state — where Enter again answers nothing.
func TestShiftTabReturnsToTheUnansweredState(t *testing.T) {
	s, rec := runUnsaved(t)
	tab := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}
	backTab := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab, Mods: tui.ModShift}
	s.Keys(t, tab, backTab, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}, decltest.Ctrl('k'))
	s.WaitFor(t, "the mark", func(string) bool { return len(rec.all()) > 0 })
	if got := logged(rec); got != "mark" {
		t.Errorf("Enter after Tab, Shift+Tab logged %q, want the mark alone", got)
	}
}
