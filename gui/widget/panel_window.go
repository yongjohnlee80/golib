package widget

import "github.com/yongjohnlee80/golib/tui"

// staticPanelTarget represents a no-op window target implementation for panels placed in
// static, non-floating layout contexts. In such layouts, move, resize, and maximize commands
// remain inert and safely return zero/false values.
type staticPanelTarget struct{}

// Bounds returns an empty bounding rectangle for static non-floating panels.
func (staticPanelTarget) Bounds() tui.Rect { return tui.Rect{} }

// RequestedBounds returns an empty requested bounding rectangle.
func (staticPanelTarget) RequestedBounds() tui.Rect { return tui.Rect{} }

// Area returns an empty layout constraint rectangle.
func (staticPanelTarget) Area() tui.Rect { return tui.Rect{} }

// SetBounds is a no-op in static layouts, returning false to indicate bounds cannot change.
func (staticPanelTarget) SetBounds(tui.Rect) bool { return false }

// Checkpoint returns a no-op restoration closure.
func (staticPanelTarget) Checkpoint() func() { return func() {} }

// ToggleMaximize is a no-op in static layouts, returning false.
func (staticPanelTarget) ToggleMaximize() bool { return false }

// Maximized reports false since static panels cannot be maximized.
func (staticPanelTarget) Maximized() bool { return false }
