package tui

// SetGestureRecognizer is [App.SetGestureRecognizer]: the recogniser is the App's, one for the
// whole tree.
//
// Deprecated: use c.App().SetGestureRecognizer. Not maintained; removed in the next minor version.
func (c *Context) SetGestureRecognizer(r GestureRecognizer) { c.app.SetGestureRecognizer(r) }
