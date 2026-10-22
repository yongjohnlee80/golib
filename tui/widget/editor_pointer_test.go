package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// sidewaysView is a one-row PointerView w cells wide that scrolls sideways, as gui's pixel view
// does: what an EditorPointer reads of a view that is not the cell Editor's.
type sidewaysView struct {
	w, left int
	steps   int // ScrollStep calls
}

func (v *sidewaysView) Context() *tui.Context { return nil }

func (v *sidewaysView) PointerAt(m tui.MouseEvent) (int, int) {
	return 0, v.left + min(max(m.X, 0), v.w-1)
}

func (v *sidewaysView) PointerEdge(m tui.MouseEvent) (int, int) {
	switch {
	case m.X < 0:
		return -1, 0
	case m.X >= v.w:
		return 1, 0
	}
	return 0, 0
}

func (v *sidewaysView) ScrollStep(dx, _ int) { v.left += dx; v.steps++ }

func (v *sidewaysView) CaretCell() (int, int, bool) { return 0, 0, true }

// A drag past a view's edge scrolls it a step, and each tick while the pointer stays there scrolls
// another and extends the selection; back inside, or released, the ticks are the chord timer's
// again. With no menu, the right button and the menu key are not the pointer's.
func TestAnEditorPointerAutoScrollsAnyView(t *testing.T) {
	core := widget.NewEditorCore(widget.CoreInitialText("abcdefghijklmnop"))
	v := &sidewaysView{w: 5}
	p := widget.NewEditorPointer(core, nil, v)
	at := func(kind tui.MouseKind, x int) tui.MouseEvent {
		return tui.MouseEvent{Kind: kind, Button: tui.MouseLeft, X: x}
	}
	caret := func() int { _, col := core.Line(); return col }

	if !p.HandleEvent(at(tui.MousePress, 0)) || !core.Dragging() {
		t.Fatal("a left press did not start a drag")
	}
	p.HandleEvent(at(tui.MouseMotion, 7)) // past the right edge
	if v.steps != 1 || caret() != 5 {
		t.Fatalf("past the edge: %d steps, caret at %d; want 1 step, the caret at the edge's column 5", v.steps, caret())
	}
	if !p.HandleEvent(tui.TickEvent{}) || v.steps != 2 || caret() != 6 {
		t.Fatalf("a tick past the edge: %d steps, caret at %d; want 2, 6", v.steps, caret())
	}
	p.HandleEvent(at(tui.MouseMotion, 2)) // back inside
	if p.HandleEvent(tui.TickEvent{}) || v.steps != 2 {
		t.Errorf("a tick inside the view was the pointer's (%d steps)", v.steps)
	}
	p.HandleEvent(at(tui.MouseRelease, 2))
	if core.Dragging() || p.HandleEvent(tui.TickEvent{}) {
		t.Error("the release did not end the drag")
	}
	right := tui.MouseEvent{Kind: tui.MousePress, Button: tui.MouseRight}
	if p.HandleEvent(right) || p.OpenMenu() || p.MenuKey(tui.KeyEvent{Code: tui.KeyF10, Mods: tui.ModShift, Kind: tui.KeyPress}) {
		t.Error("with no menu, the right button or the menu key was the pointer's")
	}
}
