package widget_test

import (
	"slices"
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A Float says why it closed: Escape reports DismissEscape and Hide reports DismissProgrammatic, in
// an OverlayDismissedEvent as Modal's. The deprecated DismissEvent is still published, just before
// it, and each exactly once per hide: a Hide of a Float already hidden publishes nothing.
func TestAFloatsDismissalSaysWhy(t *testing.T) {
	dialog := widget.NewFloat(widget.NewBox(widget.NewTextInput()), widget.WithModal(true))
	host := widget.NewOverlayHost(widget.NewBox(widget.NewTextInput()))
	host.Attach(dialog)
	sh := newShell(host)
	h := startApp(t, sh, 30, 8)
	var mu sync.Mutex
	var got []string
	note := func(s string) { mu.Lock(); got = append(got, s); mu.Unlock() }
	tui.Subscribe(h.app.Bus(), func(widget.DismissEvent) { note("DismissEvent") })
	tui.Subscribe(h.app.Bus(), func(e widget.OverlayDismissedEvent) { note("Overlay:" + e.Reason.String()) })
	snapshot := func() []string { mu.Lock(); defer mu.Unlock(); return slices.Clone(got) }

	h.onLoop(dialog.Show)
	h.settle()
	h.inject(key(tui.KeyEscape))
	h.barrier(sh)
	h.onLoop(dialog.Show)
	h.settle()
	h.onLoop(func() { dialog.Hide(); dialog.Hide() })
	h.settle()
	want := []string{"DismissEvent", "Overlay:escape", "DismissEvent", "Overlay:programmatic"}
	if g := snapshot(); !slices.Equal(g, want) {
		t.Fatalf("published %q, want %q", g, want)
	}
}
