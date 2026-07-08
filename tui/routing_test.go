package tui

// Routing tests: target-then-bubble keyboard, mouse hit-testing with
// per-hop local coordinate rewriting, topmost Stack layer wins, addressed
// events never bubble (ADR-0004 §5.3).

import (
	"testing"
)

// bubbleProbe consumes events at a configurable point and logs the visit.
type bubbleProbe struct {
	focusProbe
	consume func(Event) bool
}

func newBubbleProbe(name string, pref Size) *bubbleProbe {
	b := &bubbleProbe{}
	b.name = name
	b.pref = pref
	b.accepts.Store(true)
	return b
}

func (b *bubbleProbe) HandleEvent(ev Event) bool {
	b.probe.HandleEvent(ev) // record + log
	if b.consume != nil {
		return b.consume(ev)
	}
	return false
}

// TestKeyTargetThenBubble: ADR-0004 §5.3 — a key event reaches
// focused-leaf → parent → root in order; a true return at any hop stops the
// walk (table-driven over consumption points).
func TestKeyTargetThenBubble(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		consumer string // which hop consumes
		want     []string
	}{
		{"leaf consumes", "leaf", []string{"leaf"}},
		{"mid consumes", "mid", []string{"leaf", "mid"}},
		{"root consumes", "root", []string{"leaf", "mid", "root"}},
		{"nobody consumes", "", []string{"leaf", "mid", "root"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			log := &callLog{}
			mk := func(name string) func(Event) bool {
				return func(ev Event) bool {
					if k, ok := ev.(KeyEvent); ok && k.Code == 'z' {
						log.add(name)
						return name == tc.consumer
					}
					return false
				}
			}
			leaf := newBubbleProbe("leaf", Size{W: 2, H: 1})
			leaf.consume = mk("leaf")
			mid := &eventFlex{Flex: NewFlex(Vertical), consume: mk("mid")}
			mid.Add(leaf)
			root := &eventFlex{Flex: NewFlex(Vertical), consume: mk("root")}
			root.Add(mid)
			h := startApp(t, root, 8, 4)

			h.onLoop(func() { leaf.ctx.RequestFocus() })
			h.inject(keyEv('z'))
			waitFor(t, "bubble walk", func() bool { return len(log.get()) >= len(tc.want) })
			h.sync()
			got := log.get()
			if len(got) != len(tc.want) {
				t.Fatalf("visits = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("visits = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// eventFlex is a Flex with an injectable HandleEvent.
type eventFlex struct {
	*Flex
	consume func(Event) bool
}

func (e *eventFlex) HandleEvent(ev Event) bool {
	if e.consume != nil {
		return e.consume(ev)
	}
	return false
}

// TestMouseHitTestTopmostAndLocalCoords: ADR-0004 §5.3 — mouse hit-testing
// on overlapping Stack layers targets the topmost, and delivered
// coordinates are local at every hop.
func TestMouseHitTestTopmostAndLocalCoords(t *testing.T) {
	t.Parallel()
	base := &probe{name: "base", pref: Size{W: 10, H: 4}}
	float := &probe{name: "float", pref: Size{W: 4, H: 2}}
	float.onEvent = func(_ *probe, ev Event) bool {
		_, ok := ev.(MouseEvent)
		return ok // the float consumes its mouse events
	}
	root := NewStack()
	root.Add(base)          // bottom layer, fills
	root.AddAt(float, 4, 1) // top layer at (4,1), 4x2
	h := startApp(t, root, 10, 4)

	// Click inside the float: topmost wins; coordinates local to it.
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 5, Y: 2})
	waitFor(t, "float hit", func() bool { return float.eventCount() == 1 })
	if m := float.recorded()[0].(MouseEvent); m.X != 1 || m.Y != 1 {
		t.Fatalf("float local coords = (%d,%d), want (1,1)", m.X, m.Y)
	}
	if base.eventCount() != 0 {
		t.Fatal("base received the click the float consumed (z-order violated)")
	}

	// Click outside the float: the base layer is the target.
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 1, Y: 3})
	waitFor(t, "base hit", func() bool { return base.eventCount() == 1 })
	if m := base.recorded()[0].(MouseEvent); m.X != 1 || m.Y != 3 {
		t.Fatalf("base local coords = (%d,%d), want (1,3)", m.X, m.Y)
	}
}

// TestMouseBubbleRewritesPerHop: an unconsumed mouse event bubbles to the
// parent with coordinates rewritten to the PARENT's local space.
func TestMouseBubbleRewritesPerHop(t *testing.T) {
	t.Parallel()
	inner := &probe{name: "inner", pref: Size{W: 3, H: 1}}
	pad := &probe{name: "pad", pref: Size{W: 2, H: 2}} // offsets inner in the flex
	outer := NewFlex(Horizontal)
	outer.Add(pad, inner)
	rootLog := &callLog{}
	root := &eventFlex{Flex: NewFlex(Vertical), consume: func(ev Event) bool {
		if m, ok := ev.(MouseEvent); ok {
			rootLog.add("root")
			if m.X != 3 || m.Y != 0 {
				rootLog.add("badcoords")
			}
		}
		return false
	}}
	root.Add(outer)
	h := startApp(t, root, 8, 4)

	// inner sits at absolute x=2 (after pad). Click abs (3,0):
	// inner local (1,0); root local (3,0).
	h.inject(MouseEvent{Kind: MousePress, Button: MouseLeft, X: 3, Y: 0})
	waitFor(t, "inner then root", func() bool { return inner.eventCount() == 1 && len(rootLog.get()) >= 1 })
	if m := inner.recorded()[0].(MouseEvent); m.X != 1 || m.Y != 0 {
		t.Fatalf("inner local coords = (%d,%d), want (1,0)", m.X, m.Y)
	}
	for _, e := range rootLog.get() {
		if e == "badcoords" {
			t.Fatal("root saw non-local coordinates (must be rewritten per hop)")
		}
	}
}

// TestAddressedEventsNeverBubble: ADR-0004 §2.5.4 — TickEvent / TaskResult
// go to the owner only; unconsumed means silently done.
func TestAddressedEventsNeverBubble(t *testing.T) {
	t.Parallel()
	child := &probe{name: "child", pref: Size{W: 2, H: 1}}
	parentLog := &callLog{}
	root := &eventFlex{Flex: NewFlex(Vertical), consume: func(ev Event) bool {
		switch ev.(type) {
		case TickEvent, TaskResult, TaskProgress:
			parentLog.add("leaked")
		}
		return false
	}}
	root.Add(child)
	h := startApp(t, root, 4, 2)

	h.app.Post(TaskResult{Owner: child.nodeID(), ID: 7, Value: "v"})
	waitFor(t, "addressed delivery", func() bool { return child.eventCount() == 1 })
	h.sync()
	if leaks := parentLog.get(); len(leaks) != 0 {
		t.Fatalf("addressed event bubbled to an ancestor: %v", leaks)
	}
}
