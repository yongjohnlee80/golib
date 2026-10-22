package widget

import (
	"image/color"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/gui"
	"github.com/yongjohnlee80/golib/tui/decl/themes"
	"github.com/yongjohnlee80/golib/tui/style"
)

// No shipped theme's panel bar changes its text colour under WCAG contrast: for every theme, each
// group's text and background pair (window, button, highlight, base), plain and reversed, picks
// the same text on the theme's backdrop, window and base as a difference of gamma-unaware
// luminances picked. "default", the terminal's own colour, is the window's default text or
// ground.
func TestNoShippedThemesPanelBarTextChangesUnderWCAG(t *testing.T) {
	def := gui.DefaultTheme()
	resolve := func(v string, ground bool) (color.NRGBA, bool) {
		c, err := style.ParseColor(v)
		if err != nil {
			return color.NRGBA{}, false
		}
		if c.IsDefault() {
			if ground {
				return def.BG, true
			}
			return def.FG, true
		}
		return colorOf(c, nil, true, nil)
	}
	luma := func(c color.NRGBA) float64 { return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B) }
	diff := func(a, b color.NRGBA) float64 {
		if d := luma(a) - luma(b); d > 0 {
			return d
		}
		return luma(b) - luma(a)
	}
	pick := func(fg, bg, page color.NRGBA, measure func(a, b color.NRGBA) float64) color.NRGBA {
		if measure(bg, page) > measure(fg, page) {
			return bg
		}
		return fg
	}
	pairs := [][2]string{{"window", "windowText"}, {"button", "buttonText"}, {"highlight", "highlightedText"}, {"base", "text"}}
	for _, name := range themes.Names() {
		vals, err := themes.Values(name)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var pages []color.NRGBA
		for _, k := range []string{"app.backdrop", "app.window", "app.base"} {
			if c, ok := resolve(vals[k], true); ok {
				pages = append(pages, c)
			}
		}
		groups := map[string]bool{}
		for k := range vals {
			if i := strings.LastIndexByte(k, '.'); i > 0 {
				groups[k[:i]] = true
			}
		}
		checked := 0
		for g := range groups {
			for _, p := range pairs {
				ground, okG := resolve(vals[g+"."+p[0]], true)
				text, okT := resolve(vals[g+"."+p[1]], false)
				if !okG || !okT {
					continue
				}
				for _, bar := range [][2]color.NRGBA{{text, ground}, {ground, text}} { // plain, reversed
					for _, page := range pages {
						checked++
						if was, now := pick(bar[0], bar[1], page, diff), pick(bar[0], bar[1], page, contrast); was != now {
							t.Errorf("%s %s.%s/%s on %v: the bar's text flips from %v to %v", name, g, p[0], p[1], page, was, now)
						}
					}
				}
			}
		}
		if checked == 0 {
			t.Errorf("%s: no bar checked", name)
		}
	}
}
