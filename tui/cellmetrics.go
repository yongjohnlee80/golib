package tui

// CellPixels is one cell's size in logical (device-independent) pixels. Zero when the backend
// does not know it, as a terminal backend usually does not.
type CellPixels struct{ W, H float32 }

// CellMetrics is an optional Backend capability: the cell's size in pixels, read live. Unlike
// Capabilities it may change while the App runs (a font zoom), and a backend changes it only
// together with a ResizeEvent, so the App lays out and paints again with the new size.
type CellMetrics interface {
	CellPixels() CellPixels
}

// CellPixels is the backend's cell size in logical pixels, or zero. Read it in Layout, Render or
// HandleEvent: it is current for the frame being built.
func (a *App) CellPixels() CellPixels {
	if m, ok := a.backend.(CellMetrics); ok {
		return m.CellPixels()
	}
	return CellPixels{}
}

// CellPixels is the App's CellPixels.
func (c *Context) CellPixels() CellPixels { return c.app.CellPixels() }

// Capabilities is the backend's, as App.Capabilities: what a widget may lay out for, such as
// NativeViews.
func (c *Context) Capabilities() Capabilities { return c.app.Capabilities() }
