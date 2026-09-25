package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// prompt_test.go runs the command prompt — a Popup holding a TextField, from
// golib's tui/decl/controls — the way a user does: Ctrl+P, a command, Enter.

// command opens the prompt, types line and runs it.
func (r *running) command(t *testing.T, line string) {
	t.Helper()
	r.key(t, ctrl('p'))
	r.waitFor(t, "the prompt", func(s string) bool { return strings.Contains(s, "┌ Command ") })
	for _, ch := range line {
		r.key(t, runeKey(ch))
	}
	r.key(t, enter)
}

func TestThePromptSavesAndQuits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := startSized(t, path, 80, 24)
	r.key(t, runeKey('i'), runeKey('a'), escape)

	r.command(t, "w")
	fileHas(t, path, "ax\n")
	r.waitFor(t, "the prompt closing", func(s string) bool { return !strings.Contains(s, "┌ Command ") })

	r.key(t, runeKey('i'), runeKey('b'), escape)
	r.command(t, "wq")
	fileHas(t, path, "bax\n")
	r.quits(t, ":wq")
}

// TestThePromptAsksBeforeQuittingOverUnsavedChanges: `q` goes through the
// same question Exit does, and quits at once when there is nothing to lose.
func TestThePromptAsksBeforeQuittingOverUnsavedChanges(t *testing.T) {
	r := start(t, "")
	r.key(t, runeKey('i'), runeKey('z'), escape)
	r.command(t, "q")
	r.waitFor(t, "the quit question", func(s string) bool { return strings.Contains(s, "Unsaved changes") })
	r.notQuit(t)

	clean := start(t, "")
	clean.command(t, "q")
	clean.quits(t, ":q over a clean buffer")
}

// TestThePromptReportsWhatItDoesNotKnowAndStartsEmpty: an unknown command is
// said in the status line, and the next time the prompt opens it is empty —
// TextField.clear() on opened.
func TestThePromptReportsWhatItDoesNotKnowAndStartsEmpty(t *testing.T) {
	r := start(t, "")
	r.command(t, "frob")
	r.waitFor(t, "the refusal", func(s string) bool { return strings.Contains(s, "not a command: frob") })
	// The placeholder shows only while the field is empty.
	r.key(t, ctrl('p'))
	r.waitFor(t, "an empty prompt", func(s string) bool {
		return strings.Contains(s, "┌ Command ") && strings.Contains(s, "w  q  wq  e <file>")
	})
}

// TestThePromptWearsTheInheritedPalette: the prompt names no colour. Its
// frame is the application palette's window, its field the panes' base —
// both from the Window.
func TestThePromptWearsTheInheritedPalette(t *testing.T) {
	r := start(t, "")
	r.key(t, ctrl('p'))
	r.waitFor(t, "the prompt", func(s string) bool { return strings.Contains(s, "┌ Command ") })
	y := rowOf(r.rows(), "w  q  wq")
	x := r.labelAt(t, y, "w  q  wq")
	if bg := r.cell(t, x, y).BG; bg != cgaBlue {
		t.Errorf("the prompt's field: bg %+v, want the inherited base %+v", bg, cgaBlue)
	}
}
