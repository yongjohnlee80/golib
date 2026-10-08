package widget

import "github.com/yongjohnlee80/golib/tui/style"

// MINIMUM CONTRAST — a program's colours are written for the terminal it expects, most often a dark
// one: ls's yellow and green, a prompt's cyan. On a light theme's page they wash out. With a
// minimum contrast set, a foreground that reads below it against its background is taken toward
// black on a light background, or white on a dark one, until it reads: its hue kept, its
// lightness moved, as a terminal's own "minimum contrast" setting does.
//
// It applies where both colours are known: the cell's own background, else the default look's
// (the page's, SetDefaultLook). The colour work is style.Legible's.

// SetMinimumContrast sets the contrast ratio (WCAG's, 1 to 21) every foreground is raised to
// against its background; 0 turns it off (the default). 4.5 is WCAG's ratio for body text.
func (t *Terminal) SetMinimumContrast(ratio float64) {
	t.minContrast = ratio
	t.contrastCache = nil
	t.MarkDirty()
}

// contrastKey is a foreground on a background, for the cache.
type contrastKey struct{ fg, bg style.Color }

// legible is fg raised to the minimum contrast against bg, or fg itself when it reads, the
// minimum is off, or either colour is unknown (a token, the terminal's default).
func (t *Terminal) legible(fg, bg style.Color) style.Color {
	if t.minContrast <= 1 {
		return fg
	}
	k := contrastKey{fg, bg}
	if c, ok := t.contrastCache[k]; ok {
		return c
	}
	out := style.Legible(fg, bg, t.minContrast)
	if t.contrastCache == nil {
		t.contrastCache = map[contrastKey]style.Color{}
	}
	t.contrastCache[k] = out
	return out
}
