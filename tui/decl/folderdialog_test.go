package decl_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// folderdialog_test.go: a FolderDialog is the picker's layout over a folder —
// the document's fields, then the path, over the listing, the preview beside
// it — and answers Select with the folder the path holds.

var notesFS = fstest.MapFS{
	"notes/a.md":       {Data: []byte("# alpha\n")},
	"notes/deep/b.md":  {Data: []byte("# beta\n")},
	"elsewhere/c.md":   {Data: []byte("# gamma\n")},
	"notes/readme.txt": {Data: []byte("plain\n")},
}

func runFolderDialog(t *testing.T, rec *recorder) *decltest.Screen {
	t.Helper()
	return runFolderDialogWith(t, rec, "", "")
}

// runFolderDialogWith is runFolderDialog with more of the Window's properties (window) and of
// the dialog's (dialog), each a line of QML.
func runFolderDialogWith(t *testing.T, rec *recorder, window, dialog string) *decltest.Screen {
	t.Helper()
	s := decltest.Run(t, 90, 22,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+`Window {
    `+window+`
    Text { text: "behind" }
    FolderDialog {
        id: fd
        title: "add a workspace"
        currentFolder: "/notes"
        `+dialog+`
        Frame { title: "title"; TextField { id: wsName; text: "untitled" } }
        onAccepted: App.use(wsName.text, selectedFolder)
    }
}`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Files(widget.FileSource{FS: notesFS, Root: "/"}),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.use": rec.handler}))
	s.WaitForText(t, "behind")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("fd", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "add a workspace")
	return s
}

// TestAFolderDialogIsThePickersLayout: the document's field on top, holding
// its text, then the path at the folder, over the listing; the preview beside
// them; Select and Close (q) beneath.
func TestAFolderDialogIsThePickersLayout(t *testing.T) {
	s := runFolderDialog(t, &recorder{})
	s.WaitFor(t, "the whole picker", func(sc string) bool {
		return strings.Contains(sc, "untitled") && strings.Contains(sc, "/notes") && strings.Contains(sc, "a.md") &&
			strings.Contains(sc, "Preview") && strings.Contains(sc, "[ Close (q) ]") && strings.Contains(sc, "[ Select ]")
	})
	rows := strings.Split(s.String(), "\n")
	at := func(sub string) int {
		for i, r := range rows {
			if strings.Contains(r, sub) {
				return i
			}
		}
		t.Fatalf("%q is not on screen:\n%s", sub, s)
		return -1
	}
	if title, folder, list, preview := at("┌ title"), at("┌ Folder"), at("a.md"), at("┌ Preview"); !(title < folder && folder < list && preview == title) {
		t.Fatalf("rows: title %d, Folder %d, list %d, Preview %d; want title over Folder over the list, the preview beside the title\n%s",
			title, folder, list, preview, s)
	}
}

// TestSelectAnswersWithTheFieldAndTheFolder: into a folder by the listing,
// then Select: the handler has what the document's field holds, and the
// folder the path followed to.
func TestSelectAnswersWithTheFieldAndTheFolder(t *testing.T) {
	rec := &recorder{}
	s := runFolderDialog(t, rec)
	s.WaitForText(t, "untitled")
	s.Keys(t, decltest.Ctrl('u')) // the title field has the keyboard first
	s.Keys(t, decltest.Type("kb")...)
	s.Keys(t, decltest.Ctrl('j'), decltest.Ctrl('j'))                                                                    // to the path, then the listing
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}) // deep/
	s.WaitForText(t, "b.md")
	s.Keys(t, decltest.Rune('s')) // Select, its mnemonic, from the listing
	s.WaitFor(t, "the dialog closed", func(sc string) bool { return !strings.Contains(sc, "add a workspace") })
	got := rec.args()
	if len(got) != 2 || got[0].Raw != "kb" || got[1].Raw != "/notes/deep" {
		t.Fatalf("App.use got %v, want (kb, /notes/deep)", got)
	}
}

// TestAFolderDialogRefusesAPathThatIsNotAFolder, and stays open; q closes it
// from the listing.
func TestAFolderDialogRefusesAPathThatIsNotAFolder(t *testing.T) {
	rec := &recorder{}
	s := runFolderDialog(t, rec)
	s.WaitForText(t, "untitled")
	s.Keys(t, decltest.Ctrl('j')) // the path field
	s.Keys(t, decltest.Ctrl('u'))
	s.Keys(t, decltest.Type("/notes/a.md")...)
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab}) // past the listing and preview
	s.Keys(t, decltest.Rune('s'))
	s.WaitForText(t, "not a folder")
	if got := rec.args(); len(got) != 0 {
		t.Fatalf("a file was chosen as the folder: %v", got)
	}
	s.Keys(t, decltest.Ctrl('j'), decltest.Rune('q'))
	s.WaitFor(t, "q closed it", func(sc string) bool { return !strings.Contains(sc, "add a workspace") })
}

// args is what the handler got, the last call's.
func (r *recorder) args() []qml.SpecValue {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]qml.SpecValue(nil), r.got...)
}

// TestAFolderDialogStartsAtItsSelectedFolder: selectedFolder, set, is where the dialog opens,
// not its currentFolder; preview: false leaves the listing the whole right of the left column.
func TestAFolderDialogStartsAtItsSelectedFolder(t *testing.T) {
	s := runFolderDialogWith(t, &recorder{}, "", `selectedFolder: "/notes/deep"
        preview: false`)
	s.WaitFor(t, "deep listed", func(sc string) bool { return strings.Contains(sc, "b.md") && strings.Contains(sc, "/notes/deep") })
	if strings.Contains(s.String(), "Preview") {
		t.Fatalf("preview: false, and a preview is on screen:\n%s", s)
	}
}

// TestAFolderDialogWearsTheWindowsPalette: the path field and the listing are dressed in the
// palette the dialog inherits, as a FileDialog's view is.
func TestAFolderDialogWearsTheWindowsPalette(t *testing.T) {
	s := runFolderDialogWith(t, &recorder{}, `palette.base: "blue"; palette.text: "white"`, "")
	s.WaitForText(t, "a.md")
	waitBG(t, s, "/notes", ansi(blue))
}

// TestAFolderDialogsShortcutIsItsKey: a Shortcut among the dialog's children fires while it is
// open, as a Dialog's does.
func TestAFolderDialogsShortcutIsItsKey(t *testing.T) {
	rec := &recorder{}
	s := runFolderDialogWith(t, rec, "", `Shortcut { sequence: "Ctrl+T"; onActivated: App.use("shortcut", "") }`)
	s.WaitForText(t, "untitled")
	s.Keys(t, decltest.Ctrl('t'))
	s.WaitFor(t, "the shortcut", func(string) bool { got := rec.args(); return len(got) > 0 && got[0].Raw == "shortcut" })
}

// TestAFolderDialogRefusesWhatItCannotTake: its buttons are its own, and its properties are typed.
func TestAFolderDialogRefusesWhatItCannotTake(t *testing.T) {
	for name, c := range map[string]struct{ src, want string }{
		"a button box":             {`FolderDialog { DialogButtonBox { Button { text: "x"; DialogButtonBox.buttonRole: DialogButtonBox.AcceptRole } } }`, "buttons are its own"},
		"a dim that is not a bool": {`FolderDialog { dim: "yes" }`, "dim"},
		"a FileDialog's children":  {`FileDialog { Text { text: "x" } }`, "takes no children"},
		"a FileDialog's bad mode":  {`FileDialog { fileMode: FileDialog.Nope }`, "fileMode"},
	} {
		if _, err := mountDoc(t, "import tui 1.0\nWindow { "+c.src+" }"); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want an error naming %q", name, err, c.want)
		}
	}
}

// TestASaveDialogsNameStartsEmptyEachOpening: with no selectedFile bound, a name typed and then
// closed away is not there the next time the dialog opens.
func TestASaveDialogsNameStartsEmptyEachOpening(t *testing.T) {
	s := decltest.Run(t, 90, 22,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\n"+`Window {
    Text { text: "behind" }
    FileDialog {
        id: sd
        title: "new file"
        fileMode: Tui.SaveFile
        currentFolder: "/notes"
    }
}`)),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Files(widget.FileSource{FS: notesFS, Root: "/"}))
	s.WaitForText(t, "behind")
	open := func() {
		onScreenLoop(t, s, func() {
			if err := s.Program.Call("sd", "open"); err != nil {
				t.Error(err)
			}
		})
		s.WaitForText(t, "new file")
	}
	open()
	s.Keys(t, decltest.Type("draft-name.md")...) // the name field has the keyboard
	s.WaitForText(t, "draft-name.md")
	s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	s.WaitFor(t, "the dialog closed", func(sc string) bool { return !strings.Contains(sc, "new file") })
	open()
	s.WaitForText(t, "a.md")
	if strings.Contains(s.String(), "draft-name.md") {
		t.Fatalf("the name typed and closed away is back:\n%s", s)
	}
}
