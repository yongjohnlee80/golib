package widget_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// panel is a container of one child that places it as a window: a WindowTargetProvider that is
// not a Float. Its target records the bounds a move sets.
type panel struct {
	*tui.Flex
	target *panelTarget
}

type panelTarget struct{ bounds tui.Rect }

func (t *panelTarget) Bounds() tui.Rect          { return t.bounds }
func (t *panelTarget) RequestedBounds() tui.Rect { return t.bounds }
func (t *panelTarget) Area() tui.Rect            { return tui.Rect{W: 40, H: 10} }
func (t *panelTarget) SetBounds(r tui.Rect) bool { t.bounds = r; return true }
func (t *panelTarget) Checkpoint() func()        { b := t.bounds; return func() { t.bounds = b } }

func (p *panel) WindowTargetFor(*widget.WindowCore, *tui.Context) (any, func(tui.Point) tui.Point) {
	return p.target, nil
}

// A WindowMod with no target of its own places itself through the nearest ancestor that is a
// WindowTargetProvider, whatever its type: a move inside a container that is not a Float reaches
// that container's target. Before, only a *Float was looked for, and the WindowMod had none.
func TestAWindowModPlacesThroughAnyWindowTargetProvider(t *testing.T) {
	w := widget.NewWindowMod(widget.NewText("x"), widget.WithWindowMove())
	p := &panel{Flex: tui.NewFlex(tui.Vertical), target: &panelTarget{bounds: tui.Rect{X: 2, Y: 1, W: 10, H: 4}}}
	p.Add(w)
	h := startApp(t, p, 40, 10)
	var moved bool
	var got tui.Rect
	h.onLoop(func() { moved = w.MoveBy(3, 2); got = p.target.bounds })
	if !moved || got != (tui.Rect{X: 5, Y: 3, W: 10, H: 4}) {
		t.Errorf("MoveBy(3, 2) = %v with the panel's target at %+v; want true, moved to {5 3 10 4}", moved, got)
	}
}
