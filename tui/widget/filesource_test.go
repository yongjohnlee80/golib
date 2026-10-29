package widget_test

import (
	"context"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
	"github.com/yongjohnlee80/golib/vfs"
	"github.com/yongjohnlee80/golib/vfs/memfs"
)

// filesource_test.go covers a VFS FileSource: listed and previewed off the
// loop, a superseded read cancelled through its context, and the Open view's
// `/` filter; and an Async fs.FS one, read off the loop with its links followed.

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
	before := g.started.Load()
	h.inject(key(tui.KeyDown)) // .., readme.md: held
	h.shows("(loading")
	// the preview says loading as its task is made, before the task reads: wait for the read
	// itself, or x.txt can supersede a task that never started, and there is nothing to cancel
	h.waitFor("the readme's read held", func() bool { return g.started.Load() > before })
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

// TestAVFSPreviewShownBeforeItsMountIsReadAtTheMount: Show on a preview not yet mounted has no
// loop to read a VFS file off, so it reads nothing and says loading; once mounted, the preview
// reads the file off the loop, and shows it when the read is let through.
func TestAVFSPreviewShownBeforeItsMountIsReadAtTheMount(t *testing.T) {
	g := newGated(t, vfsFiles)
	p := widget.NewFilePreview(widget.FilePaneStyles{}, false)
	shown := make(chan struct{})
	go func() {
		p.Show(widget.FileSource{VFS: g, Root: "vfs://"}, "a/x.txt", false)
		close(shown)
	}()
	select {
	case <-shown:
	case <-time.After(5 * time.Second):
		t.Fatal("Show on an unmounted preview waited on the source's read")
	}
	if n := g.started.Load(); n != 0 {
		t.Fatalf("Show read the source %d times before the mount", n)
	}
	h := startApp(t, p, 60, 10)
	defer h.stop()
	h.shows("(loading")
	h.waitFor("the read at the mount", func() bool { return g.started.Load() == 1 })
	g.release("file:a/x.txt")
	h.shows("x body")
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

// TestAFilterTypedRightAfterSlashReachesTheField: the keys typed in one burst after `/` are the
// filter field's, though the frame that shows the field is held back: a frame cap here, a loaded
// machine on CI. A frame has just run, so the cap holds the next one while the burst is routed.
func TestAFilterTypedRightAfterSlashReachesTheField(t *testing.T) {
	root := tree(t, map[string]string{"alpha.md": "a", "beta.md": "b", "gamma.txt": "g", "sub/c.md": "c"})
	v := widget.NewFileOpenView(widget.WithFileViewDir(root), widget.WithFileViewPreview(false))
	h := startAppOpts(t, v, 80, 20, tui.WithMinFrameInterval(300*time.Millisecond))
	defer h.stop()
	h.onLoop(v.FocusInitial)
	h.shows("gamma.txt")
	f := h.tb.Flushes()
	h.inject(key(tui.KeyDown))
	h.waitFor("a frame", func() bool { return h.tb.Flushes() > f })
	h.inject(key('/'), key('s'), key('u'), key('b'))
	h.waitFor("only sub left", func() bool { g := h.grid(); return strings.Contains(g, "sub/") && !strings.Contains(g, "alpha.md") })
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

// gatedDirFS is an fs.FS whose ReadDir and Stat hold on a name until the test
// releases it, as a slow mount does; an fs.FS read takes no context, so a
// superseded one is never stopped. A read held past a few seconds goes on, so
// one made on the loop fails the test rather than hanging it.
type gatedDirFS struct {
	fstest.MapFS
	g     *gatedFS
	stats *atomic.Int32
}

func newGatedDir(files map[string]string) gatedDirFS {
	m := fstest.MapFS{}
	for p, body := range files {
		m[p] = &fstest.MapFile{Data: []byte(body)}
	}
	return gatedDirFS{MapFS: m, g: &gatedFS{gates: map[string]chan struct{}{}}, stats: &atomic.Int32{}}
}

func (d gatedDirFS) hold(name string) {
	select {
	case <-d.g.gate(name):
	case <-time.After(5 * time.Second):
	}
}

func (d gatedDirFS) ReadDir(name string) ([]fs.DirEntry, error) {
	d.g.started.Add(1)
	d.hold("dir:" + name)
	return d.MapFS.ReadDir(name)
}

func (d gatedDirFS) Stat(name string) (fs.FileInfo, error) {
	d.stats.Add(1)
	d.hold("stat:" + name)
	return d.MapFS.Stat(name)
}

// TestAnAsyncFSIsListedOffTheLoop: an Async fs.FS is listed as a VFS source
// is, the list loading while the loop runs; a listing that a newer one
// superseded cannot be stopped, and when it answers late it is dropped.
func TestAnAsyncFSIsListedOffTheLoop(t *testing.T) {
	d := newGatedDir(vfsFiles)
	v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{FS: d, Root: "mem://", Async: true}),
		widget.WithFileViewDir("mem://"), widget.WithFileViewPreview(false))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("loading")
	ran := false
	h.onLoop(func() { ran = true })
	if !ran || strings.Contains(h.grid(), "top.txt") {
		t.Fatalf("the loop ran %v; rows shown before the read was released:\n%s", ran, h.grid())
	}
	d.g.release("dir:.")
	h.shows("top.txt")
	before := d.g.started.Load()
	h.onLoop(func() { v.SetDir("mem://a") }) // held
	h.waitFor("a's read held", func() bool { return d.g.started.Load() > before })
	h.onLoop(func() { v.SetDir("mem://b") })
	d.g.release("dir:b")
	h.shows("notes.md")
	d.g.release("dir:a") // answers late
	h.settle()
	var dir string
	h.onLoop(func() { dir = v.Dir() })
	if dir != "mem://b" || strings.Contains(h.grid(), "readme.md") {
		t.Fatalf("Dir %q: the late listing of a replaced b's:\n%s", dir, h.grid())
	}
}

// TestAnAsyncLocalSourceFollowsLinksToFolders: through an Async os.DirFS, a
// link to a folder is a folder, whether the link is absolute or relative;
// Enter goes into it, and a file there previews. vfs/local cannot stand in:
// its root refuses an absolute link, and it lists every link as a file.
func TestAnAsyncLocalSourceFollowsLinksToFolders(t *testing.T) {
	root := tree(t, map[string]string{"real/n.md": "note body"})
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "abs")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(root, "rel")); err != nil {
		t.Fatal(err)
	}
	v := widget.NewFileOpenView(widget.WithFileViewSource(widget.FileSource{FS: os.DirFS("/"), Root: "/", Async: true}),
		widget.WithFileViewDir(root))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("rel/")
	h.shows("abs/")
	// .., abs/, real/, rel/: the cursor starts on ..
	for link, downs := range map[string]int{"abs": 1, "rel": 3} {
		h.onLoop(func() { v.SetDir(root) })
		// real/ is a row of the root's alone: its rows are in, not the last folder's. Dir is the
		// folder asked for while its listing is still on its way, and the first time round the
		// rows on screen are already the root's: wait for the listing itself, or the keys below
		// land on the old rows and the new ones put the cursor back on .. under them.
		h.waitFor("the root listed", func() bool {
			var dir string
			h.onLoop(func() { dir = v.Dir() })
			g := h.grid()
			return dir == root && strings.Contains(g, "real/") && !strings.Contains(g, "loading…")
		})
		for range downs {
			h.inject(key(tui.KeyDown))
		}
		h.inject(key(tui.KeyEnter))
		h.waitFor("into "+link, func() bool {
			var dir string
			h.onLoop(func() { dir = v.Dir() })
			return dir == filepath.Join(root, link)
		})
		h.shows("n.md")
		h.inject(key(tui.KeyDown)) // .., n.md
		h.shows("note body")
	}
}

// TestASaveViewChecksATypedNameOffTheLoop: of an Async fs.FS, Enter on a typed
// name asks off the loop whether it is a folder. The loop answers while the
// check is held; the answer then chooses the file, or goes into the folder; an
// edit to the name before the answer drops it, even one back to the same name.
func TestASaveViewChecksATypedNameOffTheLoop(t *testing.T) {
	d := newGatedDir(vfsFiles)
	d.g.release("dir:.")
	d.g.release("dir:a")
	var chosen atomic.Int32
	v := widget.NewFileSaveView(widget.WithFileViewSource(widget.FileSource{FS: d, Root: "mem://", Async: true}),
		widget.WithFileViewDir("mem://"), widget.WithOnChoose(func() { chosen.Add(1) }))
	h := startBrowser(t, v) // the name field has the keyboard
	defer h.stop()
	h.shows("top.txt")

	h.onLoop(func() { v.SetName("b") })
	h.inject(key(tui.KeyEnter))
	h.waitFor("b's check held", func() bool { return d.stats.Load() >= 1 })
	// edits, back to the name asked about; each seen on the loop before the
	// answer is let through, since injected keys and a task's answer are not
	// ordered
	nameIs := func(want string) func() bool {
		return func() bool {
			var n string
			h.onLoop(func() { n = v.Name() })
			return n == want
		}
	}
	h.inject(key('x'))
	h.waitFor("the x typed", nameIs("bx"))
	h.inject(key(tui.KeyBackspace))
	h.waitFor("the x deleted", nameIs("b"))
	d.g.release("stat:b")
	h.settle()
	var dir, name string
	h.onLoop(func() { dir, name = v.Dir(), v.Name() })
	if dir != "mem://" || name != "b" || chosen.Load() != 0 {
		t.Fatalf("after an edit, the dropped check applied: Dir %q, name %q, chosen %d", dir, name, chosen.Load())
	}

	h.onLoop(func() { v.SetName("top.txt") })
	h.inject(key(tui.KeyEnter))
	h.waitFor("top.txt's check held", func() bool { return d.stats.Load() >= 2 })
	ran := false
	h.onLoop(func() { ran = true })
	if !ran || chosen.Load() != 0 {
		t.Fatalf("while the check is held: the loop ran %v, chosen %d", ran, chosen.Load())
	}
	d.g.release("stat:top.txt")
	h.waitFor("the file chosen", func() bool { return chosen.Load() == 1 })
	var confirmed bool
	h.onLoop(func() { confirmed = v.Confirm() })
	if !confirmed {
		t.Fatal("Confirm after the answer: false, want the file")
	}

	h.onLoop(func() { v.SetName("a") })
	h.inject(key(tui.KeyEnter))
	d.g.release("stat:a")
	h.shows("readme.md")
	h.onLoop(func() { dir, name = v.Dir(), v.Name() })
	if dir != "mem://a" || name != "" || chosen.Load() != 1 {
		t.Fatalf("a folder's name: Dir %q, name %q, chosen %d; want into a, the name cleared, nothing chosen", dir, name, chosen.Load())
	}
}

// TestASourceSwapDropsATypedNamesCheck: two sources under one root, x a file in
// one and a folder in the other. A check on its way from the first, answering
// after the swap, is dropped; and the first's answer, kept for the Confirm
// that follows it, is not taken for the second's.
func TestASourceSwapDropsATypedNamesCheck(t *testing.T) {
	file := newGatedDir(map[string]string{"x": "a file"})
	folder := newGatedDir(map[string]string{"x/inner.md": "inside"})
	file.g.release("dir:.")
	folder.g.release("dir:.")
	folder.g.release("dir:x")
	srcOf := func(d gatedDirFS) widget.FileSource { return widget.FileSource{FS: d, Root: "mem://", Async: true} }
	var chosen atomic.Int32
	v := widget.NewFileSaveView(widget.WithFileViewSource(srcOf(file)), widget.WithFileViewDir("mem://"),
		widget.WithOnChoose(func() { chosen.Add(1) }))
	h := startBrowser(t, v)
	defer h.stop()
	// the cursor lands on x once the file source's rows are in, and names it
	named := func() bool {
		var n string
		h.onLoop(func() { n = v.Name() })
		return n == "x"
	}
	h.waitFor("the file source listed", named)

	h.onLoop(func() { v.SetName("x") })
	h.inject(key(tui.KeyEnter))
	h.waitFor("x's check held", func() bool { return file.stats.Load() >= 1 })
	h.onLoop(func() { v.SetSource(srcOf(folder)) })
	h.shows("x/")
	file.g.release("stat:x") // the first source answers: a file
	h.settle()
	if chosen.Load() != 0 {
		t.Fatal("the old source's answer chose x from the new source")
	}

	h.onLoop(func() { v.SetSource(srcOf(file)); v.SetName("") })
	h.waitFor("the file source listed again", named)
	h.inject(key(tui.KeyEnter))
	h.waitFor("x chosen, a file here", func() bool { return chosen.Load() == 1 })
	h.onLoop(func() { v.SetSource(srcOf(folder)) })
	h.shows("x/")
	var confirmed bool
	h.onLoop(func() { confirmed = v.Confirm() })
	if confirmed {
		t.Fatal("Confirm took the old source's answer: x confirmed as a file of the new source, where it is a folder")
	}
	folder.g.release("stat:x")
	h.shows("inner.md")
}

// TestAFileListReportsACursorMoveOnce: SetCurrent onto another entry, and a key moving the cursor,
// each call WithOnCursor once, at once: the List tells its FileList directly, not through the bus a
// tick later on top of FileList's own call (tui-widget-data-I4).
func TestAFileListReportsACursorMoveOnce(t *testing.T) {
	var calls []string
	l := widget.NewFileList(
		widget.WithFileListSource(widget.FileSource{FS: fstest.MapFS{
			"a.txt": {Data: []byte("a")}, "b.txt": {Data: []byte("b")}, "c.txt": {Data: []byte("c")},
		}, Root: "/"}),
		widget.WithFileListDir("/"),
		widget.WithOnCursor(func(p string, _ bool) { calls = append(calls, path.Base(p)) }))
	h := startApp(t, l, 60, 12)
	defer h.stop()
	h.waitFor("listed", func() bool { return strings.Contains(h.grid(), "c.txt") })

	var now []string
	h.onLoop(func() { calls = nil; l.SetCurrent("c.txt"); now = append([]string(nil), calls...) })
	h.settle()
	var later []string
	h.onLoop(func() { later = append([]string(nil), calls...) })
	if len(now) != 1 || now[0] != "c.txt" || len(later) != 1 {
		t.Errorf("SetCurrent(c.txt): %v at once, %v after the loop drained; want [c.txt] both", now, later)
	}

	h.inject(tab()) // the list takes the keys
	h.settle()
	h.onLoop(func() { calls = nil })
	h.inject(key(tui.KeyUp))
	h.settle()
	h.onLoop(func() { later = append([]string(nil), calls...) })
	if len(later) != 1 || later[0] != "b.txt" {
		t.Errorf("Up from c.txt: %v, want [b.txt]", later)
	}
}

// refusingFS is a VFS store that refuses to say what one name is, as a store does a path its user
// may not see.
type refusingFS struct {
	vfs.FS
	refused string
}

func (r refusingFS) Stat(ctx context.Context, name string) (vfs.FileInfo, error) {
	if name == r.refused {
		return vfs.FileInfo{}, fs.ErrPermission
	}
	return r.FS.Stat(ctx, name)
}

// A VFS source answers the path field through its backend's stat: a name it does not have is
// not found, one it refuses to describe cannot be read, and a rooted path outside its root is not
// found, even where the store has that name inside its root. The folder listed stays as it was.
func TestAVFSSourceTellsThePathFieldWhatAPathIs(t *testing.T) {
	g := newGated(t, map[string]string{"in/f.md": "f", "secret/x": "x"})
	src := widget.FileSource{VFS: refusingFS{FS: g.FS, refused: "secret"}, Root: "vfs://"}
	v := widget.NewFileFolderView(widget.WithFileViewSource(src), widget.WithFileViewDir("vfs://"))
	h := startBrowser(t, v)
	defer h.stop()
	h.shows("in/")
	h.inject(keyMod('k', tui.ModCtrl)) // to the path field
	typePath(h, "vfs://nothere")
	h.shows("Folder — not found")
	typePath(h, "vfs://secret")
	h.shows("Folder — cannot read")
	typePath(h, "/in") // "in" is the store's, under vfs://, not under /
	h.shows("Folder — not found")
	if !dirIs(h, v, "vfs://")() {
		t.Fatal("a path outside the source's root moved the listing")
	}
	typePath(h, "vfs://in")
	h.waitFor("in listed", dirIs(h, v, "vfs://in"))
	h.shows("f.md")
}
