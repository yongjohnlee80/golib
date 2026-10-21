package widget_test

import (
	"sync"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// A commit reports the choice, then the close, then tells the owner: the bus hears
// SelectionChangedEvent before ClosedEvent, and the owner's hook runs once the options have closed.
func TestASelectCommitsThenClosesThenTellsItsOwner(t *testing.T) {
	var sel *widget.Select[string]
	var mu sync.Mutex
	var bus []string
	var openAtChange []bool
	sel = widget.NewSelect[string](widget.WithOptions(selectItems("alpha", "beta", "gamma")),
		widget.WithSelectOnChange[string](func(i int) { openAtChange = append(openAtChange, sel.Open()) }))
	flex := tui.NewFlex(tui.Vertical)
	flex.Add(sel)
	sh := newShell(widget.NewOverlayHost(flex))
	h := startApp(t, sh, 30, 10)
	tui.Subscribe(h.app.Bus(), func(widget.SelectionChangedEvent) { mu.Lock(); bus = append(bus, "selected"); mu.Unlock() })
	tui.Subscribe(h.app.Bus(), func(widget.ClosedEvent) { mu.Lock(); bus = append(bus, "closed"); mu.Unlock() })
	h.inject(tab())
	h.barrier(sh)
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	h.inject(key(tui.KeyDown), key(tui.KeyEnter))
	h.barrier(sh)
	h.waitFor("the choice and the close on the bus", func() bool { mu.Lock(); defer mu.Unlock(); return len(bus) >= 2 })
	var changes []bool
	h.onLoop(func() { changes = append(changes, openAtChange...) })
	mu.Lock()
	got := append([]string(nil), bus...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "selected" || got[1] != "closed" {
		t.Errorf("bus heard %q, want [selected closed]", got)
	}
	if len(changes) != 1 || changes[0] {
		t.Errorf("the owner's hook ran %d times, seeing open=%v; want once, closed", len(changes), changes)
	}
}

// A wheel notch over the open options' backdrop, outside their panel, moves the highlight, as one
// over the panel does: Enter then commits the next option.
func TestASelectsWheelOverItsBackdropMovesTheHighlight(t *testing.T) {
	h, sel, sh := selectFixture(t, widget.WithOptions(selectItems("alpha", "beta", "gamma")))
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	h.wantContains("gamma")
	h.inject(tui.MouseEvent{Kind: tui.MouseWheel, Button: tui.WheelDown, X: 28, Y: 9})
	h.barrier(sh)
	h.inject(key(tui.KeyEnter))
	h.barrier(sh)
	var val string
	h.onLoop(func() { val, _ = sel.Value() })
	if val != "BETA" {
		t.Errorf("after a wheel notch over the backdrop, Enter chose %q; want BETA", val)
	}
}
