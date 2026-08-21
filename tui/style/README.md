# tui/style

The styling, design-token, and theming layer of `golib/tui` (ADR-0006).
Stdlib-only, imports no other tui package; the render-time resolver
(`Style` → cell attributes) lives in package `tui` and consumes this
package's exported read surface.

```bash
go get github.com/yongjohnlee80/golib/tui/style
```

```go
import "github.com/yongjohnlee80/golib/tui/style"
```

## Style

An immutable value with a fluent, lipgloss-familiar surface. Setters take
the receiver by value, mutate the copy, and return it — assignment is a deep
copy, there is no `Copy()`, and derived styles never alias their parents:

```go
st := style.New().
    Foreground(style.TokenPrimary).   // ColorSpec: a Color or a theme Token
    Background(style.ANSI(0)).
    Bold(true).
    Padding(1, 2).                    // CSS shorthand: 1=all, 2=v/h, 3=t/h/b, 4=t/r/b/l
    Border(style.BorderRounded).      // edges variadic: no args = all four
    Align(style.AlignCenter)

focused := st.BorderForeground(style.TokenBorderFocused) // st is untouched
```

Storage is typed private fields plus a `uint64` set-bitfield (the lipgloss
v0.11 model), so **"explicitly set" is tracked per property**, distinct from
the zero value:

```go
style.New().GetBold()            // (false, false) — untouched
style.New().Bold(false).GetBold() // (false, true) — explicitly bold-off
```

- `Inherit(other)` copies from `other` **only the properties not already set
  on the receiver** — and never margins or padding (placement, not
  appearance; the lipgloss rule, kept verbatim).
- `Unset*` clears the set bit _and_ zeroes the value, so
  `st.Bold(true).UnsetBold() == style.New()`.
- **`Style` is comparable** — `==` and map keys work, which is what makes the
  tui resolver's attribute cache a plain map. Binding constraint: future
  properties must be comparable values.
- Frame math for outer↔content rect conversion (box model: content → padding
  → border → margin): `GetHorizontalFrameSize`, `GetVerticalFrameSize`,
  `GetHorizontalPadding`, `GetVerticalPadding`, `GetHorizontalBorderSize`,
  `GetVerticalBorderSize`.

One setter is ~18ns, 0 allocs; an is-set check is one `uint64` AND.

## Color

A flat comparable sum type, resolved at render time against the terminal's
capability profile (ADR-0002) — truecolor → 256 → 16 → mono, never
upsampling (an ANSI-16 color always emits the ANSI-16 SGR so the user's own
palette renders it):

```go
style.Default()               // terminal default fg/bg (the zero Color)
style.ANSI(4)                 // ANSI-16 palette index; panics outside 0-15
style.ANSI256(214)            // ANSI-256 index; panics outside 0-255
style.RGB(0x87, 0x5f, 0xff)   // truecolor
style.Adaptive(light, dark)   // picked by background darkness at resolve time
```

`Adaptive` **rejects token and adaptive leaves** (panics at construction):
resolution stays single-pass over flat values. Themes that want adaptive
tokens put the `Adaptive(...)` color in the Theme slot instead.

Color setters accept a `ColorSpec` — a `Color` or a `Token` — and flatten it
at set time, so `Style` never stores an interface.

## Tokens & themes

Widgets style themselves in semantic terms; swapping the active `Theme`
re-skins the whole application (the swap itself is App-level, ADR-0005).
Eleven base slots (Textual's vocabulary: `TokenPrimary`, `TokenSecondary`,
`TokenForeground`, `TokenBackground`, `TokenSurface`, `TokenPanel`,
`TokenWarning`, `TokenError`, `TokenSuccess`, `TokenAccent`, `TokenBoost`)
plus nine derived slots (`TokenTextMuted`, `TokenTextOnPrimary`,
`TokenTextOnSecondary`, `TokenTextOnAccent`, `TokenTextOnError`,
`TokenTextOnSuccess`, `TokenTextOnWarning`, `TokenBorder`,
`TokenBorderFocused`).

Only Primary is required; every other slot has a documented derivation
default (no color math):

```go
th := style.NewTheme(style.ANSI(4))                    // == style.DefaultTheme()
th = style.NewTheme(style.RGB(0x7a, 0x5c, 0xff),       // truecolor is an explicit opt-in
    style.WithToken(style.TokenSurface, style.ANSI256(236)),
    style.WithDark(true),                              // force adaptivity instead of probing
)
```

| Slot                      | Default                            |
| ------------------------- | ---------------------------------- |
| Secondary, Accent         | Primary                            |
| Foreground, Background    | `Default()` (terminal's own fg/bg) |
| Surface                   | Background                         |
| Panel, Boost              | Surface                            |
| Warning / Error / Success | `ANSI(3)` / `ANSI(1)` / `ANSI(2)`  |
| TextMuted                 | Foreground (color only)            |
| TextOn\*                  | Background                         |
| Border                    | Foreground                         |
| BorderFocused             | Accent                             |

Derivations cascade: overriding Surface also moves the Panel and Boost
defaults. The default theme is ANSI-16-first (G7): every slot resolves to an
ANSI-16 index or the terminal default, respecting the user's palette,
contrast, and colorblind-safe scheme.

**Tokens are colors, never attributes.** `TokenTextMuted` is the muted text
_color_; widgets that want faint muted text say so themselves:

```go
style.New().Foreground(style.TokenTextMuted).Faint(true)
```

## Borders

`BorderStyle` is a comparable struct of edge/corner grapheme strings, with
prefabs `BorderNormal`, `BorderRounded`, `BorderThick`, `BorderDouble`, and
`BorderHidden` — hidden paints spaces, preserving frame size so focus/blur
border swaps don't shift content.

## Extensibility

Third parties can't add typed fields to `Style`; two seams cover them
without taxing the core (the render path never consults extras; styles that
never touch `Ext` carry one nil pointer):

```go
key := style.ExtKey{Pkg: "acme/tuix", Name: "underline-color"}
st = st.Ext(key, myColor)      // copy-on-write; receiver untouched
v, ok := st.GetExt(key)

// Batching: N extensions land in ONE clone (Ext-chaining would be O(N²)).
st = st.Apply(tuix.WithUnderlineColor(c), tuix.WithHyperlink(url))
```

`StyleOption` (`func(*Style)`) composes any transformation — core setters
included — and `Apply` runs the options against a private working copy, so
value semantics hold end to end.

## Resolver read surface

The resolution pipeline (token lookup → adaptive pick → capability
downsample) runs in package `tui`. What it reads from here: the `Get*`
getters on `Style`; `Color.IsDefault` / `Color.Token` / `Color.AdaptivePair`
/ `Color.ANSIIndex` / `Color.ANSI256Index` / `Color.RGBValues` (exactly one
reports ok per Color); `Theme.Color` and `Theme.Dark`. No mutation surface.

## File layout

| File        | Contents                                                                        |
| ----------- | ------------------------------------------------------------------------------- |
| `style.go`  | `Style`, `propKey` bitfield, `New`, fluent setters, `Align`, `Inherit`          |
| `unset.go`  | `Unset*` — one per settable property                                            |
| `get.go`    | `Get*` getters, frame-math getters                                              |
| `color.go`  | `Color`, `colorKind`, constructors, `ColorSpec`, resolver read accessors        |
| `theme.go`  | `Token` vocabulary, `Theme`, `NewTheme`, `WithToken`/`WithDark`, `DefaultTheme` |
| `border.go` | `BorderStyle` + prefabs                                                         |
| `ext.go`    | `StyleOption`, `Apply`, `ExtKey`, `Ext`/`GetExt`                                |

## License

[MIT](../../LICENSE)
