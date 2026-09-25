package widget_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// filebrowser_test.go runs the browser on a real App and reads the screen. The
// claims are about what a user sees and chooses, so they are checked there.

// tree lays out a folder: files map a slash path to contents, "" is a folder.
func tree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if strings.HasSuffix(p, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

var browserStyles = widget.FilePaneStyles{
	Surface:       style.New().Background(style.ANSI(4)).Foreground(style.ANSI(7)),
	Cursor:        style.New().Background(style.ANSI(6)).Foreground(style.ANSI(0)),
	CursorBlurred: style.New().Background(style.ANSI(8)).Foreground(style.ANSI(6)),
	Border:        style.New().Foreground(style.ANSI(8)),
	FocusedBorder: style.New().Foreground(style.ANSI(15)),
}

func startBrowser(t *testing.T, b widget.FileChooser) *harness {
	t.Helper()
	h := startApp(t, b, 80, 20)
	h.onLoop(b.FocusInitial)
	h.settle()
	return h
}

// shows waits for text to reach the screen. The browser follows its list
// through the event bus, so what a key changes lands a frame or two later, and
// a single settle can read the screen before it does.
func (h *harness) shows(sub string) {
	h.t.Helper()
	h.waitFor(sub, func() bool { return strings.Contains(h.grid(), sub) })
}

func (h *harness) rowWith(sub string) int {
	h.t.Helper()
	for i, r := range strings.Split(h.grid(), "\n") {
		if strings.Contains(r, sub) {
			return i
		}
	}
	h.t.Fatalf("%q is not on screen:\n%s", sub, h.grid())
	return -1
}

func TestAnOpenBrowserListsFoldersThenFilesUnderDotDot(t *testing.T) {
	root := tree(t, map[string]string{"b.txt": "bee", "A.txt": "ay", "zdir/": "", "adir/": ""})
	b := widget.NewFileOpenView(widget.WithFileViewDir(root))
	h := startBrowser(t, b)
	defer h.stop()
	rows := []int{h.rowWith("../"), h.rowWith("adir/"), h.rowWith("zdir/"), h.rowWith("A.txt"), h.rowWith("b.txt")}
	for i := 1; i < len(rows); i++ {
		if rows[i] != rows[i-1]+1 {
			t.Fatalf("order is not .., folders, files (rows %v):\n%s", rows, h.grid())
		}
	}
	// Titled with its folder, elided from the LEFT: the tail is what differs.
	if title := h.row(rows[0] - 1); !strings.Contains(title, "…") || !strings.Contains(title, root[len(root)-12:]) {
		t.Errorf("the listing's title is %q, want the folder's tail", title)
	}
}

func TestThePreviewFollowsTheCursorAndShowsTheWholeFile(t *testing.T) {
	long := strings.Repeat("line\n", 60) + "THE-END\n"
	root := tree(t, map[string]string{"a.txt": "first file", "b.txt": long, "c.bin": "x\x00y"})
	b := widget.NewFileOpenView(widget.WithFileViewDir(root))
	h := startBrowser(t, b)
	defer h.stop()
	h.inject(key(tui.KeyDown)) // a.txt
	h.shows("first file")
	h.inject(key(tui.KeyDown)) // b.txt
	h.settle()
	var preview string
	h.onLoop(func() { preview = b.PreviewText() })
	if !strings.HasSuffix(preview, "THE-END\n") {
		t.Fatalf("the preview holds %d bytes, not the whole file", len(preview))
	}
	h.inject(key(tui.KeyDown)) // c.bin
	h.shows("binary file")
}

// TestTabMovesIntoThePreviewToReadIt: the preview is a pane the keyboard can
// enter and scroll, not a caption.
func TestTabMovesIntoThePreviewToReadIt(t *testing.T) {
	long := ""
	for i := 1; i <= 80; i++ {
		long += "row " + strings.Repeat("x", i%7) + itoaT(i) + "\n"
	}
	root := tree(t, map[string]string{"long.txt": long})
	b := widget.NewFileOpenView(widget.WithFileViewDir(root))
	h := startBrowser(t, b)
	defer h.stop()
	h.inject(key(tui.KeyDown))
	h.settle()
	if strings.Contains(h.grid(), "row 80") {
		t.Fatal("fixture: the whole file fits, so nothing needs scrolling")
	}
	// Two injections: focus moves when Tab is handled, and a G sent in the same
	// batch can reach the list before it has.
	h.inject(tab())
	h.settle()
	h.inject(key('G'))
	// WAITED FOR, not settled once: the scroll lands a frame after the key,
	// and a single settle raced it.
	h.waitFor("the end of the file", func() bool { return strings.Contains(h.grid(), "xxx80") })
}

func itoaT(i int) string {
	return strings.TrimSpace(strings.Repeat(" ", 0) + string(rune('0'+i/10)) + string(rune('0'+i%10)))
}

func TestEnterOpensFoldersAndDotDotGoesBack(t *testing.T) {
	root := tree(t, map[string]string{"sub/inner.txt": "in"})
	b := widget.NewFileOpenView(widget.WithFileViewDir(root))
	h := startBrowser(t, b)
	defer h.stop()
	h.inject(key(tui.KeyDown), key(tui.KeyEnter)) // sub/
	h.shows("inner.txt")
	h.inject(key(tui.KeyEnter)) // ../
	h.shows("sub/")
	var dir string
	h.onLoop(func() { dir = b.Dir() })
	if dir != root {
		t.Fatalf("Dir = %q after .., want %q", dir, root)
	}
}

// TestEnterOnAFileChoosesIt, and Confirm agrees: one decision for every way in.
func TestEnterOnAFileChoosesIt(t *testing.T) {
	root := tree(t, map[string]string{"pick.txt": "p", "dir/": ""})
	var chosen atomic.Int32
	b := widget.NewFileOpenView(widget.WithFileViewDir(root),
		widget.WithOnChoose(func() { chosen.Add(1) }))
	h := startBrowser(t, b)
	defer h.stop()
	var ok bool
	h.onLoop(func() { ok = b.Confirm() }) // on ../
	if ok || chosen.Load() != 0 {
		t.Fatal("confirming .. was a choice")
	}
	h.onLoop(func() {})
	h.inject(key(tui.KeyDown)) // back in root after ..? Confirm entered the parent
	h.settle()
	h.onLoop(func() { b.SetDir(root) })
	h.settle()
	h.inject(key(tui.KeyDown), key(tui.KeyDown), key(tui.KeyEnter)) // pick.txt
	h.settle()
	var sel string
	h.onLoop(func() { sel = b.Selected() })
	if chosen.Load() != 1 || sel != filepath.Join(root, "pick.txt") {
		t.Fatalf("chosen %d times, Selected %q", chosen.Load(), sel)
	}
}

func TestASaveBrowserNamesTheFile(t *testing.T) {
	root := tree(t, map[string]string{"old.txt": "o", "into/": ""})
	var chosen atomic.Int32
	b := widget.NewFileSaveView(widget.WithFileViewDir(root),
		widget.WithOnChoose(func() { chosen.Add(1) }))
	h := startBrowser(t, b)
	defer h.stop()
	h.wantContains("File name")
	// Typing goes to the name field, which has the keyboard first.
	for _, r := range "new.md" {
		h.inject(key(r))
	}
	h.inject(key(tui.KeyEnter))
	h.settle()
	var sel string
	h.onLoop(func() { sel = b.Selected() })
	if chosen.Load() != 1 || sel != filepath.Join(root, "new.md") {
		t.Fatalf("chosen %d, Selected %q", chosen.Load(), sel)
	}
	// Moving onto a file in the list names it.
	h.inject(tab(), key(tui.KeyDown), key(tui.KeyDown))
	h.settle()
	h.onLoop(func() { sel = b.Selected() })
	if sel != filepath.Join(root, "old.txt") {
		t.Fatalf("after moving onto old.txt, Selected = %q", sel)
	}
}

// TestSavingOntoAFolderNameOpensTheFolder rather than writing over it.
func TestSavingOntoAFolderNameOpensTheFolder(t *testing.T) {
	root := tree(t, map[string]string{"into/": ""})
	b := widget.NewFileSaveView(widget.WithFileViewDir(root))
	h := startBrowser(t, b)
	defer h.stop()
	for _, r := range "into" {
		h.inject(key(r))
	}
	h.settle()
	var ok bool
	var dir string
	h.onLoop(func() { ok = b.Confirm(); dir = b.Dir() })
	if ok || dir != filepath.Join(root, "into") {
		t.Fatalf("Confirm = %v, Dir = %q; want no choice and the folder opened", ok, dir)
	}
}

// TestTheCursorIsAFullRowBarBrightWithTheKeyboardAndDimWithout.
func TestTheCursorIsAFullRowBarBrightWithTheKeyboardAndDimWithout(t *testing.T) {
	root := tree(t, map[string]string{"a.txt": "a"})
	b := widget.NewFileOpenView(widget.WithFileViewDir(root),
		widget.WithFileViewStyles(browserStyles))
	h := startBrowser(t, b)
	defer h.stop()
	y := h.rowWith("../")
	x := strings.Index(h.row(y), "../")
	x = len([]rune(h.row(y)[:x]))
	far := x + 20 // past the label, still inside the pane
	cell := func(cx int) tui.CellAttrs { return h.tb.Snapshot()[y][cx].Attrs }
	cyan, grey := tui.CellColor{Kind: tui.CellColorANSI, Index: 6}, tui.CellColor{Kind: tui.CellColorANSI, Index: 8}
	if cell(x).BG != cyan || cell(far).BG != cyan {
		t.Fatalf("focused cursor: label %+v, row end %+v; want a cyan bar the row's width", cell(x), cell(far))
	}
	h.inject(tab()) // into the preview
	h.waitFor("the cursor to dim", func() bool { return cell(x).BG != cyan })
	if cell(x).BG != grey || cell(far).BG != grey || cell(x).FG != cyan {
		t.Fatalf("blurred cursor: label %+v, row end %+v; want a grey bar in cyan", cell(x), cell(far))
	}
}

// TestThePreviewIsOptional: off, the listing has the width to itself; on for a
// Save view, the same listing sits beside it under the name field.
func TestThePreviewIsOptional(t *testing.T) {
	root := tree(t, map[string]string{"a.txt": "alpha body"})
	for _, c := range []struct {
		name string
		view widget.FileChooser
		want bool
	}{
		{"open, preview off", widget.NewFileOpenView(widget.WithFileViewDir(root), widget.WithFileViewPreview(false)), false},
		{"save, preview on", widget.NewFileSaveView(widget.WithFileViewDir(root), widget.WithFileViewPreview(true)), true},
	} {
		h := startBrowser(t, c.view)
		h.inject(tab(), key(tui.KeyDown), key(tui.KeyDown)) // a.txt, from the name field or the list
		h.settle()
		if c.want {
			h.shows("alpha body")
		}
		// The pane's frame, not the word: the temp path carries this test's name.
		if got := strings.Contains(h.grid(), "┌ Preview "); got != c.want {
			t.Errorf("%s: Preview pane on screen = %v:\n%s", c.name, got, h.grid())
		}
		h.stop()
	}
}

// TestAViewListsAnyFilesystem: the file widgets read through io/fs, so a
// store that is not the local disk — here an in-memory one standing in for a
// remote — lists, moves through folders and previews the same, and reports
// its paths under its own root.
func TestAViewListsAnyFilesystem(t *testing.T) {
	mem := fstest.MapFS{
		"docs/readme.md": {Data: []byte("remote readme")},
		"docs/sub/x.txt": {Data: []byte("x")},
		"top.txt":        {Data: []byte("top")},
	}
	src := widget.FileSource{FS: mem, Root: "mem://"}
	var chosen atomic.Int32
	v := widget.NewFileOpenView(widget.WithFileViewSource(src), widget.WithFileViewDir("mem://docs"),
		widget.WithOnChoose(func() { chosen.Add(1) }))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("readme.md")
	h.shows("sub/")
	if strings.Contains(h.grid(), "top.txt") {
		t.Fatalf("the listing is not of docs:\n%s", h.grid())
	}
	h.inject(key(tui.KeyDown), key(tui.KeyDown)) // .., sub/, readme.md
	h.shows("remote readme")
	h.inject(key(tui.KeyEnter))
	h.settle()
	var sel, dir string
	h.onLoop(func() { sel, dir = v.Selected(), v.Dir() })
	if chosen.Load() != 1 || sel != "mem://docs/readme.md" || dir != "mem://docs" {
		t.Fatalf("chosen %d, Selected %q, Dir %q", chosen.Load(), sel, dir)
	}
	// Up to the root, which has no `..`.
	h.onLoop(func() { v.SetDir("mem://") })
	h.shows("top.txt")
	if strings.Contains(h.grid(), "../") {
		t.Fatalf("the root offers a way up:\n%s", h.grid())
	}
}
