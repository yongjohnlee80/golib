package gui

import (
	"image/color"

	"gioui.org/unit"
)

// Theme is what the window draws with where a cell asks for the terminal's own colours: the
// default foreground and background (tui's CellColorDefault), the cursor, and the sixteen ANSI
// colours a cell may name by index. A tui App's own theme resolves everything else to RGB.
type Theme struct {
	FG, BG color.NRGBA
	Cursor color.NRGBA
	ANSI   [16]color.NRGBA
}

// DefaultTheme is a dark theme with the xterm ANSI palette.
func DefaultTheme() Theme {
	return Theme{
		FG:     color.NRGBA{R: 0xd8, G: 0xde, B: 0xe9, A: 0xff},
		BG:     color.NRGBA{R: 0x1e, G: 0x1e, B: 0x2e, A: 0xff},
		Cursor: color.NRGBA{R: 0xf5, G: 0xe0, B: 0xdc, A: 0xff},
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
	fontSize   unit.Sp
	theme      Theme
	decorated  bool
	decoration bool // decorated was set explicitly
}

func defaultConfig() config {
	return config{
		title:    "golib",
		width:    1100,
		height:   700,
		typeface: "Go Mono",
		fontSize: 14,
		theme:    DefaultTheme(),
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
// typeface list ("CaskaydiaMono Nerd Font, Go Mono"): the first that is installed wins, and
// characters it lacks fall back to the system's fonts. The default is the embedded Go Mono at 14sp.
func WithFont(typeface string, size unit.Sp) Option {
	return func(c *config) {
		if typeface != "" {
			c.typeface = typeface
		}
		if size > 0 {
			c.fontSize = size
		}
	}
}

// WithTheme sets the default colours, the cursor and the ANSI palette.
func WithTheme(t Theme) Option { return func(c *config) { c.theme = t } }

// WithDecorations chooses whether the system draws the window's decorations (title bar, edges).
// The platform decides when it is not set.
func WithDecorations(on bool) Option {
	return func(c *config) { c.decorated, c.decoration = on, true }
}
