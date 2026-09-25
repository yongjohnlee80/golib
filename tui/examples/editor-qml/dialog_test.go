package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// dialog_test.go holds the two dialogs to what reaches the screen and what
// the program does. Nothing in editor.qml says how a dialog closes, so every
// way out below is the Dialog type's own.

const quitQ = "Are you sure to quit?"

func (r *running) quits(t *testing.T, what string) {
	t.Helper()
	select {
	case <-r.quit:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not quit:\n%s", what, r.screen())
	}
}

func (r *running) notQuit(t *testing.T) {
	t.Helper()
	select {
	case <-r.quit:
		t.Fatal("the program quit without being answered")
	case <-time.After(50 * time.Millisecond):
	}
}

var (
	escape = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape}
	enter  = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter}
)

// openQuit opens the quit dialog from File > Exit, the way the user asked for.
func (r *running) openQuit(t *testing.T) {
	t.Helper()
	r.key(t, alt('f'))
	r.waitFor(t, "the File dropdown", func(s string) bool { return strings.Contains(s, "Exit") })
	r.clickLabel(t, rowOf(r.rows(), "Exit"), "Exit")
	r.waitFor(t, "the quit dialog", func(s string) bool { return strings.Contains(s, quitQ) })
}

// typesAgain proves the editor has the keyboard: the dialog handed it back.
func (r *running) typesAgain(t *testing.T, what string) {
	t.Helper()
	r.key(t, runeKey('i'), runeKey('z'), runeKey('q'))
	r.waitFor(t, "typing after "+what, func(s string) bool { return strings.Contains(s, "zq") })
}

func TestTheExitMenuQuitsOnYes(t *testing.T) {
	r := start(t, "")
	r.openQuit(t)
	r.notQuit(t)
	r.key(t, runeKey('y'))
	r.quits(t, "Exit, then y")
}

// TestTheQuitDialogLooksLikeTheAskedFor: title in the frame, the question, a
// rule, then the buttons — and no help line, which this dialog does not have.
func TestTheQuitDialogLooksLikeTheAskedFor(t *testing.T) {
	r := start(t, "")
	r.openQuit(t)
	rows := r.rows()
	q, rule, btn := rowOf(rows, quitQ), rowOf(rows, "├"), rowOf(rows, "[ Yes ]")
	if !strings.Contains(rows[q-2], "┌ Quit ") {
		t.Errorf("the title is not in the frame above the question:\n%s", r.screen())
	}
	if rule != q+2 || btn != rule+2 || !strings.Contains(rows[btn], "[ No ]") {
		t.Errorf("rows: question %d, rule %d, buttons %d:\n%s", q, rule, btn, r.screen())
	}
	if !strings.Contains(rows[btn+2], "└") {
		t.Errorf("something sits under the buttons, and this dialog has no help line:\n%s", r.screen())
	}
	// The mnemonics are underlined: Y of Yes, N of No.
	for _, label := range []string{"Yes", "No"} {
		x := r.labelAt(t, btn, label)
		if r.cell(t, x, btn).Mask&tui.AttrUnderline == 0 {
			t.Errorf("the %c of %s is not underlined", label[0], label)
		}
	}
}

// TestNoEscapeAndTheNoButtonAllStay: every way to decline closes the dialog,
// quits nothing, and gives the editor back the keyboard.
func TestNoEscapeAndTheNoButtonAllStay(t *testing.T) {
	for name, answer := range map[string]func(*running, *testing.T){
		"n":      func(r *running, t *testing.T) { r.key(t, runeKey('n')) },
		"Escape": func(r *running, t *testing.T) { r.key(t, escape) },
		"the No button": func(r *running, t *testing.T) {
			r.clickLabel(t, rowOf(r.rows(), "[ No ]"), "No")
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := start(t, "")
			r.openQuit(t)
			answer(r, t)
			r.waitFor(t, "the dialog closing", func(s string) bool { return !strings.Contains(s, quitQ) })
			r.notQuit(t)
			r.typesAgain(t, name)
		})
	}
}

// TestTheQuitQuestionMentionsUnsavedChanges — and stops once they are saved.
func TestTheQuitQuestionMentionsUnsavedChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := start(t, path)
	r.key(t, runeKey('i'), runeKey('a'), escape)
	r.key(t, ctrl('q'))
	r.waitFor(t, "the warning", func(s string) bool { return strings.Contains(s, "Unsaved changes will be lost.") })
	r.key(t, runeKey('n'))
	r.key(t, ctrl('s'))
	for i := 0; ; i++ {
		if b, _ := os.ReadFile(path); string(b) == "ax\n" {
			break
		}
		if i > 300 {
			t.Fatalf("Ctrl+S never wrote the file")
		}
		time.Sleep(5 * time.Millisecond)
	}
	r.key(t, ctrl('q'))
	r.waitFor(t, "the quit dialog", func(s string) bool { return strings.Contains(s, quitQ) })
	if strings.Contains(r.screen(), "Unsaved") {
		t.Errorf("the question still warns after a save:\n%s", r.screen())
	}
}

// TestAboutOpensFromHelpAndClosesOnEnterOrEscape, with its help line.
func TestAboutOpensFromHelpAndClosesOnEnterOrEscape(t *testing.T) {
	for name, key := range map[string]tui.KeyEvent{"Enter": enter, "Escape": escape} {
		t.Run(name, func(t *testing.T) {
			r := start(t, "")
			r.key(t, alt('h'))
			r.waitFor(t, "the Help dropdown", func(s string) bool { return strings.Contains(s, "About") })
			r.clickLabel(t, rowOf(r.rows(), "About"), "About")
			r.waitFor(t, "the About dialog", func(s string) bool { return strings.Contains(s, "┌ About ") })
			rows := r.rows()
			btn, help := rowOf(rows, "[ OK ]"), rowOf(rows, "Enter or Esc to close")
			if help != btn+2 {
				t.Errorf("the help line is not one row under the button:\n%s", r.screen())
			}
			// Newlines are the author's: the name, a blank line, then two lines.
			name := rowOf(rows, "editor-qml")
			if !strings.Contains(rows[name+2], "A text editor") || !strings.Contains(rows[name+3], "running on golib/tui.") {
				t.Errorf("the About text lost its line breaks:\n%s", r.screen())
			}
			r.key(t, key)
			r.waitFor(t, "About closing", func(s string) bool { return !strings.Contains(s, "┌ About ") })
			r.typesAgain(t, "About")
		})
	}
}

// TestRetroDressesTheDialog: grey card, green buttons, the focused one in white.
func TestRetroDressesTheDialog(t *testing.T) {
	r := start(t, "")
	r.openQuit(t)
	rows := r.rows()
	q := rowOf(rows, quitQ)
	btn := rowOf(rows, "[ Yes ]")
	yes, no := r.labelAt(t, btn, "Yes"), r.labelAt(t, btn, "No")
	r.expect(t, []look{
		{"the question", r.labelAt(t, q, "Are"), q, cgaBlack, cgaGrey},
		{"the focused Yes", yes + 1, btn, cgaWhite, cgaGreen},
		{"No, unfocused", no + 1, btn, cgaBlack, cgaGreen},
	})
}

// TestTheLayoutsThemeImportDressesTheDialogFiles: the dialogs are separate
// files that import nothing, so the ONE theme line in editor.qml reaches them.
func TestTheLayoutsThemeImportDressesTheDialogFiles(t *testing.T) {
	r := startLayout(t, "", withImport(t, "import editor.theme.mono 1.0"))
	r.openQuit(t)
	btn := rowOf(r.rows(), "[ Yes ]")
	yes := r.labelAt(t, btn, "Yes")
	r.expect(t, []look{
		{"mono's focused Yes", yes + 1, btn, ansi(7), ansi(0)},
		{"mono's No", r.labelAt(t, btn, "No") + 1, btn, ansi(0), ansi(7)},
	})
}
