package tui

import (
	"sync/atomic"
	"testing"
)

// The too-small screen's quit follows the key map: rebound to x, x quits and q no longer does, and
// the screen prints the key that quits it rather than the default ones.
func TestTooSmall_QuitFollowsTheKeymap(t *testing.T) {
	m := mustMap(t, Commands(), mustScheme(t, `<mapping>
<shortcut id="tui.tooSmall.quit"><key value="X"/></shortcut>
</mapping>`))
	var quits atomic.Int64
	h := startApp(t, &probe{name: "root"}, 30, 8, WithMinimumSize(Size{W: 40, H: 10}), WithKeymap(m),
		WithQuit(func() { quits.Add(1) }))
	waitFor(t, "the rebound key printed", func() bool { return screenHas(h.tb, "or press X") })
	if screenHas(h.tb, "Ctrl+C") {
		t.Errorf("the screen names Ctrl+C, which no longer quits:\n%s", h.tb.String())
	}
	h.inject(KeyEvent{Code: 'q', Text: "q"}, KeyEvent{Code: 'c', Mods: ModCtrl})
	inOrder(t, h, 31)
	if n := quits.Load(); n != 0 {
		t.Fatalf("q or Ctrl+C quit (%d) after the map rebound the quit to x", n)
	}
	h.inject(KeyEvent{Code: 'x', Text: "x"})
	waitFor(t, "x quits", func() bool { return quits.Load() == 1 })
}
