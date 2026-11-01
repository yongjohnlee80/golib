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

// A rejected handler may open its dialog again ("ask again"): Escape, which
// presses the RejectRole button, then leaves the reopened dialog open, never
// closed again by the dismissal that raised rejected.
func TestARejectedHandlerMayReopenItsDialog(t *testing.T) {
	again := strings.Replace(unsavedDoc, `  onRejected: App.log("rejected")`,
		`  onRejected: d.open()
  onClosed: App.log("closed")`, 1)
	if again == unsavedDoc {
		t.Fatal("fixture: the reopening onRejected did not apply")
	}
	s, rec := runUnsavedDoc(t, again)
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	// the first opening's closed is still raised, once, after its rejected
	s.WaitFor(t, "the Reject button pressed", func(string) bool { return logged(rec) == "stay,closed" })
	onScreenLoop(t, s, func() {})
	// the reopened dialog is open if it answers: closed again, &Save reaches nothing
	s.Keys(t, decltest.Rune('s'))
	s.WaitFor(t, "the reopened dialog accepted", func(string) bool { return logged(rec) == "stay,closed,save,accepted,closed" })
	s.WaitFor(t, "closed by its answer", func(sc string) bool { return !strings.Contains(sc, "┌ unsaved ") })
}

// Every way out raises closed, once, after its answer: a consumer that marks an
// answer in accepted or rejected clears the mark in the closed that follows, so
// an answer never followed by closed would leave the mark set and swallow the
// next close(). Escape: stay, rejected, closed; Save:
// save, accepted, closed; Discard: discard, closed; close(): closed alone.
func TestEveryWayOutRaisesClosedAfterItsAnswer(t *testing.T) {
	doc := strings.Replace(unsavedDoc, `  onRejected: App.log("rejected")`,
		`  onRejected: App.log("rejected")
  onClosed: App.log("closed")`, 1)
	if doc == unsavedDoc {
		t.Fatal("fixture: onClosed did not apply")
	}
	s, rec := runUnsavedDoc(t, doc)
	reopen := func() {
		onScreenLoop(t, s, func() {
			if err := s.Program.Call("d", "open"); err != nil {
				t.Error(err)
			}
		})
		s.WaitForText(t, "┌ unsaved ")
	}
	want := ""
	for _, step := range []struct {
		name string
		do   func()
		logs string
	}{
		{"Escape", func() { s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape}) }, "stay,rejected,closed"},
		{"Save", func() { s.Keys(t, decltest.Rune('s')) }, "save,accepted,closed"},
		{"Discard", func() { s.Keys(t, decltest.Rune('d')) }, "discard,closed"},
		{"close()", func() {
			onScreenLoop(t, s, func() {
				if err := s.Program.Call("d", "close"); err != nil {
					t.Error(err)
				}
			})
		}, "closed"},
	} {
		if want != "" {
			reopen()
			want += ","
		}
		want += step.logs
		step.do()
		s.WaitFor(t, step.name+" closed", func(sc string) bool { return !strings.Contains(sc, "┌ unsaved ") })
		onScreenLoop(t, s, func() {})
		if got := logged(rec); got != want {
			t.Fatalf("after %s: logged %q, want %q", step.name, got, want)
		}
	}
}

// No default: Enter still presses the focused button, just like Space. A
// DialogButtonBox's buttons are not auto-defaults for other focused controls.
func TestEnterPressesFocusedButtonWithoutADefault(t *testing.T) {
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
	right := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight}
	tab := tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}
	for _, c := range []struct {
		name string
		move []tui.Event
		want string // what Enter answers on the focused button
	}{
		{"on opening, on Save", nil, "save,accepted"},
		{"after a step right, on Discard", []tui.Event{right}, "discard"},
		{"after Tab, on Discard", []tui.Event{tab}, "discard"},
	} {
		s, rec := runUnsavedDoc(t, saveFirst)
		s.Keys(t, append(c.move, enter)...)
		s.WaitFor(t, "the dialog answered and closed", func(sc string) bool { return !strings.Contains(sc, "┌ unsaved ") })
		if got := logged(rec); got != c.want {
			t.Errorf("%s, Enter: logged %q, want %q", c.name, got, c.want)
		}
	}
}

// A button outside a box keeps Enter: auto-default is the usual button.
func TestAButtonOutsideABoxTakesEnter(t *testing.T) {
	rec := &recorder{}
	s := decltest.Run(t, 30, 3,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nButton { text: \"Go\"; onClicked: App.log(\"go\") }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.log": rec.handler}))
	s.WaitForText(t, "Go")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitFor(t, "Enter pressed it", func(string) bool { return logged(rec) == "go" })
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
