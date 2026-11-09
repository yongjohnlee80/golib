package gui

import (
	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/tui/style"
)

// TUIMock is a GUI widget that can draw a useful approximation of itself in cells: what a terminal
// shows of it.
type TUIMock interface {
	RenderTUI(s tui.Surface)
}

// msgNotInTerminal is the notice a GUI widget without a mock shows on a terminal.
var msgNotInTerminal = tui.Msg("tui.gui.notInTerminal")

// Base gives a GUI widget its NativeView and its terminal fallback. Embed it, Bind the widget's
// View, and call RenderFallback from the widget's Render:
//
//	type Shape struct {
//		gui.Base
//		ctx *tui.Context
//	}
//
//	func NewShape() *Shape { s := &Shape{}; s.Bind(s); return s }
//	func (s *Shape) Render(surf tui.Surface) { s.RenderFallback(s.ctx, surf) }
//	func (s *Shape) Paint(c gui.Canvas) { … }
//
// On a GUI backend the view is painted over the widget's cells and Render draws nothing. On a
// terminal, a View that implements TUIMock draws its mock; one that doesn't (or a widget set to
// SetNotice) shows a centred, muted, localised notice.
type Base struct {
	self   View
	notice bool
}

// Bind makes v the widget's pixel side: what NativeView returns, and what a terminal mocks when v
// implements TUIMock.
func (b *Base) Bind(v View) { b.self = v }

// SetNotice makes a terminal show the notice even when the widget has a mock.
func (b *Base) SetNotice(on bool) { b.notice = on }

// NativeView is the bound View; false before Bind.
func (b *Base) NativeView() (any, bool) {
	if b.self == nil {
		return nil, false
	}
	return b.self, true
}

// RenderFallback draws the widget's terminal fallback on s: nothing when the backend paints native
// views, else the mock or the notice. ctx is the widget's Context, for the notice's language.
func (b *Base) RenderFallback(ctx *tui.Context, s tui.Surface) {
	if s.Caps().NativeViews {
		return // the backend paints the view
	}
	if m, ok := b.self.(TUIMock); ok && !b.notice {
		m.RenderTUI(s)
		return
	}
	text := msgNotInTerminal.ID
	if ctx != nil {
		text = ctx.Translate(msgNotInTerminal)
	}
	paintNotice(s, text)
}

// paintNotice centres text on s in the theme's muted colour, cut to s's width.
func paintNotice(s tui.Surface, text string) {
	sz := s.Size()
	if sz.W <= 0 || sz.H <= 0 {
		return
	}
	st := style.New().Foreground(style.TokenTextMuted).Faint(true)
	var cells []string
	w := 0
	for g := range tui.Graphemes(text) {
		gw := max(s.StringWidth(g), 1)
		if w+gw > sz.W {
			break
		}
		cells = append(cells, g)
		w += gw
	}
	x, y := (sz.W-w)/2, (sz.H-1)/2
	for _, g := range cells {
		s.SetCell(x, y, g, st)
		x += max(s.StringWidth(g), 1)
	}
}
