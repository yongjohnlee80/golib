package style

import "fmt"

// colorKind enumerates the representable color variants. kindAdaptive and
// kindToken exist only at the Color level; colorLeaf carries the first four.
type colorKind uint8

const (
	kindDefault  colorKind = iota // terminal default fg/bg (SGR 39/49); the zero Color
	kindANSI                      // ANSI-16 palette index
	kindANSI256                   // ANSI-256 palette index
	kindRGB                       // truecolor
	kindAdaptive                  // light/dark leaf pair, picked at resolve time (Color-level only)
	kindToken                     // theme token; light.n holds the Token index (Color-level only)
)

// Color is a comparable value describing a color at one of the kinds above,
// resolved to concrete output at render time against the Backend capability
// profile. The zero Color means "unset / terminal default"
// (SGR 39/49).
//
// Colors are constructed only through [ANSI], [ANSI256], [RGB], [Adaptive],
// and [Default] (or flattened from a [Token] via [ColorSpec]); the fields are
// private so every representable value is valid by construction.
type Color struct {
	kind        colorKind
	light, dark colorLeaf // dark used only for kindAdaptive; light is the sole leaf otherwise
}

// colorLeaf is one concrete, non-composite color:
// kindDefault | kindANSI | kindANSI256 | kindRGB only.
type colorLeaf struct {
	kind    colorKind
	n       uint8 // ANSI-16 index (0-15) or ANSI-256 index
	r, g, b uint8
}

// ANSI returns a Color addressing the ANSI-16 palette (index 0-15). ANSI-16
// colors are never upsampled: on a truecolor terminal they still emit the
// ANSI-16 SGR so the user's own palette renders them (G7, ANSI-16-first).
// It panics if n is outside [0, 15] — misconfiguration fails loud at
// construction (golib convention).
func ANSI(n int) Color {
	if n < 0 || n > 15 {
		panic(fmt.Sprintf("style.ANSI: palette index %d outside the ANSI-16 range [0, 15]", n))
	}
	return Color{kind: kindANSI, light: colorLeaf{kind: kindANSI, n: uint8(n)}}
}

// ANSI256 returns a Color addressing the ANSI-256 palette (index 0-255).
// It panics if n is outside [0, 255].
func ANSI256(n int) Color {
	if n < 0 || n > 255 {
		panic(fmt.Sprintf("style.ANSI256: palette index %d outside the ANSI-256 range [0, 255]", n))
	}
	return Color{kind: kindANSI256, light: colorLeaf{kind: kindANSI256, n: uint8(n)}}
}

// RGB returns a truecolor Color. It downsamples per the resolver's chain on
// terminals with a lesser color profile.
func RGB(r, g, b uint8) Color {
	return Color{kind: kindRGB, light: colorLeaf{kind: kindRGB, r: r, g: g, b: b}}
}

// Default returns the explicit terminal-default color (SGR 39/49). It is
// also the zero Color value.
func Default() Color { return Color{} }

// Adaptive returns a Color that resolves to light or dark depending on the
// terminal background darkness at resolve time ('s OSC 10/11 probe, or a
// Theme's WithDark override).
//
// Token and adaptive leaves are REJECTED in v1:
// resolution stays single-pass over flat comparable values. The type system
// already prevents tokens here (Token is not a Color); this panic is the
// belt-and-braces guard for Colors of token or adaptive kind. Theme authors
// who want adaptive tokens put the Adaptive(...) color in the Theme slot —
// adaptivity then lives behind the token.
func Adaptive(light, dark Color) Color {
	for _, c := range [...]Color{light, dark} {
		switch c.kind {
		case kindToken:
			panic("style.Adaptive: token leaves are rejected in v1 — put the Adaptive color in the Theme slot instead (ADR-0006 §2.4)")
		case kindAdaptive:
			panic("style.Adaptive: adaptive leaves cannot nest — pass concrete colors (ADR-0006 §2.4)")
		}
	}
	return Color{kind: kindAdaptive, light: light.light, dark: dark.light}
}

// ColorSpec is what Foreground/Background/Border*Foreground accept: a literal
// Color or a theme Token. Both flatten into the internal Color representation
// at set time, so Style never stores an interface and the resolve hot path
// never type-switches.
type ColorSpec interface{ spec() Color }

func (c Color) spec() Color { return c }
func (t Token) spec() Color { return Color{kind: kindToken, light: colorLeaf{n: uint8(t)}} }

// colorFromLeaf lifts a concrete leaf back to a Color value.
func colorFromLeaf(l colorLeaf) Color { return Color{kind: l.kind, light: l} }

// Resolver read surface.
//
// The style resolver lives in package tui (/), outside
// this package, so it cannot see Color's private fields. The accessors below
// are the minimal exported read surface it needs to run the documented
// resolution chain (token lookup → adaptive pick → downsample); exactly one
// of them reports ok for any Color. They add no mutation surface and no new
// representable states.

// IsDefault reports whether c is the terminal-default color (the zero Color).
func (c Color) IsDefault() bool { return c.kind == kindDefault }

// Token returns the theme token c carries, if c is a token color
// (flattened from a Token via ColorSpec).
func (c Color) Token() (Token, bool) {
	if c.kind != kindToken {
		return 0, false
	}
	return Token(c.light.n), true
}

// AdaptivePair returns the light and dark leaves of an adaptive color.
// The returned Colors are concrete (default/ANSI/ANSI-256/RGB) — nesting is
// rejected at construction.
func (c Color) AdaptivePair() (light, dark Color, ok bool) {
	if c.kind != kindAdaptive {
		return Color{}, Color{}, false
	}
	return colorFromLeaf(c.light), colorFromLeaf(c.dark), true
}

// ANSIIndex returns the ANSI-16 palette index of an ANSI color.
func (c Color) ANSIIndex() (int, bool) {
	if c.kind != kindANSI {
		return 0, false
	}
	return int(c.light.n), true
}

// ANSI256Index returns the ANSI-256 palette index of an ANSI-256 color.
func (c Color) ANSI256Index() (int, bool) {
	if c.kind != kindANSI256 {
		return 0, false
	}
	return int(c.light.n), true
}

// RGBValues returns the components of a truecolor color.
func (c Color) RGBValues() (r, g, b uint8, ok bool) {
	if c.kind != kindRGB {
		return 0, 0, 0, false
	}
	return c.light.r, c.light.g, c.light.b, true
}
