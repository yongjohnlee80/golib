package style

import (
	"fmt"

	"github.com/yongjohnlee80/golib/errs"
)

// Token is a semantic color slot, resolved through the active [Theme] at render time.
//
// # What Tokens and Themes Solve
//
// In traditional TUI applications, hardcoding raw ANSI escape codes or hex RGB values directly
// inside widgets creates severe usability and maintenance challenges:
//  1. Theme Brittleness: Swapping a color palette requires finding and modifying color constants
//     across every widget in the codebase.
//  2. Contrast & Readability Inversion: Hardcoded dark-mode colors become unreadable when run on
//     a user's light-mode terminal emulator.
//  3. Palette Hijacking: Truecolor assumptions break when running on 16-color or 256-color
//     terminals or restricted SSH/serial environments.
//
// The Token and Theme subsystem resolves these issues by decoupling widget authoring from
// concrete color definitions:
//  - Widgets style themselves strictly in semantic terms ([TokenPrimary], [TokenError], [TokenSurface]).
//  - Applications supply a [Theme] that binds those tokens to concrete [Color] instances.
//  - The TUI resolver maps tokens to colors and handles capability downsampling (Truecolor → 256 → 16 → mono)
//    and light/dark background adaptation seamlessly.
//
// # Invariant: Tokens Are Colors, Never Attributes
//
// A Token resolves to exactly one [Color]. The resolver never attaches SGR attributes (bold, faint,
// underline) on a token's behalf.
//
// For example, [TokenTextMuted] is solely the de-emphasized text color (typically matching Foreground);
// widgets that desire dim/faint text must explicitly combine the token with the faint attribute:
//
//	style.New().Foreground(style.TokenTextMuted).Faint(true)
//
// # Usage Examples
//
// 1. Styling a widget using semantic tokens:
//
//	badgeStyle := style.New().
//		Foreground(style.TokenTextOnPrimary).
//		Background(style.TokenPrimary).
//		Bold(true).
//		Padding(0, 1)
//
// 2. Styling an error callout box:
//
//	errorBox := style.New().
//		Foreground(style.TokenTextOnError).
//		Background(style.TokenError).
//		Border(style.BorderThick).
//		BorderForeground(style.TokenError)
type Token int

const (
	// The 11 base slots — Textual's theme vocabulary, adopted verbatim
	// (https://textual.textualize.io/guide/design/):

	TokenPrimary Token = iota
	TokenSecondary
	TokenForeground // default text
	TokenBackground // default backdrop
	TokenSurface    // widget background
	TokenPanel      // side-panel / chrome background
	TokenWarning
	TokenError
	TokenSuccess
	TokenAccent
	TokenBoost // translucent-overlay analogue: a slightly offset surface

	// Derived variants — the deliberately minimal set:

	TokenTextMuted       // de-emphasized text color on Background/Surface (color ONLY)
	TokenTextOnPrimary   // readable text atop Primary fills
	TokenTextOnSecondary // readable text atop Secondary fills (Select/List secondary selection fills)
	TokenTextOnAccent
	TokenTextOnError
	TokenTextOnSuccess
	TokenTextOnWarning
	TokenBorder        // default border color (Box and friends)
	TokenBorderFocused // border color of the focused panel (Box focus visuals)

	numTokens // internal bound
)

// Theme maps tokens to Colors. Only Primary is required; every other slot
// has a documented derivation default applied by NewTheme. Theme values are
// immutable after construction.
type Theme struct {
	colors  [numTokens]Color
	dark    bool // WithDark override value
	darkSet bool // whether WithDark was supplied
}

// themeBuilder is the mutable state ThemeOptions act on inside NewTheme.
type themeBuilder struct {
	th  Theme
	set [numTokens]bool
}

// ThemeOption customizes a Theme under construction.
type ThemeOption func(*themeBuilder)

// WithToken supplies an explicit Color for one token slot, overriding its
// derivation default. It panics on an out-of-range token or a token-kind
// Color (a theme slot cannot indirect to another token — resolution is
// single-pass). Adaptive colors are allowed: putting an
// Adaptive(...) color in a slot is exactly how a theme makes a token
// adaptive.
func WithToken(t Token, c Color) ThemeOption {
	if t < 0 || t >= numTokens {
		panic(fmt.Sprintf("style.WithToken: token %d outside the token range [0, %d)", t, numTokens))
	}
	if c.kind == kindToken {
		panic("style.WithToken: a theme slot cannot hold a token color — supply a concrete or adaptive Color")
	}
	return func(b *themeBuilder) {
		b.th.colors[t] = c
		b.set[t] = true
	}
}

// WithDark forces the theme's light/dark adaptivity instead of the terminal
// background probe: adaptive colors in this theme's slots resolve against
// this value rather than Capabilities.DarkBackground.
func WithDark(dark bool) ThemeOption {
	return func(b *themeBuilder) {
		b.th.dark = dark
		b.th.darkSet = true
	}
}

// NewTheme builds an immutable [Theme] from a required Primary color and optional slot overrides.
//
// # Derivation Cascade Architecture
//
// Only the Primary color is mandatory. Every other slot is derived deterministically
// using single-pass logic without heuristic RGB blending or color-math distortion:
//
//	                     Primary
//	                    ┌───┴───┐
//	                    ▼       ▼
//	                Secondary  Accent ──────► BorderFocused
//	                            │
//	   Default() ───────────────┼───────────► Surface ────┬► Panel
//	  (Background)              │                         └► Boost
//	                            ▼
//	   Default() ─────► TextMuted, Border
//	  (Foreground)
//	                            ▼
//	   Default() ─────► TextOn* (TextOnPrimary, TextOnSecondary, TextOnAccent...)
//	  (Background)
//
//	  Fixed Status Slots:
//	    Warning ──► ANSI(3) [Yellow]
//	    Error   ──► ANSI(1) [Red]
//	    Success ──► ANSI(2) [Green]
//
// # Cascading Overrides
//
// Derivations inspect the slot values after all options have been processed. Therefore,
// overriding an upstream slot automatically recalculates all unconfigured downstream slots:
//  - Overriding [TokenPrimary] cascades to [TokenSecondary] and [TokenAccent].
//  - Overriding [TokenAccent] cascades to [TokenBorderFocused].
//  - Overriding [TokenBackground] cascades to [TokenSurface], [TokenPanel], [TokenBoost], and all [TokenTextOn*] slots.
//  - Overriding [TokenSurface] cascades to [TokenPanel] and [TokenBoost].
//  - Overriding [TokenForeground] cascades to [TokenTextMuted] and [TokenBorder].
//
// # Invariants
//
// Primary cannot be a token-kind Color (panics at construction). Resolution is strictly
// single-pass (Token → Color); nested token indirections are disallowed.
//
// # Usage Examples
//
// 1. Framework default theme (ANSI-16-first, G7):
//
//	th := style.DefaultTheme() // NewTheme(style.ANSI(4))
//
// 2. Custom truecolor brand theme:
//
//	th := style.NewTheme(
//		style.RGB(0x7a, 0x5c, 0xff), // Primary purple
//		style.WithToken(style.TokenSurface, style.ANSI256(236)),
//		style.WithToken(style.TokenAccent, style.RGB(0x00, 0xd7, 0xaf)),
//	)
//
// 3. Adaptive light/dark theme:
//
//	th := style.NewTheme(
//		style.Adaptive(style.ANSI(4), style.ANSI(12)), // Light/dark primary
//		style.WithToken(style.TokenSurface, style.Adaptive(style.ANSI(15), style.ANSI(0))),
//	)
func NewTheme(primary Color, opts ...ThemeOption) Theme {
	if primary.kind == kindToken {
		panic("style.NewTheme: primary cannot be a token color — supply a concrete or adaptive Color")
	}
	var b themeBuilder
	b.th.colors[TokenPrimary] = primary
	b.set[TokenPrimary] = true
	for _, opt := range opts {
		if opt != nil {
			opt(&b)
		}
	}

	derive := func(t Token, c Color) {
		if !b.set[t] {
			b.th.colors[t] = c
		}
	}
	c := &b.th.colors
	derive(TokenSecondary, c[TokenPrimary])
	derive(TokenAccent, c[TokenPrimary])
	derive(TokenForeground, Default())
	derive(TokenBackground, Default())
	derive(TokenSurface, c[TokenBackground])
	derive(TokenPanel, c[TokenSurface])
	derive(TokenBoost, c[TokenSurface])
	derive(TokenWarning, ANSI(3))
	derive(TokenError, ANSI(1))
	derive(TokenSuccess, ANSI(2))
	derive(TokenTextMuted, c[TokenForeground])
	derive(TokenTextOnPrimary, c[TokenBackground])
	derive(TokenTextOnSecondary, c[TokenBackground])
	derive(TokenTextOnAccent, c[TokenBackground])
	derive(TokenTextOnError, c[TokenBackground])
	derive(TokenTextOnSuccess, c[TokenBackground])
	derive(TokenTextOnWarning, c[TokenBackground])
	derive(TokenBorder, c[TokenForeground])
	derive(TokenBorderFocused, c[TokenAccent])
	return b.th
}

// DefaultTheme returns the framework default theme: NewTheme(ANSI(4)) — a
// blue primary with every derivation default. All of its tokens resolve to
// ANSI-16 indices or the terminal default (G7, ANSI-16-first): the user's
// terminal palette, contrast, and colorblind-safe scheme are respected out
// of the box. Truecolor themes are a deliberate authoring opt-in via
// NewTheme(RGB(...), WithToken(...)).
func DefaultTheme() Theme { return NewTheme(ANSI(4)) }

// Color returns the Color mapped to token t. It panics on an out-of-range
// token. The returned Color is concrete or adaptive, never a token-kind
// Color (enforced at construction), so the resolver's token lookup is a
// single pass.
func (th Theme) Color(t Token) Color {
	if t < 0 || t >= numTokens {
		panic(errs.Fatal{Op: "style.Theme.Color", Rule: fmt.Sprintf("token %d outside the token range [0, %d)", t, numTokens)})
	}
	return th.colors[t]
}

// Dark returns the WithDark override: forced reports whether the theme
// forces adaptivity, and dark is the forced value when it does. When forced
// is false the resolver falls back to the probed
// Capabilities.DarkBackground.
func (th Theme) Dark() (dark, forced bool) { return th.dark, th.darkSet }
