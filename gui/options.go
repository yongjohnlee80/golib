package gui

import (
	"image/color"
	"strings"

	"gioui.org/unit"
)

// Theme is what the window draws with where a cell asks for the terminal's own colours: the
// default foreground and background (tui's CellColorDefault), the cursor, and the sixteen ANSI
// colours a cell may name by index. A tui App's own theme resolves everything else to RGB.
type Theme struct {
	FG, BG color.NRGBA
	Cursor color.NRGBA // the caret; zero draws it in the colour of the text under it
	ANSI   [16]color.NRGBA
}

// DefaultTheme is a dark theme with the xterm ANSI palette.
func DefaultTheme() Theme {
	return Theme{
		FG: color.NRGBA{R: 0xd8, G: 0xde, B: 0xe9, A: 0xff},
		BG: color.NRGBA{R: 0x1e, G: 0x1e, B: 0x2e, A: 0xff},
		ANSI: [16]color.NRGBA{
			{0x00, 0x00, 0x00, 0xff}, {0xcd, 0x00, 0x00, 0xff}, {0x00, 0xcd, 0x00, 0xff}, {0xcd, 0xcd, 0x00, 0xff},
			{0x00, 0x00, 0xee, 0xff}, {0xcd, 0x00, 0xcd, 0xff}, {0x00, 0xcd, 0xcd, 0xff}, {0xe5, 0xe5, 0xe5, 0xff},
			{0x7f, 0x7f, 0x7f, 0xff}, {0xff, 0x00, 0x00, 0xff}, {0x00, 0xff, 0x00, 0xff}, {0xff, 0xff, 0x00, 0xff},
			{0x5c, 0x5c, 0xff, 0xff}, {0xff, 0x00, 0xff, 0xff}, {0x00, 0xff, 0xff, 0xff}, {0xff, 0xff, 0xff, 0xff},
		},
	}
}

// dark reports whether t's background is dark, by relative luminance, as tui/term decides
// Capabilities.DarkBackground from a terminal's OSC 11 reply.
func (t Theme) dark() bool {
	l := 0.2126*float64(t.BG.R) + 0.7152*float64(t.BG.G) + 0.0722*float64(t.BG.B)
	return l/255 < 0.5
}

type config struct {
	title      string
	width      unit.Dp
	height     unit.Dp
	minWidth   unit.Dp
	minHeight  unit.Dp
	typeface   string
	fontSet    bool // WithFont named a typeface: no system font goes before it
	fontSize   unit.Sp
	padding    unit.Dp
	theme      Theme
	decorated  bool
	decoration bool // decorated was set explicitly
	style      *Style
}

func defaultConfig() config {
	return config{
		title:    "golib",
		width:    1100,
		height:   700,
		typeface: "Go Mono",
		fontSize: 14,
		padding:  4,
		theme:    DefaultTheme(),
		style:    NativeStyle(),
	}
}

// Option configures a Backend.
type Option func(*config)

// WithTitle sets the window's title.
func WithTitle(title string) Option { return func(c *config) { c.title = title } }

// WithSize sets the window's initial size, in device-independent pixels.
func WithSize(w, h unit.Dp) Option { return func(c *config) { c.width, c.height = w, h } }

// WithMinSize sets the smallest size the window may be resized to.
func WithMinSize(w, h unit.Dp) Option { return func(c *config) { c.minWidth, c.minHeight = w, h } }

// WithFont sets the monospace typeface the cells are drawn in, and its size. typeface is a Gio
// typeface list, a CSS font-family list ("CaskaydiaMono NFM, Go Mono"), naming families as
// fontconfig lists them (fc-list : family). The first family that is installed sets the cell's
// size. A character it lacks comes from the next family that has it, then from an installed Nerd
// Font (its icons), and then from the system's monospace and emoji fonts, which are always
// appended (see fallbackChain, cellTypeface). The default is the system's monospace family where
// fontconfig names one, then the embedded Go Mono, at 14sp.
func WithFont(typeface string, size unit.Sp) Option {
	return func(c *config) {
		if typeface != "" {
			c.typeface, c.fontSet = typeface, true
		}
		if size > 0 {
			c.fontSize = size
		}
	}
}

// fallbackChain is typeface with the generic monospace and emoji families appended, unless it
// names them already. Without them, a character the named families lack draws as the
// not-defined box: Gio's own fallback does not reach every system font (▸ U+25B8 and colour
// emoji, among others, on Linux with Noto and DejaVu installed).
func fallbackChain(typeface string) string {
	chain := strings.TrimSpace(typeface)
	for _, generic := range []string{"monospace", "emoji"} {
		if !namesFamily(chain, generic) {
			if chain != "" {
				chain += ", "
			}
			chain += generic
		}
	}
	return chain
}

// namesFamily reports whether the comma-separated family list names family, ignoring quotes and case.
func namesFamily(list, family string) bool {
	for _, f := range strings.Split(list, ",") {
		if strings.EqualFold(strings.Trim(strings.TrimSpace(f), `"'`), family) {
			return true
		}
	}
	return false
}

// WithPadding sets the space between the window's edge and its cells on every side, as a
// terminal's window padding does. It takes the colour of the edge cells beside it. Default 4dp.
func WithPadding(p unit.Dp) Option { return func(c *config) { c.padding = max(p, 0) } }

// WithTheme sets the default colours, the cursor and the ANSI palette.
func WithTheme(t Theme) Option { return func(c *config) { c.theme = t } }

// WithDecorations chooses whether the system draws the window's decorations (title bar, edges).
// The platform decides when it is not set.
func WithDecorations(on bool) Option {
	return func(c *config) { c.decorated, c.decoration = on, true }
}
