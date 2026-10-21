package tui

import (
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/tui/key"
	"github.com/yongjohnlee80/golib/tui/style"
)

// THE TOO-SMALL SCREEN.
//
// An App given a minimum size (WithMinimumSize, SetMinimumSize) lays nothing out on a smaller
// screen. It shows this instead, as btop does: the size the screen has and the size it needs, a
// request to enlarge it, and a Quit button with its keys (q, Ctrl+C). The program is not ended
// for being small: the user resizes, and the next frame lays the application out again.
//
// While it shows, input is the screen's alone: keys, the pointer and paste are not delivered.
// Everything else carries on — the program lane, timers, tasks, resize and focus events — so the
// application's state is current when the screen is large enough again.

// tooSmallState is the smallest screen the App lays out for (WithMinimumSize, SetMinimumSize), and
// quitButton the too-small screen's Quit, in screen cells, while it shows.
type tooSmallState struct {
	minSize    Size
	quitButton Rect
}

// SetMinimumSize sets the smallest screen, in cells, the App lays out for (see WithMinimumSize).
// A zero dimension sets no minimum on that axis. On the loop goroutine: a handler or an Update.
func (a *App) SetMinimumSize(s Size) {
	s = Size{W: max(s.W, 0), H: max(s.H, 0)}
	if s == a.minSize {
		return
	}
	a.minSize = s
	a.invalidate(true)
}

// MinimumSize is the smallest screen the App lays out for; zero on an axis without one.
func (a *App) MinimumSize() Size { return a.minSize }

// tooSmall reports whether the screen is under the minimum on either axis.
func (a *App) tooSmall() bool {
	return (a.minSize.W > 0 && a.size.W < a.minSize.W) || (a.minSize.H > 0 && a.size.H < a.minSize.H)
}

// dispatchTooSmall handles an event while the too-small screen shows. It reports true for input,
// which the screen takes whatever it is: TooSmallQuit's keys (q and Ctrl+C unless the key map
// rebinds them) and a click on Quit quit, and nothing else reaches the application.
func (a *App) dispatchTooSmall(ev Event) bool {
	switch e := ev.(type) {
	case KeyEvent:
		// The exact chord only, not keyCommand's legacy Ctrl+Shift fallback: Ctrl+Shift+C is a
		// terminal's copy, and must not quit.
		if chord, ok := key.Of(e.Code, e.Mods, e.Text); ok && e.Kind != KeyRelease && a.cfg.keymap.Has(TooSmallQuit, chord) {
			a.requestQuit()
		}
		return true
	case MouseEvent:
		if e.Kind == MousePress && e.Button == MouseLeft && a.quitButton.Contains(e.X, e.Y) {
			a.requestQuit()
		}
		return true
	case PasteEvent:
		return true
	}
	return false
}

// requestQuit ends the program the way WithQuit says, or ends Run: the too-small screen's Quit and
// a window's close request.
func (a *App) requestQuit() {
	if a.cfg.quit != nil {
		a.cfg.quit()
		return
	}
	if a.stopRun != nil {
		a.stopRun()
	}
}

// renderTooSmall paints the too-small screen as the frame: the whole screen in the theme's
// background, the notice centred, no cursor and no images.
func (a *App) renderTooSmall() {
	// A gesture in progress has nothing on screen to continue on.
	a.loseCapture(CaptureLostHidden)

	a.beginFrame()
	s := newRootSurface(a.buf, a.rctx)
	s.Fill(Rect{W: a.size.W, H: a.size.H}, " ", style.New().Background(style.TokenBackground))

	need := a.Translate(Msg("tui.tooSmall.size"))
	for i, v := range []int{a.size.W, a.size.H, a.minSize.W, a.minSize.H} {
		need = strings.ReplaceAll(need, "%"+strconv.Itoa(i+1), strconv.Itoa(v))
	}
	quit := "[ " + a.Translate(Msg("tui.tooSmall.quit")) + " ]"
	keys := strings.ReplaceAll(a.Translate(Msg("tui.tooSmall.quitKeys")), "%1", a.tooSmallQuitKeys())
	type line struct {
		text string
		st   style.Style
	}
	base := style.New().Background(style.TokenBackground)
	lines := []line{
		{a.Translate(Msg("tui.tooSmall.title")), base.Foreground(style.TokenWarning).Bold(true)},
		{need, base.Foreground(style.TokenForeground)},
		{a.Translate(Msg("tui.tooSmall.enlarge")), base.Foreground(style.TokenTextMuted)},
		{"", base},
	}
	y := max((a.size.H-len(lines)-1)/2, 0)
	for _, l := range lines {
		writeCentered(s, y, l.text, l.st)
		y++
	}
	// The Quit button, then its keys, centred together on one row.
	gap := 2
	total := s.StringWidth(quit) + gap + s.StringWidth(keys)
	x := max((a.size.W-total)/2, 0)
	x = DrawText(s, x, y, quit, base.Background(style.TokenPrimary).Foreground(style.TokenTextOnPrimary))
	a.quitButton = Rect{X: max((a.size.W-total)/2, 0), Y: y, W: s.StringWidth(quit), H: 1}
	DrawText(s, x+gap, y, keys, base.Foreground(style.TokenTextMuted))

	a.presentFrame(false) // nothing reported: every native view and placed image goes
}

// writeCentered writes text centred on row y.
func writeCentered(s Surface, y int, text string, st style.Style) {
	DrawText(s, max((s.Size().W-s.StringWidth(text))/2, 0), y, text, st)
}

// tooSmallQuitKeys is TooSmallQuit's keys as the key map binds them, as a menu prints an
// accelerator, so the screen names the keys that quit it.
func (a *App) tooSmallQuitKeys() string {
	var names []string
	for _, s := range a.cfg.keymap.KeysOf(TooSmallQuit) {
		names = append(names, s.String())
	}
	return strings.Join(names, " / ")
}
