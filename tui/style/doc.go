// Package style provides the styling, design-token, and theming layer of
// golib/tui (ADR-0006): an immutable, fluent [Style] value, a resolvable
// [Color] sum type, a semantic [Token] vocabulary with a derivation-driven
// [Theme], and the [BorderStyle] prefabs. It is stdlib-only and imports no
// other tui package; the render-time resolver (Style → cell attributes)
// lives in package tui and consumes this package's exported read surface.
//
// # Style
//
// [Style] is an immutable value: setters take the receiver by value, mutate
// the copy, and return it — assignment is a deep copy, there is no Copy()
// method, and derived styles never alias their parents. The surface is
// lipgloss-familiar:
//
//	st := style.New().
//		Foreground(style.TokenPrimary).
//		Background(style.ANSI(0)).
//		Bold(true).
//		Padding(1, 2).
//		Border(style.BorderRounded)
//
// Storage is typed private fields plus a uint64 set-bitfield (the lipgloss
// v0.11 model), so "explicitly set" is tracked per property, distinct from
// the zero value: New().Bold(false) reports GetBold() = (false, true) while
// New() reports (false, false). [Style.Inherit] copies from its argument
// only the properties not already set on the receiver — and never margins
// or padding (placement, not appearance). Unset* methods clear both the set
// bit and the value, so an unset style compares == to one that never set
// the property.
//
// Style is comparable — usable with == and as a map key — which is what
// makes the tui resolver's resolved-attribute cache a plain map. This is a
// binding constraint on evolution: new properties must be comparable values.
//
// # Color
//
// [Color] is a flat comparable sum type: terminal default, ANSI-16 index,
// ANSI-256 index, truecolor RGB, or an adaptive light/dark pair, built via
// [Default], [ANSI], [ANSI256], [RGB], and [Adaptive]. Colors resolve at
// render time against the terminal's capability profile (ADR-0002),
// downsampling truecolor → 256 → 16 → mono and never upsampling: an ANSI-16
// color always emits the ANSI-16 SGR so the user's own palette renders it.
//
// Setters accept a [ColorSpec] — either a Color or a [Token] — and flatten
// it into the internal Color representation at set time, so Style never
// stores an interface. [Adaptive] rejects token and adaptive leaves (panics
// at construction); themes that want adaptive tokens put the Adaptive color
// in the Theme slot.
//
// # Tokens and themes
//
// [Token] is a semantic color slot (TokenPrimary, TokenError, TokenSurface,
// …): 11 base slots adopting Textual's theme vocabulary plus 9 derived
// slots. Widgets style themselves in tokens; swapping the active [Theme]
// re-skins the whole application with zero widget cooperation (the swap
// itself is App-level, ADR-0005).
//
// [NewTheme] requires only Primary; every other slot has a documented
// derivation default. [DefaultTheme] is NewTheme(ANSI(4)): every default
// token resolves to an ANSI-16 index or the terminal default (the
// ANSI-16-first philosophy, G7), respecting the user's palette, contrast,
// and colorblind-safe scheme. Tokens are colors, never attributes: muted
// text is a widget convention — Foreground(TokenTextMuted).Faint(true).
//
// # Extensibility
//
// Third parties extend styles through [Style.Ext] (a namespaced,
// copy-on-write extras map, nil until first use) and compose transformations
// through [StyleOption] and [Style.Apply]. The core resolver never consults
// extras; only an opted-in renderer reads them. Chaining Ext is O(N²) by
// design — Apply is the batching path that lands N extension properties in
// one clone.
//
// # Resolver read surface
//
// The resolution pipeline (token lookup → adaptive pick → capability
// downsample) runs in package tui, which cannot see this package's private
// fields. The exported read surface it consumes: the Get* property getters
// and frame-math getters on Style; [Color.IsDefault], [Color.Token],
// [Color.AdaptivePair], [Color.ANSIIndex], [Color.ANSI256Index], and
// [Color.RGBValues] (exactly one reports ok per Color); [Theme.Color] and
// [Theme.Dark]. None of it adds mutation surface.
package style
