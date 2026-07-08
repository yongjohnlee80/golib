
# ADR-0001 — golib/tui: Overview & Architecture

**Tags:** `type:adr` `status:accepted` `owner:shared` `repo:golib` `area:tui` `kind:overview` `kind:architecture`

**Abstract:** Umbrella ADR for `golib/tui`, a minimal-dependency, retained-mode terminal UI component framework: layering, package layout, dependency policy, core vocabulary, and the map of detail ADRs 0002–0007. Start here.

> **Authored natively in the KB** (jarvis, 2026-07-08) as part of the golib/tui design
> dossier, from the task
> `2026-07-08-create-standard-tui-components-in-golib` and an external research pass over
> the Go TUI ecosystem (dossier citations inline). golib/tui keeps its own 0001–0007 ADR
> numbering; the `golib-tui-` filename prefix namespaces it against this KB's auto-agents
> ADR sequence. Navigation hub: golib-tui.

- **Status:** **Accepted** (Johno, 2026-07-08) — revision 1 (Lector r1 `change_requested`
  → r2 `approved_with_amendments`, all amendments folded).
- **Date:** 2026-07-08
- **Reviewed by:** lector, 2026-07-08 — agents/lector/reviews/2026-07-08-golib-tui-adrs-review.md (r1); agents/lector/reviews/2026-07-08-golib-tui-adrs-rereview.md (r2, `approved_with_amendments`)
- **Module:** github.com/yongjohnlee80/golib
- **Supersedes:** none (greenfield)
- **Related:** ADR-0002 (terminal backend & capability model), ADR-0003 (cell buffer &
  render pipeline), ADR-0004 (component tree, lifecycle & layout), ADR-0005 (runtime,
  events, pub/sub & async tasks), ADR-0006 (styling & theming), ADR-0007 (standard widget
  set). golib server ADR-0006 (transport scaffold — the `Run(ctx)`/drain lifecycle this
  runtime mirrors).

> **Self-containment contract.** This ADR is implementable with no prior context beyond the
> golib conventions (golang, golib) and the sibling ADRs listed under Related.

> **Revision history.** **Rev 1 (2026-07-08)** — folded from Lector's r1 review
> (`change_requested` on the series; this umbrella ADR had no must-fix of its own): §2.3
> Backend vocabulary gains `Err() error` and the `Capabilities` profile fields
> (`ColorProfile`, `DarkBackground`, tri-state `Mouse`) per must-fix #1/#2 (canonical
> definitions in ADR-0002 rev 1); §2.3 Component contract gains the comparability
> requirement per must-fix #4 (canonical in ADR-0004 rev 1); §6 questions annotated with
> Lector's answers — Q1–Q4 confirmed as decided; Q5 conditionally accepted, satisfied by
> the `ListSource[T]` seam added in ADR-0007 rev 1.

## 1. Context

The task (`.todo-list` id above) asks for a standard TUI component framework in golib:
input widgets (text input, text area, dropdown, buffer views), display widgets (tabs,
floating windows, splits, status bar, progress bar, titled windows), a component **tree**,
a **master runtime** owning keyboard/mouse/IO event dispatch with pub/sub between runtime
and components, **async results routed back to the originating component**, a fluent
design-token styling system, and a driver abstraction leaving room for non-terminal
targets. Target caliber: apps like lazygit and sqlit. Hard constraint: golib's
zero-dependency-core philosophy (README.md; every existing package is stdlib-only in core
with third-party deps confined to leaf driver packages such as `dao/postgres`,
`server/ws`).

### 1.1 The ecosystem gap (why build, not adopt)

Research across the Go TUI ecosystem (full dossier cited per-claim in ADRs 0002–0007):

- **bubbletea** (charm) is Elm-architecture: a single root `Update(Msg)` with no component
  addressing. Message routing to nested components, focus, and layout are user-space
  problems every non-trivial app re-solves (bubbletea discussions #176, #751, #943;
  https://leg100.github.io/en/posts/building-bubbletea-programs/). Its v2 dependency tree
  (ultraviolet, colorprofile, x/ansi, go-colorful, cancelreader) is far outside golib's
  dependency budget.
- **tview** is the retained-tree precedent but is tied to tcell v2, has no built-in focus
  traversal, a proportional-only Flex (no min/max, no content measurement), and a
  documented `QueueUpdate` self-deadlock footgun (rivo/tview wiki/Concurrency).
- **tcell** solves the low-level layer well (cell diff, terminfo) but is a dependency, not
  a framework — and its terminfo database approach is the legacy road; modern stacks
  (vaxis, charm v2, Neovim) probe the live terminal instead.
- **lazygit vendored gocui into its own repo** (pkg/gocui, 2026-04) — the single strongest
  signal that lazygit-class apps have no maintained off-the-shelf answer (see also Jesse
  Duffield's assessment of bubbletea for lazygit: lazygit issue #2705).
- **vaxis** (go.rockorager.dev/vaxis) is the closest existing design (live capability
  probing, grapheme cells, Flutter-inspired vxfw) and validates the architecture below,
  but it is a third-party dependency and its widget layer is young.

Conclusion: the gap is real, the architecture below is convergent with where the healthiest
projects (vaxis, tcell v3, bubbletea v2 internals) are all heading, and golib's philosophy
(hand-roll a small, well-understood core over adopting a framework) applies cleanly.

### 1.2 Goals

- **G1** — A retained component tree with framework-owned focus traversal, event routing
  (target-then-bubble; ADR-0004 §2), lifecycle (mount/unmount), and layout. Dense multi-panel apps
  (lazygit-class) are the design target.
- **G2** — A master runtime (`tui.App`) owning a single-goroutine event loop: keyboard,
  mouse, resize, paste, timers, queued closures, and async task completion — with results
  **addressed to the originating component**, not broadcast.
- **G3** — Pub/sub between runtime and components with typed, generics-based subscription;
  delivery always on the loop goroutine.
- **G4** — Fluent, immutable, design-token-driven styling (lipgloss-familiar surface) that
  resolves through a theme + terminal capability profile at render time.
- **G5** — Flicker-free, allocation-conscious rendering: grapheme-aware cell buffer,
  cell diff, single buffered write per frame, synchronized-output bracketing, zero bytes
  when idle.
- **G6** — Modern terminal support from day one: live capability probing, kitty keyboard
  protocol (flags 1+2), SGR mouse, bracketed paste, truecolor with graceful degradation,
  Windows Terminal parity via VT input mode.
- **G7** — Two explicit portability seams — `tui.Backend` (driver) and `tui.Surface`
  (render target) — such that a test backend, an SSH backend, or a future non-terminal
  driver can be added without touching component code.
- **G8** — Standard widget set sufficient to build sqlit/lazygit-shaped apps out of the box
  (ADR-0007's inventory).
- **G9** — Zero third-party dependencies. `golang.org/x/term` + `golang.org/x/sys` only,
  confined to the `tui/term` leaf (both are already indirect deps of the module and are
  quasi-stdlib).

### 1.3 Non-goals

- **N1 — Native pixel rendering (desktop/mobile) in v1.** Kept as a seam (G7), not built.
  §4.6 records why; this is a deliberate challenge to task objective 8.
- **N2 — Elm/TEA compatibility.** No `Update(Msg) (Model, Cmd)` surface.
- **N3 — A constraint solver.** Flex + dock layout only (ADR-0004). Ratatui is the only
  major TUI paying for Cassowary and documents non-determinism on infeasible sets.
- **N4 — terminfo.** Live probing replaces it (ADR-0002).
- **N5 — Actor-per-component concurrency.** Single loop + addressed events (ADR-0005).
- **N6 — CSS-like stylesheet files / selector cascade.** Styles are Go values; themes are
  Go structs (ADR-0006). A text stylesheet layer could sit on top later without core changes.

## 2. Decision

### 2.1 Layered architecture

Six layers, each depending only on those below it:

```
tui/widget      standard components (inputs, containers, chrome)      [ADR-0007]
tui  (tree)     Component, lifecycle, focus, layout, hit-testing      [ADR-0004]
tui  (runtime)  App loop, events, Bus, task routing, frame scheduler  [ADR-0005]
tui/style       Style, Color, Token, Theme                            [ADR-0006]
tui  (render)   Surface, grapheme cell buffer, diff                   [ADR-0003]
tui  (seam)     Backend interface, TestBackend                        [ADR-0002]
tui/term        ANSI driver: raw mode, VT parser, capability probe    [ADR-0002]
```

### 2.2 Package layout & dependency policy

```
tui/                     core (everything above the driver): stdlib ONLY
tui/term                 terminal Backend impl; MAY import golang.org/x/term, golang.org/x/sys
tui/style                styling; stdlib only
tui/widget               standard widgets; imports tui, tui/style; stdlib only
tui/internal/grapheme    generated UAX #11 + UAX #29 tables (go:generate from UCD files)
```

This mirrors the established golib shape: pure core + leaf driver holding the platform
deps (`dao` vs `dao/postgres`; `server` vs `server/ws`). `x/term`/`x/sys` are already in
the module graph (go.mod indirect) and are the sanctioned escape hatch for
platform syscalls — hand-rolling `SetConsoleMode` via `NewLazySystemDLL` to avoid them
would be zero-dep theater (std `syscall` on Windows lacks `SetConsoleMode` entirely).

Unicode width and grapheme segmentation get **generated tables** (the
runewidth/uniseg recipe: `EastAsianWidth.txt`, `emoji-data.txt`, `GraphemeBreakProperty.txt`
→ sorted ranges + binary search) rather than a dependency. This is the one genuinely
laborious hand-roll item and is scoped in ADR-0003.

### 2.3 Core vocabulary (binding on ADRs 0002–0007)

```go
// The component contract (full semantics in ADR-0004).
// Rev 1 (Lector must-fix #4): a Component's dynamic type MUST be comparable —
// normatively, components are pointers to their state structs. Mount verifies and
// panics loudly on violation (ADR-0004 rev 1 §2).
type Component interface {
    Init(ctx *Context)                 // mount: runtime handles, NodeID, unmount context
    Layout(c Constraints) Size         // constraints down, size up (parent positions)
    Render(s Surface)                  // paint into a clipped view of the cell buffer
    HandleEvent(ev Event) bool         // true = consumed (stops bubbling)
}

// The master runtime (full semantics in ADR-0005).
func NewApp(root Component, opts ...AppOption) *App
func (a *App) Run(ctx context.Context) error            // scaffold-style lifecycle
func (a *App) Post(ev Event)                            // enqueue from any goroutine
func (a *App) Update(fn func())                         // queued closure on loop goroutine
func (a *App) Go(owner NodeID, task Task, opts ...TaskOption) TaskID  // addressed async

// Events (marker interface; concrete set in ADR-0005).
type Event interface{ isEvent() }
// KeyEvent, MouseEvent, ResizeEvent, PasteEvent, FocusEvent, TickEvent,
// TaskResult{Owner NodeID, ID TaskID, Value any, Err error}

// Pub/sub (ADR-0005): typed subscribe, enqueue-only publish, loop-goroutine delivery.
func Subscribe[T any](b *Bus, fn func(T)) (cancel func())

// The driver seam (ADR-0002): Ratatui-Backend-shaped. Rev 1: gains Err() error (terminal-
// reader error after Events() closes); Capabilities carries ColorProfile (Mono/ANSI16/
// ANSI256/TrueColor), DarkBackground, and tri-state Mouse. Backends emit ordered,
// un-coalesced events; ALL coalescing/overflow policy is App-owned (ADR-0005 rev 1).
type Backend interface { /* Size, Flush(diff), cursor ops, Capabilities, Events() <-chan Event, Err, lifecycle */ }

// The render target (ADR-0003).
type Surface interface { /* SetCell, Fill, Sub(Rect) Surface, Size */ }

// Layout primitives (ADR-0004).
type Constraints struct{ MinW, MaxW, MinH, MaxH int }
type Size struct{ W, H int }
type Rect struct{ X, Y, W, H int }

// Styling (ADR-0006): immutable value semantics, typed fields + set-bitfield.
st := style.New().Background(style.TokenSurface).Foreground(style.TokenText).
    Padding(1, 2).Bold(true).Border(style.BorderRounded)
```

Naming: the task's "master component" is `tui.App`. Component identity is `tui.NodeID`
(uint64, assigned at mount, stable for the component's mounted lifetime).

### 2.4 The five load-bearing architectural decisions

1. **Retained tree, two layers.** A mutable `Component` tree plus a cell buffer — not
   Flutter's three trees (immutable widget configs + element reconciliation exist to serve
   declarative rebuild/hot-reload, which we are not doing, and Go lacks cheap persistent
   data structures), and not TEA (framework must own routing/focus/layout — G1). Components
   hold state and call `Context.MarkDirty()`; the runtime coalesces dirty marks into
   frames. A declarative layer (Xilem-style view diffing) can be added on top later
   without changing this core.
2. **Single-goroutine loop; everything addressed.** All component access happens on the
   loop goroutine (ADR-0005). Async work enters only as posted events. Task results carry
   the owner `NodeID`; unmount cancels the component's context and orphaned results are
   dropped. This is the convergent design of tview/gocui/vaxis minus their footguns
   (non-blocking queue with same-goroutine detection, where tview's `QueueUpdate`
   deadlocks).
3. **Grapheme-string cells + diff + one write.** Cells hold grapheme clusters (not runes) —
   the tcell v3 / vaxis / bubbletea-v2 consensus — diffed curr-vs-last, emitted as one
   buffered write per frame inside synchronized-output (mode 2026) brackets (ADR-0003).
4. **Probe, don't database.** Startup capability negotiation via DA1-fenced queries
   (DECRQM 2004/2026/2027/2048, XTGETTCAP, OSC 10/11, kitty `CSI ? u`), no terminfo
   (ADR-0002). Degradation is per-capability, not per-`$TERM`.
5. **Portability is two seams.** `Backend` (what a terminal is) and `Surface` (what
   components draw on). Test/SSH/web backends are cheap and ship or are trivially
   possible; a pixel driver is *possible* behind the same seams but explicitly out of
   scope (§1.3 N1, §4.6).

### 2.5 ADR map

| ADR | Scope |
|-----|-------|
| 0001 (this) | philosophy, layering, packages, vocabulary, goals/non-goals |
| 0002 | `tui/term` + `Backend` seam: raw mode, VT input parser, capability probing, Windows, TestBackend |
| 0003 | cell buffer, grapheme width tables, Surface, diff/flush pipeline, frame scheduling |
| 0004 | Component interface, tree, mount/unmount, focus, event routing, layout (flex+dock), hit-testing |
| 0005 | App runtime: loop, event set, Bus, `App.Go` task routing, timers, lifecycle/shutdown |
| 0006 | style: Style value, Color model, tokens, Theme, capability-aware resolution |
| 0007 | widget set v1: inventory, per-widget contracts, shared chrome (title/status), extension points |

## 3. Consequences

**Positive**
- Framework-owned focus/routing/layout removes the entire class of "root model as manual
  message router" boilerplate that dominates bubbletea apps at scale.
- Zero third-party deps: no supply-chain surface, no upstream architecture churn (the
  bubbletea v1→v2 module-path break is the cautionary tale), golib stays self-contained.
- Addressed async (G2) makes the hardest real-world TUI problem — panels streaming
  subprocess/IO results concurrently — a framework primitive instead of an app pattern.
- The seams give deterministic headless testing (TestBackend) for free, which the existing
  golib test culture (std testing, table-driven) can drive without a PTY.

**Negative (costs)**
- We own the low-level terminal layer forever: VT parser, capability probe, Unicode tables
  (with a UCD update cadence), Windows console handling. This is the "maintenance burden of
  low-level view-layer stuff" Duffield names; the mitigation is that the layer is bounded,
  spec-driven, and heavily table-tested.
- Retained mutable tree demands discipline about the loop-goroutine invariant; the API
  makes off-loop mutation hard (no exported setters that bypass `Post`/`Update`) but Go
  cannot make it impossible.
- Generated Unicode tables add a `go:generate` toolchain and ~100–200 KB of tables to the
  binary.

**Evolution**
- New capabilities (e.g. mode 2027 adoption, OSC 52 clipboard) slot into the capability
  profile without API breaks.
- A declarative composition layer, stylesheet loading, or a pixel backend are all additive
  layers on existing seams.

## 4. Alternatives considered

1. **Adopt bubbletea (wrap it).** Rejected: dependency budget; Elm routing/focus/layout
   gaps are architectural, not wrappable (G1/G2 unmet at the root).
2. **Adopt tcell as the low layer, build the tree on top (tview's shape).** Rejected:
   single heaviest dependency in the space (terminfo DB + transitive deps); terminfo model
   is legacy vs live probing; golib philosophy is hand-roll small cores.
3. **Elm architecture, hand-rolled.** Rejected: reproduces the routing problem we're
   building to solve; async addressing (task objective 6) is exactly TEA's weak point.
4. **Flutter's three trees.** Rejected: the immutable-config layer buys declarative
   rebuild we don't do; two layers give the same layout/paint benefits at half the
   machinery. Revisitable additively (Xilem precedent).
5. **Actor-per-component (Textual's model).** Rejected: ordering hazards + shared-screen
   synchronization in exchange for parallelism no TUI needs; Textual's design is an
   asyncio artifact (ADR-0005 details).
6. **Build the GUI abstraction now (task objective 8 as stated).** Scoped down to seams:
   there is **no prior art** of one widget tree rendering natively to both cells and
   pixels — every real bridge (egui_ratatui, soft_ratatui, Textual-web, tcell-WASM)
   pivots on the cell grid or the ANSI byte stream. True pixel targets force driver-owned
   text measurement (Fyne's `RenderedTextSize` lesson), float layout, and font shaping
   into the core, poisoning TUI ergonomics. The practical GUI path later is a cell-grid
   rasterizer driver (soft_ratatui model) or byte-stream remoting (Textual-web model) —
   both already accommodated by the `Backend` seam.
7. **`map[string]interface{}` style property bag (task objectives 11–12 as stated).**
   Rejected in favor of typed fields + set-bitfield; full evidence and the extensibility
   answer in ADR-0006 §4 (summary: lipgloss ran the property-bag design in production
   until v0.11.0 and reversed it for immutability, allocation, and type-safety costs; the
   bitfield preserves the needed "is-set" cascade semantics; a nil-by-default `extras`
   map preserves open extension).

## 5. Acceptance criteria

1. `go build ./tui/...` succeeds with `GOFLAGS=-mod=readonly` and **no new module
   requirements** beyond `golang.org/x/term`, `golang.org/x/sys` (promoted from indirect).
2. `tui`, `tui/style`, `tui/widget` import nothing outside stdlib + golib.
3. A demo app with a split layout, focus cycling, a text input, an async-filled list, and
   a status bar runs against `term.Backend` on Linux and Windows Terminal, and its full
   interaction script passes deterministically against `TestBackend` in CI (no PTY).
4. Idle app emits zero bytes; any single state change produces exactly one `Write`.
5. ADRs 0002–0007 accepted; each's own acceptance criteria are the per-layer gates.
6. Package docs: `tui/doc.go` + `tui/README.md` state the loop-goroutine invariant and the
   two-seam portability contract verbatim.

## 6. Questions for the reviewer

- **Q1.** Package granularity: is folding runtime+tree+render+seam into one `tui` package
  (relying on file organization, with `style`/`widget`/`term` split out) the right golib
  shape, or should the cell/render layer be its own `tui/cell` package to keep the core
  package small? (Current call: one package — the types are mutually entangled and the
  public surface stays modest.)
  — **Lector r1:** keep one core `tui` package; a separate `tui/cell` would prematurely
  split mutually entangled types.
- **Q2.** Is promoting `x/term`+`x/sys` to direct deps acceptable under the zero-dep-core
  policy given they're confined to `tui/term`, or must even the leaf hand-roll termios
  ioctls over std `syscall` on unix (leaving x/sys for Windows only)?
  — **Lector r1:** acceptable; hand-rolling termios/Win32 for a symbolic zero-dep leaf
  would be worse engineering and does not violate zero-dep core.
- **Q3.** The task asked for desktop/mobile portability (objective 8); §1.3 N1/§4.6 scope
  it to seams-only. Does the reviewer accept this scope reduction, or should ADR-0002's
  Backend interface be widened now (e.g. pixel-aware measurement hooks) at the cost of
  cell-layer simplicity?
  — **Lector r1:** scope reduction accepted; do NOT add pixel-aware measurement hooks now —
  they would distort the cell-first API before any real pixel backend exists.
- **Q4.** Unicode tables: generate-in-repo (go:generate + committed tables, UCD refresh
  cadence on us) vs vendoring uniseg wholesale under `internal/` with attribution. We
  chose generation for provenance and size control — agree?
  — **Lector r1:** agreed; generated committed tables over vendoring uniseg.
- **Q5.** Is starting the widget inventory at ADR-0007's v1 set (no table widget, no
  virtualized list in v1 — deferred to 0007's follow-ups) acceptable for the sqlit/lazygit
  target, or is a virtualized list load-bearing enough to be v1?
  — **Lector r1:** acceptable ONLY IF the list data-source/virtualization seam is designed
  now; a non-virtualized-only `List[T]` is not enough for sqlit-class results. Satisfied in
  rev 1 by ADR-0007's `ListSource[T]` seam (windowed fetching may ship post-v1; the API
  shape is fixed in v1).
