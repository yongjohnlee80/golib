package widget_test

import (
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// TestProgressBarIdleZeroFlush asserts no tick registration while
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
