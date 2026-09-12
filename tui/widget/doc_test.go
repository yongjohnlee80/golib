package widget_test

// Package widget composition checks and shared TestBackend harness.
//
// This file pairs with doc.go to verify that the complete v1 widget inventory
// assembles into complex, full-featured terminal applications (e.g. sqlit- and
// lazygit-shaped tools) with zero custom plumbing, and provides the shared
// test harness, event builders, and assertion fixtures for all widget contract suites.

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// harness runs an App on a TestBackend in a background goroutine.
type harness struct {
	t   *testing.T
	app *tui.App
	tb  *tui.TestBackend

	cancel   context.CancelFunc
	resc     chan error
	stopOnce sync.Once
}

// startApp builds and runs an App over a fresh TestBackend. The frame cap
// is disabled (WithMinFrameInterval(0)) for deterministic
// one-flush-per-change assertions.
func startApp(t *testing.T, root tui.Component, w, h int) *harness {
	t.Helper()
	return startAppOpts(t, root, w, h)
}

// startAppOpts is startApp with extra App options (e.g. WithWidthPolicy).
// The backend and frame-cap options are always applied first.
func startAppOpts(t *testing.T, root tui.Component, w, h int, opts ...tui.AppOption) *harness {
	t.Helper()
	tb := tui.NewTestBackend(w, h)
	appOpts := append([]tui.AppOption{tui.WithBackend(tb), tui.WithMinFrameInterval(0)}, opts...)
	app := tui.NewApp(root, appOpts...)
	ctx, cancel := context.WithCancel(context.Background())
	h2 := &harness{t: t, app: app, tb: tb, cancel: cancel, resc: make(chan error, 1)}
	go func() { h2.resc <- app.Run(ctx) }()
	h2.sync()
	t.Cleanup(func() {
		tui.FailOnViolations(t, tb)
		h2.stop()
	})
	return h2
}

// stop cancels Run and waits for it.
func (h *harness) stop() {
	h.stopOnce.Do(func() {
		h.cancel()
		select {
		case <-h.resc:
		case <-time.After(5 * time.Second):
			h.t.Errorf("app did not shut down within 5s")
		}
	})
}

// sync round-trips an Update through the loop: every previously enqueued
// program-lane item has drained when it returns.
func (h *harness) sync() {
	h.t.Helper()
	done := make(chan struct{})
	h.app.Update(func() { close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		h.t.Fatalf("loop did not respond to Update within 3s")
	}
}

// onLoop runs fn on the loop goroutine and waits — the sanctioned way for
// tests to touch loop-owned widget state.
func (h *harness) onLoop(fn func()) {
	h.t.Helper()
	done := make(chan struct{})
	h.app.Update(func() {
		fn()
		close(done)
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		h.t.Fatalf("loop did not run Update within 3s")
	}
}

// inject scripts backend events (raw — no waiting; pair with a barrier,
// waitFor, or settle).
func (h *harness) inject(evs ...tui.Event) {
	h.t.Helper()
	if err := h.tb.Inject(evs...); err != nil {
		h.t.Fatalf("inject: %v", err)
	}
}

// settle waits for flush quiescence: no new Flush arrives across a
// double program-lane round-trip plus a small grace window. Use it before
// taking flush-count baselines.
func (h *harness) settle() {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		h.sync()
		h.sync()
		f := h.tb.Flushes()
		time.Sleep(3 * time.Millisecond)
		h.sync()
		if h.tb.Flushes() == f {
			return
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("frame activity did not settle within 3s")
		}
	}
}

// waitFor polls cond until it holds or the deadline expires.
func (h *harness) waitFor(desc string, cond func() bool) {
	h.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	h.t.Fatalf("timed out waiting for %s", desc)
}

// grid returns the current grid text.
func (h *harness) grid() string { return h.tb.String() }

// row returns row y of the grid.
func (h *harness) row(y int) string {
	rows := strings.Split(h.grid(), "\n")
	if y < 0 || y >= len(rows) {
		h.t.Fatalf("row %d outside the %d-row grid", y, len(rows))
	}
	return rows[y]
}

// wantContains asserts the grid contains sub.
func (h *harness) wantContains(sub string) {
	h.t.Helper()
	if !strings.Contains(h.grid(), sub) {
		h.t.Fatalf("grid does not contain %q:\n%s", sub, h.grid())
	}
}

// wantNotContains asserts the grid does not contain sub.
func (h *harness) wantNotContains(sub string) {
	h.t.Helper()
	if strings.Contains(h.grid(), sub) {
		h.t.Fatalf("grid unexpectedly contains %q:\n%s", sub, h.grid())
	}
}

// --- event builders ---

// key builds a plain key press; printable codes carry their text.
func key(code rune) tui.KeyEvent {
	text := ""
	if code >= 0x20 && code < 0xE000 {
		text = string(code)
	}
	return tui.KeyEvent{Kind: tui.KeyPress, Code: code, Text: text}
}

// keyMod builds a modified key press (no text — modifier chords).
func keyMod(code rune, mods tui.Mods) tui.KeyEvent {
	return tui.KeyEvent{Kind: tui.KeyPress, Code: code, Mods: mods}
}

// keyShift builds a Shift-modified key press.
func keyShift(code rune) tui.KeyEvent { return keyMod(code, tui.ModShift) }

// tab and shiftTab drive framework focus traversal.
func tab() tui.KeyEvent      { return key(tui.KeyTab) }
func shiftTab() tui.KeyEvent { return keyMod(tui.KeyTab, tui.ModShift) }

// typeString yields one key press per rune.
func typeString(s string) []tui.Event {
	evs := make([]tui.Event, 0, len(s))
	for _, r := range s {
		evs = append(evs, key(r))
	}
	return evs
}

// click builds a left mouse press at absolute (x, y).
func click(x, y int) tui.MouseEvent {
	return tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: x, Y: y}
}

// --- Bus event recorder ---

// recorder captures Bus events of type T in publish order.
type recorder[T any] struct {
	mu  sync.Mutex
	got []T
}

// record subscribes a recorder for T on the app's bus.
func record[T any](h *harness) *recorder[T] {
	r := &recorder[T]{}
	tui.Subscribe(h.app.Bus(), func(v T) {
		r.mu.Lock()
		r.got = append(r.got, v)
		r.mu.Unlock()
	})
	return r
}

// events returns a copy of the captured events.
func (r *recorder[T]) events() []T {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]T(nil), r.got...)
}

func (r *recorder[T]) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.got)
}

func (r *recorder[T]) last() (T, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.got) == 0 {
		var zero T
		return zero, false
	}
	return r.got[len(r.got)-1], true
}

// --- consumption probe ---

// shell wraps a child and records every event that bubbles up unconsumed —
// the "keys consumed vs bubbled" assertion hook.
type shell struct {
	child tui.Component
	ctx   *tui.Context

	mu     sync.Mutex
	keys   []tui.KeyEvent
	pastes int
}

func newShell(child tui.Component) *shell { return &shell{child: child} }

func (s *shell) Init(ctx *tui.Context) {
	s.ctx = ctx
	ctx.Mount(s.child)
}

func (s *shell) Layout(c tui.Constraints) tui.Size {
	w := c.MaxW
	h := c.MaxH
	if s.child != nil {
		sz := s.ctx.LayoutChild(s.child, tui.Tight(tui.Size{W: w, H: h}))
		s.ctx.PlaceChild(s.child, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
	}
	return c.Constrain(tui.Size{W: w, H: h})
}

// unmountChild unmounts the wrapped child (loop goroutine).
func (s *shell) unmountChild() {
	if s.child != nil {
		s.ctx.Unmount(s.child)
		s.child = nil
	}
}

func (s *shell) Render(tui.Surface) {}

func (s *shell) HandleEvent(ev tui.Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch e := ev.(type) {
	case tui.KeyEvent:
		s.keys = append(s.keys, e)
	case tui.PasteEvent:
		s.pastes++
	}
	return false
}

// bubbledKeys returns the key events that reached the shell (unconsumed by
// the child), excluding barrier sentinels.
func (s *shell) bubbledKeys() []tui.KeyEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]tui.KeyEvent, 0, len(s.keys))
	for _, k := range s.keys {
		if k.Code == barrierKey {
			continue
		}
		out = append(out, k)
	}
	return out
}

// sawBarriers counts barrier sentinels received so far.
func (s *shell) sawBarriers() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range s.keys {
		if k.Code == barrierKey {
			n++
		}
	}
	return n
}

// barrierKey is a sentinel no widget consumes: injected after the events
// under test, its arrival at the shell proves everything before it was
// dispatched (lane-A order is preserved).
const barrierKey = tui.KeyF12

// barrier injects the sentinel and waits for the shell to see it.
func (h *harness) barrier(s *shell) {
	h.t.Helper()
	want := s.sawBarriers() + 1
	h.inject(key(barrierKey))
	h.waitFor("input barrier", func() bool { return s.sawBarriers() >= want })
	h.settle()
}

// cellAttrs returns the resolved attrs of the grid cell at (x, y).
func cellAttrs(h *harness, x, y int) tui.CellAttrs {
	snap := h.tb.Snapshot()
	if y >= len(snap) || x >= len(snap[y]) {
		h.t.Fatalf("cell (%d,%d) outside grid", x, y)
	}
	return snap[y][x].Attrs
}

// --- Composition fixtures & tests ---

// sqlitApp is the sqlit shape: tables sidebar, query editor, results
// pane, status bar.
type sqlitApp struct {
	root    tui.Component
	tables  *widget.List[string]
	editor  *widget.TextArea
	results *widget.List[string]
	status  *widget.StatusBar

	ctx *tui.Context
}

func newSqlitApp() *sqlitApp {
	a := &sqlitApp{
		tables:  widget.NewList(widget.WithItems[string](nil, func(s string) string { return s })),
		editor:  widget.NewTextArea(widget.WithWrap(widget.WrapNone)),
		results: widget.NewList(widget.WithItems[string](nil, func(s string) string { return s })),
		status:  widget.NewStatusBar(),
	}
	main := widget.NewSplit(widget.Vertical,
		widget.NewBox(a.editor, widget.WithTitle("Query"), widget.WithStatus("F5 run")),
		widget.NewBox(a.results, widget.WithTitle("Results")),
		widget.WithRatio(0.4))
	body := widget.NewSplit(widget.Horizontal,
		widget.NewBox(a.tables, widget.WithTitle("Tables")),
		main, widget.WithRatio(0.25), widget.WithMinSizes(20, 40))
	dock := tui.NewDock()
	dock.Pin(tui.DockBottom, a.status)
	dock.Add(body)
	a.root = dock
	return a
}

func (a *sqlitApp) Init(ctx *tui.Context) {
	a.ctx = ctx
	ctx.Mount(a.root)
	a.status.SetLeft("sqlit")
	a.status.SetRight("Tab: focus · F5: run")
	// Async table fill: owner = this controller.
	ctx.Go(func(context.Context) (any, error) {
		return []string{"users", "orders", "invoices"}, nil
	})
}

func (a *sqlitApp) Layout(c tui.Constraints) tui.Size {
	sz := a.ctx.LayoutChild(a.root, c)
	a.ctx.PlaceChild(a.root, tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H})
	return sz
}

func (a *sqlitApp) Render(tui.Surface) {}

func (a *sqlitApp) HandleEvent(ev tui.Event) bool {
	switch e := ev.(type) {
	case tui.TaskResult:
		if e.Err == nil {
			if rows, ok := e.Value.([]string); ok {
				a.tables.SetItems(rows)
				a.status.SetCenter(fmt.Sprintf("%d tables", len(rows)))
				return true
			}
		}
		return false
	case tui.KeyEvent:
		if e.Kind == tui.KeyPress && e.Code == tui.KeyF5 {
			// Query → results flow: run the editor's query.
			q := a.editor.Value()
			a.results.SetItems([]string{"row-1 · " + q, "row-2 · " + q})
			a.status.SetCenter("2 rows")
			return true
		}
	}
	return false
}

// TestSqlitComposition asserts the composition builds and its
// interaction script (focus cycle, async table fill, query→results flow,
// status updates) passes deterministically.
func TestSqlitComposition(t *testing.T) {
	app := newSqlitApp()
	h := startApp(t, app, 80, 20)

	// Async table fill lands.
	h.waitFor("tables filled", func() bool { return strings.Contains(h.grid(), "invoices") })
	h.wantContains("3 tables")
	h.wantContains("Tables")
	h.wantContains("Query")
	h.wantContains("Results")
	h.wantContains("F5 run")

	// Focus cycle: Tables → Query → Results (document order), with the
	// focused Box border highlighted (TokenBorderFocused = ANSI 4).
	h.inject(tab())
	h.waitFor("tables pane focused", func() bool {
		a := cellAttrs(h, 0, 0)
		return a.FG.Kind == tui.CellColorANSI && a.FG.Index == 4
	})
	h.inject(tab()) // Query editor
	h.waitFor("query pane focused", func() bool {
		a := cellAttrs(h, 0, 0)
		return a.FG.Kind == tui.CellColorDefault // Tables pane un-highlights
	})

	// Type a query into the editor, then F5 (app-level keybinding —
	// TextArea has no submit key, Q5) runs it.
	h.inject(typeString("select 1")...)
	h.waitFor("query typed", func() bool { return strings.Contains(h.grid(), "select 1") })
	h.inject(key(tui.KeyF5))
	h.waitFor("results", func() bool { return strings.Contains(h.grid(), "row-2 · select 1") })
	h.wantContains("2 rows")

	// Third stop: Results pane.
	h.inject(tab())
	h.inject(key(tui.KeyDown), key(tui.KeyEnter))
	acts := record[widget.ActivateEvent](h)
	h.inject(key(tui.KeyEnter))
	h.waitFor("activate", func() bool { return acts.count() > 0 })
}

// TestLazygitComposition asserts the 5-panel lazygit shape builds;
// the modal Float traps focus, dims, dismisses on Esc with DismissEvent;
// BufferView streams a fake subprocess script with ANSI colors intact.
func TestLazygitComposition(t *testing.T) {
	statusBox := widget.NewBox(widget.NewList(widget.WithItems([]string{"master → origin"}, func(s string) string { return s })), widget.WithTitle("Status"))
	filesBox := widget.NewBox(widget.NewList(widget.WithItems([]string{"M main.go"}, func(s string) string { return s })), widget.WithTitle("Files"))
	branchesBox := widget.NewBox(widget.NewList(widget.WithItems([]string{"master"}, func(s string) string { return s })), widget.WithTitle("Branches"))
	commitsBox := widget.NewBox(widget.NewList(widget.WithItems([]string{"abc123 init"}, func(s string) string { return s })), widget.WithTitle("Commits"))
	view := widget.NewBufferView()
	mainBox := widget.NewBox(view, widget.WithTitle("Diff"))
	hints := widget.NewStatusBar()

	left := widget.NewSplit(widget.Vertical, statusBox,
		widget.NewSplit(widget.Vertical, filesBox,
			widget.NewSplit(widget.Vertical, branchesBox, commitsBox)))
	dock := tui.NewDock()
	dock.Pin(tui.DockBottom, hints)
	dock.Add(widget.NewSplit(widget.Horizontal, left, mainBox, widget.WithRatio(0.35)))

	msgInput := widget.NewTextInput(widget.WithPlaceholder("commit message"))
	dialog := widget.NewFloat(widget.NewBox(msgInput, widget.WithTitle("Commit message")),
		widget.WithModal(true), widget.WithDimBackground(true))
	host := widget.NewOverlayHost(dock)
	host.Attach(dialog)

	h := startApp(t, host, 80, 24)
	dismissed := record[widget.DismissEvent](h)
	h.onLoop(func() { hints.SetLeft("c: commit · q: quit") })
	h.settle()

	for _, panel := range []string{"Status", "Files", "Branches", "Commits", "Diff"} {
		h.wantContains(panel)
	}

	// Fake `git log --color=always` through the Writer handle.
	var w io.Writer
	h.onLoop(func() { w = view.Writer() })
	if _, err := w.Write([]byte("\x1b[33mcommit abc123\x1b[0m\nAuthor: dev\n")); err != nil {
		t.Fatalf("stream write: %v", err)
	}
	h.waitFor("git output", func() bool { return strings.Contains(h.grid(), "commit abc123") })
	// Color intact: find the 'c' of "commit" and assert ANSI yellow (3).
	// Byte offsets map to columns rune-wise (all cells here are width 1).
	found := false
	for y, row := range strings.Split(h.grid(), "\n") {
		x := strings.Index(row, "commit abc123")
		if x < 0 {
			continue
		}
		a := cellAttrs(h, utf8.RuneCountInString(row[:x]), y)
		if a.FG.Kind != tui.CellColorANSI || a.FG.Index != 3 {
			t.Fatalf("streamed SGR lost: %+v", a.FG)
		}
		found = true
	}
	if !found {
		t.Fatalf("streamed line not found")
	}

	// Modal dialog: dim + trap + Esc dismiss.
	h.onLoop(dialog.Show)
	h.settle()
	h.wantContains("Commit message")
	h.wantContains("░")
	h.inject(typeString("fix: things")...)
	h.waitFor("dialog input filled", func() bool {
		var msg string
		h.onLoop(func() { msg = msgInput.Value() })
		return msg == "fix: things"
	})
	// Tab cannot escape the trap: further typing still lands in the dialog.
	h.inject(tab(), tab())
	h.inject(typeString("!")...)
	h.waitFor("trap holds", func() bool {
		var msg string
		h.onLoop(func() { msg = msgInput.Value() })
		return msg == "fix: things!"
	})
	h.inject(key(tui.KeyEscape))
	h.waitFor("dialog dismissed", func() bool { return dismissed.count() == 1 })
	h.waitFor("dialog gone", func() bool { return !strings.Contains(h.grid(), "Commit message") })
	h.wantNotContains("░")
}

func ExampleOverlayHost() {
	sidebar := widget.NewBox(widget.NewText("Sidebar"))
	mainView := widget.NewBox(widget.NewText("Main View"))
	confirmBox := widget.NewBox(widget.NewText("Confirm action"))

	rootLayout := widget.NewSplit(widget.Horizontal, sidebar, mainView, widget.WithRatio(0.25))
	overlayHost := widget.NewOverlayHost(rootLayout)

	modalDialog := widget.NewFloat(confirmBox,
		widget.WithModal(true),
		widget.WithDimBackground(true),
		widget.WithAnchor(widget.Center),
	)
	overlayHost.Attach(modalDialog)
	_ = overlayHost
	// Output:
}

func ExampleText() {
	_ = widget.NewText("Active Project: golib / tui / widget",
		widget.WithTextStyle(style.New().Bold(true)),
		widget.WithWrapMode(widget.Truncate),
	)

	_ = widget.NewText("This panel displays high-volume logging output with backpressure.",
		widget.WithTextStyle(style.New().Faint(true)),
		widget.WithWrapMode(widget.Wrap),
	)
	// Output:
}
