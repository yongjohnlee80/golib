package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// filedialog_test.go holds the Open and Save dialogs to the screen and to the
// disk. Tests that start without a file change into a temporary folder first:
// the dialogs open where the program was started, and that must not be the
// source tree.

// folderWith makes a temporary folder of files and changes into it.
func folderWith(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
	return dir
}

func (r *running) shows(t *testing.T, sub string) {
	t.Helper()
	r.waitFor(t, sub, func(s string) bool { return strings.Contains(s, sub) })
}

func (r *running) openOpen(t *testing.T) {
	t.Helper()
	r.key(t, alt('f'))
	r.shows(t, "Exit")
	r.clickLabel(t, rowOf(r.rows(), "Open"), "Open")
	r.shows(t, "┌ Open ")
}

func fileHas(t *testing.T, path, want string) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if b, err := os.ReadFile(path); err == nil && string(b) == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	b, _ := os.ReadFile(path)
	t.Fatalf("%s holds %q, want %q", path, b, want)
}

var down = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown}

func TestOpenChoosesAFileWithAPreviewAndLoadsIt(t *testing.T) {
	folderWith(t, map[string]string{"notes.md": "# groceries\nmilk\n"})
	r := startSized(t, "", 80, 24)
	r.openOpen(t)
	for _, want := range []string{"┌ Preview ", "../", "notes.md", "[ Cancel ]", "[ Open ]"} {
		r.shows(t, want)
	}
	r.key(t, down) // notes.md
	r.shows(t, "# groceries")
	r.key(t, enter)
	r.waitFor(t, "the dialog closing", func(s string) bool { return !strings.Contains(s, "┌ Open ") })
	r.shows(t, "milk")
	if last := lastNonEmpty(r.rows()); !strings.Contains(last, "notes.md") {
		t.Errorf("the status line does not name the opened file: %q", last)
	}
}

func TestOpenRefusesToDiscardUnsavedChanges(t *testing.T) {
	folderWith(t, map[string]string{"other.txt": "OTHER"})
	r := startSized(t, "", 80, 24)
	r.key(t, runeKey('i'), runeKey('k'), escape)
	r.openOpen(t)
	r.key(t, down)
	r.key(t, enter)
	r.shows(t, "unsaved changes")
	if strings.Contains(r.screen(), "OTHER") {
		t.Fatalf("the buffer was replaced over unsaved changes:\n%s", r.screen())
	}
}

func TestSaveWritesStraightToANamedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "named.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := startSized(t, path, 80, 24)
	r.key(t, runeKey('i'), runeKey('a'), escape)
	r.key(t, ctrl('s'))
	fileHas(t, path, "ax\n")
	if strings.Contains(r.screen(), "┌ Save ") {
		t.Fatalf("Save asked for a name the buffer already has:\n%s", r.screen())
	}
}

// TestSaveAsksForANameOnlyWhenThereIsNone — and not again once it has one.
func TestSaveAsksForANameOnlyWhenThereIsNone(t *testing.T) {
	dir := folderWith(t, map[string]string{"existing.txt": "e"})
	r := startSized(t, "", 80, 24)
	r.key(t, runeKey('i'), runeKey('h'), runeKey('i'), escape)
	r.key(t, ctrl('s'))
	r.shows(t, "┌ Save ")
	r.shows(t, "┌ File name ")
	r.shows(t, "existing.txt")
	for _, c := range "fresh.txt" {
		r.key(t, runeKey(c))
	}
	r.key(t, enter)
	fileHas(t, filepath.Join(dir, "fresh.txt"), "hi")
	r.waitFor(t, "the dialog closing", func(s string) bool { return !strings.Contains(s, "┌ Save ") })
	if last := lastNonEmpty(r.rows()); !strings.Contains(last, "fresh.txt") {
		t.Errorf("the status line does not name the new file: %q", last)
	}
	// It has a name now: the next save writes, without asking.
	r.key(t, runeKey('A'), runeKey('!'), escape)
	r.key(t, ctrl('s'))
	fileHas(t, filepath.Join(dir, "fresh.txt"), "hi!")
	if strings.Contains(r.screen(), "┌ Save ") {
		t.Fatalf("Save asked again for a name it has:\n%s", r.screen())
	}
}

func TestEscapeCancelsTheOpenDialog(t *testing.T) {
	folderWith(t, map[string]string{"a.txt": "A"})
	r := startSized(t, "", 80, 24)
	r.openOpen(t)
	r.key(t, escape)
	r.waitFor(t, "the dialog closing", func(s string) bool { return !strings.Contains(s, "┌ Open ") })
	r.typesAgain(t, "Escape from Open")
}

// TestTheFooterFollowsTheKeyboard: what Enter does in the list, the preview
// and the buttons is said where it is true.
func TestTheFooterFollowsTheKeyboard(t *testing.T) {
	folderWith(t, map[string]string{"a.txt": "A"})
	r := startSized(t, "", 80, 24)
	r.openOpen(t)
	r.shows(t, "Enter:open folder, or open file")
	r.key(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
	r.shows(t, "j/k:scroll")
	r.key(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
	r.shows(t, "Enter:press")
}

// TestRetroLightsThePaneInUse: the cursor bar is green where the keyboard is,
// and a grey bar in cyan once it has moved to the preview.
func TestRetroLightsThePaneInUse(t *testing.T) {
	folderWith(t, map[string]string{"a.txt": "A"})
	r := startSized(t, "", 80, 24)
	r.openOpen(t)
	y := rowOf(r.rows(), "../")
	x := r.labelAt(t, y, "../")
	r.waitFor(t, "the focused cursor", func(string) bool { return r.cell(t, x, y).BG == cgaGreen })
	if c := r.cell(t, x+10, y); c.BG != cgaGreen {
		t.Errorf("the cursor bar stops at the name: %+v", c)
	}
	r.key(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyTab})
	grey := rgb(0x55, 0x55, 0x55)
	r.waitFor(t, "the cursor to dim", func(string) bool { return r.cell(t, x, y).BG == grey })
	if c := r.cell(t, x, y); c.FG != rgb(0x55, 0xff, 0xff) || c.Mask&tui.AttrReverse != 0 {
		t.Errorf("the dimmed cursor is %+v, want cyan on grey", c)
	}
}

// TestRetroFileDialogsWearTheEditorsBlueOnTheCard: the panes are the editor's
// dark blue; the column between them is the CARD's grey, not the terminal's;
// and the editor behind stays undimmed.
func TestRetroFileDialogsWearTheEditorsBlueOnTheCard(t *testing.T) {
	folderWith(t, map[string]string{"a.txt": "A"})
	r := startSized(t, "", 100, 24)
	// Behind the dialog: the status bar's first cell, taken before it opens.
	statusY := len(r.rows()) - 1
	for statusY > 0 && strings.TrimSpace(r.rows()[statusY]) == "" {
		statusY--
	}
	r.openOpen(t)
	y := rowOf(r.rows(), "../")
	row := []rune(r.rows()[y])
	// The gap is the first `│ │` AFTER the listing's `..` — the frame of the
	// card and its padding make the same pattern at the left edge.
	gap := -1
	for i := r.labelAt(t, y, "../") + 1; i+1 < len(row); i++ {
		if row[i-1] == '│' && row[i] == ' ' && row[i+1] == '│' {
			gap = i
			break
		}
	}
	if gap < 0 {
		t.Fatalf("no gap between the panes on row %d: %q", y, string(row))
	}
	if c := r.cell(t, gap, y); c.BG != cgaGrey {
		t.Errorf("the gap between the panes is %+v, want the card's grey", c)
	}
	if c := r.cell(t, gap+2, y); c.BG != cgaBlue {
		t.Errorf("the preview pane is %+v, want the editor's blue", c)
	}
	if c := r.cell(t, 0, statusY); c.BG != cgaGrey || c.Mask&tui.AttrFaint != 0 {
		t.Errorf("behind the Open dialog the status bar is %+v, want it undimmed", c)
	}
}

var backspace = tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyBackspace}

// TestSaveAsWritesACopyUnderANewName: it starts from the file being edited —
// its folder, its name — writes the buffer to the name given, leaves the
// original alone, and the buffer is the new file from then on.
func TestSaveAsWritesACopyUnderANewName(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "orig.txt")
	if err := os.WriteFile(orig, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := startSized(t, orig, 80, 24)
	r.key(t, runeKey('i'), runeKey('a'), escape)
	r.key(t, alt('f'))
	r.shows(t, "Save As")
	r.clickLabel(t, rowOf(r.rows(), "Save As"), "Save As")
	r.shows(t, "┌ Save ")
	// Prefilled from the file being edited.
	nameRow := rowOf(r.rows(), "┌ File name ") + 1
	if !strings.Contains(r.rows()[nameRow], "orig.txt") {
		t.Fatalf("the name field does not start from the file being edited:\n%s", r.screen())
	}
	for range len("orig.txt") {
		r.key(t, backspace)
	}
	for _, c := range "copy.txt" {
		r.key(t, runeKey(c))
	}
	r.key(t, enter)
	fileHas(t, filepath.Join(dir, "copy.txt"), "ax\n")
	fileHas(t, orig, "x\n")
	r.waitFor(t, "the dialog closing", func(s string) bool { return !strings.Contains(s, "┌ Save ") })
	if last := lastNonEmpty(r.rows()); !strings.Contains(last, "copy.txt") {
		t.Errorf("the status line does not name the new file: %q", last)
	}
	// The buffer is copy.txt now: Save writes there, without asking.
	r.key(t, runeKey('A'), runeKey('!'), escape)
	r.key(t, ctrl('s'))
	fileHas(t, filepath.Join(dir, "copy.txt"), "ax!\n")
	fileHas(t, orig, "x\n")
}

// TestSaveAsStartsFromTheFileEachTime: a name typed and cancelled is not what
// the next Save As starts from.
func TestSaveAsStartsFromTheFileEachTime(t *testing.T) {
	dir := t.TempDir()
	orig := filepath.Join(dir, "keep.txt")
	if err := os.WriteFile(orig, []byte("k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := startSized(t, orig, 80, 24)
	openSaveAs := func() {
		r.key(t, alt('f'))
		r.shows(t, "Save As")
		r.clickLabel(t, rowOf(r.rows(), "Save As"), "Save As")
		r.shows(t, "┌ Save ")
	}
	openSaveAs()
	r.key(t, runeKey('Z'))
	r.key(t, escape)
	r.waitFor(t, "the dialog closing", func(s string) bool { return !strings.Contains(s, "┌ Save ") })
	openSaveAs()
	nameRow := rowOf(r.rows(), "┌ File name ") + 1
	if row := r.rows()[nameRow]; !strings.Contains(row, "keep.txt") || strings.Contains(row, "keep.txtZ") {
		t.Fatalf("Save As did not start from the file again: %q", row)
	}
}
