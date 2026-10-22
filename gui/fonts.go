package gui

import (
	"sync/atomic"

	"github.com/yongjohnlee80/gio/font"
	"github.com/yongjohnlee80/gio/unit"
)

// fontState is one hand-over of the cell font to the Gio goroutine.
type fontState struct {
	fm       fontMetrics
	typeface string  // the cell typeface list, fallbackChain's
	size     unit.Sp // the size the cells are drawn at: SetFont's, times SetZoom's percent
	gen      uint64  // bumped by every change of font, family or size
	fonts    Fonts   // the native views' families, as the renderer has them
}

// fontControl is the Backend's cell font: the size and zoom SetFont and SetZoom were last given,
// which the tui loop owns, and the fontState published from them, which the Gio goroutine Loads
// once a frame, for measure and the IME's preedit alike. The tui loop replaces the state
// (publish) and never mutates one.
type fontControl struct {
	state atomic.Pointer[fontState]
	size  unit.Sp // the size before zoom
	zoom  int     // percent
}

// load is the font as published last.
func (f *fontControl) load() *fontState { return f.state.Load() }

// setZoom keeps pct within 50 to 300 percent.
func (f *fontControl) setZoom(pct int) { f.zoom = min(max(pct, 50), 300) }

// publish publishes a new fontState for typeface (a CSS-style family list), at size times zoom: a
// new generation, and, when face, the cell font measured again and r's face changed with it (the
// glyphs r cached are cleared by the generation). r's families go with the state, for other
// goroutines (a CanvasStream's producer).
func (f *fontControl) publish(r *renderer, typeface string, face bool) {
	old := f.load()
	tf := fallbackChain(typeface)
	fs := &fontState{fm: old.fm, typeface: tf, size: f.size * unit.Sp(f.zoom) / 100, gen: old.gen + 1}
	if face {
		fs.fm = measureFont(r.shaper, tf)
		r.typeface = font.Typeface(tf)
		r.fonts.Mono = tf
	}
	r.fonts.Gen = fs.gen
	fs.fonts = r.fonts
	f.state.Store(fs)
}
