package tui

import "testing"

func TestContextUpdateDefersWithoutGrowingLifetimeHooks(t *testing.T) {
	t.Parallel()
	p := &probe{name: "owner", pref: Size{W: 2, H: 1}}
	root := NewFlex(Vertical)
	root.Add(p)
	h := startApp(t, root, 20, 5)
	calls := 0
	h.onLoop(func() {
		hooks := len(p.ctx.node.hooks)
		for range 1000 {
			p.ctx.Update(func() { calls++ })
		}
		if calls != 0 {
			t.Error("owner update ran inline")
		}
		if len(p.ctx.node.hooks) != hooks {
			t.Error("deferred updates accumulated lifetime hooks")
		}
	})
	h.sync()
	h.onLoop(func() {
		if calls != 1000 {
			t.Errorf("updates=%d", calls)
		}
	})
}

func TestContextUpdateDropsAnUnmountedOriginatingOwner(t *testing.T) {
	t.Parallel()
	p := &probe{name: "owner", pref: Size{W: 2, H: 1}}
	root := NewFlex(Vertical)
	root.Add(p)
	h := startApp(t, root, 20, 5)
	called := false
	h.onLoop(func() { p.ctx.Update(func() { called = true }); root.Remove(p) })
	h.sync()
	h.onLoop(func() {
		if called {
			t.Error("update ran after its originating mount ended")
		}
	})
}
