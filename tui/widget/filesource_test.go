package widget_test

import (
	"context"
	"io"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
	"github.com/yongjohnlee80/golib/vfs"
	"github.com/yongjohnlee80/golib/vfs/memfs"
)

// filesource_test.go covers a VFS FileSource: listed and previewed off the
// loop, a superseded read cancelled through its context, and the Open view's
// `/` filter.

// gatedFS holds each ReadDir and Open on a path until the test releases it,
// and counts the reads whose context ended while they were held.
type gatedFS struct {
	vfs.FS
	mu        sync.Mutex
	gates     map[string]chan struct{}
	cancelled atomic.Int32
	started   atomic.Int32
	// stubborn reads ignore their context: a held read finishes when it is
	// released, as a driver that cannot be interrupted does, and answers late.
	stubborn bool
}

func newGated(t *testing.T, files map[string]string) *gatedFS {
	t.Helper()
	m := memfs.New()
	for p, body := range files {
		if dir := path.Dir(p); dir != "." {
			if err := m.MkdirAll(context.Background(), dir); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := m.WriteFile(context.Background(), p, strings.NewReader(body)); err != nil {
			t.Fatal(err)
		}
	}
	return &gatedFS{FS: m, gates: map[string]chan struct{}{}}
}

// gate is the channel that releases reads of name, made held on first use.
func (g *gatedFS) gate(name string) chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	c, ok := g.gates[name]
	if !ok {
		c = make(chan struct{})
		g.gates[name] = c
	}
	return c
}

// release lets every read of name through, now and later.
func (g *gatedFS) release(name string) { close(g.gate(name)) }

func (g *gatedFS) wait(ctx context.Context, name string) error {
	g.started.Add(1)
	if g.stubborn {
		<-g.gate(name)
		return nil
	}
	select {
	case <-g.gate(name):
		return nil
	case <-ctx.Done():
		g.cancelled.Add(1)
		return ctx.Err()
	}
}

func (g *gatedFS) ReadDir(ctx context.Context, name string) ([]vfs.FileInfo, error) {
	if err := g.wait(ctx, "dir:"+name); err != nil {
		return nil, err
	}
	return g.FS.ReadDir(ctx, name)
}

func (g *gatedFS) Open(ctx context.Context, name string, offset int64) (io.ReadCloser, error) {
	if err := g.wait(ctx, "file:"+name); err != nil {
		return nil, err
	}
	return g.FS.Open(ctx, name, offset)
}

var vfsFiles = map[string]string{
	"a/readme.md": "alpha readme",
	"a/x.txt":     "x body",
	"b/notes.md":  "bee notes",
	"top.txt":     "top",
}

// TestAVFSSourceIsListedOffTheLoop: while its read is held, the list says it
// is loading and the loop keeps running; once released, the rows are there.
func TestAVFSSourceIsListedOffTheLoop(t *testing.T) {
	g := newGated(t, vfsFiles)
	g.release("file:top.txt")
	v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{VFS: g, Root: "vfs://"}),
		widget.WithFileViewDir("vfs://"))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("loading")
	ran := false
	h.onLoop(func() { ran = true }) // the loop answers while the read is held
	if !ran || strings.Contains(h.grid(), "a/") {
		t.Fatalf("the loop ran %v; rows shown before the read was released:\n%s", ran, h.grid())
	}
	g.release("dir:.")
	h.shows("top.txt")
	h.shows("a/")
}

// TestALaterLoadSupersedesAPendingName: Select's name waits for its folder's
// listing; a SetDir before that listing arrives supersedes it, the held read is
// cancelled, and the stale listing never moves the cursor.
func TestALaterLoadSupersedesAPendingName(t *testing.T) {
	g := newGated(t, vfsFiles)
	g.release("dir:.")
	v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{VFS: g, Root: "vfs://"}),
		widget.WithFileViewDir("vfs://"), widget.WithFileViewPreview(false))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("top.txt")
	before := g.started.Load()
	h.onLoop(func() { v.Select("vfs://a/x.txt") }) // a's listing is held
	h.waitFor("a's read held", func() bool { return g.started.Load() > before })
	h.onLoop(func() { v.SetDir("vfs://b") }) // supersedes it
	g.release("dir:b")
	h.shows("notes.md")
	h.waitFor("the held listing of a cancelled", func() bool { return g.cancelled.Load() >= 1 })
	g.release("dir:a") // nothing waits on it any more
	h.settle()
	var sel, dir string
	h.onLoop(func() { sel, dir = v.Selected(), v.Dir() })
	if dir != "vfs://b" || sel == "vfs://a/x.txt" || strings.Contains(h.grid(), "x.txt") {
		t.Fatalf("Dir %q, Selected %q: the superseded listing or its name was applied:\n%s", dir, sel, h.grid())
	}
	// A Select whose listing arrives places the cursor on its name.
	h.onLoop(func() { v.Select("vfs://a/x.txt") })
	h.waitFor("Select's name placed", func() bool {
		var s string
		h.onLoop(func() { s = v.Selected() })
		return s == "vfs://a/x.txt"
	})
}

// TestARemovedViewCancelsItsReadAndAppliesNothing: a view taken off the screen
// mid-read (a dialog closed) cancels the read through its context.
func TestARemovedViewCancelsItsReadAndAppliesNothing(t *testing.T) {
	g := newGated(t, vfsFiles)
	v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{VFS: g, Root: "vfs://"}),
		widget.WithFileViewDir("vfs://"))
	root := tui.NewFlex(tui.Vertical)
	root.AddWeighted(v, 1)
	h := startApp(t, root, 80, 20)
	defer h.stop()
	h.waitFor("the listing started", func() bool { return g.started.Load() >= 1 })
	h.onLoop(func() { root.Remove(v) })
	h.waitFor("the held read cancelled", func() bool { return g.cancelled.Load() >= 1 })
	g.release("dir:.")
	h.settle()
	if strings.Contains(h.grid(), "top.txt") {
		t.Fatalf("a removed view's listing was shown:\n%s", h.grid())
	}
}

// TestAVFSPreviewIsReadOffTheLoop: the preview says it is loading until the
// file's text arrives, and moving on supersedes and cancels the read.
func TestAVFSPreviewIsReadOffTheLoop(t *testing.T) {
	g := newGated(t, vfsFiles)
	g.release("dir:a")
	v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{VFS: g, Root: "vfs://"}),
		widget.WithFileViewDir("vfs://a"))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("readme.md")
	h.inject(key(tui.KeyDown)) // .., readme.md: held
	h.shows("(loading")
	h.inject(key(tui.KeyDown)) // x.txt supersedes it
	h.waitFor("the readme's read cancelled", func() bool { return g.cancelled.Load() >= 1 })
	g.release("file:a/x.txt")
	h.shows("x body")
	g.release("file:a/readme.md")
	h.settle()
	if strings.Contains(h.grid(), "alpha readme") {
		t.Fatalf("a superseded preview was shown:\n%s", h.grid())
	}
}

// TestTheOpenViewFilters: `/` opens the filter, typing narrows the rows (`..`
// stays), Enter returns to the list with the filter kept, and Esc clears it.
// A new folder clears it too.
func TestTheOpenViewFilters(t *testing.T) {
	root := tree(t, map[string]string{"alpha.md": "a", "beta.md": "b", "gamma.txt": "g", "sub/c.md": "c"})
	v := widget.NewFileOpenView(widget.WithFileViewDir(root), widget.WithFileViewPreview(false))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("gamma.txt")
	h.inject(key('/'))
	h.shows("┌ Filter")
	h.inject(key('m'), key('d'))
	h.waitFor("the rows narrowed", func() bool {
		g := h.grid()
		return strings.Contains(g, "alpha.md") && !strings.Contains(g, "gamma.txt") && !strings.Contains(g, "sub/")
	})
	h.inject(key(tui.KeyEnter)) // back to the list, the filter kept
	h.inject(key('j'))          // the list's own key again, not the field's
	h.settle()
	if strings.Contains(h.grid(), "gamma.txt") {
		t.Fatalf("Enter dropped the filter:\n%s", h.grid())
	}
	var sel string
	h.onLoop(func() { sel = v.Selected() })
	if !strings.HasSuffix(sel, "alpha.md") {
		t.Fatalf("j in the list after the filter: Selected %q, want alpha.md (the row under ..)", sel)
	}
	h.inject(key(tui.KeyEscape)) // clears
	h.shows("gamma.txt")
	if strings.Contains(h.grid(), "┌ Filter") {
		t.Fatalf("the filter field stayed after Esc:\n%s", h.grid())
	}
	// A new folder clears a filter.
	h.inject(key('/'), key('s'), key('u'), key('b'), key(tui.KeyEnter))
	h.shows("sub/")
	h.waitFor("only sub left", func() bool { return !strings.Contains(h.grid(), "alpha.md") })
	h.inject(key(tui.KeyDown), key(tui.KeyEnter)) // .., sub/: into it
	h.shows("c.md")
	var filter string
	h.onLoop(func() { filter = h.grid() })
	if strings.Contains(filter, "┌ Filter") {
		t.Fatalf("a new folder kept the filter field:\n%s", filter)
	}
}

// TestALateAnswerIsDropped: a read that cannot be stopped answers after the one
// that superseded it; its listing and its preview are dropped, not shown over
// the newer.
func TestALateAnswerIsDropped(t *testing.T) {
	g := newGated(t, vfsFiles)
	g.stubborn = true
	g.release("dir:.")
	v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{VFS: g, Root: "vfs://"}),
		widget.WithFileViewDir("vfs://"))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("top.txt")
	before := g.started.Load()
	h.onLoop(func() { v.SetDir("vfs://a") }) // held
	h.waitFor("a's read held", func() bool { return g.started.Load() > before })
	h.onLoop(func() { v.SetDir("vfs://b") })
	g.release("dir:b")
	h.shows("notes.md")
	g.release("dir:a") // answers late
	h.settle()
	var dir string
	h.onLoop(func() { dir = v.Dir() })
	if dir != "vfs://b" || strings.Contains(h.grid(), "readme.md") {
		t.Fatalf("Dir %q: the late listing of a replaced b's:\n%s", dir, h.grid())
	}
	// The preview: notes.md's text is held, the cursor moves back to .., and
	// the late text does not replace the folder note.
	h.inject(key(tui.KeyDown)) // .., notes.md: held
	h.shows("(loading")
	h.inject(key(tui.KeyUp)) // back to ..: a folder, shown at once
	h.settle()
	g.release("file:b/notes.md") // answers late
	h.settle()
	if strings.Contains(h.grid(), "bee notes") {
		t.Fatalf("a late preview replaced the newer one:\n%s", h.grid())
	}
}

// TestTheLastFoldersRowsCannotBeChosenWhileTheNextLoads: while a VFS listing
// is held, the rows on screen are the last folder's, and none of them can be
// chosen: no old name is joined to the new folder.
func TestTheLastFoldersRowsCannotBeChosenWhileTheNextLoads(t *testing.T) {
	g := newGated(t, vfsFiles)
	g.release("dir:.")
	g.release("file:top.txt")
	var chosen atomic.Int32
	v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{VFS: g, Root: "vfs://"}),
		widget.WithFileViewDir("vfs://"), widget.WithOnChoose(func() { chosen.Add(1) }))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("top.txt")
	h.inject(key(tui.KeyDown), key(tui.KeyDown)) // a/, b/, top.txt
	var sel string
	h.waitFor("the cursor on top.txt", func() bool {
		h.onLoop(func() { sel = v.Selected() })
		return sel == "vfs://top.txt"
	})
	h.onLoop(func() { v.SetDir("vfs://b") }) // held
	var confirmed bool
	h.onLoop(func() { sel, confirmed = v.Selected(), v.Confirm() })
	if sel != "" || confirmed {
		t.Fatalf("while b loads: Selected %q, Confirm %v; want nothing chosen", sel, confirmed)
	}
	h.inject(key(tui.KeyEnter))
	h.settle()
	if chosen.Load() != 0 {
		t.Fatalf("Enter on a row of the last folder chose it while b loaded")
	}
	g.release("dir:b")
	h.shows("notes.md")
	var dir string
	h.onLoop(func() { dir = v.Dir() })
	if dir != "vfs://b" {
		t.Fatalf("Dir %q once b arrived", dir)
	}
}

// TestAReadIsStoppedWhenTheLoopTakesOver: a folder row stops the preview's read
// in flight, and an fs.FS source stops the listing's: cancelled, not left to
// finish on a worker.
func TestAReadIsStoppedWhenTheLoopTakesOver(t *testing.T) {
	g := newGated(t, vfsFiles)
	g.release("dir:a")
	v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{VFS: g, Root: "vfs://"}),
		widget.WithFileViewDir("vfs://a"))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("readme.md")
	h.inject(key(tui.KeyDown)) // readme.md: held
	h.shows("(loading")
	before := g.cancelled.Load()
	h.inject(key(tui.KeyUp)) // ..: a folder, shown at once
	h.waitFor("the preview's read stopped", func() bool { return g.cancelled.Load() > before })

	before = g.cancelled.Load()
	started := g.started.Load()
	h.onLoop(func() { v.SetDir("vfs://b") }) // held
	h.waitFor("b's read held", func() bool { return g.started.Load() > started })
	h.onLoop(func() {
		v.SetSource(widget.FileSource{FS: fstest.MapFS{"local.txt": {Data: []byte("l")}}, Root: "/"})
		v.SetDir("/")
	})
	h.waitFor("the listing's read stopped", func() bool { return g.cancelled.Load() > before })
	h.shows("local.txt")
}

// TestAFileListWaitsForItsVFSFolder: a FileList of a VFS source, before it is
// mounted, takes a folder to list at mount; mounted, while a folder loads, it
// reports the folder on its way and keeps a name to put the cursor on for
// when the rows arrive. A filter set to what it already is changes nothing.
func TestAFileListWaitsForItsVFSFolder(t *testing.T) {
	g := newGated(t, vfsFiles)
	g.release("dir:b")
	l := widget.NewFileList(widget.WithFileListSource(widget.FileSource{VFS: g, Root: "vfs://"}),
		widget.WithFileListDir("vfs://a"))
	l.SetDir("vfs://b") // not mounted: b is listed at mount, a never
	if l.Loading() || l.Dir() != "b" {
		t.Fatalf("before mount: Loading %v, Dir %q; want b, nothing in flight", l.Loading(), l.Dir())
	}
	h := startApp(t, l, 60, 12)
	defer h.stop()
	h.waitFor("b listed", func() bool { return strings.Contains(h.grid(), "notes.md") })
	h.onLoop(func() { l.SetDir("vfs://a") }) // held
	var loading, placed bool
	var dir string
	h.onLoop(func() { loading, dir, placed = l.Loading(), l.Dir(), l.SetCurrent("x.txt") })
	if !loading || dir != "a" || placed {
		t.Fatalf("while a loads: Loading %v, Dir %q, SetCurrent placed %v; want a on its way, the name kept", loading, dir, placed)
	}
	g.release("dir:a")
	h.waitFor("the kept name placed", func() bool {
		var p string
		var ok bool
		h.onLoop(func() { p, _, ok = l.Current() })
		return ok && p == "a/x.txt"
	})
	// The filter puts the cursor on the first row; the same filter again
	// leaves it where it was put since.
	h.onLoop(func() { l.SetFilter("x"); l.SetCurrent("x.txt"); l.SetFilter("x") })
	var p string
	h.onLoop(func() { p, _, _ = l.Current() })
	if p != "a/x.txt" {
		t.Fatalf("after the filter: Current %q, want a/x.txt", p)
	}
}

// TestASaveViewAsksAVFSSourceWhetherANameIsAFolder: Enter on a typed name
// that is a folder of a VFS source goes into it, as on the local disk.
func TestASaveViewAsksAVFSSourceWhetherANameIsAFolder(t *testing.T) {
	g := newGated(t, vfsFiles)
	g.release("dir:.")
	g.release("dir:a")
	v := widget.NewFileSaveView(widget.WithFileViewSource(widget.FileSource{VFS: g, Root: "vfs://"}),
		widget.WithFileViewDir("vfs://"))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("top.txt")
	var confirmed bool
	h.onLoop(func() { v.SetName("a"); confirmed = v.Confirm() })
	if confirmed {
		t.Fatal("a folder's name was confirmed as a file to save")
	}
	h.shows("readme.md")
}
