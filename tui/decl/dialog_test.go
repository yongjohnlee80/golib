package decl_test

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// dialog_test.go covers what the Dialog vocabulary REFUSES. How a dialog
// behaves on screen — opening, answering, closing itself — is the editor
// example's acceptance tests, on a running App.

func TestStandardButtonsAreDialogFlags(t *testing.T) {
	for _, c := range []struct{ value, want string }{
		{`Dialog.Yes | 1`, "is not a Dialog flag"},
		{`2048 | 1`, "is not a Dialog flag"},
		{`"yes"`, "must be Dialog flags"},
		{`Tui.Top`, "must be Dialog flags"},
	} {
		_, err := mountDoc(t, "import tui 1.0\nWindow {\n Text { }\n Dialog { standardButtons: "+
			c.value+"\n  Text { } }\n}")
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("standardButtons: %s: err = %v, want %q", c.value, err, c.want)
		}
	}
	if _, err := mountDoc(t, "import tui 1.0\nWindow {\n Text { }\n Dialog { standardButtons: Dialog.Ok | Dialog.Cancel\n  Text { } }\n}"); err != nil {
		t.Errorf("Dialog.Ok | Dialog.Cancel was refused: %v", err)
	}
}

func TestADialogHoldsExactlyItsContent(t *testing.T) {
	for _, src := range []string{
		"Window {\n Text { }\n Dialog { }\n}",
		"Window {\n Text { }\n Dialog { Text { }\n Text { } }\n}",
	} {
		if _, err := mountDoc(t, src); err == nil || !strings.Contains(err.Error(), "exactly 1 child") {
			t.Errorf("%q: err = %v, want the content refused", src, err)
		}
	}
}

// TestADialogOutsideAWindowSaysSoWhenOpened: it has nothing to open over, and
// open() says so rather than doing nothing.
func TestADialogOutsideAWindowSaysSoWhenOpened(t *testing.T) {
	a := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(),
		tuidecl.WithErrorSink(func(error) {}))...)
	tr := decl.New(a)
	spec, err := qml.QML{}.Parse([]byte("Flex {\n Button { onClicked: d.open() }\n Dialog { id: d; Text { } }\n}"))
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Mount(spec); err != nil {
		t.Fatalf("mount: %v", err)
	}
	id, _ := tr.NodeByID("d")
	err = a.Invoke(id, "open", nil)
	if err == nil || !strings.Contains(err.Error(), "not in one") {
		t.Fatalf("open() outside a Window: err = %v, want the missing Window named", err)
	}
	if err := a.Invoke(id, "open", []qml.SpecValue{{Kind: qml.SpecValueString, Raw: "x"}}); err == nil ||
		!strings.Contains(err.Error(), "no arguments") {
		t.Fatalf("open(\"x\"): err = %v, want arguments refused", err)
	}
}

func TestAMethodADialogLacksIsRefusedAtMount(t *testing.T) {
	_, err := mountDoc(t, "Window {\n Button { onClicked: d.shake() }\n Dialog { id: d; Text { } }\n}")
	if !errors.Is(err, decl.ErrNoMethod) || !strings.Contains(err.Error(), "close, forceActiveFocus, open") {
		t.Fatalf("err = %v, want ErrNoMethod listing close, forceActiveFocus, open", err)
	}
}

func TestWrapModeIsATextEnum(t *testing.T) {
	if _, err := mountDoc(t, "import tui 1.0\nText { wrapMode: Tui.WordWrap }"); err != nil {
		t.Errorf("Tui.WordWrap refused: %v", err)
	}
	if _, err := mountDoc(t, "import tui 1.0\nText { wrapMode: Tui.Top }"); err == nil ||
		!strings.Contains(err.Error(), "Tui.NoWrap or Tui.WordWrap") {
		t.Errorf("Tui.Top as a wrap mode: err = %v", err)
	}
}

// TestAComponentMayNotReplaceAVocabularyType: a file named Dialog.qml would
// otherwise take the place of the Dialog every other file means.
func TestAComponentMayNotReplaceAVocabularyType(t *testing.T) {
	a := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(),
		tuidecl.WithErrorSink(func(error) {}))...)
	tr := decl.New(a)
	fsys := fstest.MapFS{"ui/Dialog.qml": &fstest.MapFile{Data: []byte("Text { }")}}
	if err := tr.OfferModule("app.ui", "", decl.ComponentFiles(fsys, "ui")); err != nil {
		t.Fatal(err)
	}
	spec, _ := qml.QML{}.Parse([]byte("import app.ui\nWindow {\n Text { }\n}"))
	err := tr.Mount(spec)
	if !errors.Is(err, decl.ErrComponent) || !strings.Contains(err.Error(), "already a type of the vocabulary") {
		t.Fatalf("err = %v, want the component refused", err)
	}
}

// TestAttachedAndFocusPropertiesRebuildWhenTheyChange: a parent reads
// Dock.edge, and the adapter reads focus, when a node is BUILT — neither has a
// setter, so a reload that changes one rebuilds the node rather than failing
// to apply it. Asked of the adapter directly, and through a real reload.
func TestAttachedAndFocusPropertiesRebuildWhenTheyChange(t *testing.T) {
	a := tuidecl.New(tuidecl.StdRegistry(), append(tuidecl.StdProperties(),
		tuidecl.WithErrorSink(func(error) {}))...)
	for _, prop := range []string{"Dock.edge", "focus"} {
		if k := a.ClassifyProperty("StatusBar", prop); k != decl.PropConstructorOnly {
			t.Errorf("%s classifies as %v, want PropConstructorOnly", prop, k)
		}
	}
	tr := decl.New(a)
	parse := func(src string) qml.SpecTree {
		spec, err := qml.QML{}.Parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		return spec
	}
	doc := func(edge string) string {
		return "import tui 1.0\nWindow {\n Text { }\n StatusBar { id: bar; Dock.edge: Tui." + edge + " }\n}"
	}
	if err := tr.Mount(parse(doc("Bottom"))); err != nil {
		t.Fatalf("mount: %v", err)
	}
	before, _ := tr.NodeByID("bar")
	res, err := tr.Reconcile(parse(doc("Top")))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	after, _ := tr.NodeByID("bar")
	if after == before || len(res.Rebuilt) == 0 {
		t.Fatalf("moving the bar's edge did not rebuild it (node %d → %d, rebuilt %v)", before, after, res.Rebuilt)
	}
}

// TestADialogWidthIsAWholeNumberOfCells: Qt's Popup.width, in cells.
func TestADialogWidthIsAWholeNumberOfCells(t *testing.T) {
	for _, v := range []string{`-1`, `2.5`, `"wide"`} {
		_, err := mountDoc(t, "Window {\n Text { }\n Dialog { width: "+v+"\n  Text { } }\n}")
		if err == nil {
			t.Errorf("width: %s was accepted", v)
		}
	}
	if _, err := mountDoc(t, "Window {\n Text { }\n Dialog { width: 40\n  Text { } }\n}"); err != nil {
		t.Errorf("width: 40 was refused: %v", err)
	}
}

// TestADialogsShortcutsAreLiveWhileItIsOpen: a Shortcut declared in a Dialog is
// that dialog's key — it fires while the dialog is open, not after, and a
// letter typed into a field inside it is the field's.
func TestADialogsShortcutsAreLiveWhileItIsOpen(t *testing.T) {
	var fired int
	s := decltest.Run(t, 40, 10,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n Text { text: \"under\" }\n"+
			" Dialog { id: d; title: \"V\"\n  Text { text: \"body\" }\n  Shortcut { sequence: \"y\"; onActivated: App.copy() } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.copy": func([]qml.SpecValue) error { fired++; return nil }}))
	s.WaitForText(t, "under")
	s.Keys(t, decltest.Rune('y'))
	// Keys and posted work travel different lanes: give the key time to land.
	time.Sleep(50 * time.Millisecond)
	onScreenLoop(t, s, func() {})
	if fired != 0 {
		t.Fatal("a dialog's shortcut fired while the dialog was closed")
	}
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "┌ V ")
	s.Keys(t, decltest.Rune('y'))
	time.Sleep(50 * time.Millisecond)
	onScreenLoop(t, s, func() {})
	if fired != 1 {
		t.Fatalf("the dialog's shortcut fired %d times while open, want 1", fired)
	}
}

// A letter typed into a field inside the dialog is the field's, not a Shortcut's.
func TestAFieldInADialogKeepsTheLettersTypedIntoIt(t *testing.T) {
	var fired int
	s := decltest.Run(t, 40, 10,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n Text { text: \"under\" }\n"+
			" Dialog { id: d; title: \"F\"\n  TextField { }\n  Shortcut { sequence: \"y\"; onActivated: App.copy() } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.copy": func([]qml.SpecValue) error { fired++; return nil }}))
	s.WaitForText(t, "under")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("d", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "┌ F ")
	s.Keys(t, decltest.Rune('y'))
	s.WaitFor(t, "y in the field", func(sc string) bool { return strings.Contains(sc, "│ y") })
	if fired != 0 {
		t.Errorf("the shortcut took a letter typed into the dialog's field")
	}
}

// TestEnterAnswersOnlyTheNamedDefault: a dialog answers Enter with the button
// its defaultButton names — from a field too, since a field lets Enter go on
// after it submits — and with nothing when it names none.
func TestEnterAnswersOnlyTheNamedDefault(t *testing.T) {
	for _, c := range []struct {
		name, buttons, def, body, want string
	}{
		{"none named", "Dialog.Ok | Dialog.Cancel", "", "TextField { }", "open"},
		{"Ok named, from a field", "Dialog.Ok | Dialog.Cancel", "Dialog.Ok", "TextField { }", "accepted"},
		{"No named", "Dialog.Yes | Dialog.No", "Dialog.No", `Text { text: "delete it?" }`, "rejected"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var mu sync.Mutex
			var answers []string
			record := func(what string) decl.HandlerFunc {
				return func([]qml.SpecValue) error {
					mu.Lock()
					defer mu.Unlock()
					answers = append(answers, what)
					return nil
				}
			}
			got := func() []string {
				mu.Lock()
				defer mu.Unlock()
				return append([]string(nil), answers...)
			}
			def := ""
			if c.def != "" {
				def = "; defaultButton: " + c.def
			}
			s := decltest.Run(t, 40, 12,
				tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n Text { text: \"under\" }\n"+
					" Dialog { id: d; title: \"Q\"; standardButtons: "+c.buttons+def+"\n"+
					"  onAccepted: App.accepted()\n  onRejected: App.rejected()\n  "+c.body+" } }")),
				tuidecl.Singleton("demo", "1.0", "App"),
				tuidecl.Types(controls.Types()...),
				tuidecl.Handlers(map[string]decl.HandlerFunc{"App.accepted": record("accepted"), "App.rejected": record("rejected")}))
			s.WaitForText(t, "under")
			onScreenLoop(t, s, func() {
				if err := s.Program.Call("d", "open"); err != nil {
					t.Error(err)
				}
			})
			s.WaitForText(t, "┌ Q ")
			s.Keys(t, decltest.Rune('x'), tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
			if c.want == "open" {
				time.Sleep(50 * time.Millisecond) // an answer would have closed it by now
				if sc := s.String(); !strings.Contains(sc, "┌ Q ") || len(got()) != 0 {
					t.Fatalf("a dialog naming no default answered Enter (%v):\n%s", got(), sc)
				}
				return
			}
			s.WaitFor(t, c.want, func(string) bool { return len(got()) == 1 })
			if a := got()[0]; a != c.want {
				t.Errorf("Enter answered %q, want %q", a, c.want)
			}
		})
	}
}

// TestDefaultButtonNamesOneOfTheDialogsButtons: Enter's answer is stated, so a
// name the dialog does not have, or two names, is refused — never read as
// "none" or "the first".
func TestDefaultButtonNamesOneOfTheDialogsButtons(t *testing.T) {
	for _, def := range []string{"Dialog.Save", "Dialog.Ok | Dialog.Cancel"} {
		_, err := mountDoc(t, "import tui 1.0\nWindow {\n Text { }\n Dialog { standardButtons: Dialog.Ok | Dialog.Cancel; defaultButton: "+
			def+"\n  Text { } }\n}")
		if err == nil || !strings.Contains(err.Error(), "defaultButton names one of the Dialog's standardButtons") {
			t.Errorf("defaultButton: %s: err = %v, want it refused", def, err)
		}
	}
	if _, err := mountDoc(t, "import tui 1.0\nWindow {\n Text { }\n Dialog { standardButtons: Dialog.Ok | Dialog.Cancel; defaultButton: Dialog.Cancel\n  Text { } }\n}"); err != nil {
		t.Errorf("defaultButton: Dialog.Cancel was refused: %v", err)
	}
}
