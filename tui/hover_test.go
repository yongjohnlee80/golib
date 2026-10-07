package tui

import (
	"sync/atomic"
	"testing"
)

// hoverBox records HoverChanged calls.
type hoverBox struct {
	box
	on, off atomic.Int64
}

func (h *hoverBox) HoverChanged(hovered bool) {
	if hovered {
		h.on.Add(1)
	} else {
		h.off.Add(1)
	}
}

func motion(x, y int) MouseEvent { return MouseEvent{Kind: MouseMotion, X: x, Y: y} }

func TestHover_OntoAndOff(t *testing.T) {
	a := &hoverBox{}
	root := &box{rects: []Rect{{X: 1, Y: 1, W: 3, H: 1}}}
	root.Add(a)
	h := startApp(t, root, 8, 3)
	h.inject(motion(2, 1))
	waitFor(t, "hover on", func() bool { return a.on.Load() == 1 })
	h.inject(motion(3, 1)) // still over it: no second call
	h.inject(motion(6, 2))
	waitFor(t, "hover off", func() bool { return a.off.Load() == 1 })
	if a.on.Load() != 1 {
		t.Fatalf("HoverChanged(true) called %d times; want once", a.on.Load())
	}
	var hovered bool
	h.onLoop(func() { hovered = a.ctx.Hovered() })
	if hovered {
		t.Fatal("Context.Hovered after the pointer left")
	}
}

// During a capture only the owner is hovered, and only while the pointer is inside it: dragging
// out of a button un-hovers it though it holds the drag.
func TestHover_CaptureDragOut(t *testing.T) {
	owner := &dragger{size: Size{W: 4, H: 2}}
	root := &box{rects: []Rect{{W: 4, H: 2}}}
	root.Add(owner)
	h := startApp(t, root, 12, 4)
	startDrag(t, h, owner, 1, 1)
	hovered := func() (v bool) { h.onLoop(func() { v = owner.ctx.Hovered() }); return v }
	if !hovered() {
		t.Fatal("the owner is not hovered under the pointer that pressed it")
	}
	h.inject(MouseEvent{Kind: MouseMotion, Button: MouseLeft, X: 9, Y: 3})
	waitFor(t, "the owner un-hovered once the drag left it", func() bool { return !hovered() })
	h.inject(MouseEvent{Kind: MouseMotion, Button: MouseLeft, X: 2, Y: 1})
	waitFor(t, "the owner hovered when the drag came back", hovered)
	h.inject(MouseEvent{Kind: MouseMotion, Button: MouseLeft, X: 9, Y: 3})
	h.inject(MouseEvent{Kind: MouseRelease, Button: MouseLeft, X: 9, Y: 3})
	waitFor(t, "the release reached the owner", func() bool { return owner.releases.Load() == 1 })
	if hovered() {
		t.Fatal("the owner is hovered after a release outside it")
	}
}

// Nothing behind a trapping scope is hovered.
func TestHover_TrapCeiling(t *testing.T) {
	behind := &hoverBox{}
	m1 := newFocusProbe("m1", Size{W: 6, H: 1})
	modal := newScopeProbe(true)
	modal.Add(m1)
	root := &box{rects: []Rect{{W: 6, H: 1}, {Y: 1, W: 6, H: 1}}}
	root.Add(behind, modal)
	h := startApp(t, root, 6, 2)
	h.onLoop(func() { m1.ctx.RequestFocus() })
	waitFor(t, "m1 focused", func() bool { return focusedID(h) == m1.nodeID() })

	h.inject(motion(1, 0), motion(1, 1))
	waitFor(t, "m1 hovered", func() bool {
		var id NodeID
		h.onLoop(func() { id = h.app.hovered })
		return id == m1.nodeID()
	})
	if behind.on.Load() != 0 {
		t.Fatal("a node behind a trapping scope was hovered")
	}
}

// Leaving the window, losing the window's focus, and unmounting each clear hover.
func TestHover_Clears(t *testing.T) {
	a := &hoverBox{}
	root := &box{rects: []Rect{{W: 4, H: 1}}}
	root.Add(a)
	h := startApp(t, root, 6, 2)

	h.inject(motion(1, 0))
	waitFor(t, "hover on", func() bool { return a.on.Load() == 1 })
	h.inject(PointerLeaveEvent{})
	waitFor(t, "cleared on leave", func() bool { return a.off.Load() == 1 })

	h.inject(motion(1, 0))
	waitFor(t, "hover on again", func() bool { return a.on.Load() == 2 })
	h.inject(FocusEvent{Terminal: true, Gained: false})
	waitFor(t, "cleared on focus loss", func() bool { return a.off.Load() == 2 })

	h.inject(motion(1, 0))
	waitFor(t, "hover on a third time", func() bool { return a.on.Load() == 3 })
	h.onLoop(func() { root.Remove(a) })
	var id NodeID
	h.onLoop(func() { id = h.app.hovered })
	if id != 0 {
		t.Fatalf("hovered = %d after the node unmounted", id)
	}
}

func TestRoleOf(t *testing.T) {
	if RoleOf(&box{}) != RoleNone {
		t.Fatal("a component that reports nothing has a role")
	}
}
