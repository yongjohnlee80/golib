package widget_test

// Tabs, Split, Float, StatusBar, ProgressBar, Text contracts (ADR-0007
// §2.5, §5.3, §5.8).

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// lifeProbe records mounts/unmounts (Tabs keep-mounted semantics).
type lifeProbe struct {
	widget.Base
	label    string
	inits    atomic.Int32
	unmounts atomic.Int32
}

func (p *lifeProbe) Init(ctx *tui.Context) {
	p.Base.Init(ctx)
	p.inits.Add(1)
	ctx.OnUnmount(func() { p.unmounts.Add(1) })
}

func (p *lifeProbe) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: 5, H: 1})
}

func (p *lifeProbe) Render(s tui.Surface) {
	for x, r := range p.label {
		s.SetCell(x, 0, string(r), style.New())
	}
}

// pane is a plain focusable filler for Split focus/resize tests.
type pane struct {
	widget.Base
	fill string
}

func (p *pane) AcceptsFocus() bool { return true }

func (p *pane) Layout(c tui.Constraints) tui.Size {
	return c.Constrain(tui.Size{W: c.MaxW, H: c.MaxH})
}

func (p *pane) Render(s tui.Surface) {
	sz := s.Size()
	s.Fill(tui.Rect{X: 0, Y: 0, W: sz.W, H: sz.H}, p.fill, style.New())
}

func TestTabsSwitching(t *testing.T) {
	a := &lifeProbe{label: "AAAAA"}
	b := &lifeProbe{label: "BBBBB"}
	tabs := widget.NewTabs(widget.WithTab("one", a), widget.WithTab("two", b))
	sh := newShell(tabs)
	h := startApp(t, sh, 20, 4)
	changed := record[widget.TabChangedEvent](h)
	h.inject(tab()) // focus the bar
	h.barrier(sh)

	h.wantContains("one")
	h.wantContains("AAAAA")
	h.wantNotContains("BBBBB")
	if b.inits.Load() != 0 {
		t.Fatalf("inactive tab content mounted eagerly")
	}

	h.inject(key(']'))
	h.barrier(sh)
	h.wantContains("BBBBB")
	h.wantNotContains("AAAAA")
	if a.unmounts.Load() != 1 {
		t.Fatalf("switching did not unmount the old tab (default mode)")
	}
	var id tui.NodeID
	h.onLoop(func() { id = tabs.NodeID() })
	if ev, ok := changed.last(); !ok || ev.Owner != id || ev.Index != 1 || ev.Label != "two" {
		t.Fatalf("TabChangedEvent = %+v", ev)
	}

	// Ctrl+PgUp cycles back even bubbling from content.
	h.inject(keyMod(tui.KeyPageUp, tui.ModCtrl))
	h.barrier(sh)
	h.wantContains("AAAAA")
	if a.inits.Load() != 2 {
		t.Fatalf("returning to tab one did not remount it (inits=%d)", a.inits.Load())
	}
}

func TestTabsKeepMounted(t *testing.T) {
	a := &lifeProbe{label: "AAAAA"}
	b := &lifeProbe{label: "BBBBB"}
	tabs := widget.NewTabs(widget.WithTab("one", a), widget.WithTab("two", b), widget.WithKeepMounted(true))
	sh := newShell(tabs)
	h := startApp(t, sh, 20, 4)
	h.inject(tab())
	h.inject(key(']'), key('['))
	h.barrier(sh)
	if a.unmounts.Load() != 0 || b.unmounts.Load() != 0 {
		t.Fatalf("keep-mounted content was unmounted (a=%d b=%d)", a.unmounts.Load(), b.unmounts.Load())
	}
	h.wantContains("AAAAA")
	h.wantNotContains("BBBBB") // mounted but not laid out → invisible
}

// TestTabsArrowSwitching: the ←/→ arrows cycle the focused bar, alongside [ ].
func TestTabsArrowSwitching(t *testing.T) {
	a := &lifeProbe{label: "AAAAA"}
	b := &lifeProbe{label: "BBBBB"}
	tabs := widget.NewTabs(widget.WithTab("one", a), widget.WithTab("two", b), widget.WithKeepMounted(true))
	sh := newShell(tabs)
	h := startApp(t, sh, 20, 4)
	h.inject(tab()) // focus the bar
	h.barrier(sh)
	h.wantContains("AAAAA")

	h.inject(key(tui.KeyRight))
	h.barrier(sh)
	h.wantContains("BBBBB")
	h.wantNotContains("AAAAA")

	h.inject(key(tui.KeyLeft))
	h.barrier(sh)
	h.wantContains("AAAAA")
	h.wantNotContains("BBBBB")
}

// TestTabsAutoFocus: WithAutoFocus makes the bar take focus on Init, so the
// arrows drive it without a preceding Tab.
func TestTabsAutoFocus(t *testing.T) {
	a := &lifeProbe{label: "AAAAA"}
	b := &lifeProbe{label: "BBBBB"}
	tabs := widget.NewTabs(widget.WithTab("one", a), widget.WithTab("two", b),
		widget.WithKeepMounted(true), widget.WithAutoFocus(true))
	sh := newShell(tabs)
	h := startApp(t, sh, 20, 4)
	h.barrier(sh)
	h.wantContains("AAAAA")

	h.inject(key(tui.KeyRight)) // no tab() first — the bar auto-focused
	h.barrier(sh)
	h.wantContains("BBBBB")
}

// TestTabsWithoutBar: bar-less mode gives the active content the full height
// (row 0) and paints no bar (no tab labels).
func TestTabsWithoutBar(t *testing.T) {
	a := &lifeProbe{label: "AAAAA"}
	b := &lifeProbe{label: "BBBBB"}
	tabs := widget.NewTabs(
		widget.WithTab("one", a), widget.WithTab("two", b),
		widget.WithKeepMounted(true), widget.WithoutBar())
	sh := newShell(tabs)
	h := startApp(t, sh, 20, 4)
	h.barrier(sh)
	h.wantContains("AAAAA")
	h.wantNotContains("one") // no bar drawn
	h.wantNotContains("two")
}

func TestSplitLayoutAndKeyboardResize(t *testing.T) {
	left := &pane{fill: "a"}
	right := &pane{fill: "b"}
	split := widget.NewSplit(widget.Horizontal, left, right, widget.WithRatio(0.5), widget.WithMinSizes(4, 4))
	sh := newShell(split)
	h := startApp(t, sh, 21, 5)
	resized := record[widget.SplitResizedEvent](h)
	h.inject(tab()) // focus the left box
	h.barrier(sh)

	// 21 wide: avail 20, ratio .5 → divider at column 10.
	if got := strings.Index(h.row(2), "│"); got != 10 {
		t.Fatalf("divider at %d, want 10\n%s", got, h.grid())
	}

	h.inject(keyMod(tui.KeyRight, tui.ModAlt))
	h.barrier(sh)
	if got := strings.Index(h.row(2), "│"); got != 11 {
		t.Fatalf("divider after Alt+Right at %d, want 11\n%s", got, h.grid())
	}
	if ev, ok := resized.last(); !ok || ev.Ratio <= 0.5 {
		t.Fatalf("SplitResizedEvent = %+v, want ratio > 0.5", ev)
	}

	// Min sizes clamp: hammer Alt+Right; pane b never shrinks below 4.
	for i := 0; i < 20; i++ {
		h.inject(keyMod(tui.KeyRight, tui.ModAlt))
	}
	h.barrier(sh)
	if got := strings.Index(h.row(2), "│"); got != 16 {
		t.Fatalf("divider clamped at %d, want 16 (minB=4)\n%s", got, h.grid())
	}
}

func TestSplitMouseDrag(t *testing.T) {
	split := widget.NewSplit(widget.Horizontal, widget.NewText("L"), widget.NewText("R"))
	sh := newShell(split)
	h := startApp(t, sh, 21, 3)
	h.settle()
	h.inject(
		tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseLeft, X: 10, Y: 1},
		tui.MouseEvent{Kind: tui.MouseMotion, X: 6, Y: 1},
		tui.MouseEvent{Kind: tui.MouseRelease, Button: tui.MouseLeft, X: 6, Y: 1},
	)
	h.waitFor("divider dragged", func() bool { return strings.Index(h.row(1), "│") == 6 })
}

func TestFloatModal(t *testing.T) {
	// Base UI: an input in a box; a modal float with its own input.
	baseInput := widget.NewTextInput(widget.WithPlaceholder("base"))
	dlgInput := widget.NewTextInput(widget.WithPlaceholder("dialog"))
	dialog := widget.NewFloat(
		widget.NewBox(dlgInput, widget.WithTitle("Commit message")),
		widget.WithModal(true), widget.WithDimBackground(true))
	host := widget.NewOverlayHost(widget.NewBox(baseInput, widget.WithTitle("Base")))
	host.Attach(dialog)
	sh := newShell(host)
	h := startApp(t, sh, 30, 9)
	dismissed := record[widget.DismissEvent](h)
	h.inject(tab()) // focus the base input
	h.barrier(sh)
	h.wantNotContains("Commit message")

	h.onLoop(dialog.Show)
	h.settle()
	h.wantContains("Commit message")
	h.wantContains("░") // dim scrim

	// Focus seeded into the dialog input: typing lands there.
	h.inject(typeString("msg")...)
	h.barrier(sh)
	var dlgVal, baseVal string
	h.onLoop(func() { dlgVal = dlgInput.Value(); baseVal = baseInput.Value() })
	if dlgVal != "msg" || baseVal != "" {
		t.Fatalf("typing went to dialog=%q base=%q, want dialog only", dlgVal, baseVal)
	}

	// Tab cycles inside the trap only: after Tab, typing still lands in
	// the dialog (its input is the only stop).
	h.inject(tab())
	h.inject(typeString("!")...)
	h.barrier(sh)
	h.onLoop(func() { dlgVal = dlgInput.Value(); baseVal = baseInput.Value() })
	if dlgVal != "msg!" || baseVal != "" {
		t.Fatalf("Tab escaped the trap: dialog=%q base=%q", dlgVal, baseVal)
	}

	// Esc dismisses with DismissEvent; focus restores to the base input.
	h.inject(key(tui.KeyEscape))
	h.barrier(sh)
	h.wantNotContains("Commit message")
	h.wantNotContains("░")
	var id tui.NodeID
	h.onLoop(func() { id = dialog.NodeID() })
	if ev, ok := dismissed.last(); !ok || ev.Owner != id {
		t.Fatalf("DismissEvent = %+v, want owner %d", ev, id)
	}
	h.inject(typeString("back")...)
	h.barrier(sh)
	h.onLoop(func() { baseVal = baseInput.Value() })
	if baseVal != "back" {
		t.Fatalf("focus did not restore to the base input: %q", baseVal)
	}
}

func TestStatusBarSegments(t *testing.T) {
	bar := widget.NewStatusBar()
	h := startApp(t, bar, 40, 1)
	h.onLoop(func() {
		bar.SetLeft("LEFT")
		bar.SetCenter("CENTER")
		bar.SetRight("RIGHT", style.New().Bold(true))
	})
	h.settle()
	row := h.row(0)
	if !strings.HasPrefix(row, "LEFT") || !strings.HasSuffix(row, "RIGHT") {
		t.Fatalf("segment placement: %q", row)
	}
	if got := strings.Index(row, "CENTER"); got != 17 {
		t.Fatalf("center segment at %d, want 17: %q", got, row)
	}
	// Truncation priority (center first, right survives): narrow to 14.
	h.tb.InjectResize(14, 1)
	h.waitFor("narrow bar", func() bool { return strings.HasSuffix(h.row(0), "RIGHT") && !strings.Contains(h.row(0), "CENTER") })
}

// TestProgressBarIdleZeroFlush asserts §5.8: no tick registration while
// determinate-and-idle — the idle app emits zero bytes (no flushes).
func TestProgressBarIdleZeroFlush(t *testing.T) {
	p := widget.NewProgressBar()
	h := startApp(t, p, 20, 1)

	// Indeterminate: the animation produces frames.
	h.onLoop(p.SetIndeterminate)
	start := h.tb.Flushes()
	h.waitFor("animation frames", func() bool { return h.tb.Flushes() >= start+2 })

	// Determinate: the subscription is cancelled; the app goes fully idle.
	h.onLoop(func() { p.SetProgress(0.5) })
	h.settle()
	idle := h.tb.Flushes()
	time.Sleep(120 * time.Millisecond) // > the 100ms animation interval
	h.sync()
	if got := h.tb.Flushes(); got != idle {
		t.Fatalf("determinate-idle progress bar produced %d extra flush(es) — tick subscription leaked (§5.8)", got-idle)
	}
	h.wantContains("█") // half-filled bar painted
}

func TestProgressBarSpinner(t *testing.T) {
	p := widget.NewProgressBar(widget.WithSpinner([]string{"|", "/", "-", "\\"}, 5*time.Millisecond))
	h := startApp(t, p, 5, 1)
	h.onLoop(p.SetIndeterminate)
	h.waitFor("spinner frame", func() bool {
		g := strings.TrimRight(h.row(0), " ")
		return g == "|" || g == "/" || g == "-" || g == "\\"
	})
	first := h.row(0)
	h.waitFor("spinner advances", func() bool { return h.row(0) != first })
}

func TestTextWrapAndTruncate(t *testing.T) {
	trunc := widget.NewText("a very long label that overflows")
	h := startApp(t, trunc, 12, 1)
	h.settle()
	if got := h.row(0); !strings.HasSuffix(strings.TrimRight(got, " "), "…") {
		t.Fatalf("Truncate mode did not ellipsize: %q", got)
	}

	wrap := widget.NewText("alpha beta gamma", widget.WithWrapMode(widget.Wrap))
	h2 := startApp(t, wrap, 6, 4)
	h2.settle()
	h2.wantContains("alpha")
	h2.wantContains("beta")
	h2.wantContains("gamma")
	if strings.Contains(h2.row(0), "beta") {
		t.Fatalf("Wrap mode did not wrap: %q", h2.row(0))
	}
}
