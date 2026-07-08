
# ADR-0004 — golib/tui: Component Tree, Lifecycle & Layout

**Tags:** `type:adr` `status:accepted` `owner:shared` `repo:golib` `area:tui` `kind:component-tree` `kind:lifecycle` `kind:layout` `kind:focus` `kind:event-routing`

**Abstract:** Specifies the `tui.Component` contract fixed by ADR-0001 §2.3 with full
semantics — `tui.Context`, the optional capability interfaces (`Focusable`, `Container`,
`CursorReporter`, `FocusScope`), tree mechanics (mount/unmount cascade, `NodeID`
assignment), target-then-bubble event routing with framework-owned mouse hit-testing,
framework-owned focus traversal with focus scopes, and the single-pass
constraints-down/sizes-up layout protocol with `Flex`, `Dock`, and `Stack` containers.

> **Authored natively in the KB** (jarvis, 2026-07-08) as part of the golib/tui design
> dossier. golib/tui keeps its own 0001–0007 ADR numbering; the `golib-tui-` filename
> prefix namespaces it against this KB's auto-agents ADR sequence. Navigation hub:
> golib-tui. Umbrella ADR: golib-tui-0001-overview-and-architecture.

- **Status:** **Accepted** (Johno, 2026-07-08) — revision 1 (Lector r1 `change_requested` → r2 `approved_with_amendments`, all amendments folded).
- **Date:** 2026-07-08
- **Reviewed by:** lector, 2026-07-08 — agents/lector/reviews/2026-07-08-golib-tui-adrs-review.md (r1); agents/lector/reviews/2026-07-08-golib-tui-adrs-rereview.md (r2, `approved_with_amendments`)
- **Module:** github.com/yongjohnlee80/golib
- **Supersedes:** none (greenfield)
- **Related:** ADR-0001 (fixes the `Component` signature and the two-layer retained-tree
  decision this ADR elaborates), ADR-0003 (the `Surface`/cell buffer `Render` paints
  into; frame scheduling), ADR-0005 (the runtime that mounts the tree, dispatches every
  event described here, and owns `Context`'s `Go`/`Post`/`Bus` handles), ADR-0006 (the
  `style.Theme` resolved through `Surface`), ADR-0007 (the widget set implementing these
  contracts).

> **Revision history.** rev 1 (2026-07-08): folded Lector r1 `change_requested` —
> (1) **must-fix #4**: component identity contract — a `Component`'s dynamic type MUST be
> comparable (normatively: components are pointers to their state structs); mount
> verifies via `reflect.TypeOf(c).Comparable()` and panics with a targeted message
> (§2.1, §2.4, §5.9); (2) **should-fix (Q3 amendment)**: the out-of-constraints clamp
> diagnostic is now failable in tests — `TestBackend.ConstraintViolations()` +
> `tui.FailOnViolations` (§2.7.1, §5.6); (3) **should-fix (naming)**: `Context.ID()`,
> `RequestLayout()`, and `Ctx()` declared the canonical cross-ADR names (§2.2);
> (4) Lector's Q1–Q5 answers appended in §6 — all five positions confirmed, with Q3's
> test-hook amendment the only body change.

> **Self-containment contract.** This ADR is implementable with no prior context beyond
> the golib conventions (golang, golib) and the sibling ADRs listed under Related.

## 1. Context

ADR-0001 fixed the load-bearing decision: a **retained, mutable component tree over a
cell buffer** — two layers, not Flutter's three, not Elm. It also fixed the `Component`
interface shape (§2.3) and named the concerns this ADR must resolve: full lifecycle
semantics, event routing, focus, and layout. The evidence that these must be
**framework-owned** rather than user-space:

- In bubbletea, routing to nested models, focus, and layout are all re-solved per app
  (discussions https://github.com/charmbracelet/bubbletea/discussions/176,
  https://github.com/charmbracelet/bubbletea/discussions/751,
  https://github.com/charmbracelet/bubbletea/discussions/943; the canonical practitioner
  account — "root model becomes message router and screen compositor" —
  https://leg100.github.io/en/posts/building-bubbletea-programs/).
- tview, the retained-tree precedent, ships `SetFocus` only — **no global Tab
  traversal**; every app wires its own focus cycling (rivo/tview `application.go`,
  research dossier §2). Its `Flex` splits remainders by integer proportion with no
  min/max and no content measurement (`flex.go`).
- Mouse hit-testing without a layout tree degenerates into bubblezone's hack for
  bubbletea (lrstanley/bubblezone): wrap component output in zero-width ANSI markers and
  scan the composed frame for offsets. A framework that owns laid-out `Rect`s gets
  hit-testing for free.
- Flutter's layout protocol — constraints down, sizes up, parent positions, single pass
  (https://docs.flutter.dev/resources/inside-flutter) — is ~50 lines in Go and strictly
  more capable than tview's Flex; Textual computes the same family of layouts with exact
  `fractions.Fraction` specifically to kill integer-rounding gaps
  (https://textual.textualize.io/guide/layout/), which we match with deterministic
  largest-remainder distribution instead.

### 1.1 Goals

- **G1** — Complete, testable semantics for every `Component` method: call order,
  goroutine, legality windows, and error behavior.
- **G2** — A mount/unmount lifecycle whose cascade is airtight: unmount cancels the
  component's `context.Context`, tears down bus subscriptions and timers, and guarantees
  pending async results addressed to it are dropped (ADR-0005 enforces the drop).
- **G3** — Event routing the framework owns end-to-end: keyboard to focus chain, mouse by
  hit-testing, consumption stops propagation, deterministic order.
- **G4** — Framework-owned focus: Tab/Shift-Tab traversal, opt-in via `Focusable`, focus
  scopes that trap traversal inside modals/floating windows, focus restore on unmount.
- **G5** — Flutter's layout protocol verbatim, with `Flex`, `Dock`, and `Stack` covering
  the lazygit/sqlit layout class, and bit-for-bit deterministic integer distribution.
- **G6** — The IME real-cursor rule as a first-class capability: a focused text input
  reports its insertion point up the tree so the runtime parks the *hardware* cursor
  there (OS IMEs anchor composition windows to the hardware cursor —
  https://github.com/xtermjs/xterm.js/issues/5734).

### 1.2 Non-goals

- **N1** — A DOM-style capture phase. ADR-0001 G1 names the capture/bubble family; this
  ADR resolves it to target-then-bubble only (§2.5, §4.5). A `Capturer` capability could
  be added later without breaking the contract.
- **N2** — Relayout boundaries (Flutter's clean-subtree skipping). v1 relayouts the whole
  visible tree when any layout dirt exists; §3 records the upgrade path.
- **N3** — Declarative composition / reconciliation. Fixed by ADR-0001 §2.4(1).
- **N4** — Constraint solvers (ADR-0001 N3) and CSS-like stylesheets (ADR-0001 N6).
- **N5** — Widget inventory. ADR-0007 owns which containers/widgets ship; this ADR owns
  the container *contracts* (`Flex`/`Dock`/`Stack` layout algorithms are core because
  hit-testing and focus order depend on them).

## 2. Decision

### 2.1 The `Component` interface — full semantics

The signature is fixed by ADR-0001 §2.3; this section is its normative contract.

```go
// Component is the single mandatory contract of every node in the tree.
// All four methods are invoked ONLY on the App loop goroutine (ADR-0005 §2.3).
//
// IDENTITY CONTRACT (rev 1): a Component's dynamic type MUST be comparable —
// normatively, a component is a POINTER to its state struct (§2.4). Mount
// panics on non-comparable component types.
type Component interface {
    // Init is called exactly once, at mount, before any Layout/Render/HandleEvent.
    // ctx is valid for the component's whole mounted lifetime; retain it.
    // Mounting children (ctx.Mount) and subscribing (SubscribeScoped, ADR-0005 §2.7)
    // are legal here.
    Init(ctx *Context)

    // Layout receives constraints from the parent and returns the size the
    // component chooses within them (§2.7). Containers lay out and place their
    // children here via ctx.LayoutChild/ctx.PlaceChild — the ONLY window in which
    // those calls are legal. Layout must not mutate tree structure (no
    // Mount/Unmount) and must not post events.
    Layout(c Constraints) Size

    // Render paints the component's own chrome into s — a Surface pre-clipped to
    // the Rect the parent placed it in (ADR-0003). It must NOT render children:
    // the framework walks the tree and hands each child its own sub-Surface
    // (§2.4). Render must be effect-free besides painting.
    Render(s Surface)

    // HandleEvent receives routed events (§2.5). Return true to consume: bubbling
    // stops. Handlers run on the loop goroutine and may freely mutate component
    // state, call ctx.MarkDirty()/RequestLayout(), mount/unmount children, post
    // events, and start tasks. A handler that blocks stalls the UI (ADR-0005 §3);
    // long work goes through ctx.Go.
    HandleEvent(ev Event) bool
}
```

Call-order guarantees the framework provides (testable, §5):

1. `Init` strictly before any other method; exactly once per mount.
2. `Layout` before the first `Render` after mount, and again before any `Render` that
   follows layout dirt (§2.7.5); `Render` never sees a stale geometry.
3. No method is ever invoked after unmount completes. A component mounted again later is
   a *new* mount: new `NodeID`, fresh `Init` (component authors must make `Init`
   re-entrant across remounts of the same Go value).

### 2.2 `tui.Context` — the component's handle to the runtime

One `*Context` per mounted node, created at mount, invalidated at unmount. It is the
*only* sanctioned channel from a component to the runtime — no globals (golib
philosophy; brief: no hidden globals).

```go
// Context is a mounted component's identity and runtime access. Methods are
// legal only on the loop goroutine except Post and Go, which are safe from any
// goroutine (they delegate to App — ADR-0005 §2.4, §2.8).
type Context struct{ /* node backref; unexported */ }

func (c *Context) ID() NodeID                // stable for this mount; 0 is never assigned
func (c *Context) Ctx() context.Context      // cancelled when this node unmounts (§2.4)
func (c *Context) MarkDirty()                // repaint my subtree; geometry unchanged (§2.7.5)
func (c *Context) RequestLayout()            // my size may have changed; relayout (§2.7.5)
func (c *Context) RequestFocus()             // ask the focus manager to focus me (§2.6)
func (c *Context) Focused() bool             // am I the focused node?
func (c *Context) OnUnmount(fn func())       // LIFO cleanup hooks, run at unmount

// Tree mutation (loop goroutine only; illegal inside Layout/Render).
func (c *Context) Mount(child Component)     // mount child under me (§2.4)
func (c *Context) Unmount(child Component)   // unmount cascade (§2.4)

// Layout-phase helpers — legal ONLY inside this component's Layout call (§2.7).
func (c *Context) LayoutChild(child Component, cc Constraints) Size
func (c *Context) PlaceChild(child Component, r Rect) // r is parent-relative

// App handles (ADR-0005 owns semantics).
func (c *Context) App() *App
func (c *Context) Post(ev Event)                                  // any goroutine
func (c *Context) Go(task Task, opts ...TaskOption) TaskID        // owner = c.ID()
func (c *Context) Bus() *Bus
func (c *Context) After(d time.Duration) (cancel func())          // one-shot TickEvent to me
func (c *Context) Every(d time.Duration) (cancel func())          // repeating TickEvent to me
```

`Ctx()` is the async-lifetime anchor: task contexts derive from it (ADR-0005 §2.8), so
unmount kills in-flight work without any bookkeeping in the component. `OnUnmount` is the
generic cleanup seam — `SubscribeScoped` (ADR-0005 §2.7) and `After`/`Every` register
their cancels through it.

> **Rev 1 (Lector should-fix, naming).** The names above are the **canonical cross-ADR
> names**, binding on ADRs 0005–0007: `ID()` (not `NodeID()`), `RequestLayout()` (not
> `MarkLayoutDirty()`), and `Ctx()` for the unmount-cancelled context (there is no
> `UnmountCtx()` — ADR-0007's snippet using it was wrong and is being normalized to
> match this section).

### 2.3 Capability interfaces

Optional interfaces a component implements to opt into framework behavior — the golib
small-seam style (archetype `logger.Logger`; cf. `server.Session`/`server.Drainer`,
`server/registry.go:12-19`, where the optional `Drainer` upgrade is discovered by type
assertion exactly as here).

```go
// Focusable opts a component into the focus system (§2.6). Components that are
// not Focusable are skipped by traversal and can never hold focus.
type Focusable interface {
    Component
    AcceptsFocus() bool // false = temporarily unfocusable (e.g. disabled input)
}

// Container is the public child-management surface for composite widgets
// (widget.Flex, widget.Dock, widget.Stack, … — ADR-0007). The framework's own
// parent/child links are built by Context.Mount/Unmount (§2.4); Container is the
// user-facing mutation API layered on top of them, plus enumeration for
// traversal-order documentation, devtools, and tests.
type Container interface {
    Component
    Add(children ...Component)      // mounts immediately if the container is mounted;
                                    // otherwise deferred to the container's Init
    Remove(child Component)         // unmount cascade (§2.4)
    Children() iter.Seq[Component]  // document order == focus order == paint order
}

// CursorReporter implements the IME real-cursor rule (G6). When the focused
// component (or, transitively, a focused descendant's ancestor chain — the
// framework asks the focused node only) reports ok=true, the runtime parks the
// REAL terminal cursor at the reported position, translated to absolute
// coordinates through the node's laid-out Rect chain, and shows it. ok=false or
// no implementation → hardware cursor hidden. Rationale: OS IMEs anchor the
// composition window to the hardware cursor — a fake drawn cursor puts CJK
// candidate windows at (0,0) (https://github.com/xtermjs/xterm.js/issues/5734);
// bubbletea v2 made cursor position declarative for the same reason. Also aids
// screen readers.
type CursorReporter interface {
    Component
    Cursor() (x, y int, ok bool) // local (Surface) coordinates
}

// FocusScope marks a subtree as a traversal boundary (§2.6). A trapping scope
// (modal, floating window) confines Tab/Shift-Tab to its subtree.
type FocusScope interface {
    Component
    TrapsFocus() bool
}
```

**No `MouseTarget` interface.** Mouse events route through `HandleEvent` like everything
else; hit-testing (§2.5.3) selects the target from laid-out rects, and the `bool` return
already expresses consumption. A separate interface would add surface without
capability. (Considered and rejected; recorded here because the design brief raised it.)

### 2.4 Tree mechanics: mount, unmount, identity

**Node table and the component identity contract.** The runtime owns
`map[NodeID]*node` (authoritative — every internal path is ID-keyed) plus a
`map[Component]*node` identity index that serves the **public Component-keyed API**:
`Context.Unmount(child Component)`, `Container.Remove`, and the layout helpers
(`LayoutChild`/`PlaceChild`). A Go interface value is only a legal map key when its
dynamic value is comparable, so the identity index forces a normative contract:

> **A `Component`'s dynamic type MUST be comparable. Normatively, components are
> POINTERS to their state structs** — every `tui/widget` type complies (ADR-0007), and
> pointer identity is exactly the "one node per mounted component value" semantics the
> tree wants.

At mount the runtime verifies `reflect.TypeOf(c).Comparable()` and panics with
`tui: component type T is not comparable; use a pointer component` — misuse fails at
construction, loudly and at the offending call site (precedent `server.NewScaffold`'s
nil-arg panic, `server/scaffold.go:87-90`). Mounting the same component value twice
simultaneously panics for the same reason. Each `node` holds: component, parent, ordered
children, `*Context`, parent-relative `Rect`, absolute `Rect` (derived), dirty flags,
and the unmount-cancel func.

> **Rev 1 (Lector must-fix #4).** The draft keyed `map[Component]*node` with no
> comparability contract: a user-defined value component embedding a slice/map/func
> would panic deep inside runtime map code at first insert or lookup — including from
> the public `Context.Unmount(child Component)` path — with no actionable message. The
> contract is now normative (pointer/comparable components), enforced eagerly at mount
> with a targeted panic, and stated in the `Component` doc comment (§2.1).

**NodeID assignment.** `NodeID` is a `uint64` drawn from a monotonic per-App counter at
mount, starting at 1; **0 is reserved as "no node"** (used by ADR-0005 for
terminal-level events). IDs are never reused for the lifetime of the App, which is what
makes ADR-0005's stale-result dropping and monotonic staleness checks sound: a late
`TaskResult` can never collide with a recycled ID.

```go
type NodeID uint64 // 0 = none; assigned monotonically at mount; never reused
```

**Mount cascade** (`Context.Mount`, or `NewApp`'s root at `Run`):

1. Allocate `NodeID`; create the node; link into the parent's child list (append —
   mount order is document order).
2. Derive the node's `context.Context` from the **parent node's** context
   (`context.WithCancel(parent.Ctx())`) — subtree cancellation then composes for free:
   cancelling an ancestor cancels every descendant, with zero bookkeeping.
3. Build `*Context`; call `Init(ctx)`. `Init` may itself `Mount` children — the cascade
   is depth-first and re-entrant.
4. Mark the parent layout-dirty (a new child means geometry may change).

**Unmount cascade** (`Context.Unmount`, `Container.Remove`, or App teardown), children
first, depth-first:

1. Cancel the node's `context.Context` → in-flight tasks owned by the node die
   (ADR-0005 §2.8).
2. Run `OnUnmount` hooks LIFO → bus subscriptions and timers cancel (the LIFO ordering
   mirrors the deliberate defer stacking in `server/ws/ws.go:230-232`, where teardown
   order is load-bearing).
3. If the focused node is inside the unmounted subtree: focus repair (§2.6.4).
4. Remove from the node tables. From this instant the runtime drops any queued or future
   event addressed to the ID (ADR-0005 §2.8) — the dead-letter rule.
5. Mark the parent layout-dirty.

There is no `Unmount` method on `Component`: cleanup is expressed through `Ctx()`
cancellation and `OnUnmount` hooks, keeping the mandatory interface at four methods and
making cleanup composable rather than override-and-forget.

### 2.5 Event routing — target-then-bubble

**Decision: target-then-bubble-to-root, no capture phase.** The runtime resolves a
single **target node** per routed event, calls its `HandleEvent`, and — while handlers
return `false` — walks parent links to the root. The first `true` consumes the event and
stops the walk.

Justification: bubbling is what every surveyed retained TUI actually uses (Textual
bubbles unhandled keys ancestor-ward, https://textual.textualize.io/guide/events/; tview
delegates through `InputHandler` chains); a DOM-style capture phase exists so ancestors
can intercept *before* the target, and the two TUI needs it serves — modal input
trapping and app-global keys — are served more directly by focus scopes (§2.6.3) and by
the root being the natural last stop of the bubble. Capture would double the tree walk
per event and force every component to reason about two visitation phases. It remains
additive later (N1).

Per event type (concrete types in ADR-0005 §2.5):

1. **KeyEvent / PasteEvent** — target = the focused node (§2.6); no focused node →
   target = root. Bubbles. An unconsumed key reaching the root is offered to the App's
   global keymap (ADR-0005 §2.2 owns the option) — this is how Tab traversal is
   implemented (§2.6.2): a component that consumes Tab (e.g. a text area inserting \t)
   thereby opts out of traversal for that press, exactly the right default.
2. **MouseEvent** — target by **hit-testing** the laid-out absolute rects: walk the tree
   from the root, descending into children **in reverse paint order** (topmost first —
   `Stack` z-order, §2.7.4) and picking the deepest node whose absolute `Rect` contains
   the point. Bubbles from that target. Coordinates are rewritten to be **local to each
   receiving node** at every hop, so handlers never do rect math against globals. This
   is the framework-owned answer to bubblezone's marker-scanning workaround
   (lrstanley/bubblezone; dossier §2) — a layout tree makes hit-testing a table lookup.
3. **FocusEvent** — addressed to the node losing focus (`Gained=false`) then the node
   gaining it (`Gained=true`); bubbles, so ancestor panels can restyle when a descendant
   focuses (the lazygit active-panel border pattern).
4. **TickEvent / TaskResult / TaskProgress** — **addressed, no bubble.** These are
   private deliveries to a known owner (ADR-0005 §2.6, §2.8); propagating them to
   ancestors would leak implementation detail. Unconsumed = silently done.
5. **ResizeEvent** — not routed through the tree at all: the runtime updates the root
   constraints, marks the tree layout-dirty, and publishes the event on the `Bus`
   (ADR-0005 §2.9) for components that care about raw dimensions.

### 2.6 Focus management — framework-owned

Contrast fixed in ADR-0001 §1.1: tview has no global traversal (apps hand-wire Tab);
bubbletea has no focus system at all. Here the framework owns the whole chain.

**2.6.1 Focus state.** The App holds one focused `NodeID` (0 = none). Focus moves only
via: `Context.RequestFocus()`, traversal (2.6.2), focus repair (2.6.4), or scope
push/pop (2.6.3). Every move delivers `FocusEvent{Gained:false}` to the loser, then
`FocusEvent{Gained:true}` to the gainer (§2.5.3). Notification is event-only — there are
no `Focus()`/`Blur()` methods to implement; one entry point (`HandleEvent`) for all
stimuli (contrast tview's three-method `Focus/Blur/HasFocus` surface).

**2.6.2 Traversal.** Tab / Shift-Tab, implemented as the App-level fallback for
unconsumed Tab keys (§2.5.1): pre-order depth-first walk of the tree in child (document)
order, filtered to nodes that (a) implement `Focusable`, (b) report `AcceptsFocus()`,
and (c) were laid out in the current frame with a non-empty `Rect` (invisible components
are not tab stops). Wraps around at the ends. Mount order defines document order;
containers therefore mount children in visual order (ADR-0007 widgets guarantee this).

**2.6.3 Focus scopes.** Traversal is confined to the subtree of the nearest ancestor
(inclusive) implementing `FocusScope` with `TrapsFocus() == true`; the root is an
implicit non-trapping scope. A floating window or modal (a `Stack` child, §2.7.4)
implements `FocusScope` and becomes a **focus trap**: Tab cycles inside it and cannot
escape — the standard modal contract. When focus enters a trapping scope via
`RequestFocus`, the previously focused `NodeID` is pushed on a scope stack; when the
scope unmounts, focus **restores** to that node if it is still mounted and focusable
(else focus repair, 2.6.4). This gives open-modal → interact → close-modal →
focus-where-you-were with zero app code.

**2.6.4 Focus repair.** When the focused node unmounts (or its scope dies with a dead
restore target): focus moves to the next focusable in traversal order within the
innermost surviving scope, or to 0 (none) when the scope has no focusables. Repair
happens inside the unmount cascade (§2.4 step 3) so no frame ever renders with a
dangling focus ID.

### 2.7 Layout — constraints down, sizes up, parent positions

Flutter's box protocol verbatim (https://docs.flutter.dev/resources/inside-flutter):
parent passes `Constraints` down; child picks a `Size` within them and returns it up;
parent positions the child. **One pass**, no measurement round-trips, geometry fully
determined after the pass.

**2.7.1 Constraints semantics.**

```go
// Constraints bound the Size a child may return: MinW <= W <= MaxW,
// MinH <= H <= MaxH. Invariants: 0 <= Min <= Max per axis.
type Constraints struct{ MinW, MaxW, MinH, MaxH int }

// Unbounded marks an axis with no maximum (scrollable viewports pass it to
// their content).
const Unbounded = math.MaxInt

func Tight(s Size) Constraints          // Min == Max == s: child has no choice
func Loose(s Size) Constraints          // Min = 0, Max = s
func (c Constraints) Constrain(s Size) Size // clamp s into c
func (c Constraints) IsTight() bool
```

- **Tight** (min==max): the child's answer is forced — parents use it to impose exact
  cells (`Dock` center fill, root = terminal size).
- **Loose** (min 0): the child sizes to content up to the max.
- **Unbounded max**: only scroll viewports pass it, only on the scroll axis. A child
  asked for unbounded must return its intrinsic extent, never `Unbounded`.

A returned `Size` outside the constraints is a component bug: the framework **clamps**
it (`Constrain`) so a misbehaving widget cannot corrupt sibling geometry, and records
the violation — Flutter's debug-assert posture adapted to golib's std-testing culture.
The diagnostic is concrete and **failable in tests**:

```go
// ConstraintViolation records one clamped Layout return (kept per run,
// bounded). Production apps observe them via WithLogger (ADR-0005 §2.1);
// TestBackend retains them for assertion.
type ConstraintViolation struct {
    Node NodeID
    Type string      // dynamic component type, for the failure message
    Got  Size        // what Layout returned
    C    Constraints // what it was given
}

func (b *TestBackend) ConstraintViolations() []ConstraintViolation

// FailOnViolations fails t (with one line per violation) when the run clamped
// anything — widget test suites call it in a helper/cleanup so silent clamps
// cannot ship.
func FailOnViolations(t *testing.T, b *TestBackend)
```

> **Rev 1 (Lector should-fix / Q3 answer).** The draft's "test-visible diagnostic" was
> unspecified. Lector confirmed clamp-and-report for production (no panic-in-dev mode
> required) but required the diagnostic to be failable in tests; the
> `ConstraintViolations()`/`FailOnViolations` hook above is that mechanism.

**2.7.2 `Flex`.** Direction `Horizontal`/`Vertical`. Each child is either **fixed**
(laid out first, with loose main-axis constraints, its measured extent consumed) or
**weighted** (`Weight(n)`, n ≥ 1). After fixed children, the remainder `R` of the main
axis is distributed to weighted children by **integer largest-remainder**:

1. `Wsum = Σ wᵢ`; child i's ideal share is `R·wᵢ/Wsum`.
2. Assign `floor(R·wᵢ/Wsum)` to each; `r = R − Σ floors` cells remain (`0 ≤ r < n`).
3. Give one extra cell each to the `r` children with the largest fractional remainders
   `(R·wᵢ) mod Wsum`, ties broken by **lowest child index**.

This is deterministic and gap-free by construction: `Σ assigned == R` always, every run,
every platform. It is the integer-exact equivalent of why Textual computes layout in
exact `fractions.Fraction` rather than floats — naive per-child rounding leaves 1-cell
gaps or overflows that jitter across resizes (https://textual.textualize.io/guide/layout/).
tview's proportional-only integer split (no min/max, no measurement — rivo/tview
`flex.go`) is the baseline we strictly exceed: fixed children are *measured* (loose
constraints), and per-child `Min`/`Max` clamps apply before distribution. Cross-axis:
children get the flex's cross extent as a tight constraint (stretch), the container's
size is the parent constraint clamped around the children.

**2.7.3 `Dock`.** Children pin to `Top`/`Bottom`/`Left`/`Right` in declaration order,
each measured (loose on its pinned axis, tight on the other) and consuming its extent
from the remaining rect; the final `Center` child fills what is left under tight
constraints. Status bars, side panels, command logs — the lazygit chrome — are
`Dock`+`Flex` compositions; the dossier's survey concludes flex+dock covers the
lazygit/sqlit layout class completely (dossier §3, ratatui/Textual comparison).

**2.7.4 `Stack`.** Z-ordered layering for floating windows, modals, dropdown popups,
toasts. Children are laid out in order with **loose** constraints of the stack's full
area and positioned by alignment or explicit offset; **later children paint on top**;
hit-testing visits them in reverse (§2.5.2), so the topmost layer wins the mouse. A
modal layer = `Stack` child implementing `FocusScope` (§2.6.3) that consumes all mouse
events on its backdrop. `Stack` is the mechanism ADR-0007's `FloatingWindow` builds on.

**2.7.5 Relayout triggering.** Two dirty bits per node, set via `Context`:

- `MarkDirty()` — **render dirt**: content changed, geometry did not (cursor blink,
  selection highlight, spinner frame). The next frame repaints the node's subtree into
  the cell buffer; **no layout pass runs**. This is the overwhelmingly common case and
  costs O(subtree cells) + cell diff (ADR-0003).
- `RequestLayout()` — **layout dirt**: the node's size may have changed (text grew, a
  child was added — mount/unmount set it implicitly, §2.4). The next frame runs **one
  full layout pass from the root** (terminal size as tight constraints), then repaints.
  v1 deliberately relayouts the whole tree rather than tracking Flutter's relayout
  boundaries: the pass is O(visible nodes) of integer arithmetic — the dossier's
  estimate of ~50 lines of protocol (§3) is borne out by the algorithms above — and
  boundaries are a pure optimization addable later behind the same two bits (N2).
- `ResizeEvent` sets layout dirt at the root **and** full render dirt (diffing against a
  wrong-sized previous frame is worse than repainting — dossier §8 resize-storm rules;
  coalescing is ADR-0005 §2.4's job, frame pacing ADR-0003's).

Dirty marks only schedule a frame (they wake the loop — ADR-0005 §2.4 borrows the
`server/registry.go:46-49` close-and-replace wake idiom); rendering never happens
synchronously inside a handler, so N marks per event coalesce into one frame for free.

## 3. Consequences

**Positive**

- Routing, focus, hit-testing, and layout — the four things every bubbletea app above
  toy size reimplements (§1 evidence) — are framework primitives with testable
  contracts. Widget code (ADR-0007) contains zero rect math for input.
- Context-derivation at mount (§2.4.2) makes lifetime management compositional: subtree
  teardown, task cancellation, and subscription cleanup are one `cancel()` deep.
- Never-reused `NodeID`s make ADR-0005's addressed delivery sound with a plain map
  lookup and no generation counters.
- Deterministic layout (largest-remainder, fixed tie-break) means `TestBackend`
  screen-shot tests are stable across platforms and runs — no float, no map-order, no
  solver nondeterminism.

**Negative (costs)**

- Whole-tree relayout on any layout dirt is O(visible tree) per dirty frame. Acceptable
  for the target class (hundreds of nodes, integer math); pathological trees (10⁵ nodes)
  would need N2's relayout boundaries.
- Mount-order-as-focus-order couples traversal to construction discipline; a container
  that mounts out of visual order produces surprising Tab order (mitigated by ADR-0007
  widgets guaranteeing visual-order mounting; Q4 covers a positional sort alternative).
- Four capability interfaces (`Focusable`, `Container`, `CursorReporter`, `FocusScope`)
  are more seams than tview's one `Primitive`; each is small and optional, but the
  concept count is real.

**Migration / evolution**

- Relayout boundaries (Flutter's tight/same-constraints skip) slot behind
  `RequestLayout()` with no API change.
- A capture phase adds as a `Capturer` capability without touching `HandleEvent`.
- A declarative layer (Xilem-style view diffing,
  https://raphlinus.github.io/rust/gui/2022/05/07/ui-architecture.html) can mutate this
  retained tree from above, as ADR-0001 §2.4(1) anticipates.

## 4. Alternatives considered

1. **Elm / immediate-mode composition (bubbletea).** Rejected at the architecture level
   by ADR-0001 §4.3; at this ADR's level the specific evidence is routing and focus:
   with no component addressing, every nested-model app hand-writes message fan-out
   (discussions #176/#751/#943), and focus is a per-app convention — leg100's write-up
   documents the "root model as message router and screen compositor" end state. The
   never-landed `Wrap()` addressing PR (#936, ADR-0005 §4.2) shows the gap is
   structural, not missing polish.
2. **Flutter's three trees (widget/element/render + reconciliation).** Rejected by
   ADR-0001 §4.4; the detail this ADR adds: reconciliation (`canUpdate`, keyed O(N)
   child matching — https://docs.flutter.dev/resources/inside-flutter) exists to map
   freely rebuilt immutable configs onto persistent state. With mutable components there
   is nothing to reconcile; we keep only the layout protocol, which is the part that
   carries its weight.
3. **Cassowary constraint solver (ratatui's Layout).** Rejected (ADR-0001 N3): ratatui
   is the only major TUI paying for a solver and documents non-determinism on infeasible
   constraint sets (https://docs.rs/ratatui/latest/ratatui/layout/struct.Layout.html);
   flex+dock+stack covers the target layout class with integer determinism (§2.7.2).
4. **gocui's layout-every-event immediate mode.** `Manager.Layout(*Gui)` re-runs on
   every event over retained view buffers (lazygit `pkg/gocui`; dossier §2) — simple and
   proven for exactly one app shape, but geometry is re-derived from scratch per
   keystroke, there is no content measurement, and every layout is hand-positioned
   math. Retained rects with dirty bits do strictly less work and feed hit-testing and
   focus for free. Jesse Duffield's own assessment is that owning this layer is a
   maintenance burden lazygit accepted for lack of an alternative
   (https://github.com/jesseduffield/lazygit/issues/2705#issuecomment-1575277807) —
   this ADR is that alternative.
5. **DOM-style capture-then-bubble.** Rejected for v1 (§2.5, N1): doubles the per-event
   tree walk and the component author's mental model; its two genuine uses (modal
   trapping, global keys) are served by focus scopes and root fallback. Additive later.
6. **bubblezone-style marker hit-testing.** Rejected: it exists to retrofit hit-testing
   onto a framework with no layout tree (scan composed output for zero-width markers —
   lrstanley/bubblezone); we own laid-out rects, making it a strictly-worse redundancy.
7. **`Focus()/Blur()` methods on `Focusable` (tview's shape).** Rejected in favor of
   `FocusEvent` through `HandleEvent`: one stimulus entry point, no half-implemented
   notification methods, and focus changes become recordable/replayable events in
   `TestBackend` scripts like everything else. (Q1 revisits.)

## 5. Acceptance criteria

1. **Lifecycle order**: a `TestBackend` instrumented component records exactly
   `Init → Layout → Render` on mount; `HandleEvent` never precedes `Init`; no method
   call lands after unmount. Remount assigns a fresh, strictly larger `NodeID`.
2. **Cascade**: unmounting a subtree of depth ≥ 3 cancels every descendant's `Ctx()`
   (observed via `context.AfterFunc`), runs `OnUnmount` hooks LIFO, children before
   parents, and a `TaskResult` posted afterward for a dead ID is dropped (ADR-0005
   test hook observes the dead-letter count).
3. **Routing**: a key event reaches focused-leaf → parent → root in order; a `true`
   return at any hop stops the walk (table-driven over consumption points). Mouse
   hit-testing on overlapping `Stack` layers targets the topmost, and delivered
   coordinates are local at every hop.
4. **Focus**: Tab from the last focusable wraps to the first; `AcceptsFocus() == false`
   and zero-`Rect` nodes are skipped; a trapping `FocusScope` confines Tab within its
   subtree; closing the scope restores focus to the pre-modal node; unmounting the
   focused node repairs focus within the same frame (no frame renders a dangling ID).
5. **Layout determinism**: table-driven `Flex` cases — `R=10` over weights `(1,1,1)` →
   `4,3,3`; `R=7` over `(2,3)` → `3,4`; ties broken by index — byte-identical
   `TestBackend` frames across 100 runs. `Σ assigned == R` proven for a fuzz range of
   `(R, weights)`.
6. **Constraint discipline**: a child returning `Size` outside its `Constraints` is
   clamped and siblings are unaffected; the violation appears in
   `TestBackend.ConstraintViolations()` with the offending node/type/sizes, and
   `FailOnViolations` fails the test (rev 1).
7. **Relayout economy**: `MarkDirty()` alone triggers a frame with **no** `Layout` calls
   (instrumented count == 0); `RequestLayout()` triggers exactly one full pass;
   `ResizeEvent` produces one pass plus a full repaint.
8. **Cursor rule**: focusing a `CursorReporter` that reports `(x,y,true)` parks the
   backend cursor at the absolute translation of `(x,y)` and shows it; focusing a
   non-reporter hides it (asserted against `TestBackend`'s cursor state).
9. **Identity contract** (rev 1): mounting a component whose dynamic type is not
   comparable (e.g. a func-typed component or a value struct embedding a slice) panics
   at `Mount` with `tui: component type … is not comparable; use a pointer component`;
   pointer components mount normally; mounting the same pointer twice simultaneously
   panics.

## 6. Questions for the reviewer

- **Q1.** Focus notification is `FocusEvent` through `HandleEvent` only — no
  `Focus()/Blur()` methods (§2.3, §4.7). tview readers will expect methods. Is
  event-only the right call, or should `Focusable` carry optional `Focused(bool)`
  convenience despite the dual-path cost?
  — **Lector r1:** event-only is right; do not add `Focus()/Blur()` methods.
- **Q2.** v1 relayouts the whole visible tree on any layout dirt (§2.7.5, N2). Given the
  lazygit/sqlit target (~10²–10³ nodes) this is well inside budget, but it makes
  `RequestLayout()` in a per-keystroke path O(tree). Accept for v1, or require Flutter's
  relayout-boundary skip (tight-constraints subtrees) from the start?
  — **Lector r1:** whole-tree relayout accepted for v1; add boundaries after profiling,
  not before.
- **Q3.** Out-of-constraints `Size` is clamped with a `TestBackend`-visible diagnostic
  rather than panicking (§2.7.1). golib's fail-loud posture could argue for a panic in a
  debug/dev mode instead. Clamp-and-report, or panic-in-dev?
  — **Lector r1:** clamp-and-report acceptable in production; no panic-in-dev required —
  but the test diagnostic must be *failable* so widget authors notice (folded: §2.7.1's
  `ConstraintViolations()`/`FailOnViolations`).
- **Q4.** Focus traversal order = mount/document order (§2.6.2). Should v1 instead sort
  tab stops by laid-out position (row-major visual order), which survives out-of-order
  mounting but makes traversal order resize-dependent?
  — **Lector r1:** keep mount/document order; visual-order sorting makes tab order
  resize-dependent and harder to reason about.
- **Q5.** `Container` is a core capability interface with `Add/Remove/Children`
  (§2.3). The alternative is keeping child mutation entirely in ADR-0007's widgets
  (core exposes only `Context.Mount/Unmount`), shrinking core's surface but losing a
  uniform enumeration seam for devtools/traversal tests. Keep `Container` in core?
  — **Lector r1:** keep `Container` in core; uniform child enumeration is worth the
  small interface surface.
