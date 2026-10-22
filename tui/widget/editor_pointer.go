package widget

import (
	"time"

	"github.com/yongjohnlee80/golib/tui"
)

// THE EDITOR'S POINTER: a press, a drag with its auto-scroll at the edges, the release, and the
// context menu, by the right button or the key map's menu key. Both golib editors route them
// through an EditorPointer, each with its own view's mapping (PointerView): the cell Editor's in
// cells, gui's in pixels. The wheel is each view's own.

// PointerView is what an EditorPointer reads of the view it works for. An editor implements it on
// a value of its own, so its methods do not join the editor's API.
type PointerView interface {
	// Context is the component's: the pointer is captured, and the auto-scroll ticks, through it.
	// nil before the mount.
	Context() *tui.Context
	// PointerAt is the buffer position under the pointer, within the view: past an edge, the
	// position at that edge.
	PointerAt(m tui.MouseEvent) (ln, col int)
	// PointerEdge is the edge the pointer is past, each way: -1 above or left of the text, +1
	// below or right of it, 0 within.
	PointerEdge(m tui.MouseEvent) (dx, dy int)
	// ScrollStep scrolls the view one auto-scroll step toward an edge, dx and dy as PointerEdge
	// gives them.
	ScrollStep(dx, dy int)
	// CaretCell is the caret's cell in the component, where the menu key opens the context menu;
	// false when the caret is not on the screen.
	CaretCell() (x, y int, ok bool)
}

// PointerInert is a PointerView with places a press does nothing at, and is consumed: the cell
// Editor's scroll-indicator column, which is not text.
type PointerInert interface {
	PointerInert(m tui.MouseEvent) bool
}

// EditorPointer is an editor's pointer over its core:
//
//   - a left press places the caret (EditorCore.PressAt), a command boundary, and captures the
//     pointer for a drag selection;
//   - a drag extends the selection; past an edge the view scrolls a step, and keeps scrolling
//     while the pointer stays there, as a desktop editor does;
//   - the release, or the capture lost, ends the drag, the selection staying;
//   - the right button opens the context menu at the pointer, and the key map's menu key
//     (EditorContextMenu) at the caret, the selection left for its Copy and Cut.
type EditorPointer struct {
	core   *EditorCore
	menu   *EditorMenu
	view   PointerView
	last   tui.MouseEvent // the drag's last motion: where a tick extends it to
	dx, dy int            // the edge the dragging pointer is past; 0, 0: within
	stop   func()         // stops the auto-scroll's ticks; nil while none run
}

// NewEditorPointer is an EditorPointer over core, opening menu (nil: none), for view.
func NewEditorPointer(core *EditorCore, menu *EditorMenu, view PointerView) *EditorPointer {
	return &EditorPointer{core: core, menu: menu, view: view}
}

// menuOn reports whether there is a context menu, turned on.
func (p *EditorPointer) menuOn() bool { return p.menu != nil && p.menu.Enabled() }

// dragScrollInterval is the auto-scroll's pace: a step per tick while the pointer is past an edge.
const dragScrollInterval = 50 * time.Millisecond

// HandleEvent handles ev when it is the pointer's: a press, drag or release, the capture lost, or
// an auto-scroll tick. Anything else (the wheel, a key, a chord's tick) is false, for the editor's
// own routing; the editor asks MenuKey of a key first.
func (p *EditorPointer) HandleEvent(ev tui.Event) bool {
	switch t := ev.(type) {
	case tui.MouseEvent:
		return p.mouse(t)
	case tui.PointerCaptureLostEvent:
		p.core.EndDrag() // the selection made so far stays
		p.setEdge(0, 0)
		return true
	case tui.TickEvent:
		if !p.core.Dragging() || p.dx == 0 && p.dy == 0 {
			return false // the chord timeout's
		}
		p.view.ScrollStep(p.dx, p.dy) // a press settled any pending rune
		p.extend(p.last)
		return true
	}
	return false
}

// MenuKey reports whether k is the key map's menu key (EditorContextMenu) with the menu on: the
// editor then opens the menu (OpenMenu) and, when it cannot, lets the key bubble. The core never
// sees the key either way, so it is not recorded into a macro or '.', and a pending operator and
// count survive it. A repeat is not the menu key: it opens no second menu.
func (p *EditorPointer) MenuKey(k tui.KeyEvent) bool {
	ctx := p.view.Context()
	if !p.menuOn() || k.Kind != tui.KeyPress || ctx == nil {
		return false
	}
	_, ok := ctx.KeyCommand(k, EditorContextMenu)
	return ok
}

// OpenMenu opens the context menu at the caret's cell, as the menu key does. False when the menu
// is off or the caret is not on the screen.
func (p *EditorPointer) OpenMenu() bool {
	if !p.menuOn() {
		return false
	}
	x, y, ok := p.view.CaretCell()
	if !ok {
		return false
	}
	return p.menu.OpenAt(tui.Point{X: x, Y: y})
}

// mouse handles the press, the right button, the drag and the release.
func (p *EditorPointer) mouse(m tui.MouseEvent) bool {
	switch {
	case m.Kind == tui.MousePress && m.Button == tui.MouseLeft:
		if in, ok := p.view.(PointerInert); ok && in.PointerInert(m) {
			return true // not text, but the widget's
		}
		handled := p.core.PressAt(p.view.PointerAt(m))
		if ctx := p.view.Context(); ctx != nil && p.core.Dragging() {
			ctx.CapturePointer()
		}
		return handled
	case m.Kind == tui.MousePress && m.Button == tui.MouseRight && p.menuOn():
		return p.menu.OpenAt(tui.Point{X: m.X, Y: m.Y})
	case m.Kind == tui.MouseMotion && m.Button == tui.MouseLeft && p.core.Dragging():
		p.last = m
		dx, dy := p.view.PointerEdge(m)
		p.setEdge(dx, dy)
		if dx != 0 || dy != 0 {
			p.view.ScrollStep(dx, dy)
		}
		p.extend(m)
		return true
	case m.Kind == tui.MouseRelease && p.core.Dragging():
		p.core.EndDrag()
		p.setEdge(0, 0)
		if ctx := p.view.Context(); ctx != nil && ctx.HasPointerCapture() {
			ctx.ReleasePointer()
		}
		return true
	}
	return false
}

// extend moves the selection's moving end to the position under m.
func (p *EditorPointer) extend(m tui.MouseEvent) { p.core.DragTo(p.view.PointerAt(m)) }

// setEdge starts the auto-scroll's ticks as the pointer leaves the view, and stops them as it
// comes back or the drag ends.
func (p *EditorPointer) setEdge(dx, dy int) {
	p.dx, p.dy = dx, dy
	switch past := dx != 0 || dy != 0; {
	case past && p.stop == nil:
		if ctx := p.view.Context(); ctx != nil {
			p.stop = ctx.Every(dragScrollInterval)
		}
	case !past && p.stop != nil:
		p.stop()
		p.stop = nil
	}
}

// editorPointerView is the cell Editor's PointerView, in cells.
type editorPointerView struct{ e *Editor }

func (v editorPointerView) Context() *tui.Context { return v.e.Context() }

// PointerAt is the cell's position: a press in the gutter at the line's start, a row past the
// top or bottom at that row.
func (v editorPointerView) PointerAt(m tui.MouseEvent) (int, int) {
	l := &v.e.cells
	return v.e.posAt(max(m.X-l.gutter, 0), min(max(m.Y, 0), max(l.h-1, 0)))
}

// PointerEdge is above or below the view; the cell Editor scrolls only by lines.
func (v editorPointerView) PointerEdge(m tui.MouseEvent) (int, int) {
	switch l := &v.e.cells; {
	case m.Y < 0:
		return 0, -1
	case l.h > 0 && m.Y >= l.h:
		return 0, 1
	}
	return 0, 0
}

func (v editorPointerView) ScrollStep(_, dy int) { v.e.cells.ScrollLines(dy) }

func (v editorPointerView) CaretCell() (int, int, bool) { return v.e.Cursor() }

// PointerInert is the scroll-indicator column: not text.
func (v editorPointerView) PointerInert(m tui.MouseEvent) bool {
	return v.e.scrollable() && max(m.X-v.e.cells.gutter, 0) >= v.e.wrapWidth()
}

// wheel scrolls by whole LOGICAL lines in both wrap modes, as the keys scroll (cellLayout.ScrollLines),
// and consumes the event. The wheel scrolls the viewport and never moves the caret, so a reader
// can scroll while a caret stays where they left it.
//
// One MouseEvent is one step: the event carries a wheel direction and no
// magnitude, so N notches arrive as N events and this never multiplies. Visual
// rows are deliberately not the unit — the viewport stores a logical-line origin
// only (top/left), so an intra-line offset is not representable without new
// state and new invariants across render, Cursor, ensureVisible, click inversion
// and clamping. That is deferred to its own ADR.
func (e *Editor) wheel(m tui.MouseEvent) bool {
	switch {
	case m.Kind == tui.MouseWheel && m.Button == tui.WheelUp:
		e.cells.ScrollLines(-1)
	case m.Kind == tui.MouseWheel && m.Button == tui.WheelDown:
		e.cells.ScrollLines(1)
	default:
		return false
	}
	return true
}
