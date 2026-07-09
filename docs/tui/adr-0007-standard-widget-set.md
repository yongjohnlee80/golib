
# ADR-0007 — golib/tui: Standard Widget Set v1

**Tags:** `type:adr` `status:accepted` `owner:shared` `repo:golib` `area:tui` `kind:widgets` `kind:components` `kind:inputs` `kind:chrome`

**Abstract:** Defines the `tui/widget` package: the `widget.Base` embedding contract, the `widget.Box` container (border + in-border title and status line, per the task's "the basic window should already accommodate title and status if configured"), the v1 inventory — TextInput, TextArea, Select, List, BufferView, Tabs, Split, Float, StatusBar, ProgressBar, Text — with per-widget contracts (state, keys consumed, bus events emitted, style hooks, layout behavior), the explicit out-of-v1 list, and a composition check assembling sqlit- and lazygit-shaped UIs from the inventory.

> **Authored natively in the KB** (jarvis, 2026-07-08) as part of the golib/tui design
> dossier. golib/tui keeps its own 0001–0007 ADR numbering; the `golib-tui-` filename
> prefix namespaces it against this KB's auto-agents ADR sequence. Navigation hub:
> golib-tui. Umbrella ADR: golib-tui-0001-overview-and-architecture.

- **Status:** **Accepted** (Johno, 2026-07-08) — revision 1 (Lector r1 `change_requested` → r2 `approved_with_amendments`, all amendments folded).
- **Date:** 2026-07-08
- **Reviewed by:** lector, 2026-07-08 — agents/lector/reviews/2026-07-08-golib-tui-adrs-review.md (r1); agents/lector/reviews/2026-07-08-golib-tui-adrs-rereview.md (r2, `approved_with_amendments`)
- **Module:** github.com/yongjohnlee80/golib
- **Supersedes:** none (greenfield)
- **Related:** ADR-0001 (umbrella; G8 mandates this inventory), ADR-0003 (Surface/cells —
  what widgets paint on; the real-cursor IME rule TextInput/TextArea implement),
  ADR-0004 (Component contract, layout, focus, Stack layers — Select/Float overlays and
  focus traps build on it), ADR-0005 (runtime — Bus events widgets emit, `App.Go`
  async examples, TickEvent for spinners), ADR-0006 (style hooks, tokens, `Box` frame
  math). golib server ADR-0006 (transport scaffold — the functional-options constructor
  convention these widgets follow).

> **Revision history.** rev 1 (2026-07-08): folded Lector r1 `change_requested` —
> (1) **should-fix / Q2**: `BufferView` no longer implements `io.Writer` itself;
> `BufferView.Writer() io.Writer` returns a separate concurrent handle (any-goroutine
> safe, bounded pending bytes, ordered delivery, `ErrClosed` after unmount) so the
> widget value stays loop-owned (§2.4, §2.6, §3, §5.7); (2) **should-fix / Q3 /
> ADR-0001 Q5**: the `ListSource[T]` data-source seam (`Len() int; Item(i int) T` +
> slice adapter) is designed NOW and is v1 API — `List[T]` renders through the source;
> windowed/virtualized *fetching* stays deferred, but no `List` v2 will be needed
> (§1.2 N3, §2.4, §2.7, §5.11); (3) **should-fix + nit (naming)**: swept all code
> fences to ADR-0004's canonical names — `Context.ID()` (was `ctx.NodeID()`),
> `RequestLayout()` (was `MarkLayoutDirty()`), `Context.Ctx()` (was
> `ctx.UnmountCtx()`) (§2.1, §2.2, §2.6); (4) cross-ADR token alignment: Box focus
> visuals reference ADR-0006 rev 1's `TokenBorderFocused` / `TokenBorder` instead of
> hardcoding `TokenAccent` (§2.2). Q1 (single-child Box), Q4 (keep generics), Q5 (no
> TextArea submit key) answered and closed in §6.

> **Self-containment contract.** This ADR is implementable with no prior context beyond
> the golib conventions (golang, golib) and the sibling ADRs listed under Related.

## 1. Context

ADR-0001 G8 requires a standard widget set sufficient to build sqlit/lazygit-shaped
applications out of the box. The originating task enumerates: text input, text area,
dropdown, buffer views, tabs, floating windows, splits, status bar, progress bar, and
titled windows — with the explicit requirement that "the basic window should already
accommodate title and status if configured." `tui/widget` imports `tui` and `tui/style`
only (ADR-0001 §2.2, stdlib beyond that).

Widgets are `tui.Component` implementations (ADR-0004): retained, mutable, living on the
loop goroutine, painting into `Surface`s, participating in constraints-down/sizes-up
layout, receiving routed events, and emitting typed events on the `tui.Bus`
(enqueue-only publish, ADR-0005). The design targets are dense multi-panel apps — the
dossier's strongest signal is lazygit vendoring gocui into its own repo because nothing
off the shelf serves that class (https://github.com/jesseduffield/lazygit/issues/2705).
Two gocui/lazygit patterns are deliberately inherited here: the View-as-`io.Writer`
buffer panel (via `BufferView.Writer()`, §2.4) and per-panel async streaming (§2.6).

The inventory is a scoping exercise: each widget added to v1 is a permanent API
commitment under golib's compatibility culture. v1 therefore contains exactly what the
composition check (§2.8) requires, and §1.2/§2.7 record what is consciously deferred.

### 1.1 Goals

- **G1** — A base container (`Box`) giving every "window" border, in-border title, and
  in-border status line via configuration alone (the task requirement, verbatim).
- **G2** — An embedding contract (`Base`) so third-party widgets get NodeID/Context
  plumbing, dirty-marking, and default handlers for free — with the limits of Go
  embedding vs inheritance documented, not papered over.
- **G3** — Per-widget contracts precise enough to implement and test against
  `tui.TestBackend` without a PTY: state, keys consumed, events emitted, style hooks,
  layout behavior.
- **G4** — First-class async ergonomics: the widgets most often fed by background work
  (Select options, List rows, BufferView streams) document their `App.Go`/Bus
  integration as part of their contract.
- **G5** — The inventory composes into sqlit's UI and a lazygit-style 5-panel layout
  with no custom widgets (task objective 5) — proven by walkthrough (§2.8).
- **G6** — golib-conventional construction: `widget.NewX(opts ...XOption)` functional
  options; misconfiguration panics at construction; zero third-party deps.

### 1.2 Non-goals (explicitly out of v1; follow-ups in §2.7)

- **N1 — Table** (columnar, sortable). sqlit's results grid v1 renders via List with a
  monospace-formatted row string; a real Table is the first follow-up.
- **N2 — Tree** (expandable hierarchy).
- **N3 — Virtualized List *fetching*.** The `ListSource[T]` data-source seam IS v1 API
  (§2.4 — rev 1, per Lector should-fix/Q3), so `List`'s API shape never needs a v2; what
  is deferred is windowed/lazy *retrieval* behind that seam. The provided v1 source is
  the in-memory slice adapter, and §2.4 states the resulting memory limit in the List
  contract itself.
- **N4 — Markdown / rich text / syntax highlighting.** Text renders styled plain text.
- **N5 — Scrollbar as a standalone widget.** Scroll indicators are per-widget chrome
  (List/TextArea/BufferView paint their own minimal indicator column when scrollable).
- **N6 — Forms/validation framework.** TextInput has a validation hook; form-level
  orchestration is app code (or a later `widget/form`).

## 2. Decision

### 2.1 `widget.Base` — the embedding contract

Every widget embeds `Base`, which supplies the plumbing `tui.Component` implementations
share:

```go
package widget

// Base provides NodeID/Context plumbing, dirty-marking, and default no-op
// event/lifecycle behavior. Widgets embed it by value.
type Base struct {
    ctx *tui.Context // set by Init; carries NodeID, App handles, unmount context
}

func (b *Base) Init(ctx *tui.Context) { b.ctx = ctx }
func (b *Base) Context() *tui.Context { return b.ctx }
func (b *Base) NodeID() tui.NodeID    { return b.ctx.ID() }        // ADR-0004 Context.ID()
func (b *Base) MarkDirty()            { b.ctx.MarkDirty() }        // repaint request
func (b *Base) RequestLayout()        { b.ctx.RequestLayout() }    // size may change (ADR-0004 name)

// Default no-op handlers — widgets override what they need:
func (b *Base) HandleEvent(ev tui.Event) bool { return false }
// Layout and Render have no sensible default; every widget implements both.
```

> **Rev 1 (Lector should-fix, naming).** The draft used `ctx.NodeID()` and
> `MarkLayoutDirty()`; ADR-0004's canonical names are `Context.ID()` and
> `RequestLayout()`. `Base.NodeID()` remains as the widget-level convenience helper
> (returning `tui.NodeID` via `ctx.ID()`); the layout-dirty helper is renamed
> `Base.RequestLayout()` to match.

**What Go embedding gives us:** method promotion (a widget that doesn't override
`HandleEvent` satisfies `tui.Component` through `Base`'s), shared state plumbing written
once, and zero-cost composition (embedded by value, no indirection).

**What it does NOT give us — documented so nobody designs against inheritance that
isn't there:** there is no virtual dispatch. If `Base.Init` calls a method also defined
on the outer widget, the *Base* version runs — the embedded struct never sees the outer
type. Consequences baked into the contract:

- `Base` never calls "overridable" methods. Template-method patterns are forbidden;
  the runtime (ADR-0004) calls the *outer* component's interface methods directly, so
  overriding works at the interface boundary, which is the only boundary Go respects.
- Widgets that override `Init` MUST call `b.Base.Init(ctx)` first (compiler can't
  enforce; acceptance test 5.2 catches it via a `nil` ctx panic in `TestBackend` runs).
- Capability interfaces (ADR-0004 `tui.Focusable`, `tui.Container`) are detected by
  type assertion on the outer type; `Base` deliberately implements none of them, so
  embedding never accidentally advertises a capability.

### 2.2 `widget.Box` — the base container ("titled window" as configuration)

`Box` implements the task's requirement directly: a bordered container where title and
status line are configuration, not custom drawing.

```go
func NewBox(child tui.Component, opts ...BoxOption) *Box

func WithTitle(s string) BoxOption
func WithTitleAlign(a style.Align) BoxOption          // left (default) | center | right
func WithStatus(s string) BoxOption                   // bottom-border status line
func WithStatusAlign(a style.Align) BoxOption         // right (default) | left | center
func WithBorder(b style.BorderStyle) BoxOption        // default style.BorderNormal
func WithStyle(s style.Style) BoxOption               // base style (padding, colors)
func WithFocusedStyle(s style.Style) BoxOption        // merged over base when focused
func WithFocusable(v bool) BoxOption                  // default false; true = Box itself
                                                      // joins the focus chain (child-less panes)

// Runtime mutators (loop-goroutine only, like all widget state):
func (x *Box) SetTitle(s string)   // MarkDirty, not RequestLayout (title lives in the border row)
func (x *Box) SetStatus(s string)
```

Contract:

- **Layout:** subtracts its frame (border + padding, via ADR-0006 frame-math getters)
  from incoming constraints, lays out the single child, reports child size + frame.
  Title and status render *inside* the top/bottom border rows — they cost zero extra
  rows and truncate with ellipsis when narrow, matching how lazygit/tview draw panel
  titles.
- **Focus visuals:** when the Box or any descendant holds focus (ADR-0004 focus path),
  `FocusedStyle` is merged (`Inherit` semantics, ADR-0006) over the base style. The
  built-in defaults are token-driven: unfocused border =
  `style.New().BorderForeground(style.TokenBorder)`, focused merge =
  `style.New().BorderForeground(style.TokenBorderFocused)` (both tokens added in
  ADR-0006 rev 1 — Box no longer hardcodes `TokenAccent`). This gives every panel the
  lazygit "active panel highlight" for free, re-themable via the Theme alone.

  > **Rev 1 (Lector should-fix / ADR-0006 Q2).** Draft hardcoded `TokenAccent` for the
  > focused border; now `TokenBorder`/`TokenBorderFocused` per ADR-0006 §2.5.
- **Events:** consumes nothing; `HandleEvent` returns false (bubbling continues).
  Mouse clicks inside are routed to the child by hit-testing (ADR-0004).
- **Style hooks:** base style, focused style, border style, title/status styles
  (defaults: title = `TokenForeground` bold, status = `TokenTextMuted`).

Every widget below that reads as a "panel" (List, BufferView, TextArea, …) is used
*wrapped in a Box* rather than growing its own title/border machinery — one chrome
implementation, uniform focus visuals.

### 2.3 Inventory overview

| Widget | Kind | Focusable | Emits (Bus) |
|--------|------|-----------|-------------|
| TextInput | input | yes | `SubmitEvent`, `ChangeEvent` |
| TextArea | input | yes | `ChangeEvent` |
| Select | input | yes | `SelectionChangedEvent`, `OpenedEvent`/`ClosedEvent` |
| List | input | yes | `SelectionChangedEvent`, `ActivateEvent` |
| BufferView | display/input | yes (scroll) | `FollowTailChangedEvent` |
| Tabs | chrome | yes | `TabChangedEvent` |
| Split | container | no (children are) | `SplitResizedEvent` |
| Float | container | children | `DismissEvent` |
| StatusBar | chrome | no | — |
| ProgressBar | chrome | no | — |
| Text | display | no | — |

All events carry `Owner tui.NodeID` as their first field so subscribers filter by
source; publication is enqueue-only onto the App loop (ADR-0005).

### 2.4 Input widgets

**`TextInput`** — single-line editor.

```go
func NewTextInput(opts ...TextInputOption) *TextInput
func WithPlaceholder(s string) TextInputOption        // rendered TokenTextMuted
func WithMask(r rune) TextInputOption                 // password masking, e.g. '•'
func WithValidate(fn func(string) error) TextInputOption
func WithInitialValue(s string) TextInputOption

func (t *TextInput) Value() string
func (t *TextInput) SetValue(s string)

type SubmitEvent struct{ Owner tui.NodeID; Value string }
type ChangeEvent struct{ Owner tui.NodeID; Value string }
```

- **State:** value (grapheme-cluster addressed, ADR-0003 tables), cursor position,
  selection anchor, horizontal scroll offset, validation error.
- **Keys consumed:** printable runes, Backspace/Delete, arrows (±word with
  Ctrl/Alt per kitty-protocol modifiers, degrading to legacy encodings — ADR-0002
  delivers both through one `KeyEvent`), Home/End, Shift+arrows (selection),
  Ctrl+A/E/U/W (readline subset), Enter (→ `SubmitEvent` if validation passes; a
  failing validation sets the error state and consumes the key). Tab is NOT consumed
  (focus traversal, ADR-0004).
- **Paste:** `tui.PasteEvent` (bracketed paste, ADR-0002) inserts atomically at the
  cursor — never replayed as keystrokes, so a pasted `\n` cannot fake a submit.
- **IME/real cursor:** while focused, TextInput reports its insertion point up the tree
  so the runtime parks the *hardware* cursor there (ADR-0003 rule) — OS IMEs anchor
  composition windows to the real cursor (dossier: xterm.js #5734;
  https://mitchellh.com/writing/grapheme-clusters-in-terminals for why addressing is
  grapheme-based).
- **Style hooks:** text, placeholder, selection, cursor-cell, error-state styles.
- **Layout:** height 1; width = constraint max (greedy); content scrolls horizontally
  to keep the cursor visible.

**`TextArea`** — multi-line editor.

- **Buffer:** `[]string` of lines (grapheme-addressed within a line). Deliberately
  simple — no rope/gap buffer in v1; the target is commit-message / query-editor scale
  (kilobytes), not code editors. The line-slice is an internal representation, swappable
  later without API change.
- **Wrap modes:** `WrapNone` (horizontal scroll) | `WrapSoft` (visual wrap at width);
  set via `WithWrap(mode)`.
- **State:** lines, cursor (line, grapheme col, desired-col for vertical moves),
  selection region, viewport scroll (top line, and left col when WrapNone).
- **Keys:** TextInput's set plus Up/Down/PgUp/PgDn, Enter = newline (no submit event —
  a Box-wrapped TextArea form submits via an app-level keybinding), Ctrl+Home/End.
- **Emits:** `ChangeEvent` (coalesced per input event, not per rune of a paste).
- **Layout:** greedy both axes; scroll indicator column painted when content exceeds
  viewport (N5 — chrome, not a widget).
- Real-cursor IME rule applies as in TextInput.

**`Select`** — dropdown.

```go
func NewSelect[T any](opts ...SelectOption[T]) *Select[T]
func WithOptions[T any](items []SelectItem[T]) SelectOption[T]
func WithFilter[T any](enabled bool) SelectOption[T]  // filter-as-you-type in open state

type SelectItem[T any] struct{ Label string; Value T }
type SelectionChangedEvent struct{ Owner tui.NodeID; Index int; Label string }

func (s *Select[T]) Value() (T, bool)     // false until a selection exists
func (s *Select[T]) SetOptions(items []SelectItem[T]) // loop-goroutine; e.g. from TaskResult
```

- **Closed state:** renders as a one-line field (current label + `▾`); Enter/Space/Down
  opens.
- **Open state:** a floating option list on the Stack overlay layer with a **focus
  trap**, both from ADR-0004 — the option list is positioned below (or above, when
  clipped) the field, sized to longest label, max-height capped to available space
  with internal scrolling. Esc closes without change; Enter commits
  (`SelectionChangedEvent`); clicks outside close (the overlay layer sees them first).
- **Filter-as-you-type:** printable keys in the open state narrow the visible options
  (case-insensitive substring); Backspace edits the filter.
- **Style hooks:** field, open-list, option, highlighted-option, filter-line styles.
- **Layout:** closed = height 1, width greedy-or-`WithWidth`. The open overlay does not
  participate in normal layout (Stack layer).

**`List`** — selectable rows over a data-source seam.

```go
// ListSource is the data seam List renders through. It is v1 API (rev 1): the shape
// that a windowed/lazy source implements later WITHOUT List needing a v2. Sources are
// consulted only on the loop goroutine.
type ListSource[T any] interface {
    Len() int      // total row count
    Item(i int) T  // row i; MUST be cheap and non-blocking (fetch elsewhere, e.g. App.Go)
}

// SliceSource adapts an in-memory slice — the provided v1 source.
func SliceSource[T any](items []T) ListSource[T]

func NewList[T any](opts ...ListOption[T]) *List[T]
func WithSource[T any](src ListSource[T], render func(T) string) ListOption[T]
func WithItems[T any](items []T, render func(T) string) ListOption[T] // sugar: WithSource(SliceSource(items), render)
func WithMultiSelect[T any](enabled bool) ListOption[T]

type ActivateEvent struct{ Owner tui.NodeID; Index int } // Enter/double-click

func (l *List[T]) SetSource(src ListSource[T])
func (l *List[T]) SetItems(items []T) // sugar: SetSource(SliceSource(items))
func (l *List[T]) RefreshSource()     // source contents changed in place: re-read Len, clamp cursor, repaint
func (l *List[T]) Selected() (int, bool)
func (l *List[T]) SelectedAll() []int // multi-select mode
```

- **State:** source, render func, cursor index, selection set (multi-select), viewport
  top.
- **Keys:** Up/Down/PgUp/PgDn/Home/End move the cursor (emitting
  `SelectionChangedEvent` in single-select), Space toggles in multi-select, Enter
  emits `ActivateEvent`. Mouse: click selects, wheel scrolls, double-click activates.
- **Rendering:** `List` calls `Item(i)` only for rows intersecting the viewport and
  `Len()` once per layout/render — the *rendering* path is already
  virtualization-shaped. What v1 does NOT ship is a windowed/lazy source: the provided
  `SliceSource` holds all items in memory, so 100k+-row datasets (sqlit full-table
  results) must page at the data layer until the windowed-source follow-up (§2.7)
  lands. A later `WindowedSource` (prefetch, invalidation, placeholder rows) is purely
  a new `ListSource` implementation — **no `List` API change**. Recording the limit in
  the contract keeps it from becoming a silent performance trap.

  > **Rev 1 (Lector should-fix / Q3 / ADR-0001 Q5).** The draft had `List` bound
  > directly to `[]T` with virtualization deferred wholesale; Lector required the
  > data-source abstraction be designed now so sqlit doesn't bake in an O(n) workaround
  > and `List` never needs a v2. `ListSource[T]` + `SliceSource` are now v1 API;
  > windowed *fetching* remains deferred (§1.2 N3, §2.7).
- **Style hooks:** row, cursor-row, selected-row, cursor+selected-row styles.
- **Layout:** greedy both axes.

**`BufferView`** — the task's "buffer input"; the lazygit-class log/pager panel.

```go
func NewBufferView(opts ...BufferViewOption) *BufferView
func WithMaxLines(n int) BufferViewOption      // ring behavior beyond n; default 10_000
func WithFollowTail(v bool) BufferViewOption   // default true
func WithANSIPassthrough(v bool) BufferViewOption // default true — see below

// ErrClosed is returned by writes to a Writer whose BufferView has been unmounted.
var ErrClosed = errors.New("widget: buffer view closed")

// Writer returns a concurrent io.Writer handle feeding this view. The HANDLE is the
// cross-goroutine surface; the BufferView value itself remains loop-owned like every
// other widget. Contract: safe from any goroutine; bounded pending bytes (writes
// block, mirroring ingestor/writer.go's semaphore model, when the loop lags — never
// unbounded buffering); ordered delivery (bytes reach the interpreter in write
// order); returns ErrClosed after the view unmounts. Bytes are handed to the loop
// via App.Post (ADR-0005); parsing and cell conversion happen on the loop goroutine.
func (v *BufferView) Writer() io.Writer

func (v *BufferView) Clear()
func (v *BufferView) ScrollTo(line int)
type FollowTailChangedEvent struct{ Owner tui.NodeID; Following bool }
```

- **Decision: BufferView exposes a separate concurrent `Writer()` handle with an
  internal ANSI interpreter feeding styled cells.** The io.Writer ergonomics are
  gocui's proven design (`View` is an `io.Writer` with an embedded escape-sequence
  interpreter), the single feature that makes lazygit possible with almost no glue:
  colored `git` output is displayed by `cmd.Stdout = view.Writer()` (dossier §2,
  lazygit's vendored `pkg/gocui`;
  https://github.com/jesseduffield/lazygit/issues/2705). The interpreter is a bounded
  SGR-only subset of the ADR-0002 parser (colors + bold/faint/italic/underline/reverse;
  cursor movement and modes are ignored/stripped) mapping directly to ADR-0003 cell
  attributes. `WithANSIPassthrough(false)` strips escapes instead, for untrusted or
  plain streams.

  > **Rev 1 (Lector should-fix / Q2).** The draft made `*BufferView` itself implement
  > `io.Writer` — the one widget with a documented any-goroutine method. Lector:
  > preserve the ergonomics but keep component methods loop-owned; `view.Writer()` is
  > one extra call and does not weaken the global invariant. Folded: the widget no
  > longer implements `io.Writer`; the handle carries the full concurrency contract
  > (any-goroutine, bounded pending bytes, ordered delivery, `ErrClosed` after
  > unmount) spelled out in the code fence above.
- **Buffer:** append-oriented line buffer with a `MaxLines` ring — beyond the cap, the
  oldest lines drop (scrollback positions adjust). Partial trailing lines (no `\n` yet)
  render and are extended by the next write.
- **Follow-tail:** while following, new writes keep the view pinned to the bottom; any
  manual scroll-up disengages following (emitting `FollowTailChangedEvent`); `End` or
  scrolling back to the bottom re-engages. This is the pager UX from `less +F`/lazygit.
- **Keys (focused):** Up/Down/PgUp/PgDn/Home/End scroll; `End` resumes follow. Wheel
  scrolls. BufferView never consumes printable keys — it is a viewer.
- **Style hooks:** base text style (pre-colored ANSI content overrides it per-cell);
  scroll-indicator style.
- **Layout:** greedy both axes; soft-wraps long lines at viewport width (v1: wrap-only,
  no horizontal scroll — matches gocui/lazygit behavior).

### 2.5 Display & chrome widgets

**`Tabs`** — tab bar + content switcher.

```go
func NewTabs(opts ...TabsOption) *Tabs
func WithTab(label string, content tui.Component) TabsOption
type TabChangedEvent struct{ Owner tui.NodeID; Index int; Label string }
func (t *Tabs) Select(i int)
func (t *Tabs) Add(label string, content tui.Component) // mounts lazily on first select
```

- One-row tab bar; active content below. Only the active child is mounted/laid
  out/rendered; switching unmounts the old child unless `WithKeepMounted(true)`
  (preserves child state across switches at the cost of live subscriptions —
  ADR-0004 mount semantics).
- **Keys:** Ctrl+PgUp/PgDn or `[`/`]` (when the bar itself is focused) cycle;
  click selects. Emits `TabChangedEvent`.
- **Style hooks:** bar, tab, active-tab, hover-tab styles.

**`Split`** — H/V two-pane splitter: a thin wrapper over ADR-0004's Flex with
interactive weights.

```go
func NewSplit(orientation Orientation, a, b tui.Component, opts ...SplitOption) *Split
func WithRatio(r float64) SplitOption      // initial division, default 0.5
func WithMinSizes(a, b int) SplitOption    // clamp during resize
type SplitResizedEvent struct{ Owner tui.NodeID; Ratio float64 }
```

- Renders a one-cell divider line; **mouse drag** on the divider adjusts the ratio
  (SGR mouse drag events, ADR-0002); **keyboard resize** via Alt+arrows when either
  child is focused (lazygit's `+`/`_` precedent, generalized). Min sizes clamp; the
  underlying integer split uses ADR-0004's deterministic largest-remainder rule, so
  the same ratio always yields the same cell split.
- Not itself focusable; its children are ordinary members of the focus chain.

**`Float`** — floating window on the Stack overlay layer (ADR-0004).

```go
func NewFloat(child tui.Component, opts ...FloatOption) *Float
func WithAnchor(a Anchor) FloatOption          // Center (default), TopLeft, … , or AtRect(Rect)
func WithModal(v bool) FloatOption             // focus trap + Esc dismiss
func WithDimBackground(v bool) FloatOption     // re-style underlying cells Faint
type DismissEvent struct{ Owner tui.NodeID }

func (f *Float) Show()  // pushes onto the Stack layer
func (f *Float) Hide()  // pops; emits DismissEvent
```

- **Modal:** installs an ADR-0004 focus trap (Tab cycles inside only), consumes Esc as
  dismiss, and — with `DimBackground` — the compositor re-styles the cells beneath with
  `Faint` (a cell-attribute transform in the ADR-0003 compositor, cheap because it
  touches attributes, not content).
- Non-modal floats (tooltips, Select's option list) don't trap focus and are dismissed
  by their owner.
- Usually wraps a `Box` (title + border) — a modal dialog is
  `NewFloat(NewBox(form, WithTitle("Confirm")), WithModal(true))`.

**`StatusBar`** — dock-bottom single line with left/center/right segments.

```go
func NewStatusBar(opts ...StatusBarOption) *StatusBar
func (s *StatusBar) SetLeft(text string, st ...style.Style)
func (s *StatusBar) SetCenter(text string, st ...style.Style)
func (s *StatusBar) SetRight(text string, st ...style.Style)
```

- Height 1, width greedy; intended for the ADR-0004 Dock container's bottom slot.
  Segments truncate center-first, then left, then right (rightmost content — usually
  keybinding hints — survives longest, the lazygit convention). Default background
  `TokenPanel`.

**`ProgressBar`** — determinate / indeterminate / spinner.

```go
func NewProgressBar(opts ...ProgressBarOption) *ProgressBar
func (p *ProgressBar) SetProgress(f float64) // 0..1; determinate mode
func (p *ProgressBar) SetIndeterminate()     // sweeping block animation
func WithSpinner(frames []string, interval time.Duration) ProgressBarOption // 1-cell variant
```

- Animation is driven by **subscribing to `tui.TickEvent`** (ADR-0005 timer facility)
  only while indeterminate/spinning — the subscription is registered on entering the
  animated state and cancelled on leaving it (and at unmount, automatically), so a
  finished progress bar costs zero wakeups and the idle-app zero-byte guarantee
  (ADR-0001 G5) holds.
- Determinate bar renders partial blocks (`▏▎▍…`) for sub-cell resolution; styles:
  filled, empty, and label.

**`Text`** — styled static label.

```go
func NewText(s string, opts ...TextOption) *Text
func WithTextStyle(st style.Style) TextOption
func WithWrapMode(m WrapMode) TextOption // Truncate (default, with ellipsis) | Wrap
func (t *Text) SetText(s string)
```

- `Layout` measures content (grapheme-width, ADR-0003) within constraints: Truncate
  reports one line and paints an ellipsis at overflow; Wrap reports the wrapped height.
  Not focusable; consumes nothing.

### 2.6 Async integration (the contracts in motion)

Both canonical patterns route through ADR-0005's addressed tasks — widgets never spawn
raw goroutines.

**Select options loaded via `App.Go`:**

```go
sel := widget.NewSelect[Table](widget.WithFilter[Table](true))
// At mount (or on user action), the owning component requests data:
ctx.App().Go(sel.NodeID(), func(c context.Context) (any, error) {
    return db.ListTables(c) // dies with the component: c is the unmount context
})
// Delivery: the runtime posts TaskResult{Owner: sel.NodeID()} to the loop; Select's
// HandleEvent converts and installs:
func (s *Select[T]) HandleEvent(ev tui.Event) bool {
    if r, ok := ev.(tui.TaskResult); ok && r.Owner == s.NodeID() {
        if r.Err == nil { s.SetOptions(toItems[T](r.Value)) } else { /* error state */ }
        return true
    }
    // …key handling…
}
```

**BufferView streaming a subprocess** (the lazygit pattern):

```go
view := widget.NewBufferView() // follow-tail on
cmd := exec.CommandContext(ctx.Ctx(), "git", "log", "--color=always") // ADR-0004 Context.Ctx(): cancelled at unmount
cmd.Stdout = view.Writer() // colored output → concurrent handle → ANSI interpreter → styled cells
app.Go(view.NodeID(), func(c context.Context) (any, error) { return nil, cmd.Run() })
// cmd exit surfaces as TaskResult to the view's owner; unmount cancels the context,
// killing the process, and the Writer handle returns ErrClosed — no orphaned writers.
```

Staleness (a second load superseding the first) uses ADR-0005's monotonic `TaskID` +
exclusive task groups — widget contracts require comparing `TaskResult.ID` against the
last-issued ID before applying results.

### 2.7 Deferred follow-ups (ordered)

1. **Table** — columnar results (unblocks sqlit's grid from its List workaround).
2. **Windowed `ListSource`** — a virtualized *source* implementation (windowed fetch,
   prefetch, invalidation, placeholder rows) behind the `ListSource[T]` seam that is
   already v1 API (§2.4 — rev 1); removes N3 with zero `List` API change.
3. **Tree** — expandable hierarchy (file pickers, object explorers).
4. **Markdown/rich text + syntax highlighting** for Text/BufferView.
5. **Scrollbar as reusable chrome** shared by scrollable widgets (replacing the
   per-widget indicator columns).

### 2.8 Composition check (task objective 5)

**sqlit** — sidebar of tables, main query editor, results pane, status bar:

```go
tables := widget.NewList[Table](widget.WithItems(nil, Table.Name)) // async-filled (§2.6)
editor  := widget.NewTextArea(widget.WithWrap(widget.WrapNone))
results := widget.NewList[Row](widget.WithItems(nil, renderRow))   // v1: formatted rows (N1);
                                                                   // large results swap in a paging ListSource (§2.4)
status  := widget.NewStatusBar()

main := widget.NewSplit(widget.Vertical,
    widget.NewBox(editor, widget.WithTitle("Query"), widget.WithStatus("F5 run")),
    widget.NewBox(results, widget.WithTitle("Results")),
    widget.WithRatio(0.4))
body := widget.NewSplit(widget.Horizontal,
    widget.NewBox(tables, widget.WithTitle("Tables")),
    main, widget.WithRatio(0.25), widget.WithMinSizes(20, 40))
root := tui.NewDock(tui.DockBottom(status), tui.DockFill(body)) // ADR-0004
```

Focus cycles Tables → Query → Results (ADR-0004 traversal); the tables List is filled
via `App.Go`; running a query streams row batches to `results.SetItems` via TaskResult;
box titles/status and focused-border highlighting come from Box configuration alone.
Every requirement is met by the v1 inventory — the results grid's columnar alignment is
the one compromise (formatted strings until Table lands, §2.7#1).

**lazygit-shaped 5-panel:** left column of four stacked panels (Status, Files,
Branches, Commits — each a Box-wrapped List), a main pane on the right (Box-wrapped
BufferView fed by git subprocesses with ANSI passthrough), a bottom StatusBar of
keybinding hints, plus a modal commit-message dialog:

```go
left := widget.NewSplit(widget.Vertical, statusBox,
    widget.NewSplit(widget.Vertical, filesBox,
        widget.NewSplit(widget.Vertical, branchesBox, commitsBox)))
root := tui.NewDock(tui.DockBottom(hints),
    tui.DockFill(widget.NewSplit(widget.Horizontal, left, mainBox, widget.WithRatio(0.35))))
dialog := widget.NewFloat(widget.NewBox(msgInput, widget.WithTitle("Commit message")),
    widget.WithModal(true), widget.WithDimBackground(true))
```

Panel focus cycling with visual highlight = Box focused-styles + ADR-0004 traversal;
per-panel async git output = `BufferView.Writer()` (§2.6); the commit dialog = Float
(modal, dim) + TextInput emitting `SubmitEvent`. Nested `Split`s express the fixed
column proportions; equal stacking could equally use a weighted Flex (ADR-0004)
directly — Split earns its place where the user resizes. The inventory suffices; the
walkthrough surfaces no missing widget for either target app.

## 3. Consequences

**Positive**

- The task's "titled window with status" is one `Box` with two options — uniform chrome
  and focus visuals across every app, no per-widget border code.
- BufferView's `Writer()` handle + ANSI interpreter makes the framework's killer
  demographic (lazygit-class subprocess UIs) nearly glue-free — the strongest
  differentiator vs bubbletea, where streaming panels are a hand-rolled pattern —
  while keeping the widget value itself loop-owned (rev 1).
- Contracts are TestBackend-testable end to end (keys consumed, events emitted, cells
  painted) with std testing — no PTY in CI (ADR-0001 acceptance 3).
- Generic `Select[T]`/`List[T]` keep app code cast-free (golib generics convention).

**Negative (costs)**

- Eleven widget contracts are permanent API surface; every option and event name here
  is a compatibility commitment.
- v1 ships no windowed `ListSource` (N3), so large sqlit result sets still need
  data-layer paging until the §2.7#2 follow-up; the rev 1 seam guarantees that
  follow-up is a new source implementation, not a `List` API change, but the fetch gap
  is real until then.
- TextArea's `[]string` buffer degrades on very large content (single-line megabyte
  files); acceptable for the stated scope, but the scope must be documented in
  `doc.go`.
- `BufferView.Writer()` is the framework's one sanctioned any-goroutine write surface
  (rev 1: a separate handle, so the widget itself stays loop-owned). Its internal
  chunking/backpressure (bounded pending-bytes with blocking writes, mirroring
  `ingestor/writer.go`'s semaphore model; delivery via Post; `ErrClosed` after
  unmount) must be implemented and race-tested exactly as specified in §2.4.

**Evolution**

- Follow-ups (§2.7) are additive widgets or additive options; nothing in v1 blocks
  them. Windowed/virtualized fetching ships as a new `ListSource` implementation behind
  the v1 seam — no `ListV2`, no breaking change (rev 1).
- New events are new types on the Bus — additive by construction (typed subscription,
  ADR-0005).

## 4. Alternatives considered

1. **Per-widget title/border/status (tview's shape — every primitive embeds Box
   drawing).** Rejected: duplicates chrome logic across widgets and couples every
   widget to border math; composition (`Box` wrapping) yields one implementation and
   lets bare widgets stay chrome-free for embedding in custom containers.
2. **BufferView without an io.Writer surface/ANSI (expose only
   `AppendLine(string, style.Style)`).** Rejected: forces every subprocess consumer to
   write their own ANSI parser or lose color — discarding the proven gocui design that
   lazygit's whole UI rests on (dossier §2). The interpreter is bounded (SGR subset)
   and reuses ADR-0002's parser machinery. **Rev 1 refinement:** the writer surface is
   a separate `Writer()` handle, not a method set on the widget — the draft's
   widget-implements-io.Writer variant is now itself a rejected alternative (it made
   the component type the concurrency boundary, weakening the loop-ownership
   invariant; Lector should-fix/Q2).
3. **Fully virtualized List (windowed fetching) in v1.** Still deferred, but narrowed
   by rev 1: the *seam* (`ListSource[T]`, §2.4) is v1 API per Lector should-fix/Q3 and
   ADR-0001 Q5, so only the windowed-source implementation (prefetch, invalidation,
   placeholder protocol — worth its own design pass) is deferred (§2.7#2). The
   explicit in-contract limit prevents silent misuse meanwhile, and no `List` API
   break is possible later.
4. **A `Form` container in v1** (field ordering, labels, validation orchestration).
   Rejected for scope: ADR-0004 focus traversal + TextInput validation hooks cover the
   dialogs in both target apps; a form layer is app-composable and revisitable.
5. **Widgets as interfaces (e.g. `type List interface{…}` with a default impl).**
   Rejected: golib is concrete-types-first; interface seams exist where a driver/test
   seam demands them (brief §2), and widgets are used, not substituted. `tui.Component`
   is the substitution seam already.
6. **Char-based drag handles on all four Box edges (mouse-resizable windows).**
   Rejected for v1: only Split's divider is interactively resizable; free-floating
   resizable windows add hit-target and layout complexity with no demand from the
   target apps.

## 5. Acceptance criteria

1. `tui/widget` compiles importing only stdlib + `tui` + `tui/style`; docs ship
   (`doc.go` + `README.md` with the inventory table and the N3 List limit stated).
2. Every widget embeds `Base`; a TestBackend suite mounts each widget and asserts
   `Init` plumbing (non-nil Context, valid NodeID) — catching missed
   `Base.Init` chaining (§2.1).
3. Per-widget TestBackend contract tests: for each widget, a table-driven script of
   injected events asserts (a) consumed vs bubbled keys exactly per §2.4/§2.5, (b)
   emitted Bus events with correct Owner and payload, (c) painted cells for
   representative states (focused/unfocused, empty/filled, truncated).
4. `Box`: title and status render inside the border rows (outer height = child + 2 with
   border), truncate with ellipsis, and focused-style merge activates when any
   descendant gains focus.
5. `TextInput`: bracketed-paste inserts atomically (multi-line paste yields no
   `SubmitEvent`); mask mode paints mask runes while `Value()` returns the raw string;
   the runtime cursor position equals the insertion point when focused (ADR-0003 rule),
   verified via TestBackend cursor state.
6. `Select`: open state renders on the overlay layer with a working focus trap (Tab
   cycles within options); Esc restores prior focus and selection; filter narrows
   options.
7. `BufferView`: writing `git log --color=always`-style SGR content through `Writer()`
   produces styled cells matching the escapes; passthrough-off strips them; follow-tail
   disengages on scroll-up and re-engages at bottom; `MaxLines` ring drops oldest
   lines; concurrent writes from 8 goroutines through one `Writer()` handle under
   `-race` produce ordered, uncorrupted lines; writes after unmount return
   `errors.Is(err, widget.ErrClosed)`; pending bytes are bounded (a stalled loop blocks
   writers rather than buffering unboundedly). `*BufferView` itself does NOT satisfy
   `io.Writer` (compile-time assertion in tests).
8. `ProgressBar`: no TickEvent subscription exists while determinate-and-idle (verified
   via Bus introspection in tests) — the idle app emits zero bytes.
9. The §2.8 sqlit composition builds and its interaction script (focus cycle, async
   table fill, query→results flow, status updates) passes deterministically against
   TestBackend in CI.
10. The §2.8 lazygit-shaped composition builds; the modal Float traps focus, dims the
    background, and Esc dismisses with `DismissEvent`; BufferView streams a fake
    subprocess script (via `Writer()`) with ANSI colors intact.
11. `List` renders through `ListSource[T]` (rev 1): a test source counts `Item(i)`
    calls and asserts only viewport-intersecting rows are fetched per frame and `Len()`
    is called once per layout/render pass; `WithItems`/`SetItems` behave as
    `SliceSource` sugar; `RefreshSource` re-reads `Len`, clamps the cursor, and
    repaints.

## 6. Questions for the reviewer

- **Q1.** `Box` wraps exactly one child, pushing multi-child arrangement to
  Split/Flex/Dock inside it. tview instead makes every primitive its own box. Is the
  single-child-wrapper model right, or should `Box` accept an optional layout +
  children to reduce nesting depth in app code (§2.8's lazygit tree is 4 levels of
  Split inside Boxes)?
  — **Lector r1:** keep Box a single-child wrapper; nested layout is explicit and
  avoids turning Box into another container framework. Confirmed, no change.
- **Q2.** `BufferView.Write` accepting calls from any goroutine (draft §2.4) was the
  one deliberate breach of "all cross-goroutine entry is Post/Update" — justified by
  the `cmd.Stdout = view` ergonomics it buys. Accept the exception, or require
  `view.Writer()` returning a separate handle so the widget type itself stays
  loop-only?
  — **Lector r1:** use `view.Writer()`; preserve the ergonomics while keeping component
  methods loop-owned. Folded in §2.4/§2.6 (handle contract: any-goroutine, bounded
  pending bytes, ordered delivery, `ErrClosed` after unmount).
- **Q3.** v1 List non-virtualized (draft N3): given sqlit result sets are the known
  first consumer, is data-layer paging an acceptable v1 answer, or does the reviewer
  want the data-source abstraction (`Len/Item`) designed *now* — even if windowed
  fetching ships later — so `List`'s API doesn't need a v2? (Interacts with ADR-0001
  Q5.)
  — **Lector r1:** design the data-source abstraction now; windowed fetching may ship
  later, but the API shape must not force a `List` v2. Folded: `ListSource[T]` +
  `SliceSource` are v1 API (§2.4); windowed source is follow-up §2.7#2.
- **Q4.** Select/List are generic (`Select[T]`, `List[T]`), which infects containers
  holding heterogeneous widget slices with `any`-boxing at collection sites. Keep
  generics for value-typed ergonomics, or de-generify to `string`-labeled + `any` value
  (bubbles' shape) for a flatter API?
  — **Lector r1:** keep generic `Select[T]`/`List[T]`; heterogeneous containers already
  traffic in `tui.Component`, and value ergonomics for app code are worth it.
  Confirmed, no change.
- **Q5.** TextArea deliberately has no `SubmitEvent` (Enter = newline; submission is an
  app keybinding, §2.4). Should the widget instead offer an opt-in
  `WithSubmitKey(key)` so common "Ctrl+Enter submits" dialogs need no app-level
  wiring?
  — **Lector r1:** no TextArea submit behavior in v1; the app-level keybinding is the
  cleaner contract for multi-line editors. Closed as answered-no; `WithSubmitKey` is
  not added.
