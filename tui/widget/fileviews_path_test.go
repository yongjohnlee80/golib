package widget_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// typePath clears the path field, which has the keyboard, types s and presses Enter.
func typePath(h *harness, s string) {
	h.inject(keyMod('u', tui.ModCtrl))
	for _, r := range s {
		h.inject(key(r))
	}
	h.inject(key(tui.KeyEnter))
}

func dirIs(h *harness, v widget.FileChooser, want string) func() bool {
	return func() bool {
		var d string
		h.onLoop(func() { d = v.Dir() })
		return d == want
	}
}

func hintIs(h *harness, v interface{ HintMessage() tui.Message }, want string) bool {
	var m tui.Message
	h.onLoop(func() { m = v.HintMessage() })
	return m == tui.Msg(want)
}

// In the Open view, Ctrl+k from the list reaches the path field, which holds the folder listed.
// A folder typed there is listed, absolute or relative; a file is chosen; a path that is not
// there, or a ~ the source has no Home for, says so in the field's title and the listing stays.
func TestTheOpenViewsPathFieldGoesWhereTyped(t *testing.T) {
	root := tree(t, map[string]string{"deep/down/n.md": "deep note", "sub/s.md": "sub note", "top.md": "top"})
	var chosen atomic.Int32
	v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{FS: os.DirFS("/"), Root: "/"}),
		widget.WithFileViewDir(root), widget.WithOnChoose(func() { chosen.Add(1) }))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("top.md")
	h.wantContains("┌ Path ")

	h.inject(keyMod('k', tui.ModCtrl))
	h.waitFor("Ctrl+k from the list in the path field", func() bool { return hintIs(h, v, "tui.files.hint.path") })
	typePath(h, filepath.Join(root, "deep", "down"))
	h.waitFor("deep/down listed", dirIs(h, v, filepath.Join(root, "deep", "down")))
	h.shows("n.md")
	h.waitFor("the keyboard back on the list", func() bool { return hintIs(h, v, "tui.files.hint.openList") })

	h.inject(keyMod('k', tui.ModCtrl))
	typePath(h, "../../sub")
	h.waitFor("a relative path listed", dirIs(h, v, filepath.Join(root, "sub")))

	h.inject(keyMod('k', tui.ModCtrl))
	typePath(h, "nothing-here")
	h.shows("Path — not found")
	h.inject(keyMod('k', tui.ModCtrl)) // already there
	typePath(h, "~/x")
	h.shows("Path — ~ is not known here")
	var dir string
	h.onLoop(func() { dir = v.Dir() })
	if dir != filepath.Join(root, "sub") || chosen.Load() != 0 {
		t.Fatalf("after two refusals: Dir %q, chosen %d", dir, chosen.Load())
	}

	typePath(h, filepath.Join(root, "top.md"))
	h.waitFor("the file chosen", func() bool { return chosen.Load() == 1 })
	var sel string
	h.onLoop(func() { sel = v.Selected() })
	if sel != filepath.Join(root, "top.md") {
		t.Fatalf("a typed file chosen: Selected %q", sel)
	}
	h.waitFor("its folder listed", dirIs(h, v, root))
	// down to the list (the field kept the keyboard), then onto sub/: the typed choice is
	// forgotten once the cursor moves
	h.inject(keyMod('j', tui.ModCtrl), key(tui.KeyUp))
	h.waitFor("Selected follows the cursor again", func() bool {
		var sel string
		h.onLoop(func() { sel = v.Selected() })
		return sel == ""
	})
}

// permFS refuses to say anything of "locked", as a folder without read permission does.
type permFS struct{ fstest.MapFS }

func (f permFS) Stat(name string) (fs.FileInfo, error) {
	if name == "locked" || strings.HasPrefix(name, "locked/") {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrPermission}
	}
	return f.MapFS.Stat(name)
}

// A path the source will not say anything of is "cannot read", not "not found".
func TestAPathTheSourceRefusesCannotBeRead(t *testing.T) {
	v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{FS: permFS{fstest.MapFS{"a.md": {Data: []byte("a")}}}, Root: "/"}),
		widget.WithFileViewDir("/"))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("a.md")
	h.inject(keyMod('k', tui.ModCtrl))
	typePath(h, "locked")
	h.shows("Path — cannot read")
	typePath(h, "gone")
	h.shows("Path — not found")
}

// ~ typed in the list opens the path field with ~/ written; ~ is the source's Home. Esc in the
// field puts back the folder listed and gives the keyboard back to the list.
func TestTildeOpensThePathFieldAndEscRestoresIt(t *testing.T) {
	root := tree(t, map[string]string{"notes/a.md": "a", "z.md": "z"})
	v := widget.NewFileFolderView(widget.WithFileViewSource(widget.FileSource{FS: os.DirFS("/"), Root: "/", Home: root}),
		widget.WithFileViewDir(root))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("z.md")

	pathIs := func(want string) func() bool {
		return func() bool {
			var p string
			h.onLoop(func() { p = v.Path() })
			return p == want
		}
	}
	h.inject(key('~'))
	h.waitFor("~/ in the field", pathIs("~/"))
	if !hintIs(h, v, "tui.files.hint.folderPath") {
		t.Fatal("~ in the list: the field does not have the keyboard")
	}
	h.inject(key('n'), key('o'), key('t'), key('e'), key('s'), key(tui.KeyEnter))
	h.waitFor("~/notes listed", dirIs(h, v, filepath.Join(root, "notes")))
	h.shows("a.md")

	h.inject(keyMod('k', tui.ModCtrl), key('x'), key('y'))
	h.waitFor("xy typed", pathIs(filepath.Join(root, "notes")+"xy"))
	h.inject(key(tui.KeyEscape))
	h.waitFor("Esc put back the folder", pathIs(filepath.Join(root, "notes")))
	h.waitFor("the list has the keyboard", func() bool { return hintIs(h, v, "tui.files.hint.listPart") })
	var d string
	h.onLoop(func() { d = v.Dir() })
	if d != filepath.Join(root, "notes") {
		t.Fatalf("Esc went somewhere: Dir %q", d)
	}
}

// In the Folder view, a file typed is not a folder and a path that is not there is not found; a
// folder is listed (as before).
func TestTheFolderViewsFieldSaysWhatATypedPathIs(t *testing.T) {
	root := tree(t, map[string]string{"in/f.md": "f"})
	v := widget.NewFileFolderView(widget.WithFileViewDir(root))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("in/")
	h.inject(keyMod('k', tui.ModCtrl))
	typePath(h, "in/f.md")
	h.shows("Folder — not a folder")
	typePath(h, "missing")
	h.shows("Folder — not found")
	typePath(h, "in")
	h.waitFor("in listed", dirIs(h, v, filepath.Join(root, "in")))
	h.hides("not found") // the status line follows a frame after the listing
}

// In the Save view, a typed path never saves: a file, or a new name in a folder that is there,
// lists its folder, fills the name and gives the name the keyboard; a folder is listed with the
// name kept; a path whose folder is not there is refused.
func TestTheSaveViewsPathFieldNamesTheTarget(t *testing.T) {
	root := tree(t, map[string]string{"docs/old.md": "old", "here.md": "here"})
	var chosen atomic.Int32
	v := widget.NewFileSaveView(widget.WithFileViewSource(widget.FileSource{FS: os.DirFS("/"), Root: "/"}),
		widget.WithFileViewDir(root), widget.WithOnChoose(func() { chosen.Add(1) }))
	h := startBrowser(t, v) // the name field has the keyboard
	defer h.stop()
	h.shows("here.md")
	nameIs := func(want string) bool {
		var n string
		h.onLoop(func() { n = v.Name() })
		return n == want
	}
	toPath := func() { h.inject(tab()) } // the name, then the path field

	h.onLoop(func() { v.SetName("kept.md") })
	toPath()
	typePath(h, "docs")
	h.waitFor("docs listed", dirIs(h, v, filepath.Join(root, "docs")))
	h.waitFor("the name has the keyboard", func() bool { return hintIs(h, v, "tui.files.hint.saveName") })
	if !nameIs("kept.md") {
		t.Fatal("a folder typed: the name was not kept")
	}

	toPath()
	typePath(h, filepath.Join(root, "docs", "old.md"))
	h.waitFor("old.md named", func() bool { return nameIs("old.md") })
	h.inject(key('x')) // the name has the keyboard: it is typed into, not saved
	h.waitFor("x typed into the name", func() bool { return nameIs("old.mdx") })
	if chosen.Load() != 0 {
		t.Fatalf("a typed file was saved (chosen %d)", chosen.Load())
	}

	toPath()
	typePath(h, filepath.Join(root, "new.md"))
	h.waitFor("the root listed", dirIs(h, v, root))
	h.waitFor("new.md named", func() bool { return nameIs("new.md") })
	var sel string
	h.onLoop(func() { sel = v.Selected() })
	if sel != filepath.Join(root, "new.md") || chosen.Load() != 0 {
		t.Fatalf("a new name typed: Selected %q, chosen %d", sel, chosen.Load())
	}

	// Ctrl+k from the list reaches the path field, past no other: the field sits right above it
	h.inject(tab(), tab()) // the name, the path field, the list
	h.waitFor("the list has the keyboard", func() bool { return hintIs(h, v, "tui.files.hint.saveList") })
	h.inject(keyMod('k', tui.ModCtrl))
	h.waitFor("Ctrl+k reached the path field", func() bool { return hintIs(h, v, "tui.files.hint.path") })
	typePath(h, filepath.Join(root, "nowhere", "x.md"))
	h.shows("Path — its folder does not exist")
	if !nameIs("new.md") {
		t.Fatal("a refused path changed the name")
	}
	if _, err := os.Stat(filepath.Join(root, "nowhere")); err == nil {
		t.Fatal("the missing folder was made")
	}
}

// Of an async source, what a typed path names is found off the loop: nothing moves while the
// check is held, and an edit to the field before its answer drops it.
func TestThePathFieldChecksAnAsyncSourceOffTheLoop(t *testing.T) {
	d := newGatedDir(vfsFiles)
	d.g.release("dir:.")
	d.g.release("dir:b") // a's listing stays held: the typed file below is chosen before it
	v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{FS: d, Root: "mem://", Async: true}),
		widget.WithFileViewDir("mem://"), widget.WithFileViewPreview(false))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("top.txt")

	h.inject(keyMod('k', tui.ModCtrl))
	typePath(h, "a")
	h.waitFor("a's check held", func() bool { return d.stats.Load() >= 1 })
	h.inject(key('x'))
	h.waitFor("the x typed", func() bool {
		var p string
		h.onLoop(func() { p = v.Path() })
		return p == "ax"
	})
	h.inject(key(tui.KeyBackspace)) // back to the very path asked about: still an edit
	h.waitFor("the x deleted", func() bool {
		var p string
		h.onLoop(func() { p = v.Path() })
		return p == "a"
	})
	d.g.release("stat:a")
	h.settle()
	var dir string
	h.onLoop(func() { dir = v.Dir() })
	if dir != "mem://" {
		t.Fatalf("an edit did not drop the check: Dir %q", dir)
	}

	typePath(h, "b")
	h.waitFor("b's check held", func() bool { return d.stats.Load() >= 2 })
	h.onLoop(func() { dir = v.Dir() })
	if dir != "mem://" {
		t.Fatalf("moved before the check's answer: Dir %q", dir)
	}
	d.g.release("stat:b")
	h.waitFor("b listed", dirIs(h, v, "mem://b"))
	h.shows("notes.md")

	// the listing moving elsewhere drops a check on its way, as an edit does
	h.inject(keyMod('k', tui.ModCtrl))
	typePath(h, "notes.md")
	h.waitFor("notes.md's check held", func() bool { return d.stats.Load() >= 3 })
	h.onLoop(func() { v.SetDir("mem://") })
	h.waitFor("the field following the root's listing", func() bool {
		var p string
		h.onLoop(func() { p = v.Path() })
		return p == "mem://" // written when the listing shows, not when it is asked for
	})
	d.g.release("stat:b/notes.md")
	h.settle()
	h.onLoop(func() { dir = v.Dir() })
	if dir != "mem://" {
		t.Fatalf("a check the listing moved past applied: Dir %q", dir)
	}

	// a typed file is chosen once its check answers, before its folder's listing arrives:
	// Selected is that file meanwhile, not the old rows' cursor
	h.inject(keyMod('k', tui.ModCtrl))
	typePath(h, "a/x.txt")
	d.g.release("stat:a/x.txt")
	h.waitFor("x.txt chosen while a's listing is held", func() bool {
		var sel string
		h.onLoop(func() { sel = v.Selected() })
		return sel == "mem://a/x.txt"
	})
	d.g.release("dir:a")
}

// A source with no Root (an fs.FS written as is) reads a relative path relative to the folder
// listed, in the path field and in the Save view's name: an empty Root is not a prefix of all.
func TestARelativePathOfARootlessSource(t *testing.T) {
	src := widget.FileSource{FS: fstest.MapFS{"a/b/c.md": {Data: []byte("c")}, "a/x.md": {Data: []byte("x")}}}
	v := widget.NewFileOpenView(widget.WithFileViewSource(src), widget.WithFileViewDir("a"))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("x.md")
	h.inject(keyMod('k', tui.ModCtrl))
	typePath(h, "b")
	h.waitFor("a/b listed", dirIs(h, v, "a/b"))
	h.shows("c.md")

	s := widget.NewFileSaveView(widget.WithFileViewSource(src), widget.WithFileViewDir("a"))
	hs := startBrowser(t, s)
	defer hs.stop()
	hs.shows("x.md")
	var sel string
	hs.onLoop(func() { s.SetName("b/new.md"); sel = s.Selected() })
	if sel != "a/b/new.md" {
		t.Fatalf("a name under a rootless source: Selected %q, want a/b/new.md", sel)
	}
}

// Of an async source, a Save view's typed target in another folder is that folder's from the
// first: Enter on the name before the folder's listing arrives saves there, not in the folder
// listed before.
func TestAnAsyncSaveTargetIsItsFolderWhileItLoads(t *testing.T) {
	d := newGatedDir(vfsFiles)
	d.g.release("dir:.")
	d.g.release("stat:b/new.md") // the typed path: not there (and, on Enter, the name's own check)
	d.g.release("stat:b")        // its folder: a folder
	// b's listing (dir:b) is held: the race is Enter on the name while it is on its way
	var chosen atomic.Int32
	v := widget.NewFileSaveView(widget.WithFileViewSource(widget.FileSource{FS: d, Root: "mem://", Async: true}),
		widget.WithFileViewDir("mem://"), widget.WithOnChoose(func() { chosen.Add(1) }))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("top.txt")
	h.inject(tab())
	typePath(h, "b/new.md") // not there: its folder is checked, then listed (held) and the name filled
	h.waitFor("new.md named", func() bool {
		var n string
		h.onLoop(func() { n = v.Name() })
		return n == "new.md"
	})
	var dir string
	h.onLoop(func() { dir = v.Dir() })
	if dir != "mem://b" || !strings.Contains(h.grid(), "top.txt") {
		t.Fatalf("the fixture: b's listing should be on its way with a's rows shown; Dir %q:\n%s", dir, h.grid())
	}
	h.inject(key(tui.KeyEnter))
	h.waitFor("saved", func() bool { return chosen.Load() == 1 })
	var sel string
	h.onLoop(func() { sel = v.Selected() })
	if sel != "mem://b/new.md" {
		t.Fatalf("saved before b's listing arrived: Selected %q, want mem://b/new.md", sel)
	}
	d.g.release("dir:b")
}

// A navigation the host starts (SetDir, Select) drops a typed path's check as it starts, not when
// its rows arrive: an answer that comes in between, about the folder being left, moves nothing.
func TestANavigationDropsATypedPathsCheckAsItStarts(t *testing.T) {
	for _, nav := range []struct {
		name string
		do   func(widget.FileChooser)
	}{
		{"SetDir", func(v widget.FileChooser) { v.SetDir("mem://a") }},
		{"Select", func(v widget.FileChooser) { v.Select("mem://a/x.txt") }},
	} {
		d := newGatedDir(vfsFiles)
		d.g.release("dir:.")
		var chosen atomic.Int32
		v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{FS: d, Root: "mem://", Async: true}),
			widget.WithFileViewDir("mem://"), widget.WithFileViewPreview(false), widget.WithOnChoose(func() { chosen.Add(1) }))
		h := startBrowser(t, v)
		h.shows("top.txt")
		h.inject(keyMod('k', tui.ModCtrl))
		typePath(h, "top.txt") // a file: would be chosen
		h.waitFor(nav.name+": top.txt's check held", func() bool { return d.stats.Load() >= 1 })
		h.onLoop(func() { nav.do(v) }) // a's listing starts, and is held
		d.g.release("stat:top.txt")    // the old check answers BEFORE the new rows arrive
		h.settle()
		var dir, sel string
		h.onLoop(func() { dir, sel = v.Dir(), v.Selected() })
		if chosen.Load() != 0 || dir != "mem://a" || sel == "mem://top.txt" {
			t.Errorf("%s: an answer about the folder left applied: chosen %d, Dir %q, Selected %q", nav.name, chosen.Load(), dir, sel)
		}
		d.g.release("dir:a")
		h.stop()
	}
}

// A source with no filesystem is the local disk, and keeps the Home it was given: ~ goes there.
func TestALocalSourceKeepsTheHomeItWasGiven(t *testing.T) {
	root := tree(t, map[string]string{"in-home/h.md": "h", "top.md": "t"})
	v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{Home: filepath.Join(root, "in-home")}),
		widget.WithFileViewDir(root), widget.WithFileViewPreview(false))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("top.md")
	h.inject(keyMod('k', tui.ModCtrl))
	typePath(h, "~")
	h.waitFor("~ listed", dirIs(h, v, filepath.Join(root, "in-home")))
	h.shows("h.md")
}

// The path field says "not found" for what names nothing it can go to: an empty path, and a path
// that climbs out of its source. The folder listed stays as it was.
func TestThePathFieldSaysAnEmptyPathAndOneOutsideTheSourceAreNotFound(t *testing.T) {
	src := widget.FileSource{FS: fstest.MapFS{"in/f.md": {}}, Root: "/"}
	v := widget.NewFileFolderView(widget.WithFileViewSource(src), widget.WithFileViewDir("/"))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("in/")
	h.inject(keyMod('k', tui.ModCtrl)) // to the path field
	typePath(h, "")
	h.shows("Folder — not found")
	typePath(h, "in")
	h.waitFor("in listed", dirIs(h, v, "/in"))
	h.hides("not found")
	typePath(h, "../../x")
	h.shows("Folder — not found")
	if !dirIs(h, v, "/in")() {
		t.Error("a path outside the source moved the listing")
	}
}
