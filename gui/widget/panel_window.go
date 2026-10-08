package widget

import "github.com/yongjohnlee80/golib/tui"

// The legacy Panel options advertise operations in ordinary layouts too, where
// they have always been inert. New capability options require a real target.
type staticPanelTarget struct{}

func (staticPanelTarget) Bounds() tui.Rect          { return tui.Rect{} }
func (staticPanelTarget) RequestedBounds() tui.Rect { return tui.Rect{} }
func (staticPanelTarget) Area() tui.Rect            { return tui.Rect{} }
func (staticPanelTarget) SetBounds(tui.Rect) bool   { return false }
func (staticPanelTarget) Checkpoint() func()        { return func() {} }
func (staticPanelTarget) ToggleMaximize() bool      { return false }
func (staticPanelTarget) Maximized() bool           { return false }
