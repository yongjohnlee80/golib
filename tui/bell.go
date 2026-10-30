package tui

// Beller is an OPTIONAL Backend capability: sounding the terminal's bell (BEL on ANSI terminals),
// which the terminal turns into the alert its user chose: a sound, a flash, an urgency hint. It is
// written on its own, never inside a frame's write. A backend without it has no bell, and
// App.Bell answers false.
type Beller interface {
	// Bell sounds the bell once.
	Bell() error
}

// Bell sounds the backend's bell (Beller). It returns false when the backend has none or the write
// failed. Loop goroutine only.
func (a *App) Bell() bool {
	b := a.caps.bell
	return b != nil && b.Bell() == nil
}

// Bell sounds the bell (see App.Bell).
func (c *Context) Bell() bool { return c.app.Bell() }
