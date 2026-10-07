package gui

import (
	"gioui.org/io/pointer"
	"github.com/yongjohnlee80/golib/tui"
)

// pointerState turns Gio pointer events into the tui.MouseEvents tui/term decodes from SGR mouse
// reports: whole cells, the button pressed or released, motion with the held button, and wheel
// steps. Count is left at 0: the App synthesises it (tui/events_mouse.go). Owned by the Gio
// goroutine.
type pointerState struct {
	held             pointer.Buttons
	scrollX, scrollY float32 // device pixels scrolled and not yet a wheel step
}

// buttonOf is the tui button for one Gio button bit, in the order a terminal reports them.
func buttonOf(b pointer.Buttons) tui.MouseButton {
	switch {
	case b.Contain(pointer.ButtonPrimary):
		return tui.MouseLeft
	case b.Contain(pointer.ButtonTertiary):
		return tui.MouseMiddle
	case b.Contain(pointer.ButtonSecondary):
		return tui.MouseRight
	}
	return tui.MouseNone
}

// translate is the tui events for e, under the cell size and grid in m.
func (p *pointerState) translate(e pointer.Event, m metrics) []tui.MouseEvent {
	x, y := m.cellAt(e.Position.X, e.Position.Y)
	mods := modsOf(e.Modifiers)
	ev := func(kind tui.MouseKind, b tui.MouseButton) tui.MouseEvent {
		return tui.MouseEvent{Kind: kind, Button: b, X: x, Y: y, Mods: mods}
	}
	switch e.Kind {
	case pointer.Press:
		pressed := e.Buttons &^ p.held
		p.held = e.Buttons
		if b := buttonOf(pressed); b != tui.MouseNone {
			return []tui.MouseEvent{ev(tui.MousePress, b)}
		}
	case pointer.Release, pointer.Cancel:
		released := p.held &^ e.Buttons
		if e.Kind == pointer.Cancel {
			released = p.held
		}
		p.held &^= released
		if b := buttonOf(released); b != tui.MouseNone {
			return []tui.MouseEvent{ev(tui.MouseRelease, b)}
		}
	case pointer.Move:
		return []tui.MouseEvent{ev(tui.MouseMotion, tui.MouseNone)}
	case pointer.Drag:
		return []tui.MouseEvent{ev(tui.MouseMotion, buttonOf(p.held))}
	case pointer.Scroll:
		return p.wheel(e, m, ev)
	}
	return nil
}

// wheel turns scrolled pixels into wheel steps of half a cell's height, so a wheel notch is at
// least one step and a touchpad's small deltas add up instead of being lost.
func (p *pointerState) wheel(e pointer.Event, m metrics, ev func(tui.MouseKind, tui.MouseButton) tui.MouseEvent) []tui.MouseEvent {
	step := max(float32(m.cell.Y)/2, 1)
	var out []tui.MouseEvent
	p.scrollY += e.Scroll.Y
	p.scrollX += e.Scroll.X
	for ; p.scrollY >= step; p.scrollY -= step {
		out = append(out, ev(tui.MouseWheel, tui.WheelDown))
	}
	for ; p.scrollY <= -step; p.scrollY += step {
		out = append(out, ev(tui.MouseWheel, tui.WheelUp))
	}
	for ; p.scrollX >= step; p.scrollX -= step {
		out = append(out, ev(tui.MouseWheel, tui.WheelRight))
	}
	for ; p.scrollX <= -step; p.scrollX += step {
		out = append(out, ev(tui.MouseWheel, tui.WheelLeft))
	}
	return out
}
