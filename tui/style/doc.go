// Package style provides the styling, design-token, and theming layer of golib/tui:
// an immutable, fluent [Style] value, a resolvable [Color] sum type, a semantic [Token]
// vocabulary with a derivation-driven [Theme], and standard [BorderStyle] prefabs.
//
// # What This Package Solves
//
// Developing robust, accessible Terminal User Interfaces (TUIs) presents several fundamental
// styling challenges:
//  1. Escape-Code Coupling: Hardcoding ANSI escape sequences directly in components destroys
//     portability, breaks palette swapping, and makes automated testing difficult.
//  2. Garbage Collector Pressure: Allocating style objects per frame causes frame drops and
//     jitter in 60 FPS terminal applications.
//  3. Mutable State Bleed: Sharing style objects across components leads to accidental pointer
//     aliasing bugs where one widget's modification alters an unrelated widget.
//  4. Palette & Contrast Breakage: Hardcoded dark/light colors break when users switch terminal
//     themes, use restricted color palettes (16-color, 256-color), or run in high-contrast modes.
//
// Package style solves these problems through four design pillars:
//   - Complete Separation of Style from Rendering: This package is stdlib-only and contains
//     zero terminal escape generation logic. It models visual intent; the render-time resolver
//     (in package tui) translates styles to terminal cell attributes against hardware profiles.
//   - Zero-Allocation Immutable Value Semantics: [Style] is a flat struct copied by value.
//     Setters execute in ~18ns with 0 heap allocations, assignment is a deep copy, and derived
//     styles never alias their parents.
//   - Strict Struct Comparability: [Style] is fully comparable with == and functions natively
//     as a map key, enabling O(1) cell attribute caching in the renderer.
//   - Semantic Token & Theming System: Widgets express styling via abstract tokens ([TokenPrimary],
//     [TokenSurface]), while [Theme] maps tokens to colors with deterministic cascades.
//
// # Architecture & Resolution Pipeline
//
// The lifecycle of a style flows from semantic definition to terminal cell emission:
//
//	┌────────────────────────────────────────────────────────┐
//	│ 1. Widget Authoring (Semantic Layer)                  │
//	│    st := style.New().Foreground(style.TokenPrimary)    │
//	│                      │                                 │
//	│                      ▼                                 │
//	│ 2. Theme Resolution (Palette Binding)                  │
//	│    Theme maps TokenPrimary ──► Color (RGB/ANSI/Adapt)  │
//	│                      │                                 │
//	│                      ▼                                 │
//	│ 3. Hardware Profile Downsampling (Package tui)         │
//	│    Terminal capabilities: Truecolor ──► 256 ──► 16     │
//	│                      │                                 │
//	│                      ▼                                 │
//	│ 4. Cell Attribute Cache & SGR Emission                 │
//	│    Style (comparable map key) ──► Terminal SGR Bytes   │
//	└────────────────────────────────────────────────────────┘
//
// # The TUI Box Model
//
// Package style implements the standard box model, computing frame dimensions from inside out:
//
//	┌────────────────────────────────────────────────────────┐
//	│ Margin (outer transparent spacing)                     │
//	│  ┌──────────────────────────────────────────────────┐  │
//	│  │ Border (box-drawing perimeter)                   │  │
//	│  │  ┌────────────────────────────────────────────┐  │  │
//	│  │  │ Padding (interior whitespace clearance)    │  │  │
//	│  │  │  ┌──────────────────────────────────────┐  │  │  │
//	│  │  │  │ Content Area (rendered text/widgets) │  │  │  │
//	│  │  │  │ w = Width, h = Height                │  │  │  │
//	│  │  │  └──────────────────────────────────────┘  │  │  │
//	│  │  └────────────────────────────────────────────┘  │  │
//	│  └──────────────────────────────────────────────────┘  │
//	└────────────────────────────────────────────────────────┘
//
// Total horizontal frame size = Margin(L+R) + Border(L+R) + Padding(L+R)
// Total vertical frame size   = Margin(T+B) + Border(T+B) + Padding(T+B)
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
// render time against the terminal's capability profile,
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
// itself is App-level).
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
//
// # Comprehensive Usage Examples
//
// 1. Defining a styled component card with rounded border and padding:
//
//	card := style.New().
//		Foreground(style.TokenForeground).
//		Background(style.TokenSurface).
//		Padding(1, 2).                   // CSS shorthand: 1 vertical, 2 horizontal
//		Border(style.BorderRounded).     // All four perimeter edges
//		BorderForeground(style.TokenBorder).
//		Margin(1).                       // Outer spacing
//		Align(style.AlignCenter)
//
// 2. Focused button state using non-destructive inheritance:
//
//	baseBtn := style.New().
//		Foreground(style.TokenTextOnPrimary).
//		Background(style.TokenPrimary).
//		Padding(0, 2)
//
//	// Focused button inherits base appearance, adds bold and focused border:
//	focusedBtn := baseBtn.
//		Bold(true).
//		Border(style.BorderNormal).
//		BorderForeground(style.TokenBorderFocused)
//
// 3. Unsetting inherited properties for compact child elements:
//
//	compactCard := card.
//		UnsetBorder().
//		UnsetMargin().
//		Padding(0, 1)
//
// 4. Constructing an adaptive brand theme:
//
//	myTheme := style.NewTheme(
//		style.RGB(0x63, 0x66, 0xf1), // Indigo primary
//		style.WithToken(style.TokenSurface, style.Adaptive(
//			style.ANSI256(255), // Clean white surface on light terminals
//			style.ANSI256(235), // Dark charcoal surface on dark terminals
//		)),
//		style.WithToken(style.TokenAccent, style.RGB(0xec, 0x48, 0x99)), // Pink accent
//	)
//
// 5. Applying namespaced third-party extensions in a single batch:
//
//	hyperlinkKey := style.ExtKey{Pkg: "acme/tuix", Name: "hyperlink"}
//	withLink := func(url string) style.StyleOption {
//		return func(s *style.Style) { *s = s.Ext(hyperlinkKey, url) }
//	}
//
//	linkStyle := style.New().
//		Foreground(style.TokenPrimary).
//		Underline(true).
//		Apply(withLink("https://example.com"))
package style
