package demoapp

// The CI gate for the demo: its full interaction script driven
// deterministically on tui.TestBackend — no PTY: mount, resize, Tab cycling
// across panes, typing, submit, async list fill via App.Go, a concurrent
// BufferView write, grid snapshots at each step, and the write
// discipline (exactly one Flush per change, zero flushes when idle).

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/yongjohnlee80/golib/tui"
)

// columnOf maps a byte offset in a TestBackend.String row to a grid column
// (valid while every painted cell is width 1 — true for this demo's grid).
func columnOf(row string, byteIdx int) int {
	return utf8.RuneCountInString(row[:byteIdx])
}

// gate drives one demo app over a TestBackend.
type gate struct {
	t      *testing.T
	app    *tui.App
	tb     *tui.TestBackend
	d      *demo
	cancel context.CancelFunc
	resc   chan error
}

func startDemo(t *testing.T, w, h int) *gate {
	t.Helper()
	tb := tui.NewTestBackend(w, h)
	ctx, cancel := context.WithCancel(context.Background())
	d := New(cancel, false) // deterministic: no ticker stream
	app := tui.NewApp(d, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
	g := &gate{t: t, app: app, tb: tb, d: d, cancel: cancel, resc: make(chan error, 1)}
	go func() { g.resc <- app.Run(ctx) }()
	g.sync()
	t.Cleanup(func() {
		tui.FailOnViolations(t, tb)
		g.cancel()
		select {
		case err := <-g.resc:
			if err != nil {
				t.Errorf("Run: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("demo did not shut down within 5s")
		}
	})
	return g
}

func (g *gate) sync() {
	g.t.Helper()
	done := make(chan struct{})
	g.app.Update(func() { close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		g.t.Fatalf("loop did not respond within 3s")
	}
}

func (g *gate) onLoop(fn func()) {
	g.t.Helper()
	done := make(chan struct{})
	g.app.Update(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		g.t.Fatalf("loop did not run Update within 3s")
	}
}

func (g *gate) inject(evs ...tui.Event) {
	g.t.Helper()
	if err := g.tb.Inject(evs...); err != nil {
		g.t.Fatalf("inject: %v", err)
	}
}

func (g *gate) waitFor(desc string, cond func() bool) {
	g.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	g.t.Fatalf("timed out waiting for %s\n%s", desc, g.tb.String())
}

// settle waits for flush quiescence, the baseline for flush counting.
func (g *gate) settle() {
	g.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		g.sync()
		g.sync()
		f := g.tb.Flushes()
		time.Sleep(3 * time.Millisecond)
		g.sync()
		if g.tb.Flushes() == f {
			return
		}
		if time.Now().After(deadline) {
			g.t.Fatalf("frames did not settle")
		}
	}
}

func (g *gate) contains(sub string) bool { return strings.Contains(g.tb.String(), sub) }

func (g *gate) want(step, sub string) {
	g.t.Helper()
	if !g.contains(sub) {
		g.t.Fatalf("%s: grid does not contain %q:\n%s", step, sub, g.tb.String())
	}
}

func (g *gate) wantNot(step, sub string) {
	g.t.Helper()
	if g.contains(sub) {
		g.t.Fatalf("%s: grid unexpectedly contains %q:\n%s", step, sub, g.tb.String())
	}
}

func key(code rune) tui.KeyEvent {
	text := ""
	if code >= 0x20 && code < 0xE000 {
		text = string(code)
	}
	return tui.KeyEvent{Kind: tui.KeyPress, Code: code, Text: text}
}

func typeString(s string) []tui.Event {
	evs := make([]tui.Event, 0, len(s))
	for _, r := range s {
		evs = append(evs, key(r))
	}
	return evs
}

// attrsAt returns the resolved cell attrs at (x, y).
func (g *gate) attrsAt(x, y int) tui.CellAttrs {
	snap := g.tb.Snapshot()
	return snap[y][x].Attrs
}

// TestDemoInteractionScript is that gate.
func TestDemoInteractionScript(t *testing.T) {
	g := startDemo(t, 80, 24)

	// 1 — mount snapshot: chrome + status bar; async fill lands (App.Go →
	// TaskResult → SetItems), status right updates.
	g.waitFor("async list fill", func() bool { return g.contains("epsilon") })
	for _, sub := range []string{"Items", "Input", "Log", "golib/tui demo", "5 items", "type and press Enter"} {
		g.want("mount", sub)
	}

	// 2 — resize: layout re-derives, content survives, one flush.
	g.settle()
	f := g.tb.Flushes()
	g.tb.InjectResize(60, 18)
	g.waitFor("resize repaint", func() bool { return g.tb.Flushes() > f })
	if got := g.tb.Flushes(); got != f+1 {
		t.Fatalf("resize produced %d flushes, want exactly 1 (§5.4)", got-f)
	}
	g.want("resize", "epsilon")
	g.want("resize", "golib/tui demo")

	// 3 — Tab cycling across the three panes, asserting the focused box
	// border (TokenBorderFocused = ANSI 4 in the default theme). The Items
	// box corner is at (0,0).
	focusedCorner := func() bool {
		a := g.attrsAt(0, 0)
		return a.FG.Kind == tui.CellColorANSI && a.FG.Index == 4
	}
	g.inject(key(tui.KeyTab)) // → Items list
	g.waitFor("Items pane focused", focusedCorner)
	g.inject(key(tui.KeyTab)) // → Input
	g.waitFor("Items pane blurred", func() bool { return !focusedCorner() })
	x, y, visible := 0, 0, false
	g.waitFor("hardware cursor in the input (IME rule)", func() bool {
		x, y, visible = g.tb.CursorPos()
		return visible
	})
	if y != 1 {
		t.Fatalf("cursor at (%d,%d), want the input row 1", x, y)
	}

	// 4 — type into the input: exactly one flush per keystroke.
	g.settle()
	f = g.tb.Flushes()
	g.inject(key('h'))
	g.waitFor("keystroke repaint", func() bool { return g.tb.Flushes() > f })
	if got := g.tb.Flushes(); got != f+1 {
		t.Fatalf("one keystroke produced %d flushes, want exactly 1 (§5.4)", got-f)
	}
	g.inject(typeString("ello")...)
	g.waitFor("typed value", func() bool { return g.contains("hello") })

	// 5 — submit: SubmitEvent → log line (SGR-colored), input cleared,
	// status counter.
	g.inject(key(tui.KeyEnter))
	g.waitFor("submit logged", func() bool { return g.contains("submitted: hello") })
	g.want("submit", "1 submitted")
	g.want("submit", "✓")
	g.waitFor("input cleared", func() bool { return g.contains("type and press Enter") })
	// The ✓ is ANSI green (2) — the log's ANSI interpreter at work.
	found := false
	for yy, row := range strings.Split(g.tb.String(), "\n") {
		if xx := strings.Index(row, "✓"); xx >= 0 {
			a := g.attrsAt(columnOf(row, xx), yy)
			if a.FG.Kind != tui.CellColorANSI || a.FG.Index != 2 {
				t.Fatalf("log ✓ FG = %+v, want ANSI 2", a.FG)
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("✓ not found in the grid")
	}

	// 6 — concurrent BufferView write from OFF the loop (the Writer handle
	// contract): ordered, styled, follow-tail pinned.
	var errc = make(chan error, 1)
	go func() {
		_, err := g.d.log.Writer().Write([]byte("\x1b[35mstream-1\x1b[0m\nstream-2\n"))
		errc <- err
	}()
	if err := <-errc; err != nil {
		t.Fatalf("concurrent write: %v", err)
	}
	g.waitFor("streamed lines", func() bool { return g.contains("stream-1") && g.contains("stream-2") })

	// 7 — idle: zero flushes with no input.
	g.settle()
	f = g.tb.Flushes()
	time.Sleep(80 * time.Millisecond)
	g.sync()
	if got := g.tb.Flushes(); got != f {
		t.Fatalf("idle app produced %d flush(es), want 0 (§5.4 / ADR-0001 G5)", got-f)
	}

	// 8 — quit: Ctrl+C cancels the run context; Run returns cleanly (the
	// cleanup asserts the error).
	g.inject(tui.KeyEvent{Kind: tui.KeyPress, Code: 'c', Mods: tui.ModCtrl})
	select {
	case err := <-g.resc:
		if err != nil {
			t.Fatalf("Run after Ctrl+C: %v", err)
		}
		g.resc <- nil // let the cleanup read it again
	case <-time.After(3 * time.Second):
		t.Fatalf("Ctrl+C did not quit the demo")
	}
}
