package tui

import (
	"strconv"
	"strings"
	"time"

	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/tui/internal/grapheme"
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

// SetMinimumSize sets the smallest screen, in cells, the App lays out for (see WithMinimumSize).
// A zero dimension sets no minimum on that axis. On the loop goroutine: a handler or an Update.
func (a *App) SetMinimumSize(s Size) {
	s = Size{W: max(s.W, 0), H: max(s.H, 0)}
	if s == a.minSize {
		return
	}
	a.minSize = s
	a.layoutDirty, a.renderDirty = true, true
	a.queue.wakeUp()
}

// MinimumSize is the smallest screen the App lays out for; zero on an axis without one.
func (a *App) MinimumSize() Size { return a.minSize }

// tooSmall reports whether the screen is under the minimum on either axis.
func (a *App) tooSmall() bool {
	return (a.minSize.W > 0 && a.size.W < a.minSize.W) || (a.minSize.H > 0 && a.size.H < a.minSize.H)
}

// dispatchTooSmall handles an event while the too-small screen shows. It reports true for input,
// which the screen takes whatever it is: q, Ctrl+C and a click on Quit quit, and nothing else
// reaches the application.
func (a *App) dispatchTooSmall(ev Event) bool {
	switch e := ev.(type) {
	case KeyEvent:
		if e.Kind != KeyRelease && isQuitKey(e) {
			a.quitTooSmall()
		}
		return true
	case MouseEvent:
		if e.Kind == MousePress && e.Button == MouseLeft && a.quitButton.Contains(e.X, e.Y) {
			a.quitTooSmall()
		}
		return true
	case PasteEvent:
		return true
	}
	return false
}

// isQuitKey is q, or Ctrl+C, with no other modifier held.
func isQuitKey(e KeyEvent) bool {
	switch mods := e.Mods.Chord(); {
	case mods == 0:
		return e.Code == 'q'
	case mods == ModCtrl:
		return e.Code == 'c'
	}
	return false
}

// quitTooSmall ends the program the way WithQuit says, or ends Run.
func (a *App) quitTooSmall() {
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

	for i := range a.buf.curr {
		a.buf.curr[i] = blankCell
	}
	s := newRootSurface(a.buf, a.rctx)
	s.Fill(Rect{W: a.size.W, H: a.size.H}, " ", style.New().Background(style.TokenBackground))

	need := a.Translate(Msg("tui.tooSmall.size"))
	for i, v := range []int{a.size.W, a.size.H, a.minSize.W, a.minSize.H} {
		need = strings.ReplaceAll(need, "%"+strconv.Itoa(i+1), strconv.Itoa(v))
	}
	quit := "[ " + a.Translate(Msg("tui.tooSmall.quit")) + " ]"
	keys := a.Translate(Msg("tui.tooSmall.quitKeys"))
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
	x = writeAt(s, x, y, quit, base.Background(style.TokenPrimary).Foreground(style.TokenTextOnPrimary))
	a.quitButton = Rect{X: max((a.size.W-total)/2, 0), Y: y, W: s.StringWidth(quit), H: 1}
	writeAt(s, x+gap, y, keys, base.Foreground(style.TokenTextMuted))

	a.renderDirty = false
	a.beginNatives()
	a.applyNatives() // nothing painted natively: the host removes every view
	a.frameImages = a.frameImages[:0]
	a.applyImages() // nothing reported: every placed image is deleted
	a.backend.HideCursor()
	if err := a.backend.Flush(a.buf.diff()); err != nil {
		logger.Error(a.cfg.logger, err, map[string]any{"tui": "backend flush failed"})
	}
	a.lastFrame = time.Now()
	a.frames++
}

// writeCentered writes text centred on row y.
func writeCentered(s Surface, y int, text string, st style.Style) {
	writeAt(s, max((s.Size().W-s.StringWidth(text))/2, 0), y, text, st)
}

// writeAt writes text from column x on row y and returns the column after it. What does not fit is
// clipped by the surface.
func writeAt(s Surface, x, y int, text string, st style.Style) int {
	for c := range grapheme.Clusters(text) {
		s.SetCell(x, y, c, st)
		x += max(s.StringWidth(c), 1)
	}
	return x
}
