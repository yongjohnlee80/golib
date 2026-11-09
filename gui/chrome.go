package gui

import (
	"github.com/yongjohnlee80/gio/io/system"

	"github.com/yongjohnlee80/golib/tui"
)

// WindowControls are the controls of the desktop window an App runs in: what a title bar drawn by
// the app itself needs. Open the window with WithDecorations(false) to draw one; mark its drag
// area with Canvas.WindowMoveArea.
type WindowControls interface {
	// Perform asks the window to minimize, maximize, unmaximize, go fullscreen or close (the
	// actions may be combined). Close asks the program first, as the system's close button does:
	// the App gets a tui.CloseRequestEvent and may keep the window open.
	Perform(a system.Action)
	// Maximized reports whether the window is maximized, as the platform last said.
	Maximized() bool
}

// Window returns the window controls when app runs on a GUI backend; ok is false on a terminal,
// where a title bar takes its fallback.
func Window(app *tui.App) (w WindowControls, ok bool) {
	if app == nil {
		return nil, false
	}
	b, _ := app.Backend().(*Backend) // another backend's assertion fails to nil
	if b == nil {
		return nil, false
	}
	return windowControls{b}, true
}

// windowControls is a Backend's WindowControls.
type windowControls struct{ b *Backend }

// Perform hands a to the window. A close comes back as the window's ClosingEvent, which asks the
// program (askToClose) exactly as the system's own close button does.
func (w windowControls) Perform(a system.Action) { w.b.perform(a) }

func (w windowControls) Maximized() bool { return w.b.maxed.Load() }

// perform hands a to the window; the window's methods may be called from any goroutine.
func (b *Backend) perform(a system.Action) {
	if b.performHook != nil {
		b.performHook(a)
		return
	}
	if b.win != nil {
		b.win.Perform(a)
	}
}
