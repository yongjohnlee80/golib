package style

import "fmt"

// Token is a semantic color slot, resolved through the active Theme at
// render time. Widgets style themselves in semantic terms (TokenPrimary,
// TokenError) and whole applications re-skin by swapping one Theme.
//
// Tokens are colors, never attributes (ADR-0006 §2.5, normative): a Token
// resolves to exactly one Color, and the resolver never attaches SGR
// attributes on a token's behalf. TokenTextMuted is therefore a color only —
// widgets that want faint muted text say so themselves:
// style.New().Foreground(style.TokenTextMuted).Faint(true).
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

	// Derived variants — the deliberately minimal set (ADR-0006 §2.5):

	TokenTextMuted       // de-emphasized text color on Background/Surface (color ONLY)
	TokenTextOnPrimary   // readable text atop Primary fills
	TokenTextOnSecondary // readable text atop Secondary fills (Select/List secondary selection fills, ADR-0007)
	TokenTextOnAccent
	TokenTextOnError
	TokenTextOnSuccess
	TokenTextOnWarning
	TokenBorder        // default border color (Box and friends, ADR-0007)
	TokenBorderFocused // border color of the focused panel (ADR-0007 Box focus visuals)

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
// single-pass, ADR-0006 §2.4). Adaptive colors are allowed: putting an
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

// NewTheme builds a Theme from the required Primary color plus options.
// Every slot not supplied via WithToken receives its derivation default
// (ADR-0006 §2.5) — one line of deterministic logic per slot, no color math:
//
//	Secondary, Accent               ← Primary
//	Foreground, Background          ← Default() (terminal's own fg/bg)
//	Surface                         ← Background
//	Panel, Boost                    ← Surface
//	Warning / Error / Success       ← ANSI(3) / ANSI(1) / ANSI(2)
//	TextMuted                       ← Foreground (color only)
//	TextOn*                         ← Background (inverted text on a colored fill)
//	Border                          ← Foreground
//	BorderFocused                   ← Accent
//
// Derivations read the slot values after options are applied, so overriding
// e.g. Surface also moves the Panel and Boost defaults.
//
// Like WithToken, NewTheme panics if primary is a token-kind Color.
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
		panic(fmt.Sprintf("style.Theme.Color: token %d outside the token range [0, %d)", t, numTokens))
	}
	return th.colors[t]
}

// Dark returns the WithDark override: forced reports whether the theme
// forces adaptivity, and dark is the forced value when it does. When forced
// is false the resolver falls back to the probed
// Capabilities.DarkBackground (ADR-0002).
func (th Theme) Dark() (dark, forced bool) { return th.dark, th.darkSet }
