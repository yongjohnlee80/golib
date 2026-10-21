package gui

import (
	"image/color"
	"strings"

	"github.com/yongjohnlee80/gio/unit"
)

// Backend Configuration Model:
//
//	             +--------------------------------------+
//	             |          defaultConfig()             |
//	             | (1100x700dp, Go Mono 14sp, 4dp pad)  |
//	             +--------------------------------------+
//	                                |
//	                                v
//	+------------------+  +-------------------+  +--------------------+
//	| WithTitle(...)   |  | WithFont(...)     |  | WithTheme(...)     |
//	| WithSize(...)    |  | WithPadding(...)  |  | WithDecorations(..)|
//	+------------------+  +-------------------+  +--------------------+
//	        \                       |                       /
//	         \                      v                      /
//	          +-------------------------------------------+
//	          |   Resolved Window & Backend Settings      |
//	          +-------------------------------------------+
//
// Theme defines the baseline color palette utilized by the window when cell styles reference
// default terminal colors (such as [tui.CellColorDefault]), default cursor styling, or indexed
// 16-color ANSI palettes.
//
// Higher-level application themes defined in [tui.App] map semantic tokens to RGB colors.
// When cells specify unmapped defaults or ANSI indices, Theme resolves them into concrete
// [color.NRGBA] values for vector rasterization.
type Theme struct {
	FG     color.NRGBA     // Default foreground text color
	BG     color.NRGBA     // Default window background color
	Cursor color.NRGBA     // Caret color; zero-value draws using the color of the underlying text cell
	ANSI   [16]color.NRGBA // Standard 16-color terminal palette (0-7 normal, 8-15 bright)
}

// DefaultTheme returns a modern dark palette featuring high-contrast text and the standard
// xterm ANSI color palette.
func DefaultTheme() Theme {
	th := Theme{
		FG: color.NRGBA{R: 0xd8, G: 0xde, B: 0xe9, A: 0xff},
		BG: color.NRGBA{R: 0x1e, G: 0x1e, B: 0x2e, A: 0xff},
	}
	for i := range th.ANSI {
		th.ANSI[i] = xtermNRGBA(uint8(i)) // xterm's sixteen, the one table in tui/style
	}
	return th
}

// dark reports whether the background color represents a dark shade based on perceived
// relative luminance calculation (standard ITU-R BT.709 coefficients).
func (t Theme) dark() bool {
	l := 0.2126*float64(t.BG.R) + 0.7152*float64(t.BG.G) + 0.0722*float64(t.BG.B)
	return l/255 < 0.5
}

// config stores all resolved launch parameters and visual styles for the window and renderer.
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
	recorder   Recorder // where the input path's records go; nil records nothing
	stage      *Panes   // WithPanes: a pixel root; nil is one cell grid
	zeroCopy   bool     // WithZeroCopyFrames
}

// WithZeroCopyFrames asks for the renderer that draws frames (Stream) without a copy: on
// Linux, Vulkan before EGL on Wayland; on macOS, Metal, the default already. If that renderer is
// unavailable the window falls back as before; FrameCaps then says which frames copy.
func WithZeroCopyFrames() Option { return func(c *config) { c.zeroCopy = true } }

// defaultConfig returns the default GUI window and backend configuration.
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

// WithRecorder sets where the backend's input recording goes: one [Record] per
// platform event, IME state, delivered key, command and preedit draw, in the
// order they happened.
//
// Privacy: a recording contains every typed character — including passwords —
// so nothing is recorded unless a recorder is given, and a recorder writes only
// where its installer points it, never anywhere by default. A recorder that is
// also an io.Closer is closed by the window's goroutine when the window finishes,
// before [Backend.Stop] returns — which flushes the [JSONLRecorder]'s buffer.
func WithRecorder(r Recorder) Option { return func(c *config) { c.recorder = r } }
