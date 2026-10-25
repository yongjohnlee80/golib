package decl_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/decl"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// hotreload_test.go holds HotReload to its acceptance rows on real files: a
// temporary directory the test edits while the program runs, and what reaches
// the screen.

const tick = 20 * time.Millisecond

// hotDir is a program's QML on disk: a layout, a theme and a component.
type hotDir struct {
	t    *testing.T
	root string

	mu       sync.Mutex
	reloads  []decl.Result
	refusals []error
	polls    atomic.Int64 // the polls looked at, when followPolls counts them
	// hot are more HotReload options, before run: a test's own polls (ReloadTicks)
	hot []tuidecl.HotReloadOption
}

func newHotDir(t *testing.T, layout string) *hotDir {
	t.Helper()
	d := &hotDir{t: t, root: t.TempDir()}
	for _, sub := range []string{"themes", "ui"} {
		if err := os.MkdirAll(filepath.Join(d.root, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	d.write("main.qml", layout)
	d.write("themes/dark.qml", `Theme { bg: "blue" }`)
	d.write("ui/Card.qml", `Text { text: "card v1" }`)
	return d
}

// write replaces file whole, by a rename, as an editor saves. os.WriteFile
// truncates first, and under load the poller can see the empty file for two
// polls running, reload it, and record a refusal no test edit made. The
// poller reads only .qml and .xml, so the .tmp beside it is never seen.
func (d *hotDir) write(file, src string) {
	d.t.Helper()
	path := filepath.Join(d.root, file)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(src), 0o644); err != nil {
		d.t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		d.t.Fatal(err)
	}
}

func (d *hotDir) run(extra ...tuidecl.ProgramOption) *decltest.Screen {
	d.t.Helper()
	return d.runWith(nil, extra...)
}

// runWith is run with setup called between building the Program and running it.
func (d *hotDir) runWith(setup func(*tuidecl.Program) error, extra ...tuidecl.ProgramOption) *decltest.Screen {
	d.t.Helper()
	fsys := os.DirFS(d.root)
	opts := append([]tuidecl.ProgramOption{
		tuidecl.Layout(fsys, "main.qml"),
		tuidecl.Themes(fsys, "themes", "demo.theme", "1.0"),
		tuidecl.Components(fsys, "ui", "demo.ui", "1.0"),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.status": "initial"}),
		tuidecl.HotReload(append([]tuidecl.HotReloadOption{
			tuidecl.ReloadInterval(tick),
			tuidecl.OnReload(func(r decl.Result) { d.mu.Lock(); d.reloads = append(d.reloads, r); d.mu.Unlock() }),
			tuidecl.OnReloadError(func(err error) { d.mu.Lock(); d.refusals = append(d.refusals, err); d.mu.Unlock() }),
		}, d.hot...)...),
	}, extra...)
	return decltest.RunWith(d.t, 40, 6, setup, opts...)
}

// followPolls feeds the poller a real ticker at tick through ReloadTicks, as its own ticker would,
// and counts each poll it has looked at, so a reload waited for in vain can say how many polls
// passed meanwhile. Call it before run.
func (d *hotDir) followPolls() {
	ticks, polled, stop := make(chan time.Time), make(chan struct{}), make(chan struct{})
	d.t.Cleanup(func() { close(stop) }) // registered before run's: it runs once the program has stopped
	go func() {
		tk := time.NewTicker(tick)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case now := <-tk.C:
				select {
				case ticks <- now:
				case <-stop:
					return
				}
			}
		}
	}()
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-polled:
				d.polls.Add(1)
			}
		}
	}()
	d.hot = append(d.hot, tuidecl.ReloadTicks(ticks, polled))
}

// waitCounted waits until cond holds for the reloads and refusals counted, and when it never does,
// fails naming what happened instead: the polls looked at since, the reloads, every refusal, and the
// screen. So a reload that never came says whether it was refused or never polled.
func (d *hotDir) waitCounted(s *decltest.Screen, what string, cond func(reloads, refusals int) bool) {
	d.t.Helper()
	from := d.polls.Load()
	deadline := time.Now().Add(decltest.WaitTimeout)
	for !cond(d.counts()) {
		if time.Now().After(deadline) {
			d.mu.Lock()
			reloads, refusals := len(d.reloads), slices.Clone(d.refusals)
			d.mu.Unlock()
			d.t.Fatalf("%s never came: %d polls looked at since, %d reloads in all, refusals %v; the screen:\n%s",
				what, d.polls.Load()-from, reloads, refusals, s.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (d *hotDir) counts() (int, int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.reloads), len(d.refusals)
}

// settle waits long enough for the poller to have acted on anything written. It proves an absence
// (nothing reloaded, nothing refused); a reload that must have happened is waited for (waitRefused).
func settle() { time.Sleep(6 * tick) }

// waitRefused waits until the poller has refused n saves, or a deadline passes: a refused reload
// runs on the poller's clock, which a fixed settle can outrun on a slow run (coverage
// instrumentation, a loaded machine). The caller asserts the count.
func (d *hotDir) waitRefused(n int) {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(tick) {
		if _, refused := d.counts(); refused >= n {
			return
		}
	}
}

const hotHead = "import tui 1.0\nimport demo 1.0\nimport demo.theme.dark 1.0\nimport demo.ui 1.0\n"

func hotLayout(body string) string {
	return hotHead + "Window { Flex { direction: Tui.Vertical\n" + body + "\n} }"
}

// R1 — an edited layout reaches the screen; a save of the same bytes is no
// reload at all.
func TestR1AnEditedLayoutReachesTheScreen(t *testing.T) {
	d := newHotDir(t, hotLayout(`Text { text: "before" }`))
	s := d.run()
	s.WaitForText(t, "before")
	d.write("main.qml", hotLayout(`Text { text: "after" }`))
	s.WaitForText(t, "after")

	settle()
	n, _ := d.counts()
	d.write("main.qml", hotLayout(`Text { text: "after" }`))
	settle()
	if again, _ := d.counts(); again != n {
		t.Errorf("an unchanged save reloaded: %d -> %d", n, again)
	}
}

// R2 — an edited theme re-dresses the screen; an edited component re-dresses
// its use.
func TestR2AnEditedThemeOrComponentReachesTheScreen(t *testing.T) {
	d := newHotDir(t, hotLayout("Card { }\nText { text: \"themed\"; palette.window: Theme.bg }"))
	s := d.run()
	waitBG(t, s, "themed", ansi(blue))
	d.write("themes/dark.qml", `Theme { bg: "green" }`)
	waitBG(t, s, "themed", ansi(green))

	s.WaitForText(t, "card v1")
	d.write("ui/Card.qml", `Text { text: "card v2" }`)
	s.WaitForText(t, "card v2")
}

// An in-memory document follows its modules, keeping the last successful Reload's source.
func TestHotReloadLayoutSourceKeepsTheSwitchedLayout(t *testing.T) {
	d := newHotDir(t, hotLayout(`Text { text: "disk layout" }`))
	s := d.runWith(func(p *tuidecl.Program) error {
		p.Post(func() {
			if _, err := p.Reload([]byte(hotLayout(`Text { text: "selected layout"; palette.window: Theme.bg }`))); err != nil {
				t.Error(err)
			}
		})
		return nil
	}, tuidecl.LayoutSource("main.qml", []byte(hotLayout(`Text { text: "initial layout" }`))))
	waitBG(t, s, "selected layout", ansi(blue))
	d.write("themes/dark.qml", `Theme { bg: "green" }`)
	waitBG(t, s, "selected layout", ansi(green))
	if strings.Contains(s.String(), "disk layout") || strings.Contains(s.String(), "initial layout") {
		t.Errorf("layout reverted: %s", s)
	}
}

// R3 — two writes inside one interval are one reload, of the second. The polls are the test's
// own (ReloadTicks), so the writes are quick by construction: both land before any poll, or one
// poll sees the first and the next sees it changed again; either way the files hold still only
// at the second, and it is the one reload.
func TestR3TwoQuickWritesAreOneReload(t *testing.T) {
	for _, between := range []int{0, 1} { // polls between the two writes
		ticks, done := make(chan time.Time), make(chan struct{})
		d := newHotDir(t, hotLayout(`Text { text: "zero" }`))
		d.hot = []tuidecl.HotReloadOption{tuidecl.ReloadTicks(ticks, done)}
		s := d.run()
		s.WaitForText(t, "zero")
		poll := func() { ticks <- time.Time{}; <-done } // returns once the poll has seen the files and acted
		d.write("main.qml", hotLayout(`Text { text: "one" }`))
		for range between {
			poll()
		}
		d.write("main.qml", hotLayout(`Text { text: "two" }`))
		poll() // sees two, not yet held still
		poll() // two again: held still, one reload
		s.WaitForText(t, "two")
		deadline := time.Now().Add(3 * time.Second)
		for n, _ := d.counts(); n == 0 && time.Now().Before(deadline); n, _ = d.counts() {
			time.Sleep(5 * time.Millisecond)
		}
		if n, _ := d.counts(); n != 1 {
			t.Errorf("%d polls between the writes: two quick writes made %d reloads, want 1", between, n)
		}
	}
}

// R4 — a half-written save is waited out in silence; a broken one is
// reported once and the screen kept; the next good save applies.
func TestR4ABrokenSaveKeepsTheScreen(t *testing.T) {
	d := newHotDir(t, hotLayout(`Text { text: "good" }`))
	s := d.run()
	s.WaitForText(t, "good")

	d.write("main.qml", hotLayout(`Text { text: "half`)[:len(hotHead)+30])
	settle()
	if _, refused := d.counts(); refused != 0 {
		t.Errorf("a half-written save was reported: %v", d.refusals)
	}
	d.write("main.qml", hotLayout(`Text { txt: "typo" }`))
	d.waitRefused(1)
	if _, refused := d.counts(); refused != 1 || !strings.Contains(d.refusals[0].Error(), "txt") {
		t.Errorf("a broken save: %d refusals %v, want 1 naming txt", refused, d.refusals)
	}
	if !strings.Contains(s.String(), "good") {
		t.Errorf("a refused save lost the screen:\n%s", s.String())
	}
	d.write("main.qml", hotLayout(`Text { text: "fixed" }`))
	s.WaitForText(t, "fixed")
}

// R5 — a reload that rebuilds the ROOT reaches the screen. The shipped
// Program.Reload left the App painting the old one.
func TestR5AReloadThatReplacesTheRootReachesTheScreen(t *testing.T) {
	src := func(dir, text string) string {
		return hotHead + "Flex { direction: Tui." + dir + "\n Text { text: \"" + text + "\" } }"
	}
	d := newHotDir(t, src("Vertical", "old root"))
	s := d.run()
	s.WaitForText(t, "old root")
	d.write("main.qml", src("Horizontal", "new root")) // direction is taken at construction
	s.WaitForText(t, "new root")
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.reloads) == 0 || !d.reloads[len(d.reloads)-1].RootReplaced {
		t.Errorf("the root was not replaced: %+v", d.reloads)
	}
}

// A save made after the files were read for the mount but before polling
// starts — while the screen is still coming up — is a change like any later
// one: the watcher starts from the files as mounted, not as it first finds them.
func TestASaveBeforeThePollingStartsIsReloaded(t *testing.T) {
	d := newHotDir(t, hotLayout(`Text { text: "mounted" }`))
	s := d.runWith(func(*tuidecl.Program) error {
		d.write("main.qml", hotLayout(`Text { text: "saved early" }`))
		return nil
	})
	s.WaitForText(t, "saved early")
}

// R6 — a patched editor keeps what was typed, and the keyboard.
func TestR6APatchedEditorKeepsItsTextAndFocus(t *testing.T) {
	d := newHotDir(t, hotLayout("Text { text: \"v1\" }\nEditor { id: ed; focus: true }"))
	s := d.run()
	s.WaitForText(t, "v1")
	s.Keys(t, decltest.Type("ikept")...)
	s.WaitForText(t, "kept")
	var before *widget.Editor
	onScreenLoop(t, s, func() { before, _ = tuidecl.FindAs[*widget.Editor](s.Program, "ed") })

	d.write("main.qml", hotLayout("Text { text: \"v2\" }\nEditor { id: ed; focus: true }"))
	s.WaitForText(t, "v2")
	onScreenLoop(t, s, func() {
		after, _ := tuidecl.FindAs[*widget.Editor](s.Program, "ed")
		if after != before || after.Core().Value() != "kept" {
			t.Errorf("the editor was replaced or lost its text: same=%v value=%q", after == before, after.Core().Value())
		}
	})
	s.Keys(t, decltest.Type("!")...)
	s.WaitForText(t, "kept!")
}

// R7 — a reload that fails PART-WAY (a colour refused while applying) leaves
// a tree only Destroy may touch; the next good save remounts, and the host's
// source keeps its current value.
func TestR7APartWayFailureIsRecoveredByARemount(t *testing.T) {
	d := newHotDir(t, hotLayout("Text { text: App.status }\nText { text: \"x\"; palette.window: \"blue\" }"))
	s := d.run()
	s.WaitForText(t, "initial")
	onScreenLoop(t, s, func() {
		if err := s.Program.Set("App.status", "moved"); err != nil {
			t.Error(err)
		}
	})
	s.WaitForText(t, "moved")

	d.write("main.qml", hotLayout("Text { text: App.status }\nText { text: \"x\"; palette.window: \"bleu\" }"))
	d.waitRefused(1)
	if _, refused := d.counts(); refused != 1 {
		t.Fatalf("the bad colour: %d refusals, want 1", refused)
	}
	onScreenLoop(t, s, func() {
		if !s.Program.Tree().Failed() {
			t.Error("the colour was refused before the tree was touched; this row needs a part-way failure")
		}
	})

	d.write("main.qml", hotLayout("Text { text: App.status }\nText { text: \"recovered\" }"))
	s.WaitForText(t, "recovered")
	if !strings.Contains(s.String(), "moved") {
		t.Errorf("the remount lost the host's source value:\n%s", s.String())
	}
}

// R8 — without HotReload nothing follows the files.
func TestR8WithoutHotReloadNothingPolls(t *testing.T) {
	d := newHotDir(t, hotLayout(`Text { text: "embedded" }`))
	fsys := os.DirFS(d.root)
	s := decltest.Run(t, 40, 6, tuidecl.Layout(fsys, "main.qml"),
		tuidecl.Themes(fsys, "themes", "demo.theme", "1.0"),
		tuidecl.Components(fsys, "ui", "demo.ui", "1.0"),
		tuidecl.Singleton("demo", "1.0", "App"))
	s.WaitForText(t, "embedded")
	d.write("main.qml", hotLayout(`Text { text: "edited" }`))
	settle()
	if strings.Contains(s.String(), "edited") {
		t.Error("a program without HotReload followed its file")
	}
}

// TestHotReloadNeedsAFileToFollow: an in-memory layout has none.
func TestHotReloadNeedsAFileToFollow(t *testing.T) {
	_, err := tuidecl.NewProgram(tuidecl.LayoutSource("m.qml", []byte("import tui 1.0\nText { }")), tuidecl.HotReload())
	if err == nil || !strings.Contains(err.Error(), "Layout(fs, file)") {
		t.Fatalf("err = %v", err)
	}
}
