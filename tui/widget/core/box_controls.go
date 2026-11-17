package core

import (
	"slices"

	"github.com/yongjohnlee80/golib/tui"
)

// A Box's window controls: _, + and x at its title row's right end, as a tab strip ends its row in
// them (TabStrip.SetWindowButtons). They request and never act: a press on one runs the Box's
// SetOnWindowControl function with its WindowOperation, and the owner decides what it does.

// boxControlCells is a control's width, and the pitch they sit at side by side: the tab strip's
// (stripControlCells), so a painter's dots are as far apart as on a strip and the title bar.
const boxControlCells = stripControlCells

// boxControlsKey is the controls' column commit (tui.Context.AfterLayout).
const boxControlsKey tui.CommitKey = "widget.box.controls"

// SetWindowControls shows ops at the title row's right end, drawn left to right in the order given
// (the title bar's order is WindowMinimize, WindowMaximize, WindowClose), or none for an empty
// list: the default. A control is " _ ", " + " or " x ", three cells, in its colour (close an
// error's, maximize a success's, minimize a warning's), with a border cell between the last and
// the corner. The title slot (SetTitleSlot) and the title keep to the room before them. A box with
// no top border, or too narrow for them (inner width under their cells + 4), hides them.
func (x *Box) SetWindowControls(ops []WindowOperation) {
	if slices.Equal(ops, x.ctlOps) {
		return
	}
	x.ctlOps = slices.Clone(ops)
	x.RequestLayout()
}

// SetOnWindowControl is what a primary press on a window control does (SetWindowControls): fn with
// the control's WindowOperation, on the press. nil does nothing, but the press is still the
// control's. Every press is one request: a double press is two, as on a tab strip's controls. The
// release goes where any release goes, routed as usual: the press took no capture, and a control
// under the pointer that needs the press it never saw (a Button, which arms on its press) does not
// act. A press outside an active focus trap (a dialog open over the
// box's window) is dropped before it reaches the box, so it asks nothing.
//
// A press on a control is always the control's. The Box claims it as a tui.PointerPressResolver,
// the nearest on the press's path, so neither a window's modified drag (a WindowMod's resolver,
// Alt by default) nor its plain drag on the frame (its unclaimed press) starts there.
func (x *Box) SetOnWindowControl(fn func(WindowOperation)) { x.onControl = fn }

// TitleWindowControls are the window controls as drawn now, for a native painter: each one's
// columns in the box and its operation, on the title row (TitleRow). None while they are hidden,
// and none before the box's first layout: the columns are the last layout's. Loop goroutine only,
// as the tab strip's TabWindowControls.
func (x *Box) TitleWindowControls() []TabControl {
	if len(x.ctlOps) == 0 || x.ctlX < 0 {
		return nil
	}
	out := make([]TabControl, len(x.ctlOps))
	for i, op := range x.ctlOps {
		out[i] = TabControl{X: x.ctlX + boxControlCells*i, W: boxControlCells, Op: op}
	}
	return out
}

// TitleRow is the row of the box its title and window controls are on: its top margin. Loop
// goroutine only.
func (x *Box) TitleRow() int {
	mt, _, _, _ := x.base.GetMargin()
	return mt
}

// controlsWidth is the window controls' cells, without the border cells either side of them.
func (x *Box) controlsWidth() int { return boxControlCells * len(x.ctlOps) }

// placeControls lays the window controls out on the top border row of a box w columns wide: the
// column the first starts at (committed after layout, for Render and TitleWindowControls), or -1
// while they don't fit. It returns the columns they take from the title row, the border cell
// before them included, 0 when hidden.
func (x *Box) placeControls(w, inL, inR int) int {
	if len(x.ctlOps) == 0 {
		x.ctx.AfterLayout(boxControlsKey, func() { x.ctlX = -1 })
		return 0
	}
	_, mr, _, ml := x.base.GetMargin()
	bt, br, _, bl := borderEdgeSizes(x.base)
	bx, bw := ml+inL, w-ml-mr-inL-inR
	inner := bw - bl - br
	cw := x.controlsWidth()
	if bt != 1 || inner < cw+4 {
		x.ctx.AfterLayout(boxControlsKey, func() { x.ctlX = -1 })
		return 0
	}
	at := bx + bl + inner - 1 - cw // a border cell between the last and the corner
	x.ctx.AfterLayout(boxControlsKey, func() { x.ctlX = at })
	return cw + 1
}

// controlAt is the window control at the box's (col, row), if any.
func (x *Box) controlAt(col, row int) (WindowOperation, bool) {
	if len(x.ctlOps) == 0 || x.ctlX < 0 || row != x.TitleRow() || col < x.ctlX || col >= x.ctlX+x.controlsWidth() {
		return 0, false
	}
	return x.ctlOps[(col-x.ctlX)/boxControlCells], true
}

// boxControlAction is a press on a Box's window control, resolved before the press is delivered.
type boxControlAction struct{ Op WindowOperation }

// ActionID implements tui.Action.
func (boxControlAction) ActionID() tui.ActionID { return "box.window.control" }

// ResolvePointerPress claims a primary press on a window control (tui.PointerPressResolver), with
// any modifiers, so no window drag starts there. Any other press is left as it was.
func (x *Box) ResolvePointerPress(e tui.MouseEvent) (tui.Action, bool) {
	if e.Kind != tui.MousePress || e.Button != tui.MouseLeft {
		return nil, false
	}
	op, ok := x.controlAt(e.X, e.Y)
	if !ok {
		return nil, false
	}
	return boxControlAction{Op: op}, true
}

// HandleAction runs a claimed press on a window control (SetOnWindowControl's function).
func (x *Box) HandleAction(inv tui.ActionInvocation) bool {
	a, ok := inv.Action.(boxControlAction)
	if !ok {
		return false
	}
	if x.onControl != nil {
		x.onControl(a.Op)
	}
	return true
}
