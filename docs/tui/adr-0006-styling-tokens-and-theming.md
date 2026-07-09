
# ADR-0006 — golib/tui: Styling, Design Tokens & Theming

**Tags:** `type:adr` `status:accepted` `owner:shared` `repo:golib` `area:tui` `kind:styling` `kind:theming` `kind:design-tokens` `kind:color`

**Abstract:** Defines `tui/style`: an immutable, value-semantics `style.Style` with fluent chained setters over typed private fields plus a `uint64` set-bitfield (the lipgloss v0.11 model — the property-bag `map[string]interface{}` design from the originating task is examined in full and rejected on production evidence), a `style.Color` sum type resolved at render time against Backend capabilities with an ANSI-16-first downsampling chain, and a `style.Token`/`style.Theme` design-token system adopting Textual's 11-slot vocabulary with a deliberately minimal derived-variant set.

> **Authored natively in the KB** (jarvis, 2026-07-08) as part of the golib/tui design
> dossier. golib/tui keeps its own 0001–0007 ADR numbering; the `golib-tui-` filename
> prefix namespaces it against this KB's auto-agents ADR sequence. Navigation hub:
> golib-tui. Umbrella ADR: golib-tui-0001-overview-and-architecture.

- **Status:** **Accepted** (Johno, 2026-07-08) — revision 1 (Lector r1 `change_requested` → r2 `approved_with_amendments`, all amendments folded).
- **Date:** 2026-07-08
- **Reviewed by:** lector, 2026-07-08 — agents/lector/reviews/2026-07-08-golib-tui-adrs-review.md (r1); agents/lector/reviews/2026-07-08-golib-tui-adrs-rereview.md (r2, `approved_with_amendments`)
- **Module:** github.com/yongjohnlee80/golib
- **Supersedes:** none (greenfield)
- **Related:** ADR-0001 (umbrella; fixes the typed-fields+bitfield decision at
  architecture level — this ADR carries the full evidence and API), ADR-0002 (terminal
  backend — supplies the `Capabilities` color profile and the OSC 10/11 light/dark probe
  this ADR resolves against), ADR-0003 (cell buffer — `Style` resolves to the concrete
  cell attribute set painted into cells), ADR-0005 (runtime — theme switching is an
  App-level event triggering full repaint), ADR-0007 (widget set — every widget's style
  hooks consume this package).

> **Revision history.** rev 1 (2026-07-08): folded Lector r1 `change_requested` —
> (1) **must-fix #1**: resolution rewritten against ADR-0002's canonical capability
> model — `type ColorProfile uint8` (`ProfileMono | ProfileANSI16 | ProfileANSI256 |
> ProfileTrueColor`) and `Capabilities.DarkBackground bool` — no phantom fields (§2.4,
> §2.6, §5.6); (2) **must-fix #1**: `kindToken` declared as a member of the `colorKind`
> enum, previously referenced but undeclared (§2.4); (3) **should-fix / Q2**: added
> `TokenBorder`, `TokenBorderFocused`, and `TokenTextOnSecondary` with derivation
> defaults (§2.5, §5.7); (4) **should-fix / Q3**: `TokenTextMuted` is color-only —
> normative rule added: tokens are colors, never attributes; widgets opt into
> `.Faint(true)` (§2.5); (5) **Q4**: `Adaptive` rejects token leaves in v1 —
> compile-shape prevention documented plus a belt-and-braces panic (§2.4);
> (6) **Q5**: `Ext`'s O(N²) copy-on-write accepted; `Apply(opts...)` documented as the
> batching path for multiple extensions (§2.7). §6 questions carry Lector's r1 answers
> inline.

> **Self-containment contract.** This ADR is implementable with no prior context beyond
> the golib conventions (golang, golib) and the sibling ADRs listed under Related.

## 1. Context

ADR-0001 G4 requires a fluent, immutable, design-token-driven styling system with a
lipgloss-familiar surface, resolving through a theme and a terminal capability profile at
render time. `tui/style` is a stdlib-only package (ADR-0001 §2.2) imported by `tui`
(render layer needs resolution) and `tui/widget` (every widget exposes style hooks).

The originating task (objectives 11–12) proposed the styling store be a
`map[string]interface{}` property bag keyed by string property names, motivated by
forward-compatible extensibility: new properties could be added by any party without
changing a struct definition, and "is this property set?" falls out of map membership.
Those two motivations are load-bearing and real — the "is-set" distinction is exactly
what a cascading `Inherit` needs, and golib packages do get extended. This ADR must
answer both while choosing the storage representation on evidence, not taste. The
evidence exists: **lipgloss, the most successful fluent styling library in the Go
ecosystem, shipped exactly the proposed property-bag design for two years and reversed
it** — §2.2 carries the full record.

Constraints from the siblings: styles are read in the render hot loop (ADR-0003 resolves
a `Style` to cell attributes once per component `Render`), so per-get map lookups and
boxing allocations land on the framework's hottest path; `Style` values are candidates
for use as cache keys (resolved-attribute memoization), which raises the question of Go
comparability; and color output must degrade per-capability (truecolor → 256 → 16)
because ADR-0002 probes capabilities live rather than trusting `$TERM`.

### 1.1 Goals

- **G1** — Immutable value semantics: `Style` is copied by assignment; every setter
  returns a modified copy; no `Copy()` method, no aliasing hazards, safe to share across
  the tree and cache.
- **G2** — Track "explicitly set" per property, distinct from the zero value — required
  for `Inherit` cascade semantics and `Unset*`.
- **G3** — Zero allocations on the get path and zero per-property allocations on the set
  path; style resolution must be cheap enough to run per-render without caching, and
  cacheable when the implementor wants more.
- **G4** — Compile-time safety: property names and value types are checked by the
  compiler, not discovered at runtime.
- **G5** — lipgloss-familiar API (ADR-0001 §2.3 fixes the surface shape) so the large
  population of charm-ecosystem users transfers intuition directly.
- **G6** — A design-token vocabulary + `Theme` such that widgets style themselves in
  semantic terms (`TokenPrimary`, `TokenError`) and whole applications re-skin by
  swapping one struct.
- **G7** — ANSI-16-first color philosophy: default theme tokens resolve to ANSI-16
  indices so the user's terminal palette and contrast choices are respected; truecolor
  themes are a deliberate opt-in (accessibility rationale, §2.4).
- **G8** — Open extensibility for third parties without making the core pay for it.

### 1.2 Non-goals

- **N1 — CSS-like stylesheet files or selector cascade** (ADR-0001 N6). Styles are Go
  values; themes are Go structs. A text stylesheet loader could sit on top later.
- **N2 — Automatic shade generation** (Textual's `-lighten-1..3`/`-darken-1..3`). Needs
  HSL/oklch color math and only makes sense for truecolor themes; it contradicts
  ANSI-16-first (G7). §2.5 keeps the derived set minimal and explicit instead.
- **N3 — Layout via style.** `Width/Height/Align/Padding/Margin` on `Style` describe how
  a widget paints *within* the rect layout gave it; they do not feed the constraint pass.
  Layout is ADR-0004's flex+dock; lipgloss's string-composition role (`JoinHorizontal`,
  `Place`) is not reproduced — the component tree composes.
- **N4 — Terminal palette manipulation** (OSC 4/10/11 *setting*). We read the background
  (ADR-0002 probe) but never rewrite the user's palette.

## 2. Decision

### 2.1 `style.Style`: typed private fields + a set-bitfield, immutable value

```go
package style

// propKey identifies one settable property; a bit in Style.props.
type propKey uint64

const (
    propForeground propKey = 1 << iota
    propBackground
    propBold
    propItalic
    propUnderline
    propStrikethrough
    propReverse
    propBlink
    propFaint
    propPaddingTop
    propPaddingRight
    propPaddingBottom
    propPaddingLeft
    propMarginTop
    propMarginRight
    propMarginBottom
    propMarginLeft
    propWidth
    propHeight
    propMaxWidth
    propMaxHeight
    propAlignHorizontal
    propAlignVertical
    propBorderStyle
    propBorderTop
    propBorderRight
    propBorderBottom
    propBorderLeft
    propBorderTopForeground
    propBorderRightForeground
    propBorderBottomForeground
    propBorderLeftForeground
    // …future bits appended here; 64 slots, ~32 used in v1.
)

// Style is an immutable style definition. The zero value is a valid empty style.
// Copy by assignment; every setter returns a new Style by value.
type Style struct {
    props propKey // bitfield: which properties are explicitly set

    fg, bg     Color
    attrs      uint16 // bold/italic/… packed bools (values; set-ness lives in props)
    padding    [4]int16
    margin     [4]int16
    width, height, maxWidth, maxHeight int16
    alignH, alignV Align
    border     BorderStyle
    borderFg   [4]Color // per-edge border foreground

    extras *extraProps // §2.7 escape hatch; nil in the common case
}

func (s Style) isSet(k propKey) bool { return s.props&k != 0 }
func (s *Style) set(k propKey)       { s.props |= k }   // internal; on the copy
func (s *Style) unset(k propKey)     { s.props &^= k }
```

Design points:

- **Immutability is structural, not disciplinary.** A setter receives `Style` by value,
  mutates the copy, returns it. Assignment is a full deep copy because every field is a
  value (the one pointer, `extras`, is copy-on-write — §2.7). This is exactly what
  lipgloss v0.11 bought by abandoning the map: `Copy()` became assignment and was
  deprecated (https://raw.githubusercontent.com/charmbracelet/lipgloss/v0.11.0/style.go,
  release notes https://github.com/charmbracelet/lipgloss/releases/tag/v0.11.0).
- **Set-ness ≠ zero value.** `Bold(false)` explicitly sets bold-off (props bit on, attr
  bit off); an untouched style has the bit off. `Inherit` and `Unset*` are defined over
  the props bitfield, giving the exact "is this property explicitly set" semantics the
  property bag was chosen for — at the cost of one `uint64` AND.
- **Comparability: `Style` MUST remain comparable** (usable with `==` and as a map key).
  Constraint enforced by construction: no map/slice/func fields directly on `Style`;
  `Color`, `BorderStyle`, `Align` are flat comparable structs/ints; the `extras` map
  hides behind a pointer, and pointer fields are comparable (identity). Consequence:
  two styles differing only in *equal but distinct* extras maps compare unequal —
  documented and acceptable, since extras are the escape hatch, not the core (§2.7).
  This is what makes the ADR-0003 resolved-attribute cache a plain
  `map[cacheKey]CellAttrs` with `cacheKey{Style, themeGen, profile}` (§2.6).

### 2.2 The property-bag proposal, addressed on evidence

The task's objectives 11–12 proposed `map[string]interface{}` with string keys, for
forward-compatible extensibility. This subsection is the direct answer; the design is
rejected, and the rejection is not speculative — **the ecosystem ran this exact
experiment in production and published the reversal.**

**The record.** lipgloss stored styles as a property bag from its first release through
v0.10: `rules map[propKey]interface{}` with every getter doing a map lookup + type
assertion and every setter boxing the value into `interface{}`
(https://raw.githubusercontent.com/charmbracelet/lipgloss/v0.10.0/style.go). In
**v0.11.0 (May 2024)** the entire storage layer was rewritten to explicit typed private
fields plus an `int64` set-bitfield — `propKey = 1 << iota`, `props.has/set/unset` —
(https://raw.githubusercontent.com/charmbracelet/lipgloss/v0.11.0/style.go;
https://github.com/charmbracelet/lipgloss/releases/tag/v0.11.0, PRs #276/#268). The
stated and structural motivations, all of which apply to `tui/style` with more force
because our styles resolve inside a per-frame render loop (ADR-0003), were:

1. **Immutability.** A struct of values copies by assignment — true value semantics for
   free. A map field means every "copy" is either a shared-aliasing bug or an O(props)
   map iteration + reallocation (`Copy()` existed *because* of the map, and was
   deprecated the release the map died).
2. **Allocation.** Boxing into `interface{}` allocates on every set; every get is a map
   lookup + type assertion. Styles are *read* in the render hot loop — resolution walks
   every set property once per `Render` — so the bag design pays hash + assertion per
   property per frame. Field reads are load instructions.
3. **GC pressure.** Hundreds of widgets × a map per style × copies per configuration
   chain is sustained garbage; a value struct is zero heap.
4. **Compile-time safety.** With string keys, a typo'd key or a wrong-typed value
   compiles fine and fails (or worse, silently no-ops) at runtime. With typed fields the
   compiler rejects both. golib's philosophy is fail-loud-fail-typed at the earliest
   possible moment (brief §2); runtime string dispatch is the opposite.

**The bag's load-bearing semantics survive the rewrite.** The honest question is not
"is the map slower" but "does the bag buy semantics the struct can't express":

- *"Is this property explicitly set?"* — needed by `Inherit`/cascade and `Unset*`. Map
  membership expressed this; the props bitfield expresses it identically (`s.isSet(k)`)
  in one AND. Nothing is lost.
- *Additive evolution.* New properties in the bag world are new string keys. In the
  bitfield world they are a new `propKey` bit plus a new typed private field — a
  backward-compatible, purely additive change, because **the struct layout is private**:
  no external code names `Style`'s fields, so adding fields breaks nobody. 64 bits with
  ~32 used leaves ample headroom; if exceeded, `props` becomes `[2]uint64` behind the
  same three private methods, still comparable, still invisible externally.
- *Third-party extension* (properties the core doesn't know about) is the one thing the
  struct genuinely can't do — answered by §2.7 without taxing the core.

**Verdict:** typed fields + bitfield, per ADR-0001 §2.4(pt. via §4.7). The property bag
is recorded as Alternative 1 in §4 with a pointer here.

### 2.3 API surface (lipgloss-familiar, per ADR-0001 §2.3)

All setters are value-receiver, return `Style`.

```go
func New() Style

// Color & attributes. ColorSpec is satisfied by Color and Token (§2.4/§2.5).
func (s Style) Foreground(c ColorSpec) Style
func (s Style) Background(c ColorSpec) Style
func (s Style) Bold(v bool) Style
func (s Style) Italic(v bool) Style
func (s Style) Underline(v bool) Style
func (s Style) Strikethrough(v bool) Style
func (s Style) Reverse(v bool) Style
func (s Style) Blink(v bool) Style
func (s Style) Faint(v bool) Style

// Box. Variadic CSS shorthand: 1 arg=all, 2=v/h, 3=t/h/b, 4=t/r/b/l; 0 or >4 panics
// (misconfiguration fails loud at construction — golib convention).
func (s Style) Padding(sides ...int) Style
func (s Style) Margin(sides ...int) Style
func (s Style) Width(w int) Style
func (s Style) Height(h int) Style
func (s Style) MaxWidth(w int) Style
func (s Style) MaxHeight(h int) Style
func (s Style) Align(h Align, v ...Align) Style // AlignLeft/Center/Right; Top/Middle/Bottom

// Borders. edges variadic bools follow the CSS shorthand rule above; no args = all edges.
func (s Style) Border(b BorderStyle, edges ...bool) Style
func (s Style) BorderForeground(c ColorSpec) Style // all four edges
func (s Style) BorderTopForeground(c ColorSpec) Style // + Right/Bottom/Left variants

// Inherit copies from other ONLY the properties not already set on s.
// Margins and padding are NEVER inherited (they are placement, not appearance) —
// the lipgloss rule, kept verbatim.
func (s Style) Inherit(other Style) Style

// Unset* clears the props bit and zeroes the field; one per property, e.g.:
func (s Style) UnsetForeground() Style
func (s Style) UnsetPadding() Style // clears all four sides
// …

// Getters mirror setters: GetForeground() (Color, bool) — bool is the is-set flag.
// Frame math (used by ADR-0004 layout and ADR-0007 Box to convert outer↔content rects):
func (s Style) GetHorizontalFrameSize() int // left+right margin + border + padding
func (s Style) GetVerticalFrameSize() int
func (s Style) GetHorizontalPadding() int
func (s Style) GetVerticalPadding() int
func (s Style) GetHorizontalBorderSize() int
func (s Style) GetVerticalBorderSize() int
```

`BorderStyle` is a comparable struct of edge/corner grapheme strings with the standard
prefabs as package vars (`BorderNormal`, `BorderRounded`, `BorderThick`, `BorderDouble`,
`BorderHidden` — hidden paints spaces, preserving frame size for alignment stability):

```go
type BorderStyle struct {
    Top, Bottom, Left, Right                     string
    TopLeft, TopRight, BottomLeft, BottomRight   string
}
```

(Strings, not runes: border pieces may be multi-byte graphemes; cells hold grapheme
strings per ADR-0003.) The box model is lipgloss's, kept verbatim so frame math is
unsurprising: content → padding → border → margin, border outside padding, margin
outside border
(https://raw.githubusercontent.com/charmbracelet/lipgloss/v0.11.0/style.go render order).

### 2.4 `style.Color`: a resolvable sum type, ANSI-16-first

```go
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
// profile (ADR-0002). The zero Color means "unset / terminal default" (SGR 39/49).
type Color struct {
    kind        colorKind
    light, dark colorLeaf // dark used only for kindAdaptive; light is the sole leaf otherwise
}

type colorLeaf struct {
    kind    colorKind // kindDefault | kindANSI | kindANSI256 | kindRGB only
    n       uint8     // ANSI-16 index (0–15) or ANSI-256 index
    r, g, b uint8
}

// Constructors (the only way to make a Color; fields are private):
func ANSI(n int) Color                  // 0–15; panics outside range
func ANSI256(n int) Color               // 0–255
func RGB(r, g, b uint8) Color           // truecolor
func Adaptive(light, dark Color) Color  // picks by background darkness at resolve time;
                                        // adaptive args are flattened to their own variant
                                        // (no nesting), so Color stays a flat comparable value.
                                        // Token leaves are REJECTED in v1 — see below.
func Default() Color                    // explicit terminal-default fg/bg

// ColorSpec is what Foreground/Background/Border*Foreground accept: a literal Color
// or a theme Token. Both flatten into the internal Color representation at set time,
// so Style never stores an interface and the resolve hot path never type-switches.
type ColorSpec interface{ spec() Color }
func (c Color) spec() Color { return c }
func (t Token) spec() Color { return Color{kind: kindToken, light: colorLeaf{n: uint8(t)}} }
```

> **Rev 1 (Lector must-fix #1).** `kindToken` was referenced by `Token.spec()` without
> being declared in the `colorKind` enum; the enum is now spelled out in full, with the
> Color-level-only kinds (`kindAdaptive`, `kindToken`) marked as such.

**`Adaptive` rejects token leaves (v1).** Resolution stays single-pass over flat
comparable values: a token inside an adaptive pair would require token lookup *then* an
adaptive pick *then* downsampling, and would force `colorLeaf` to represent tokens.
Rejection is primarily **compile-shape prevention** — `Adaptive` takes `Color`
parameters, `Token` is not a `Color`, and `ColorSpec.spec()` is unexported, so user code
cannot express a token leaf at the API surface. Belt-and-braces, `Adaptive` panics if
handed a `kindToken` or `kindAdaptive` Color (misconfiguration fails loud at
construction — golib convention). Theme authors who want adaptive *tokens* put the
`Adaptive(...)` color in the Theme slot — adaptivity then lives behind the token, where
it resolves in the same single pass.

> **Rev 1 (Lector r1 Q4 answer folded).** Token-in-`Adaptive` was posed as Q4; Lector:
> reject for v1, keep resolution single-pass and representable by flat comparable
> values. The paragraph above is now normative.

**Resolution** happens once per style per render (§2.6), through ADR-0002's canonical
capability model — `Capabilities.ColorProfile` of type `ColorProfile uint8`
(`ProfileMono | ProfileANSI16 | ProfileANSI256 | ProfileTrueColor`) and
`Capabilities.DarkBackground bool` (set by the OSC 10/11 probe):

1. `kindToken` → look up the active `Theme` (§2.5) → a concrete `Color`.
2. `kindAdaptive` → pick `light` or `dark` leaf by `Capabilities.DarkBackground`.
3. Downsample to `Capabilities.ColorProfile`: **`ProfileTrueColor` → `ProfileANSI256`**
   (nearest in the 6×6×6 cube plus the 24-step gray ramp, Euclidean in RGB —
   deterministic table math, no color-science dependency) → **`ProfileANSI256` →
   `ProfileANSI16`** (static 256→16 mapping table) → **`ProfileANSI16` →
   `ProfileMono`** (drop color, keep attributes). A color never upsamples; an ANSI-16
   color on a truecolor terminal emits the ANSI-16 SGR so the user's palette renders it.

> **Rev 1 (Lector must-fix #1).** This section previously named a phantom profile shape
> (`{Mono, ANSI16, ANSI256, TrueColor}`); it now consumes exactly the fields ADR-0002
> (as amended in r1) defines: `Capabilities.ColorProfile` with the `Profile*` constants
> and `Capabilities.DarkBackground`.

**ANSI-16-first token philosophy (G7).** The default theme's tokens resolve to ANSI-16
indices, *not* truecolor values. Rationale is accessibility and user sovereignty:
ANSI-16-indexed output respects the palette, contrast, and colorblind-safe scheme the
user configured in their terminal, whereas fixed truecolor themes override them
(ratatui's accessibility discussion,
https://github.com/ratatui/ratatui/discussions/877, and the palette-convention record at
https://github.com/termstandard/colors). Truecolor themes are constructed explicitly
(`Theme` fields set to `RGB(...)` colors) — opting in is one struct literal, but it is
an authoring decision, never a default.

### 2.5 Tokens & `style.Theme`

```go
// Token is a semantic color slot, resolved through the active Theme at render time.
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
    TokenBoost      // translucent-overlay analogue: a slightly offset surface

    // Derived variants — the deliberately minimal set (see below):
    TokenTextMuted       // de-emphasized text color on Background/Surface (color ONLY — see below)
    TokenTextOnPrimary   // readable text atop Primary fills
    TokenTextOnSecondary // readable text atop Secondary fills (Select/List secondary selection fills, ADR-0007)
    TokenTextOnAccent
    TokenTextOnError
    TokenTextOnSuccess
    TokenTextOnWarning
    TokenBorder          // default border color (Box and friends, ADR-0007)
    TokenBorderFocused   // border color of the focused panel (ADR-0007 Box focus visuals)

    numTokens // internal bound
)

// Theme maps tokens to Colors. Only Primary is required; every other slot has a
// documented derivation default applied by NewTheme. Theme values are immutable
// after construction.
type Theme struct { /* private [numTokens]Color, built by NewTheme */ }

func NewTheme(primary Color, opts ...ThemeOption) Theme
func WithToken(t Token, c Color) ThemeOption
func WithDark(dark bool) ThemeOption // force adaptivity instead of probing
func (th Theme) Color(t Token) Color
```

**Derivation defaults** (applied for any slot not supplied; each is one line of
documented, deterministic logic — no color math):

| Slot | Default |
|------|---------|
| Secondary, Accent | Primary |
| Foreground, Background | `Default()` (terminal's own fg/bg) |
| Surface | Background |
| Panel, Boost | Surface |
| Warning / Error / Success | `ANSI(3)` / `ANSI(1)` / `ANSI(2)` |
| TextMuted | Foreground (color only — see the normative rule below) |
| TextOn* | Background (i.e. inverted text on a colored fill) |
| Border | Foreground |
| BorderFocused | Accent |

**Tokens are colors, never attributes (normative).** A `Token` resolves to exactly one
`Color`; the resolver never attaches SGR attributes on a token's behalf. `TokenTextMuted`
therefore defaults to the Foreground *color*, and widgets that want faint muted text say
so themselves: `style.New().Foreground(style.TokenTextMuted).Faint(true)`. Mixing an
attribute into a color token would make `ColorSpec` semantics surprising — a
"color" argument that also mutates the attribute set — and would break the resolve
pipeline's clean color-only downsampling chain (§2.4).

> **Rev 1 (Lector should-fix, Q2/Q3 answers folded).** (a) `TokenBorder` and
> `TokenBorderFocused` added — load-bearing per Lector; ADR-0007's Box focus visuals now
> reference `TokenBorderFocused` instead of hardcoding `TokenAccent`. (b)
> `TokenTextOnSecondary` added — ADR-0007's Select/List do use Secondary fills.
> (c) The draft had the resolver adding SGR faint for `TokenTextMuted`; that attribute
> semantics is removed per Q3 — the normative color-only rule above replaces it.

**Why this derived set and not Textual's auto-generation.** Textual derives
`-lighten-1..3`/`-darken-1..3` shades and a dozen semantic text variants automatically
(https://textual.textualize.io/guide/design/), which requires real color math (HSL
manipulation) and only produces sensible results for truecolor themes — under G7's
ANSI-16-first defaults there is nothing to lighten (index 4 has no "10% lighter"). The
nine derived slots above are the ones ADR-0007's widget set actually consumes (muted
placeholder/status text; readable text on Primary/Secondary/Accent selection fills and
on status-colored fills; default and focused panel borders); each is one explicit Theme
slot a truecolor theme can override with properly computed values if its author wants
Textual-grade polish. Growing the derived set is an additive, compatible change (new
`Token` constants before `numTokens` plus a derivation row).

**Theme switching at runtime** is an App-level operation (ADR-0005):
`App.SetTheme(th)` runs on the loop goroutine, swaps the active theme, bumps the theme
generation counter (invalidating every resolution cache entry, §2.6), and marks the root
dirty → one full repaint next frame. Widgets never observe the theme directly; they
carry Tokens and the resolver does the lookup, so a theme swap requires zero widget
cooperation. Light/dark adaptivity: the OSC 10/11 background probe (ADR-0002) sets
`Capabilities.DarkBackground` at startup; terminals that report background *changes*
(rare) surface as a capability event that likewise bumps the generation and repaints.

### 2.6 Rendering integration: resolve-once, cacheable by value

Styles attach to cells through the ADR-0003 pipeline: a component's `Render(s Surface)`
paints grapheme strings with a `Style`; the Surface resolves the Style to a concrete
`CellAttrs` (packed fg/bg in output form + attribute bits — ADR-0003's cell payload)
and stamps it on cells.

```go
// In package tui (ADR-0003); shown here for the contract:
// resolve is pure: (Style, theme generation, capability profile, dark) → CellAttrs.
func resolveStyle(st style.Style, rc ResolveContext) CellAttrs
```

- **Resolve once per Render call, not per cell.** A widget painting 2,000 cells with
  one style pays one resolution.
- **Cacheable because Style is a comparable value (§2.1).** The runtime keeps a small
  resolution cache `map[resolveKey]CellAttrs` with
  `resolveKey{st style.Style; themeGen uint32; profile tui.ColorProfile; dark bool}` —
  `profile` and `dark` are copied from ADR-0002's `Capabilities.ColorProfile` /
  `Capabilities.DarkBackground` (rev 1: the canonical fields, no phantom shape) — and
  the key is legal as a map key precisely because `Style` is comparable. The cache is owned by the
  render layer, bounded, and flushed on theme-generation bump. Styles carrying a
  non-nil `extras` pointer still work as keys (pointer identity) but third-party
  extra properties are **never consulted by the core resolver** — see §2.7 — so cache
  correctness doesn't depend on extras contents.
- The decision that **Style must remain comparable** is binding on all future property
  additions: any new property whose natural value is a slice/map/func must either be
  interned behind a comparable handle or live in `extras`.

### 2.7 Extensibility: `StyleOption` hooks + one `extras` escape hatch

G8, and the honest residue of the property-bag debate (§2.2): third parties cannot add
typed fields to `Style`. Two mechanisms, neither taxing the core:

```go
// StyleOption composes third-party style transformations into a fluent chain.
type StyleOption func(*Style)

// Apply runs opts against a copy — the value-semantics-preserving extension seam.
func (s Style) Apply(opts ...StyleOption) Style

// ExtKey namespaces third-party extra properties. Comparable.
type ExtKey struct{ Pkg, Name string }

// Ext sets an extra property (copy-on-write: the extras map is cloned, never
// mutated in place, preserving value semantics). GetExt reads one.
func (s Style) Ext(k ExtKey, v any) Style
func (s Style) GetExt(k ExtKey) (any, bool)
```

- `extras *extraProps` (wrapping `map[ExtKey]any`) is **nil in the common case** —
  styles that never touch `Ext` carry one nil pointer and zero overhead.
- **The core render path never consults extras.** Only a renderer/Backend that opts in
  (e.g. a hypothetical sixel-underline extension reading
  `ExtKey{"acme/tuix", "underline-color"}`) reads them, in its own code. Core
  resolution cost is therefore identical whether extras exist or not.
- Copy-on-write on `Ext` keeps immutability honest at the cost of an O(extras) clone
  per `Ext` call — acceptable because extras are rare configuration-time operations,
  the exact inversion of the property bag's cost model (which charged *every* style
  *every* frame for extensibility *nobody* was using). Chaining N `Ext` calls is
  therefore O(N²); **`Apply(opts ...StyleOption)` is the documented batching path** for
  multiple extensions — the option functions receive the *same* working copy, so a
  third-party package exposing `tuix.WithUnderlineColor(...) StyleOption` etc. lets N
  extension properties land in one clone:
  `st.Apply(tuix.WithUnderlineColor(c), tuix.WithHyperlink(url))`.

> **Rev 1 (Lector r1 Q5 answer folded).** The O(N²) copy-on-write is accepted as-is for
> the escape hatch; no `Exts(map[ExtKey]any)` variant is added. `Apply` is now
> explicitly documented (above) as the batching path.

## 3. Consequences

**Positive**

- The render hot loop reads plain struct fields; style resolution is branch-y integer
  work with zero allocation and no map traffic (G3) — the profiling pathology the
  dossier flags in real bubbletea apps (~25% of frame time in lipgloss string/style ops,
  https://eieio.games/blog/secure-massively-multiplayer-snake/) is structurally avoided.
- Typo'd properties and wrong-typed values are compile errors (G4); `Unset`/`Inherit`
  semantics are explicit and table-testable via the props bitfield.
- `Style` comparability makes resolved-attribute caching a plain map and makes styles
  usable in table-driven test expectations with `==` (golib std-testing culture).
- Theme swapping is O(1) + one repaint, with zero widget cooperation.
- ANSI-16-first defaults keep golib TUIs legible under user-tuned palettes,
  high-contrast schemes, and colorblind-safe terminal themes out of the box.

**Negative (costs)**

- ~32 `propKey` bits and their setter/getter/unset triples are boilerplate (~500 lines
  of mechanical code). Mitigation: it is trivially reviewable, and lipgloss maintains
  the same surface by hand.
- Third parties get a two-tier system: first-class typed properties only via upstream
  golib changes; everything else through `extras` with runtime-typed values. This is
  deliberate (§2.2) but is a real asymmetry.
- Comparability is a standing constraint on evolution (§2.6): future properties must be
  comparable values. Border already forced this shape (strings, not slices).
- The minimal derived-token set means truecolor theme authors wanting Textual-grade
  shade ramps must compute them externally; we trade that polish for zero color-math
  code and G7 coherence.

**Evolution**

- New core properties: new bit + field + setter triple; additive, layout-private, no
  break. Past 64 bits: `props [2]uint64`, private, no break.
- New tokens: append before `numTokens` + a derivation row; additive.
- A stylesheet/selector layer (N1) or auto-shade derivation for truecolor themes (N2)
  can both be built on top of `Theme`/`Token` without touching `Style`.

## 4. Alternatives considered

1. **`map[string]interface{}` property bag (the task's objectives 11–12 proposal).**
   Rejected on the production evidence detailed in §2.2: lipgloss shipped this exact
   design through v0.10 and rewrote to typed-fields+bitfield in v0.11.0 (May 2024) for
   immutability (value copy vs map iteration + realloc), allocation (boxing per set,
   lookup + assertion per get in the render hot loop), GC pressure, and compile-time
   safety (v0.10.0 vs v0.11.0 style.go; release notes). The bag's load-bearing
   semantics — is-set tracking for cascade, additive evolution — are fully preserved by
   the bitfield + private struct layout; its one unique power (third-party properties)
   is preserved by §2.7 at zero core cost.
2. **`map[propKey]interface{}` (typed keys, bag storage).** lipgloss's actual pre-v0.11
   design. Fixes the typo'd-key class only; boxing, per-get lookup/assertion, O(props)
   copies, GC pressure, and non-comparability all remain. Rejected.
3. **Pointer-based builder (`*Style` with fluent mutating setters).** One alloc per
   style, aliasing hazards (two widgets sharing a `*Style` see each other's edits),
   needs an explicit `Clone()`, not comparable, not safely shareable across the tree.
   Value semantics won this argument in lipgloss's history already. Rejected.
4. **Interface-typed color fields (`fg ColorSpec` stored directly).** Simplest way to
   accept `Color`-or-`Token`, but stores an interface in `Style` — boxing on set,
   dynamic dispatch on resolve, and comparability now depends on dynamic types.
   Rejected for the flatten-at-set-time representation (§2.4) that keeps `Style` flat.
5. **Adopting Textual's full auto-derivation theme system.** Requires HSL color math
   and truecolor-only assumptions; contradicts G7 (ANSI-16-first) and N2. The 11-slot
   *vocabulary* is adopted; the *generation machinery* is not. Rejected (minimal
   explicit derived set instead, §2.5).
6. **base16/base24 as the token vocabulary** (https://github.com/tinted-theming/home).
   Portable and well-themed, but its slots are palette-positional (base00–base0F), not
   semantic; widget code wants `TokenError`, not `base08`. A base16 *importer* producing
   a `Theme` is a trivial future add-on. Rejected as the primary vocabulary.
7. **Styles resolved eagerly at set time** (store output-form SGR attrs directly).
   Removes render-time resolution but destroys theming (tokens must survive until
   resolve), adaptivity (light/dark decided too early), and capability degradation
   (profile unknown at authoring time). Rejected — deferred resolution is the point.

## 5. Acceptance criteria

1. `tui/style` compiles with stdlib imports only; `go vet` and `go test ./tui/style/...`
   pass with std `testing` (table-driven), no third-party test deps.
2. `Style` is comparable: a test uses `Style` values as map keys and asserts `==`
   equality for identically-built chains, and inequality after any single setter.
3. Value semantics proven by test: mutate-after-copy independence — `b := a.Bold(true)`
   leaves `a` unchanged (props bit and attr both).
4. `Inherit` tests: only unset receiver props are copied; margins and padding are never
   inherited; `Unset*` then `Inherit` re-admits a property.
5. Set-ness distinct from zero value: `New().Bold(false)` reports
   `GetBold() = (false, true)`; `New()` reports `(false, false)`.
6. Resolution downsampling table-tests: for each `tui.ColorProfile` value
   (`ProfileTrueColor`/`ProfileANSI256`/`ProfileANSI16`/`ProfileMono`, per ADR-0002
   rev 1) × each Color kind (default/ANSI/256/RGB/adaptive/token), the emitted
   attribute form matches the documented chain; ANSI-16 colors are emitted as ANSI-16
   SGR even on truecolor terminals (no upsampling). `Adaptive` picks by
   `Capabilities.DarkBackground`; `Adaptive` handed a token- or adaptive-kind Color
   panics at construction (§2.4).
7. Default `Theme` derivation: `NewTheme(ANSI(4))` yields the documented defaults for
   all 20 slots (11 base + 9 derived, incl. `TokenBorder`/`TokenBorderFocused`/
   `TokenTextOnSecondary`); every default token resolves to `kindANSI` or `kindDefault`
   (G7 — no truecolor in the default theme); no token resolution ever contributes SGR
   attributes (`TokenTextMuted` is color-only — §2.5 normative rule).
8. Benchmarks co-located (golib convention): `BenchmarkStyleSet` (one setter) and
   `BenchmarkResolve` (full resolve, no cache) report 0 allocs/op with `extras == nil`.
9. A style with `Ext(...)` set renders identically to the same style without it through
   the core resolver (extras never consulted), and `Ext` does not mutate the receiver's
   map (copy-on-write test).
10. Frame-math getters agree with the box model: for a style with known
    padding/border/margin, `GetHorizontalFrameSize()` equals the measured difference
    between outer and content widths in a `TestBackend` render (cross-checked with
    ADR-0007 `Box`).

## 6. Questions for the reviewer

- **Q1.** Comparability (§2.1/§2.6) is adopted as a *binding* constraint — it buys
  map-key caching and `==` in tests, but permanently constrains property types
  (everything must be a comparable value; `BorderStyle` is already shaped by this). Is
  that trade correct, or should we drop the guarantee now (freeing future properties to
  hold slices) and key caches by an explicit `Hash() uint64` instead?
  — **Lector r1:** keep comparability as a binding constraint; it is a real simplifier
  for diffing, caching, and tests — future slice/map-shaped properties go through
  handles or `extras`.
- **Q2.** The derived-token set (§2.5) was six slots, chosen as "what ADR-0007's widgets
  consume." Textual ships ~20+ derived variables. Is six too austere for real theme
  authors — specifically, should `TokenTextOnSecondary` and a `TokenBorder` /
  `TokenBorderFocused` pair be added now, given ADR-0007's Box focus-visuals will
  otherwise hardcode `TokenAccent` for focused borders?
  — **Lector r1:** add `TokenBorder` + `TokenBorderFocused` now (load-bearing) and
  `TokenTextOnSecondary` since Select/List secondary fills are expected. Folded in §2.5.
- **Q3.** `TokenTextMuted` resolved (in the draft) to Foreground's color plus
  resolver-added SGR faint — a token carrying an *attribute*, not just a color. Is that
  acceptable token semantics, or should tokens be strictly colors (making muted text a
  widget-side `.Faint(true)` convention instead)?
  — **Lector r1:** tokens are colors only; muted text is a widget/style convention via
  `.Faint(true)`. Folded as the normative rule in §2.5.
- **Q4.** The `ColorSpec` flatten-at-set-time trick (§2.4) means `Token` is representable
  *inside* `Color` (`kindToken`), so token-in-adaptive nestings are conceivably
  expressible. Should `Adaptive` accept tokens (resolve order: token → adaptive pick →
  downsample), or reject them at construction to keep resolution single-pass?
  — **Lector r1:** reject tokens inside `Adaptive` for v1; keep resolution single-pass
  over flat comparable values. Folded in §2.4 (compile-shape prevention + panic).
- **Q5.** `Ext`'s copy-on-write clone per call (§2.7) preserves immutability but makes
  building a style with N extras O(N²). Acceptable for an escape hatch, or should
  `Apply` batch extras into one clone (an `Exts(map[ExtKey]any)` variant)?
  — **Lector r1:** O(N²) accepted for the escape hatch; document `Apply(opts...)` as the
  batching path for multiple extensions. Folded in §2.7; no `Exts` variant.
