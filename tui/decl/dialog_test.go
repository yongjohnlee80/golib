package decl_test

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
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
	if !errors.Is(err, decl.ErrNoMethod) || !strings.Contains(err.Error(), "close, open") {
		t.Fatalf("err = %v, want ErrNoMethod listing close, open", err)
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
