package gui

import (
	"github.com/yongjohnlee80/gio/io/pointer"
	"github.com/yongjohnlee80/golib/tui"
)

// Mouse and pointer event translation and sub-cell coordinate projection.
//
// Modern graphical window systems report mouse pointer coordinates as continuous,
// floating-point physical pixel positions. In contrast, terminal user interface (TUI)
// frameworks model layouts in discrete rows and columns of character cells.
//
// # Sub-Cell Coordinate Projection
//
// To provide terminal applications with pixel-level precision for drawing, dragging, and
// hit-testing without breaking the cell-based layout contract, [pointerState] translates
// raw pointer coordinates into hybrid cell coordinates:
//
//	Display Window:
//	┌─────────────────────────────────────────────────────────────┐
//	│ Pixel position (e.g. X: 142.4px, Y: 88.6px)                 │
//	│       │                                                     │
//	│       ▼ Metrics projection (scaled by font cell width/height)│
//	│  Cell Column X: 14, Row Y: 5                                │
//	│  Sub-cell offset SubX: 0.62, SubY: 0.85                     │
//	└─────────────────────────────────────────────────────────────┘
//
// This enables:
//   - Standard cell hit testing: Widgets query [tui.MouseEvent.X] and [tui.MouseEvent.Y]
//     to detect focus, item clicks, or selection boundaries.
//   - Fluid graphical interaction: Native views and drawing surfaces inspect
//     [tui.MouseEvent.SubX] and [tui.MouseEvent.SubY] to track smooth drawing curves,
//     sub-character cursor positioning, and slider thumb dragging.
//
// # Concurrency & Ownership
//
// [pointerState] is owned exclusively by the Gio window event goroutine inside [Backend.run].
// It tracks held buttons and accumulates fractional touchpad scroll deltas.

// pointerState tracks active pointer buttons and scroll accumulator state across frames.
type pointerState struct {
	held             pointer.Buttons
	scrollX, scrollY float32 // device pixels scrolled and not yet a wheel step
	// press is the pane a press landed in, while a button is held: a drag maps through it, past
	// its edge, so a captured drag's cells stay continuous across panes (a Panes window).
	press    paneMap
	pressing bool
}

// buttonOf maps a Gio pointer button bitflag to the corresponding [tui.MouseButton] identifier.
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

// translate converts a Gio [pointer.Event] into corresponding [tui.MouseEvent] instances.
//
// Translates pointer types:
//   - Press / Release: Resolves changed buttons via bitwise diff against held state.
//   - Move / Drag: Projects coordinates into cell (X, Y) and fractional offsets (SubX, SubY).
//   - Scroll: Accumulates fractional deltas and converts them into discrete wheel step events.
//   - Cancel: Clears all held buttons and issues release events.
func (p *pointerState) translate(e pointer.Event, m metrics) []tui.MouseEvent {
	if e.Kind == pointer.Press && !p.pressing {
		p.press, p.pressing = m.paneAt(e.Position.X, e.Position.Y), true
	}
	pane, x, y := m.paneAt(e.Position.X, e.Position.Y), 0, 0
	if p.pressing && m.layout != nil {
		pane = p.press
		x, y = m.cellIn(pane, e.Position.X, e.Position.Y, true)
	} else {
		x, y = m.cellIn(pane, e.Position.X, e.Position.Y, false)
	}
	sx, sy := m.subCellIn(pane, e.Position.X, e.Position.Y, x, y)
	defer func() {
		if p.held == 0 {
			p.pressing = false
		}
	}()
	mods := modsOf(e.Modifiers)
	ev := func(kind tui.MouseKind, b tui.MouseButton) tui.MouseEvent {
		return tui.MouseEvent{Kind: kind, Button: b, X: x, Y: y, SubX: sx, SubY: sy, Mods: mods}
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

// wheel converts raw floating-point scroll deltas into discrete [tui.MouseWheel] steps.
//
// Because smooth trackpads deliver high-frequency fractional scroll offsets, deltas
// are accumulated in scrollX and scrollY until crossing a step threshold (half a character
// cell height). This ensures both high-resolution trackpad gestures and traditional notched
// mouse wheels feel natural and responsive.
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
