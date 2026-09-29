package widget_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// picker_test.go: the file views are one layout — the fields over the list in
// the left column, the preview on the right — and Ctrl+h/j/k/l move among
// their parts.

func ctrl(r rune) tui.KeyEvent { return tui.KeyEvent{Code: r, Mods: tui.ModCtrl} }

// TestTheSaveViewsNameIsInTheLeftColumn: with a preview, the name field tops
// the left column, beside the preview, not across it.
func TestTheSaveViewsNameIsInTheLeftColumn(t *testing.T) {
	root := tree(t, map[string]string{"a.txt": "alpha"})
	b := widget.NewFileSaveView(widget.WithFileViewDir(root), widget.WithFileViewPreview(true))
	h := startBrowser(t, b)
	defer h.stop()
	h.shows("Preview")
	name, preview := h.rowWith("File name"), h.rowWith("Preview")
	if name != preview {
		t.Fatalf("the name field (row %d) is not beside the preview (row %d):\n%s", name, preview, h.grid())
	}
	if list := h.rowWith("a.txt"); list <= name {
		t.Fatalf("the list (row %d) is not under the name field (row %d):\n%s", list, name, h.grid())
	}
}

// TestAFolderViewsPathFollowsTheListing: the path field holds the folder
// listed, into a folder and back out, and the folder chosen is the field's.
func TestAFolderViewsPathFollowsTheListing(t *testing.T) {
	root := tree(t, map[string]string{"sub/inner.md": "# in", "top.md": "# top"})
	b := widget.NewFileFolderView(widget.WithFileViewDir(root))
	h := startBrowser(t, b)
	defer h.stop()
	var p, sel string
	h.onLoop(func() { p, sel = b.Path(), b.Selected() })
	if p != root || sel != root {
		t.Fatalf("at the start: path %q, Selected %q; want %q", p, sel, root)
	}
	h.inject(key(tui.KeyDown), key(tui.KeyEnter)) // sub/
	h.shows("inner.md")
	var ok bool
	h.onLoop(func() { p, sel, ok = b.Path(), b.Selected(), b.Confirm() })
	if want := filepath.Join(root, "sub"); p != want || sel != want || !ok {
		t.Fatalf("in sub: path %q, Selected %q, Confirm %v; want %q, a choice", p, sel, ok, want)
	}
	h.inject(key(tui.KeyEnter)) // ../
	h.shows("top.md")
	h.onLoop(func() { p = b.Path() })
	if p != root {
		t.Fatalf("back out: path %q, want %q", p, root)
	}
}

// TestAPathTypedInTheFolderFieldListsIt, and one that is not a folder says so
// and is not chosen.
func TestAPathTypedInTheFolderFieldListsIt(t *testing.T) {
	root := tree(t, map[string]string{"deep/down/note.md": "# n", "file.md": "f"})
	b := widget.NewFileFolderView(widget.WithFileViewDir(root))
	h := startBrowser(t, b)
	defer h.stop()
	h.inject(ctrl('k')) // up from the list to the path field
	h.settle()
	h.inject(ctrl('u'))
	for _, r := range filepath.Join(root, "deep", "down") {
		h.inject(key(r))
	}
	h.inject(key(tui.KeyEnter))
	h.shows("note.md")
	h.inject(ctrl('u'))
	for _, r := range filepath.Join(root, "file.md") {
		h.inject(key(r))
	}
	h.inject(key(tui.KeyEnter))
	h.shows("not a folder")
	var ok bool
	var sel string
	h.onLoop(func() { ok, sel = b.Confirm(), b.Selected() })
	if ok || sel != "" {
		t.Fatalf("a file typed as the folder: Confirm %v, Selected %q; want neither", ok, sel)
	}
	if !strings.Contains(h.grid(), "note.md") {
		t.Fatalf("a bad path changed the listing:\n%s", h.grid())
	}
}

// TestAHostsFieldsTopTheColumnAndHaveTheKeyboard: a field the host gives is
// the first part: above the path, and where typing starts.
func TestAHostsFieldsTopTheColumnAndHaveTheKeyboard(t *testing.T) {
	root := tree(t, map[string]string{"n.md": "n"})
	title := widget.NewTextInput()
	pane := widget.NewBox(title, widget.WithTitle("title"))
	b := widget.NewFileFolderView(widget.WithFileViewDir(root), widget.WithFileViewFields(pane))
	h := startBrowser(t, b)
	defer h.stop()
	h.shows("title")
	if top, folder, list := h.rowWith("title"), h.rowWith("Folder"), h.rowWith("n.md"); !(top < folder && folder < list) {
		t.Fatalf("rows title %d, Folder %d, list %d: want them in that order\n%s", top, folder, list, h.grid())
	}
	for _, r := range "kb" {
		h.inject(key(r))
	}
	h.settle()
	var got string
	h.onLoop(func() { got = title.Value() })
	if got != "kb" {
		t.Fatalf("typing reached %q, want the host's field to hold kb", got)
	}
}

// TestCtrlHJKLMoveAmongTheParts: down and up the left column, across to the
// preview and back to where the keyboard was, and nowhere past the ends.
func TestCtrlHJKLMoveAmongTheParts(t *testing.T) {
	root := tree(t, map[string]string{"n.md": "n"})
	title := widget.NewTextInput()
	b := widget.NewFileFolderView(widget.WithFileViewDir(root),
		widget.WithFileViewFields(widget.NewBox(title, widget.WithTitle("title"))))
	h := startBrowser(t, b)
	defer h.stop()
	hint := func() string {
		var s string
		h.onLoop(func() { s = b.Hint() })
		return s
	}
	step := func(k tui.KeyEvent, want string) {
		t.Helper()
		h.inject(k)
		h.settle()
		if got := hint(); !strings.Contains(got, want) {
			t.Fatalf("after Ctrl+%c the keyboard is where the footer says %q, want %q", k.Code, got, want)
		}
	}
	step(ctrl('k'), "Enter:press") // the top: stays in the title field
	step(ctrl('j'), "Enter:go to folder")
	step(ctrl('j'), "↑↓:move")
	step(ctrl('j'), "↑↓:move") // the bottom: stays on the list
	step(ctrl('l'), "scroll")
	step(ctrl('h'), "↑↓:move") // back to the list, where it was
	step(ctrl('k'), "Enter:go to folder")
	step(ctrl('l'), "scroll")
	step(ctrl('h'), "Enter:go to folder") // back to the path, where it was
}
