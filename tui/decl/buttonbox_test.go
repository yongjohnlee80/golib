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
  onAccepted: App.log("accepted")
  onRejected: App.log("rejected")
 }
}`

func runUnsaved(t *testing.T) (*decltest.Screen, *recorder) {
	t.Helper()
	rec := &recorder{}
	s := decltest.Run(t, 50, 12,
		tuidecl.LayoutSource("main.qml", []byte(unsavedDoc)),
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
// the dialog stays for a handler to close.
func TestADestructiveRoleButtonIsItsClickedOnly(t *testing.T) {
	s, rec := runUnsaved(t)
	s.Keys(t, decltest.Rune('d'))
	s.WaitFor(t, "clicked", func(string) bool { return len(rec.all()) == 1 })
	onScreenLoop(t, s, func() {})
	if got := logged(rec); got != "discard" {
		t.Errorf("logged %q, want discard alone", got)
	}
	if !strings.Contains(s.String(), "┌ unsaved ") {
		t.Error("a destructive answer closed the dialog on its own")
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

// No default: Enter answers only through the focused button — the first,
// Stay — never the irreversible one.
func TestEnterIsTheFocusedButtonsNeverADefault(t *testing.T) {
	s, rec := runUnsaved(t)
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
	s.WaitFor(t, "closed", func(sc string) bool { return !strings.Contains(sc, "┌ unsaved ") })
	if got := logged(rec); got != "stay,rejected" {
		t.Errorf("Enter logged %q, want the focused Stay's — never discard or save", got)
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
