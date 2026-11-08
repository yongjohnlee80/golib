package widget

// The unkeyed overlay operations are UNEXPORTED — the layers they mount are this
// package's own private types — so these tests live inside the package: nothing
// outside can call them, which is exactly why nothing outside can prove they are
// idempotent and safe during teardown.

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui/widget/internal/widgettest"
)

// TestARefusedAnchoredOpenChangesNothing.
//
// OpenAnchored is a registration transaction and the caller gets the error
// directly — there is no longer an asynchronous handshake to lose it in. What
// still has to hold is the other half: a refusal leaves the host exactly as it
// was, with nothing mounted and nothing registered.
func TestARefusedAnchoredOpenChangesNothing(t *testing.T) {
	base := NewButton("base")
	host := NewOverlayHost(base)
	h := widgettest.StartApp(t, host, 20, 6)
	defer h.Stop()

	before := host.Stack.Len()
	var err error
	h.OnLoop(func() {
		// The zero AnchorRef names no owner, so the host must refuse it.
		err = host.OpenAnchored("p", NewText("x"), AnchorSpec{}, nil)
	})
	h.Sync()

	if err == nil {
		t.Fatal("OpenAnchored accepted the zero AnchorRef")
	}
	if got := host.Stack.Len(); got != before {
		t.Errorf("the stack holds %d layers after a refused open, want the %d it had",
			got, before)
	}
	n := 0
	h.OnLoop(func() {
		for range host.AnchoredLayers() {
			n++
		}
	})
	if n != 0 {
		t.Errorf("a refused open registered %d anchored layers", n)
	}
}
