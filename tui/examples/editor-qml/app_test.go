package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// app_test.go runs editor.qml on a real backend. Every assertion is about what
// reaches the SCREEN, because "the tree was constructed" has never been the
// claim — a builder that wired a widget to nothing passes every structural test
// and fails here.

var fixedNow = func() time.Time { return time.Date(2026, 9, 25, 8, 3, 0, 0, time.UTC) }

type running struct {
	host *Host
	be   *tui.TestBackend
	// quit is closed when Run returns.
	quit chan struct{}
}

func start(t *testing.T, path string) *running {
	t.Helper()
	return startLayout(t, path, nil)
}

// startLayout runs a replacement for editor.qml; nil runs the real one.
func startLayout(t *testing.T, path string, src []byte) *running {
	t.Helper()
	return startWith(t, path, src, 80, 14)
}

// startSized runs editor.qml on a screen of the given size — a file dialog
// wants the rows an ordinary terminal has.
func startSized(t *testing.T, path string, w, h int) *running {
	t.Helper()
	return startWith(t, path, nil, w, h)
}

func startWith(t *testing.T, path string, src []byte, w, h int) *running {
	t.Helper()
	return startOpts(t, Options{Path: path, Layout: src, Now: fixedNow, Tick: time.Hour}, w, h)
}

// startOpts runs the editor as main does — one Program, Run until it quits —
// on a test backend of the given size. The clock does not tick during a test
// unless its Options ask it to.
func startOpts(t *testing.T, opt Options, w, h int) *running {
	t.Helper()
	r := &running{quit: make(chan struct{}), be: tui.NewTestBackend(w, h)}
	if opt.Sink == nil {
		opt.Sink = func(err error) { t.Errorf("handler error: %v", err) }
	}
	opt.App = []tui.AppOption{tui.WithBackend(r.be), tui.WithMinFrameInterval(0)}
	host, err := New(opt)
	if err != nil {
		t.Fatalf("editor.qml did not mount: %v", err)
	}
	r.host = host
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		// QUITTING IS RUN RETURNING: App.quit ends the Program, and nothing
		// else in the test ends it before the cleanup does.
		err := host.Run(ctx)
		close(r.quit)
		done <- err
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Errorf("Run: %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("the app did not stop")
		}
	})
	r.waitFor(t, "the first frame", func(s string) bool { return strings.Contains(s, "NORMAL") })
	return r
}

func (r *running) screen() string { return r.be.String() }

func (r *running) rows() []string { return strings.Split(r.screen(), "\n") }

func (r *running) waitFor(t *testing.T, what string, cond func(string) bool) {
	t.Helper()
	for range 300 {
		if cond(r.screen()) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s never appeared:\n%s", what, r.screen())
}

func (r *running) key(t *testing.T, evs ...tui.Event) {
	t.Helper()
	if err := r.be.Inject(evs...); err != nil {
		t.Fatalf("inject: %v", err)
	}
}

func runeKey(ch rune) tui.KeyEvent {
	return tui.KeyEvent{Kind: tui.KeyPress, Code: ch, Base: ch, Text: string(ch)}
}

// TestTheLayoutMatchesTheEditor is the screenshot, asserted: the menu bar on
// the top row with Help at the far end, the frame in the middle, and the three
// status segments on the bottom row.
func TestTheLayoutMatchesTheEditor(t *testing.T) {
	r := start(t, "")
	rows := r.rows()

	top := rows[0]
	for _, want := range []string{"File", "Option", "Help"} {
		if !strings.Contains(top, want) {
			t.Errorf("the menu bar lacks %q: %q", want, top)
		}
	}
	// Help is pegged to the RIGHT: it sits after a run of blank bar, not
	// beside Option.
	if strings.Index(top, "Help") < len(top)/2 {
		t.Errorf("Help is not at the far end of the bar: %q", top)
	}

	// The frame is below the bar and above the status line.
	if !strings.ContainsAny(rows[1], "┌╭+") {
		t.Errorf("row 1 is not the top of the frame: %q", rows[1])
	}

	bottom := lastNonEmpty(rows)
	for _, want := range []string{"NORMAL", "[No Name]", "08:03:00"} {
		if !strings.Contains(bottom, want) {
			t.Errorf("the status line lacks %q: %q", want, bottom)
		}
	}
	// And in that order: mode on the left, file in the centre, clock on the right.
	if !(strings.Index(bottom, "NORMAL") < strings.Index(bottom, "[No Name]") &&
		strings.Index(bottom, "[No Name]") < strings.Index(bottom, "08:03:00")) {
		t.Errorf("the status segments are out of order: %q", bottom)
	}
}

// TestTheFileNameIsShownAndTheFileLoaded.
func TestTheFileNameIsShownAndTheFileLoaded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.md")
	if err := os.WriteFile(path, []byte("hello from sample.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := start(t, path)
	r.waitFor(t, "the file's contents", func(s string) bool {
		return strings.Contains(s, "hello from sample.md")
	})
	if !strings.Contains(lastNonEmpty(r.rows()), "sample.md") {
		t.Errorf("the status line does not name the file:\n%s", r.screen())
	}
}

// TestTheModeReachesTheStatusLine is the loop the document declares: the
// editor's modeChanged signal runs App.syncStatus, which moves App.mode, which
// the StatusBar's `left` is bound to.
func TestTheModeReachesTheStatusLine(t *testing.T) {
	r := start(t, "")
	r.key(t, runeKey('i'))
	r.waitFor(t, "INSERT on the status line", func(s string) bool {
		return strings.Contains(lastNonEmpty(strings.Split(s, "\n")), "INSERT")
	})
	r.key(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	r.waitFor(t, "NORMAL again", func(s string) bool {
		return strings.Contains(lastNonEmpty(strings.Split(s, "\n")), "NORMAL")
	})
}

// TestTheClockTicksThroughTheProvider: the clock's ticker goroutine delivers,
// the scheduler moves the delivery onto the loop, and the status line
// repaints.
//
// The assertion is on a time that is NOT the one the first frame showed, so a
// clock that never ticked cannot pass it.
func TestTheClockTicksThroughTheProvider(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	now := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return fixedNow().Add(time.Duration(calls-1) * time.Minute)
	}
	r := startOpts(t, Options{Now: now, Tick: 10 * time.Millisecond}, 80, 14)
	r.waitFor(t, "a tick past the first frame", func(s string) bool {
		return strings.Contains(s, "08:04:00") || strings.Contains(s, "08:05:00")
	})
}

func lastNonEmpty(rows []string) string {
	for i := len(rows) - 1; i >= 0; i-- {
		if strings.TrimSpace(rows[i]) != "" {
			return rows[i]
		}
	}
	return ""
}

func click(x, y int) []tui.Event {
	return []tui.Event{
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: x, Y: y},
	}
}

// clickLabel clicks the first cell of a label on the given row.
func (r *running) clickLabel(t *testing.T, row int, label string) {
	t.Helper()
	rows := r.rows()
	x := strings.Index(rows[row], label)
	if x < 0 {
		t.Fatalf("%q is not on row %d:\n%s", label, row, r.screen())
	}
	// Columns are RUNES, and the frame's border runes are multi-byte.
	x = len([]rune(rows[row][:x]))
	r.key(t, click(x, row)...)
}

// TestTheFileMenuDropsDownWithItsRows is the dropdown in the screenshot.
func TestTheFileMenuDropsDownWithItsRows(t *testing.T) {
	r := start(t, "")
	r.clickLabel(t, 0, "File")
	r.waitFor(t, "the File dropdown", func(s string) bool {
		return strings.Contains(s, "New") && strings.Contains(s, "Open") &&
			strings.Contains(s, "Save") && strings.Contains(s, "Exit")
	})
}

// TestAMenuRowRunsItsHandler: a row declared in QML, triggered through the real
// widget, runs the host command its onTriggered names — and the result reaches
// the screen through a bound source.
func TestAMenuRowRunsItsHandler(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	r := start(t, path)

	r.key(t, runeKey('i'))
	r.key(t, runeKey('h'), runeKey('i'))
	r.key(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	r.waitFor(t, "the typed text", func(s string) bool { return strings.Contains(s, "hi") })

	r.clickLabel(t, 0, "File")
	r.waitFor(t, "the dropdown", func(s string) bool { return strings.Contains(s, "Save") })
	row := rowOf(r.rows(), "Save")
	r.clickLabel(t, row, "Save")

	r.waitFor(t, "the write confirmation", func(s string) bool {
		return strings.Contains(lastNonEmpty(strings.Split(s, "\n")), "written")
	})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Save wrote nothing: %v", err)
	}
	if !strings.Contains(string(b), "hi") {
		t.Errorf("the file holds %q, want the typed text", b)
	}
}

// TestTheKeymapRadioSwitchesTheEditorThroughABinding: Option → Keymaps → Nano
// runs App.useNano, which moves App.keyset, which the Editor's `keyset` is
// BOUND to. Nano is modeless, so the status line must stop saying NORMAL.
func TestTheKeymapRadioSwitchesTheEditorThroughABinding(t *testing.T) {
	r := start(t, "")
	r.clickLabel(t, 0, "Option")
	r.waitFor(t, "the Option dropdown", func(s string) bool { return strings.Contains(s, "Keymaps") })
	r.clickLabel(t, rowOf(r.rows(), "Keymaps"), "Keymaps")
	r.waitFor(t, "the Keymaps submenu", func(s string) bool { return strings.Contains(s, "Nano") })
	r.clickLabel(t, rowOf(r.rows(), "Nano"), "Nano")

	r.waitFor(t, "the switch reported", func(s string) bool {
		return strings.Contains(lastNonEmpty(strings.Split(s, "\n")), "Nano")
	})
	if strings.Contains(lastNonEmpty(r.rows()), "NORMAL") {
		t.Errorf("the status line still says NORMAL after switching to a modeless keymap:\n%s", r.screen())
	}
}

func rowOf(rows []string, label string) int {
	for i, row := range rows {
		if strings.Contains(row, label) {
			return i
		}
	}
	return -1
}

func ctrl(ch rune) tui.KeyEvent { return tui.KeyEvent{Kind: tui.KeyPress, Code: ch, Mods: tui.ModCtrl} }
func alt(ch rune) tui.KeyEvent  { return tui.KeyEvent{Kind: tui.KeyPress, Code: ch, Mods: tui.ModAlt} }

// TestCtrlQAsksBeforeQuitting — the Shortcut opens the quit dialog, and only
// its answer quits.
func TestCtrlQAsksBeforeQuitting(t *testing.T) {
	r := start(t, "")
	r.key(t, ctrl('q'))
	r.waitFor(t, "the quit dialog", func(s string) bool { return strings.Contains(s, "Are you sure to quit?") })
	r.notQuit(t)
	r.key(t, runeKey('y'))
	r.quits(t, "y in the quit dialog")
}

// TestAltFOpensTheFileMenu — the menu is reachable from the keyboard, by the
// mnemonic the document underlined.
func TestAltFOpensTheFileMenu(t *testing.T) {
	r := start(t, "")
	r.key(t, alt('f'))
	r.waitFor(t, "the File dropdown", func(s string) bool {
		return strings.Contains(s, "Open") && strings.Contains(s, "Exit")
	})
}

// TestTypingWorksAfterAMenuAction.
//
// A menu row that ran closes the menu; if focus stayed on the closed menu, the
// next keystroke would go nowhere the user can see. The Window hands it back
// to the document's `focus: true` target.
func TestTypingWorksAfterAMenuAction(t *testing.T) {
	r := start(t, "")
	r.key(t, alt('f'))
	r.waitFor(t, "the File dropdown", func(s string) bool { return strings.Contains(s, "New") })
	r.clickLabel(t, rowOf(r.rows(), "New"), "New")
	r.waitFor(t, "the New message", func(s string) bool {
		return strings.Contains(lastNonEmpty(strings.Split(s, "\n")), "new buffer")
	})

	r.key(t, runeKey('i'))
	r.key(t, runeKey('o'), runeKey('k'))
	r.waitFor(t, "typing after the menu", func(s string) bool { return strings.Contains(s, "ok") })
}

// TestEscapeLeavesTheMenuAndReturnsTheKeyboard.
func TestEscapeLeavesTheMenuAndReturnsTheKeyboard(t *testing.T) {
	r := start(t, "")
	r.key(t, alt('f'))
	r.waitFor(t, "the File dropdown", func(s string) bool { return strings.Contains(s, "Exit") })
	for range 3 {
		r.key(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEscape})
	}
	r.key(t, runeKey('i'))
	r.key(t, runeKey('z'))
	r.waitFor(t, "typing after Escape", func(s string) bool {
		return strings.Contains(s, "z") && !strings.Contains(s, "Exit")
	})
}

// TestClickingIntoTheEditorClosesAnOpenMenu.
//
// golib keeps a menu level open across a focus loss on purpose — an involuntary
// loss is not a decision the user made about the menu. This editor's policy is
// the other one: clicking into the buffer means "I am done with the menu", and
// a dropdown left hanging covers the very line the user just aimed at.
//
// Found by manual testing: the stray dropdown stayed open.
func TestClickingIntoTheEditorClosesAnOpenMenu(t *testing.T) {
	r := start(t, "")
	r.clickLabel(t, 0, "File")
	r.waitFor(t, "the File dropdown", func(s string) bool { return strings.Contains(s, "Exit") })

	// Click inside the editor, well clear of the dropdown.
	r.key(t, click(40, 8)...)
	r.waitFor(t, "the dropdown to close", func(s string) bool { return !strings.Contains(s, "Exit") })

	// And the click put the keyboard in the editor, so typing works at once.
	r.key(t, runeKey('i'), runeKey('w'))
	r.waitFor(t, "typing after the click", func(s string) bool {
		return strings.Contains(lastNonEmpty(strings.Split(s, "\n")), "INSERT")
	})
}
