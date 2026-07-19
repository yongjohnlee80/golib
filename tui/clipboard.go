package tui

// ClipboardWriter is an OPTIONAL Backend capability: pushing text into the
// host terminal's system clipboard (OSC 52 on ANSI terminals). Backends that
// don't implement it simply don't support copy; callers must treat a false
// return from CopyToClipboard as "not supported / not delivered".
//
// OSC 52 is the only clipboard mechanism that works everywhere a terminal
// does — including over SSH and inside terminal multiplexers and editor
// :terminal windows — because the TERMINAL performs the copy, not the app.
type ClipboardWriter interface {
	// WriteClipboard sets the system clipboard to p (UTF-8 text).
	WriteClipboard(p []byte) error
}

// CopyToClipboard pushes s into the system clipboard via the backend's
// ClipboardWriter capability. Returns false when the backend doesn't support
// clipboard writes or the write failed. Loop goroutine only.
func (a *App) CopyToClipboard(s string) bool {
	cw, ok := a.backend.(ClipboardWriter)
	if !ok {
		return false
	}
	return cw.WriteClipboard([]byte(s)) == nil
}

// CopyToClipboard pushes s into the system clipboard (see App.CopyToClipboard).
func (c *Context) CopyToClipboard(s string) bool { return c.app.CopyToClipboard(s) }
