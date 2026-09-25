package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// prompt_test.go runs the command prompt — a Dialog holding a TextField, from
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
	r.key(t, ctrl('p'))
	r.waitFor(t, "the prompt", func(s string) bool { return strings.Contains(s, "┌ Command ") })
	x, y := r.field(t)
	if got := strings.TrimRight(string([]rune(r.rows()[y])[x:x+10]), " "); got != "" {
		t.Errorf("the reopened prompt holds %q, want it empty", got)
	}
}

// field is where the prompt's field starts: inside the card's border and
// padding, on its first row.
func (r *running) field(t *testing.T) (x, y int) {
	t.Helper()
	top := rowOf(r.rows(), "┌ Command ")
	if top < 0 {
		t.Fatalf("no prompt on screen:\n%s", strings.Join(r.rows(), "\n"))
	}
	return r.labelAt(t, top, "┌ Command ") + 2, top + 2
}

// TestThePromptIsANarrowCardWithItsHelpUnderARule: the prompt is as wide as
// its `width` says, not the screen, and its help line sits under a rule, as
// the About dialog's does.
func TestThePromptIsANarrowCardWithItsHelpUnderARule(t *testing.T) {
	r := startSized(t, "", 100, 30)
	r.key(t, ctrl('p'))
	r.waitFor(t, "the prompt", func(s string) bool { return strings.Contains(s, "┌ Command ") })
	rows := r.rows()
	top := rowOf(rows, "┌ Command ")
	row := []rune(rows[top])
	left := r.labelAt(t, top, "┌ Command ")
	right := left
	for right < len(row) && row[right] != '┐' {
		right++
	}
	if w := right - left + 1; w != 48 {
		t.Errorf("the prompt is %d wide, want its width, 48:\n%s", w, strings.Join(rows, "\n"))
	}
	help := rowOf(rows, "Enter runs")
	if help < 0 || help-2 < 0 || !strings.Contains(rows[help-2], "├") {
		t.Errorf("the help line is not under a rule:\n%s", strings.Join(rows, "\n"))
	}
	if _, y := r.field(t); y >= help-2 {
		t.Errorf("the field (row %d) is not above the rule (row %d)", y, help-2)
	}
}

// TestThePromptWearsTheInheritedPalette: the prompt names no colour. Its
// frame is the application palette's window, its field the panes' base —
// both from the Window.
func TestThePromptWearsTheInheritedPalette(t *testing.T) {
	r := start(t, "")
	r.key(t, ctrl('p'))
	r.waitFor(t, "the prompt", func(s string) bool { return strings.Contains(s, "┌ Command ") })
	x, y := r.field(t)
	if bg := r.cell(t, x, y).BG; bg != cgaBlue {
		t.Errorf("the prompt's field: bg %+v, want the inherited base %+v", bg, cgaBlue)
	}
}

// saveAsName types a name into the open Save dialog and confirms it.
func (r *running) saveAsName(t *testing.T, name string) {
	t.Helper()
	r.shows(t, "┌ Save ")
	for _, c := range name {
		r.key(t, runeKey(c))
	}
	r.key(t, enter)
}

// TestWqOnAnUnnamedBufferQuitsOnceItIsSaved: `wq` asks for a name, and the
// quit waits for the file to be written.
func TestWqOnAnUnnamedBufferQuitsOnceItIsSaved(t *testing.T) {
	dir := folderWith(t, nil)
	r := startSized(t, "", 80, 24)
	r.key(t, runeKey('i'), runeKey('q'), escape)
	r.command(t, "wq")
	r.saveAsName(t, "named.txt")
	fileHas(t, filepath.Join(dir, "named.txt"), "q")
	r.quits(t, "wq, then a name")
}

// TestWqDoesNotQuitWhenTheSaveIsCancelledOrFails: the buffer is not saved,
// so the editor stays — and a LATER Save As is only a save.
func TestWqDoesNotQuitWhenTheSaveIsCancelledOrFails(t *testing.T) {
	dir := folderWith(t, nil)
	r := startSized(t, "", 80, 24)
	r.key(t, runeKey('i'), runeKey('q'), escape)
	r.command(t, "wq")
	r.shows(t, "┌ Save ")
	r.key(t, escape)
	r.waitFor(t, "the dialog closing", func(s string) bool { return !strings.Contains(s, "┌ Save ") })
	r.notQuit(t)

	r.key(t, ctrl('s'))
	r.saveAsName(t, "later.txt")
	fileHas(t, filepath.Join(dir, "later.txt"), "q")
	r.notQuit(t)

	// A write that fails: the folder refuses new files.
	locked := folderWith(t, nil)
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	// The failed write returns its error, as every failure does; this test
	// expects it, so it keeps it rather than let it fail the test.
	failures := make(chan error, 4)
	r2 := startOpts(t, Options{Now: fixedNow, Tick: time.Hour, Sink: func(err error) { failures <- err }}, 80, 24)
	r2.key(t, runeKey('i'), runeKey('x'), escape)
	r2.command(t, "wq")
	r2.saveAsName(t, "nope.txt")
	r2.waitFor(t, "the failure", func(s string) bool { return strings.Contains(s, "write failed") })
	if err := <-failures; !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("the handler error: %v", err)
	}
	r2.notQuit(t)
}
