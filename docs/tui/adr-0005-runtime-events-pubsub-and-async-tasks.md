
# ADR-0005 — golib/tui: Runtime, Events, Pub/Sub & Async Tasks

**Tags:** `type:adr` `status:accepted` `owner:shared` `repo:golib` `area:tui` `kind:runtime` `kind:events` `kind:pubsub` `kind:async` `kind:concurrency`

**Abstract:** Specifies the `tui.App` master runtime: `NewApp` construction with
functional options, the `Run(ctx)` lifecycle mirroring `server.Scaffold`, the normative
single-goroutine invariant, the two-lane event queue with a never-block/never-drop
`Post` (panic only under the opt-in `WithEventQueueLimit`), the concrete `Event` type
set (kitty-protocol keys included), demand-scheduled
timers, the typed `Bus` with enqueue-only publish, and `App.Go` — semaphore-bounded async
tasks whose results are posted as `TaskResult` events addressed to the owning `NodeID`,
cancelled at unmount, with exclusive groups, monotonic staleness IDs, per-task panic
isolation, and `TaskProgress` streaming via `Post`.

> **Authored natively in the KB** (jarvis, 2026-07-08) as part of the golib/tui design
> dossier. golib/tui keeps its own 0001–0007 ADR numbering; the `golib-tui-` filename
> prefix namespaces it against this KB's auto-agents ADR sequence. Navigation hub:
> golib-tui. Umbrella ADR: golib-tui-0001-overview-and-architecture.

- **Status:** **Accepted** (Johno, 2026-07-08) — revision 1 (Lector r1 `change_requested` → r2 `approved_with_amendments`, all amendments folded).
- **Date:** 2026-07-08
- **Reviewed by:** lector, 2026-07-08 — agents/lector/reviews/2026-07-08-golib-tui-adrs-review.md (r1); agents/lector/reviews/2026-07-08-golib-tui-adrs-rereview.md (r2, `approved_with_amendments`)
- **Module:** github.com/yongjohnlee80/golib
- **Supersedes:** none (greenfield)
- **Related:** ADR-0001 (fixes `App`/`Event`/`Bus`/`NodeID` vocabulary and the
  single-loop decision this ADR elaborates), ADR-0002 (the `Backend` whose `Events()`
  channel, `Err()` accessor, and lifecycle this loop consumes; rev 1 moves ALL input
  queue policy out of the backend into this ADR's intake stage), ADR-0003 (frame
  scheduling the loop triggers), ADR-0004 (the tree, routing, and `Context` this runtime
  drives; unmount semantics the task system depends on), golib server ADR-0006 (the
  transport scaffold whose `Run(ctx)`/drain lifecycle `App.Run` deliberately mirrors).

> **Revision history.** rev 1 (2026-07-08): folded Lector r1 `change_requested` —
> (1) **must-fix #2**: input-queue ownership resolved per Lector's stated preference —
> the backend only decodes and emits ordered, un-coalesced events and gains
> `Err() error`; an App-owned **intake stage** now applies all lane-A policy
> (drop-oldest, resize latest-wins, motion coalescing, `WithInputQueueSize`) under a
> normative promptness invariant (§2.2, §2.4); (2) **should-fix (Q2 answer)**: the
> `runtime.Stack` goroutine-id parse is dropped — `Update` always enqueues; in-handler
> code mutates directly (§2.4, §3, §5.2); (3) **Q1 answer**: unbounded lane B kept, with
> mandatory high-water-mark logging + test and a new optional `WithEventQueueLimit(n)`
> fail-fast ceiling (§2.1, §2.4, §5.11); (4) Q3 (flat pool of 16), Q4 (`TaskProgress`
> convention, no `GoStream`), Q5 (`PanicRepanic` default) confirmed unchanged — answers
> appended in §6.

> **Self-containment contract.** This ADR is implementable with no prior context beyond
> the golib conventions (golang, golib) and the sibling ADRs listed under Related.

## 1. Context

ADR-0001 §2.4(2) fixed the concurrency model: **one loop goroutine owns all component
state; everything else enters as posted events**. This ADR makes that normative and
designs the machinery, informed by the failure modes of every surveyed runtime:

- **tview** selects over tcell events and an `updates` channel whose `QueueUpdate`
  **blocks — and deadlocks when called from inside the event loop** (documented footgun:
  https://github.com/rivo/tview/wiki/Concurrency; dossier §2).
- **gocui** (lazygit's vendored copy, `pkg/gocui`) queues closures via `Gui.Update` and
  **panics at 256 queued** rather than blocking (dossier §2). Panic beats deadlock, but
  both are the queue design punishing the caller.
- **bubbletea** runs every `Cmd` in its own goroutine, fire-and-forget, and delivers the
  resulting `Msg` to the **root `Update` only** — no addressing; correlating an async
  result to the subcomponent that asked is manual plumbing in every app (discussions
  https://github.com/charmbracelet/bubbletea/discussions/176, /751, /943; the `Wrap()`
  addressing draft PR https://github.com/charmbracelet/bubbletea/pull/936 never landed).
  Its v1 renderer also runs an **always-on 60fps ticker** even when idle (dossier §2).
- **Textual** gives every widget an asyncio task + mailbox — measured cheap in Python
  (https://textual.textualize.io/blog/2023/03/08/overhead-of-python-asyncio-tasks/) but
  an asyncio artifact; in Go it buys ordering hazards against a shared screen
  (ADR-0001 N5). Its **workers** system, however, is the right async model: results
  delivered into the owning widget's context
  (https://textual.textualize.io/guide/workers/), which lazygit's `pkg/tasks`
  independently converges on (per-view managers, monotonic task IDs, preemption —
  dossier §2).

golib already ships the lifecycle idioms this runtime needs, and this ADR borrows them
deliberately: `server.Scaffold.Run`'s synchronous-bind/goroutine/select/`errors.Join`
shape (`server/scaffold.go:144-168`), its panic-isolated worker goroutines
(`server/scaffold.go:201-214`), the registry's close-and-replace wake broadcast
(`server/registry.go:46-49`) and deadline-bounded drain (`server/registry.go:155-198`),
the ws package's one-reader contract and `sync.Once`/`done chan struct{}` teardown
(`server/ws/ws.go:36-48`, `server/ws/ws.go:97-117`), and the ingestor's
semaphore-bounded background work with a synchronous `ctx.Done` fallback
(`ingestor/writer.go:28`, `ingestor/writer.go:62-83`).

### 1.1 Goals

- **G1** — `App.Run(ctx)` with scaffold-grade lifecycle: synchronous backend start
  (errors before any goroutine), select-driven loop, `errors.Join` teardown, and a
  **guaranteed terminal restore even on panic**.
- **G2** — A `Post` that never blocks, never deadlocks, and never drops program events —
  designed explicitly around gocui's panic-at-256 and tview's self-deadlock. Panic occurs
  only under an explicit `WithEventQueueLimit` opt-in (rev 1, Lector Q1).
- **G3** — Async results **addressed to the originating component** (`NodeID`), delivered
  on the loop goroutine, dead-lettered after unmount — the task-objective-6 primitive.
- **G4** — Typed pub/sub with zero reflection at dispatch, enqueue-only publish, and
  unmount-scoped subscriptions.
- **G5** — Idle apps schedule nothing: no ticker, no timer, no wakeups — zero bytes and
  zero CPU at rest (with ADR-0003's render-on-dirty this completes ADR-0001 G5).
- **G6** — Bounded async execution: a semaphore-capped pool (the `ingestor` pattern), so
  a burst of `Go` calls cannot spawn unbounded concurrent work.

### 1.2 Non-goals

- **N1** — Parallel component updates or actor mailboxes (ADR-0001 N5; §4.1).
- **N2** — A general job system (retries, priorities, persistence). `App.Go` is UI
  async glue; real pipelines belong to application code or `ingestor`-like packages.
- **N3** — Frame pacing and diff/flush mechanics — ADR-0003 owns them; this ADR only
  wakes the frame scheduler.
- **N4** — Backend event *production* (parser, capability probe, input goroutine
  internals) — ADR-0002 owns the `Events() <-chan Event` contract consumed here.

## 2. Decision

### 2.1 Construction: `NewApp` + functional `AppOption`

```go
// NewApp builds the master runtime around a root component. It panics on nil
// root or missing backend — misconfiguration fails at construction (golib
// convention; precedent server.NewScaffold, server/scaffold.go:87-90).
func NewApp(root Component, opts ...AppOption) *App

type AppOption func(*appConfig)

// WithBackend sets the driver (REQUIRED). There is no default: the core tui
// package cannot construct term.Backend (tui/term imports tui, not vice versa
// — ADR-0001 §2.2), and a hidden registry/init() default is forbidden by golib
// philosophy. Real apps pass term.New(...); tests pass tui.NewTestBackend().
func WithBackend(b Backend) AppOption

// WithTheme sets the initial style.Theme (ADR-0006). Default: style.Default().
func WithTheme(t *style.Theme) AppOption

// WithMinFrameInterval caps the render rate: dirty marks arriving faster are
// coalesced into one frame per interval (ADR-0003). Default 16ms (~60fps cap).
// This is a CAP, not a ticker — no dirt, no frame, no wakeup (G5).
func WithMinFrameInterval(d time.Duration) AppOption

// WithPanicPolicy selects what Run does after a loop/handler panic has been
// recovered and the terminal restored (§2.2): PanicRepanic (default — the
// panic propagates with its original stack) or PanicReturn (Run returns it as
// an error wrapping ErrPanic).
func WithPanicPolicy(p PanicPolicy) AppOption

// WithInputQueueSize sets the capacity of the App-owned input intake queue
// (lane A) fed from backend.Events() (default 256; §2.4). Rev 1: this queue —
// and all coalescing/overflow policy — belongs to the App, not the backend.
func WithInputQueueSize(n int) AppOption

// WithEventQueueLimit sets an OPTIONAL hard ceiling on pending lane-B program
// events (default: unlimited; §2.4). Exceeding it PANICS with
// "tui: program event queue exceeded N — runaway producer": lane-B growth past
// any sane bound is an app bug, and apps preferring fail-fast crash detection
// over memory growth opt in here. (Rev 1, Lector Q1.)
func WithEventQueueLimit(n int) AppOption

// WithTaskPoolSize bounds concurrently RUNNING tasks (default 16; §2.8).
func WithTaskPoolSize(n int) AppOption

// WithWidthPolicy fixes the App-wide grapheme width policy (ADR-0003 §2.4:
// WidthPolicyDefault = East Asian Ambiguous narrow; WidthPolicyAmbiguousWide
// for CJK-legacy contexts). The policy travels with every Surface's resolution
// context; components measure via Surface.StringWidth to respect it.
// Default: WidthPolicyDefault. (Rev 1: companion to ADR-0003 must-fix #3.)
func WithWidthPolicy(p WidthPolicy) AppOption

// WithTaskDrainTimeout bounds how long Run waits for in-flight tasks after the
// tree unmounts at shutdown (default 5s; §2.2 step T3).
func WithTaskDrainTimeout(d time.Duration) AppOption

// WithLogger sets the diagnostics logger for queue high-water marks, dropped
// events, dead-lettered results, and recovered task panics (default logger.Nop{};
// precedent server/scaffold.go:49-51).
func WithLogger(l logger.Logger) AppOption
```

### 2.2 `Run(ctx)` — the scaffold lifecycle, terminal edition

`App.Run` mirrors `server.Scaffold.Run` (`server/scaffold.go:144-168`) stage for stage —
synchronous resource acquisition, goroutine feeding a channel, one select, `errors.Join`
teardown — with one addition the terminal forces: **restore-before-repanic**.

```go
// Run starts the backend synchronously (raw mode, alternate screen, capability
// probe — ADR-0002; errors return before any goroutine starts, mirroring the
// scaffold's synchronous bind at server/scaffold.go:144-147), mounts root, runs
// the event loop ON THE CALLING GOROUTINE until ctx is cancelled or a terminal
// backend error occurs, then unmounts the tree, drains tasks, and stops the
// backend. The returned error is errors.Join of the loop error and teardown
// errors (never swallowed — the scaffold's rule, server/scaffold.go:157,166).
//
// PANIC CONTRACT: any panic on the loop goroutine (component handler, layout,
// render, or runtime bug) is recovered, the terminal is restored FIRST
// (backend.Stop always runs), and then the policy applies — default: repanic
// with the original value. A TUI that dies must never leave the terminal in
// raw mode with the alternate screen active.
func (a *App) Run(ctx context.Context) (err error) {
    if err := a.backend.Start(ctx); err != nil { return err }   // synchronous
    defer func() {
        rec := recover()
        err = errors.Join(err, a.backend.Stop())                 // restore ALWAYS
        if rec != nil {
            if a.cfg.panicPolicy == PanicRepanic { panic(rec) }  // after restore
            err = errors.Join(err, fmt.Errorf("%w: %v", ErrPanic, rec))
        }
    }()
    // ... mount root, start the intake goroutine (§2.4), loop (below) ...
}
```

The loop body is one select — the scaffold's `errc`-vs-`ctx.Done` select
(`server/scaffold.go:152-167`) widened to four arms:

```go
go a.intake() // App-owned (rev 1): pulls promptly from backend.Events(), applies
              // ALL lane-A policy (coalesce/drop-oldest/§2.4), enqueues onto
              // a.input; closes a.input when backend.Events() closes.

for {
    select {
    case ev, ok := <-a.input:              // lane A output, post-intake (§2.4)
        if !ok {                           // backend Events() closed
            return errors.Join(a.backend.Err(), a.teardown()) // Err() valid after close (ADR-0002)
        }
        a.dispatch(ev)                     // route per ADR-0004 §2.5
    case <-a.wake:                         // program lane has items and/or dirt (§2.4)
        a.drainProgramLane()               // closures, posts, bus deliveries, results
        a.maybeFrame()                     // ADR-0003 scheduler, min-frame-interval
    case <-a.timer.C:                      // earliest timer deadline (§2.6) — armed
        a.fireDueTimers()                  // only when timers/frames are pending (G5)
    case <-ctx.Done():
        return a.teardown()                // T1..T3 below
    }
}
```

> **Rev 1 (Lector must-fix #2).** The draft loop consumed `backend.Events()` directly
> and called an `a.backend.Err()` that no interface declared, while ADR-0002
> simultaneously claimed the backend's channel never drops — contradictory queue
> ownership. Resolved per Lector's stated preference: the backend only decodes and
> emits ordered, un-coalesced events, and `Backend` gains `Err() error` (valid after
> `Events()` closes or `Stop` returns — ADR-0002 rev 1, amended in parallel); every
> queueing/coalescing/overflow policy now lives in the App-owned intake stage (§2.4).

**Teardown** (`a.teardown()`), the registry-drain shape (`server/registry.go:155-198`):

- **T1** — Unmount the tree root-down (children first per ADR-0004 §2.4): every node's
  context cancels, which signals every in-flight task.
- **T2** — Wait for the task pool bounded by `WithTaskDrainTimeout`; tasks still running
  at the deadline are abandoned (their goroutines keep their cancelled ctx; results are
  dead-lettered) and counted in the returned error — exactly the registry's
  deadline-force-close report (`server/registry.go:184-194`):
  `fmt.Errorf("tui: task drain deadline: %d task(s) abandoned", n)`.
- **T3** — Return; the deferred `backend.Stop()` restores the terminal.

### 2.3 The single-goroutine invariant (normative)

> **INVARIANT.** All component state — the tree, every component's fields, focus, layout
> rects, the cell buffer — is owned by the loop goroutine. `Init`, `Layout`, `Render`,
> `HandleEvent`, bus handlers, and queued closures execute **only** there. The only
> operations legal from other goroutines are `App.Post`, `App.Update`, `App.Go`,
> `Bus.Publish`, and `Context.Post`/`Context.Go` — all of which enqueue and return.

The API is shaped to make violation hard (Go cannot make it impossible — ADR-0001 §3):
no exported runtime state is reachable without going through the queue; `Context`'s
mutating methods document loop-goroutine-only and verify it in `TestBackend` builds;
task closures receive a `context.Context` and **no** `Surface`, `*Context`, or tree
handle, so the compiler steers off-loop code toward `Post`. This invariant is stated
verbatim in `tui/doc.go` (ADR-0001 acceptance criterion 6).

### 2.4 The event queue — two lanes, never block, never drop program events

The failure space is fully mapped by prior art: **block** and you get tview's
self-deadlock (`QueueUpdate` from inside a handler waits for the loop that is executing
it — rivo/tview wiki/Concurrency); **panic** and you get gocui's 256-closure abort
(lazygit `pkg/gocui`); **drop silently and uniformly** and you lose a `TaskResult`, which
is a correctness bug, not degraded input. No single policy fits both kinds of traffic,
so the queue is **two lanes with different contracts**:

**Lane A — input events, fed by the App-owned intake stage (rev 1).** The ownership
split is explicit: the backend (ADR-0002) only **decodes** — its `Events()` channel
carries ordered, un-coalesced events and the backend itself never drops or reorders;
all queueing policy belongs to the App. An **intake goroutine** started by `Run` (§2.2)
pulls promptly from `backend.Events()` and applies lane-A policy into a bounded queue,
default cap 256 (`WithInputQueueSize`). Input is *refreshable* — a newer mouse-motion or
resize supersedes an older one — so overflow policy is **drop-oldest, with coalescing**:
`ResizeEvent` is latest-wins (an atomic slot, never a queue of sizes — the dossier §8
resize-storm rule), and consecutive `MouseEvent{Kind: MouseMotion}` events collapse to
the newest. Drops are counted and logged via `WithLogger`. Key presses and paste chunks
are never coalesced (each is semantically distinct); a full lane under key flood drops
the *oldest motion/resize* first and only then oldest keys — at that point the app is
seconds behind and stale keys are the least-bad loss.

> **Promptness invariant (normative).** Intake does **no component work** — it only
> classifies, coalesces, and enqueues (O(1) per event, no dispatch, no allocation beyond
> the slot). `backend.Events()` is therefore always drained promptly and the backend's
> single reader goroutine (one-reader contract per `server/ws/ws.go:36-48`) effectively
> never blocks on the App — even while the loop goroutine is stuck inside a slow
> handler. When `Events()` closes (terminal input failure or backend stop), intake
> closes `a.input` and the loop collects `backend.Err()` (§2.2).

**Lane B — program events** (producers: any goroutine): `Post`, `Update` closures,
`Bus.Publish` deliveries, `TaskResult`/`TaskProgress`. These are **never dropped and
never block**: a mutex-guarded growable slice (swap-and-drain on the loop side, so the
producer lock is O(append)), paired with a **wake channel**. The wake is the
close-and-replace broadcast idiom from `server/registry.go:46-49` reduced to its
single-waiter form: a `chan struct{}` of capacity 1 with a non-blocking send —
`select { case a.wake <- struct{}{}: default: }` — many producers, one wakeup, zero
lost wakeups, zero blocking. Unbounded growth is deliberate: lane-B traffic is generated
by the app's own finite tasks and handlers, dropping it corrupts program state, and
blocking reintroduces the deadlock; memory pressure from a runaway producer is an app
bug the runtime makes *visible* rather than converting into a hang. Two rev-1
hardenings (Lector Q1): **(a)** high-water-mark logging is **mandatory**, not best
effort — the drain tracks the maximum pending count and logs threshold crossings via
`WithLogger`, with an acceptance test exercising it (§5.11); **(b)** apps that prefer
fail-fast runaway detection opt into `WithEventQueueLimit(n)` (§2.1), under which
exceeding the ceiling panics with a targeted runaway-producer message. The default
remains unlimited.

```go
// Post enqueues ev for delivery on the loop goroutine. Safe from any
// goroutine, including from inside handlers on the loop itself. Never blocks;
// program-lane events are never dropped. Panics only when the app explicitly
// opts into WithEventQueueLimit and the ceiling is exceeded (§2.1).
func (a *App) Post(ev Event)

// Update enqueues fn to run on the loop goroutine. It ALWAYS enqueues and
// returns immediately — safe from any goroutine INCLUDING the loop itself,
// because lane B never blocks (the tview QueueUpdate self-deadlock is
// unrepresentable). Convention: code already running in a handler on the loop
// goroutine does NOT need Update — it owns the state and mutates directly; an
// fn enqueued from a handler runs in a later drain, before the next frame.
func (a *App) Update(fn func())
```

> **Rev 1 (Lector should-fix / Q2 answer).** The draft ran `Update` inline when called
> from the loop goroutine, detected by parsing the `runtime.Stack` header for the
> goroutine id — self-described as the least principled line in the design. **Dropped
> entirely.** Since lane B never blocks, always-enqueue is deadlock-free by the same
> argument, with zero dependency on runtime stack-header format and no special
> "sometimes-inline" ordering rule to teach. The in-handler idiom is direct mutation,
> documented in `tui/doc.go` alongside the invariant (§2.3).

### 2.5 The concrete `Event` set

Fixed by ADR-0001 §2.3 as a marker interface; the concrete types (all in package `tui`,
all value types, all implementing the unexported `isEvent()`):

```go
type Event interface{ isEvent() }

// --- keyboard (kitty protocol fields — ADR-0002 negotiates flags 1+2;
//     https://sw.kovidgoyal.net/kitty/keyboard-protocol/) ---

type KeyKind uint8
const (
    KeyPress KeyKind = iota
    KeyRepeat        // kitty flag 2 terminals only
    KeyRelease       // kitty flag 2 terminals only; never synthesized elsewhere
)

type Mods uint8
const (
    ModShift Mods = 1 << iota
    ModAlt
    ModCtrl
    ModSuper
    ModHyper
    ModMeta
    ModCapsLock
    ModNumLock // kitty modifier order
)

// KeyEvent is one key action. Code is the key's Unicode codepoint or a
// tui.Key* constant (KeyEnter, KeyEscape, KeyUp, KeyF1, … — private-use plane,
// ADR-0002's table). Base/Shifted are the kitty "alternate keys" (base-layout
// and shifted codepoints; 0 when unreported) enabling layout-independent
// shortcut matching. Text is the associated text ("" for non-text keys); on
// legacy terminals Kind is always KeyPress and Base/Shifted are 0.
type KeyEvent struct {
    Kind    KeyKind
    Code    rune
    Base    rune
    Shifted rune
    Mods    Mods
    Text    string
}

// --- mouse (SGR encoding — ADR-0002) ---

type MouseKind uint8
const (
    MousePress MouseKind = iota
    MouseRelease
    MouseMotion
    MouseWheel
)

type MouseButton uint8
const (
    MouseNone MouseButton = iota
    MouseLeft; MouseMiddle; MouseRight
    WheelUp; WheelDown; WheelLeft; WheelRight
)

// MouseEvent coordinates are LOCAL to the receiving component at every routing
// hop (ADR-0004 §2.5.2 rewrites them per level).
type MouseEvent struct {
    Kind   MouseKind
    Button MouseButton
    X, Y   int
    Mods   Mods
}

// --- lifecycle / terminal ---

type ResizeEvent struct{ W, H int }        // coalesced latest-wins (§2.4)
type PasteEvent struct{ Text string }      // one bracketed paste, CR/NL-normalized (ADR-0002)

// FocusEvent covers both component focus (routed per ADR-0004 §2.6.1) and
// terminal focus in/out (mode 1004; Terminal=true, delivered to the focused
// component and published on the Bus).
type FocusEvent struct {
    Gained   bool
    Terminal bool
}

// --- addressed deliveries (no bubbling — ADR-0004 §2.5.4) ---

type TimerID uint64
type TickEvent struct {
    Owner NodeID
    Timer TimerID
    At    time.Time
}

type TaskID uint64 // monotonic per App; never reused (staleness checks, §2.8)

type TaskResult struct {
    Owner NodeID
    ID    TaskID
    Value any
    Err   error // wraps ErrTaskPanic if the task panicked (§2.8)
}

// TaskProgress is an intermediate, addressed update from a still-running task
// (§2.8 streaming). Routed exactly like TaskResult.
type TaskProgress struct {
    Owner NodeID
    ID    TaskID
    Value any
}
```

### 2.6 Timers — demand-scheduled, no standing ticker

**Decision: one App-owned `time.Timer` armed to the earliest pending deadline over a
small min-heap of registrations; no per-subscription goroutines, no standing ticker.**
Components register through `Context.After(d)` (one-shot) and `Context.Every(d)`
(repeating), both returning `cancel` and both auto-cancelled at unmount via
`Context.OnUnmount` (ADR-0004 §2.2). A due deadline posts a `TickEvent{Owner, Timer,
At}` addressed to the registrant; `Every` re-arms after delivery (fixed-delay, not
fixed-rate — no burst catch-up after a stall).

Rationale: bubbletea's v1 renderer wakes 60 times a second whether or not anything
changed (dossier §2) — the antithesis of ADR-0001 G5. With demand scheduling, an idle
app has an **empty heap and a stopped timer**: the loop's select blocks on input alone;
zero wakeups, zero bytes (G5). The min-frame-interval cap (ADR-0003) rides the same
timer: a pending frame is just one more deadline in the heap. The per-goroutine
alternative (`time.AfterFunc` posting per registration) was rejected: it scatters timer
state across goroutines the teardown path must reason about, where the heap is loop-local
state torn down for free. The ws keepalive loop (`server/ws/ws.go:97-117`) shows the
per-goroutine ticker pattern working well for *one* timer per session; a UI with dozens
of blinking cursors, spinners, and debounces wants them multiplexed.

### 2.7 `tui.Bus` — typed pub/sub, enqueue-only publish

Fixed shape from ADR-0001 §2.3; full contract:

```go
// Bus is the App's broadcast channel: one instance per App (App.Bus()).
type Bus struct{ /* mu, app backref, map[reflect.Type][]subscription */ }

// Subscribe registers fn for every published value of dynamic type T.
// The reflect.Type key is resolved ONCE here (reflect.TypeOf((*T)(nil)).Elem());
// the stored handler is a compiler-generated closure doing a plain type
// assertion — dispatch performs NO reflect.Call and NO per-publish allocation.
// fn always runs on the loop goroutine. cancel is idempotent and safe from any
// goroutine. (Pattern: https://www.yellowduck.be/posts/writing-an-event-bus-using-generics-in-go,
// https://github.com/goxiaoy/go-eventbus.)
func Subscribe[T any](b *Bus, fn func(T)) (cancel func())

// SubscribeScoped ties the subscription to c's mounted lifetime: unmount
// cancels it automatically (registered via c.OnUnmount — ADR-0004 §2.4 step 2).
// This is the form components use; bare Subscribe is for App-lifetime listeners.
func SubscribeScoped[T any](c *Context, fn func(T)) (cancel func())

// Publish enqueues v for delivery. ENQUEUE-ONLY: it never invokes handlers on
// the caller's goroutine — delivery happens on the loop goroutine when the
// program lane drains (§2.4). Safe from any goroutine; never blocks.
func (b *Bus) Publish(v any)
```

Delivery semantics: the loop looks up subscribers by the published value's dynamic type
(exact match — no interface-assignability fan-out in v1), snapshots the handler slice
(copy-on-write: `Subscribe`/`cancel` replace the slice under the mutex), and invokes in
subscription order. Handlers registered *during* a delivery see only subsequent
publishes; a handler cancelled during delivery of the same batch is skipped (checked via
a per-subscription tombstone). Handlers are ordinary loop-goroutine code: they may
mutate state, `MarkDirty`, mount/unmount, publish again (delivered in a later drain —
publish-during-drain cannot livelock the frame because `drainProgramLane` drains only
the batch snapshot taken at wake).

**Why enqueue-only is non-negotiable:** a bus that calls handlers synchronously on the
publisher's goroutine hands every background task a direct line into component state,
reintroducing exactly the races the single loop exists to kill (§4.3; dossier §4's
"critical rule"). Synchronous delivery is also re-entrancy-hostile: publish-from-handler
becomes recursive dispatch with surprise ordering.

### 2.8 Async tasks — `App.Go`, addressed results, bounded pool

```go
// Task is one unit of background work. It runs OFF the loop goroutine and must
// treat ctx as its lifetime: ctx derives from the owner's unmount context
// (ADR-0004 §2.2) AND the App's run context — whichever dies first.
type Task func(ctx context.Context) (any, error)

// Go schedules task on the bounded pool and returns immediately (never blocks
// the caller — acquisition happens inside the task's goroutine). On completion
// a TaskResult{Owner: owner, ID: id, Value, Err} is posted on the program lane
// and delivered to owner's HandleEvent on the loop goroutine. If owner has
// unmounted by delivery time, the result is dead-lettered (dropped + counted).
// Components normally call the owner-implied form ctx.Go (ADR-0004 §2.2).
func (a *App) Go(owner NodeID, task Task, opts ...TaskOption) TaskID

type TaskOption func(*taskConfig)

// Exclusive assigns the task to a per-owner named group and CANCELS the
// contexts of all in-flight tasks in that (owner, group) before this one
// starts — lazygit's preemption semantics (pkg/tasks: Stop channel + monotonic
// staleness guard; dossier §2). Superseded tasks still emit their TaskResult
// (with ctx.Err()), which the monotonic ID check makes trivially ignorable.
func Exclusive(group string) TaskOption

// ErrTaskPanic is wrapped by TaskResult.Err when the task panicked.
var ErrTaskPanic = errors.New("tui: task panicked")

// TaskInfo extracts the identity Go injected into the task's context — the
// handle a streaming task needs to address TaskProgress to itself (§2.8.3).
func TaskInfo(ctx context.Context) (owner NodeID, id TaskID, ok bool)
```

**2.8.1 Execution.** `Go` allocates the next `TaskID` (monotonic per App — with
never-reused `NodeID`s, ADR-0004 §2.4, the pair is globally unambiguous for the App's
lifetime), derives the task context, applies `Exclusive` preemption, and spawns the
goroutine. The goroutine **first acquires the pool semaphore** — `sem chan struct{}`
sized by `WithTaskPoolSize`, the ingestor's bounded-background-writes pattern
(`ingestor/writer.go:28`) — in a select against `ctx.Done()`, mirroring the ingestor's
acquire-or-fallback select (`ingestor/writer.go:66-75`): a task cancelled while queued
never runs, and completes immediately with `ctx.Err()`. Acquiring *inside* the goroutine
(rather than in `Go`, where the ingestor blocks its caller for backpressure,
`ingestor/writer.go:54-56`) is the deliberate inversion: the ingestor *wants* commit
backpressure; a UI thread must never block, so `Go` bounds *running* tasks, not calls.

**2.8.2 Completion, panic isolation, staleness.** The goroutine runs
`recover`-protected (the scaffold's per-connection isolation,
`server/scaffold.go:205-212`): a panic becomes
`TaskResult{Err: fmt.Errorf("%w: %v", ErrTaskPanic, rec)}` (stack logged via
`WithLogger`) — one crashing task never kills the app, and the owner finds out through
the same channel as any failure (`errors.Is(res.Err, tui.ErrTaskPanic)`). Delivery: the
loop looks up `Owner` in the node table; unmounted → dead-letter (drop, count, log).
For last-request-wins flows the owner stores the `TaskID` of its latest request and
ignores results with a smaller ID — lazygit's monotonic staleness guard as a
one-field-one-comparison app idiom, made sound by IDs never being reused.

**2.8.3 Streaming tasks — decision: `Post`-based, no `App.GoStream`.** A long-running
task that produces intermediate updates posts them itself:

```go
ctx.Go(func(tctx context.Context) (any, error) {
    owner, id, _ := tui.TaskInfo(tctx)
    for line := range produce(tctx) {
        app.Post(tui.TaskProgress{Owner: owner, ID: id, Value: line})
    }
    return summary, nil // final TaskResult still delivered by the framework
})
```

Justification for rejecting a dedicated `App.GoStream(owner, func(ctx, yield))`: `Post`
is already goroutine-safe, never blocks, and delivers on the loop (§2.4) — a streaming
primitive would duplicate that path plus force a channel-buffering policy the two-lane
queue already answers. `TaskProgress` keeps streams **addressed and staleness-checkable**
(same `Owner`/`ID` discipline as results) rather than pushing users to invent broadcast
event types for point-to-point data. This is lazygit's streaming shape —
`ViewBufferManager` reads incrementally and marshals updates onto the UI loop
(dossier §2) — expressed through the one queue primitive we already guarantee. The cost
(tasks that stream must capture `app` and call `TaskInfo`) is small and explicit; if it
proves noisy, `GoStream` can be added later as sugar over exactly this mechanism.

### 2.9 The two event categories, explicitly

The originating task description named two kinds of traffic; they map onto two
primitives and must not be conflated:

| Category | Primitive | Addressing | Delivery | Examples |
|---|---|---|---|---|
| General broadcast | `Bus.Publish` + `Subscribe[T]` | none — every subscriber of type T | loop goroutine, subscription order | theme changed, data model updated, `ResizeEvent`, terminal `FocusEvent`, app-level notifications |
| Component-addressed async | `TaskResult` / `TaskProgress` / `TickEvent` via the program lane | `Owner NodeID`, dead-lettered after unmount | loop goroutine, direct to `Owner`'s `HandleEvent`, no bubbling | subprocess output for one panel, query results for one view, a debounce tick |

Rule of thumb, normative for ADR-0007 widgets: **if a second component could ever
legitimately care, it is a Bus event; if only the requester can interpret it, it is
addressed.** Broadcasting private results (bubbletea's only option — every `Msg` visits
the root) is precisely the anti-pattern G3 exists to kill.

## 3. Consequences

**Positive**

- **Deadlock- and panic-free queueing by construction**: `Post`/`Update`/`Publish` never
  block and — unless the app explicitly opts into `WithEventQueueLimit` — never panic
  (lane B unbounded, lane A drop-oldest); tview's self-deadlock and gocui's 256-panic
  are unrepresentable in the default API.
- **Backend simplicity** (rev 1): the driver's input contract is "decode and emit, in
  order" — every policy decision (coalescing, overflow, capacity) is App-owned intake
  code that is identical across `term.Backend` and `TestBackend`, so queue behavior is
  testable without a PTY.
- **The addressed-async primitive** (G3) turns the hardest recurring TUI problem —
  concurrent per-panel IO — into `ctx.Go` + a `TaskResult` case in `HandleEvent`, with
  cancellation, preemption, staleness, and panic isolation supplied by the framework.
- **True idle**: no ticker, empty timer heap, blocked select — combined with ADR-0003's
  render-on-dirty, an idle app consumes zero CPU and emits zero bytes (G5).
- Lifecycle behavior is *familiar golib*: `Run(ctx)` reads like `Scaffold.Run`,
  teardown errors join like the scaffold's, drains report like the registry's — one
  mental model across the module.

**Negative (costs)**

- **A slow handler still stalls the UI** — the single loop's inherent tradeoff (25% of
  frame time inside string ops was enough to hurt a real bubbletea app:
  https://eieio.games/blog/secure-massively-multiplayer-snake/). Mitigations: the rule
  "handlers do no IO and no unbounded compute — that is what `ctx.Go` is for" is
  documented in `tui/doc.go`, and `TestBackend` can assert per-dispatch time budgets.
- **Lane B is unbounded by default**: a runaway producer converts into memory growth
  instead of backpressure. Chosen deliberately (§2.4); rev 1 pairs it with mandatory
  high-water logging and the opt-in `WithEventQueueLimit` ceiling, so the failure mode
  is visible-or-fail-fast at the app's choice.
- `Update` from off-loop goroutines is asynchronous with no inline fast-path (rev 1):
  callers needing a result back must round-trip through a posted event or their own
  synchronization — a small ergonomic cost accepted to remove the gid-parse mechanism.
- Dead-lettering is silent by design (unmount races are normal, not errors); a genuinely
  lost result (wrong `Owner` bug) is only visible in logs/test hooks.

**Migration / evolution**

- Priority lanes (e.g. resize before pastes) can be added inside the drain order without
  API change.
- `GoStream` sugar (§2.8.3), per-task timeouts (`TaskOption`), and a frame-budget
  profiler hook are all additive.
- If interface-assignability publish (subscribe to an interface, receive implementors)
  is ever needed, it extends `Subscribe`'s type-index without changing the call sites.

## 4. Alternatives considered

1. **Actor-per-component (Textual's model).** One mailbox-fed task per widget —
   validated cheap *in asyncio*
   (https://textual.textualize.io/blog/2023/03/08/overhead-of-python-asyncio-tasks/),
   where cooperative scheduling means mailbox handlers never truly run in parallel.
   Goroutines are preemptive: the same design in Go puts N handlers in *actual*
   concurrent execution against one screen and one tree, demanding either per-component
   locking (deadlock lattice) or serialization back onto one goroutine (this design,
   with extra steps). Ordering across mailboxes also becomes undefined where a single
   queue gives one total order. Rejected — ADR-0001 N5; Textual's need is a Python
   artifact, and every successful Go TUI (tview, gocui, bubbletea, vaxis) already
   converged on the single loop (dossier §4).
2. **bubbletea's unaddressed `Cmd`/`Msg`.** Every command's result lands at the root
   `Update` only; addressing is the user's problem, forever — the routing discussions
   (https://github.com/charmbracelet/bubbletea/discussions/176, /751, /943) span years,
   the practitioner literature codifies root-as-router as the norm
   (https://leg100.github.io/en/posts/building-bubbletea-programs/), and the one
   upstream attempt at addressed wrapping (draft PR
   https://github.com/charmbracelet/bubbletea/pull/936, int-ID `Wrap()`) never landed.
   Fire-and-forget goroutine-per-Cmd also offers no pool bound, no cancellation tied to
   component lifetime, and no preemption groups. Rejected: G3 is this ADR's raison
   d'être.
3. **Synchronous bus delivery (call handlers at `Publish`).** Rejected: handlers would
   execute on arbitrary publisher goroutines, reintroducing every data race the
   single-loop invariant (§2.3) eliminates — the dossier's critical rule (§4). Even
   loop-goroutine-only synchronous delivery fails re-entrancy: publish-from-handler
   nests dispatch with surprise ordering. Enqueue-only costs one queue hop and buys a
   total order.
4. **One uniform bounded queue with a single overflow policy.** Block → tview deadlock;
   panic → gocui's 256 abort; drop → lost `TaskResult`s (correctness). The traffic is
   heterogeneous — refreshable input vs must-deliver program events — so the queue is
   two-lane (§2.4). Rejected as a category error.
5. **Always-on frame ticker (bubbletea v1's 60fps renderer).** Rejected: idle burn
   violates G5/ADR-0001 G5; render-on-dirty with a min-interval cap produces identical
   worst-case pacing and zero idle cost. Demand-armed timers (§2.6) close the last
   standing-wakeup hole.
6. **`time.AfterFunc` per timer subscription.** Rejected (§2.6): correct but scatters
   cancellation state across goroutines; the loop-local heap multiplexes every deadline
   onto the one `time.Timer` the select already owns and tears down trivially.

## 5. Acceptance criteria

1. **Panic restore**: a component handler that panics under `term.Backend` leaves the
   terminal restored (cooked mode, main screen) before the panic propagates; under
   `PanicReturn` the same panic surfaces as `errors.Is(err, tui.ErrPanic)`. Verified via
   `TestBackend` recording `Stop()` strictly before repanic.
2. **Queue safety**: 100 goroutines calling `Post` concurrently while the loop is
   blocked inside a slow handler: no call blocks >1ms, none panics, all events deliver
   after the handler returns, in enqueue order per producer. `Update` called from inside
   a handler returns immediately without deadlock, and its fn runs on the loop goroutine
   in a later drain, before the next frame (rev 1: no inline execution).
3. **Coalescing + intake promptness**: a burst of 1000 `ResizeEvent`s yields exactly one
   delivery carrying the final size; interleaved motion events collapse to the newest
   per drain; no `KeyEvent` is ever dropped below lane-A capacity. `TestBackend`
   injection during a slow handler never stalls the backend side — the intake stage
   keeps `Events()` drained while the loop is busy (rev 1 promptness invariant, §2.4).
4. **Addressed delivery**: `ctx.Go` from component X delivers `TaskResult{Owner: X.ID}`
   to X's `HandleEvent` on the loop goroutine (goroutine identity asserted); unmounting
   X first → the result is dead-lettered (test hook exposes the count) and no method of
   X is invoked; X's in-flight task observes `ctx.Err() != nil` within the test timeout.
5. **Exclusive + staleness**: two `Exclusive("search")` tasks from one owner — the first
   observes cancellation; both results arrive; their `TaskID`s are strictly increasing;
   the last-request-wins comparison discards the stale one.
6. **Panic isolation**: a panicking task produces
   `errors.Is(res.Err, tui.ErrTaskPanic)`, the app keeps processing events, and the
   recovered stack appears via `WithLogger`.
7. **Pool bound**: with `WithTaskPoolSize(2)`, 10 queued tasks never exceed 2 running
   concurrently (atomic high-water assert); cancelling a queued task prevents it from
   ever starting.
8. **Bus**: `Subscribe[Foo]` receives only `Foo` publishes, on the loop goroutine, in
   subscription order; `SubscribeScoped` stops after owner unmount; `Publish` from a
   non-loop goroutine returns before the handler runs; cancel is idempotent.
9. **Idle**: an app with no timers, no tasks, and no dirt performs zero timer wakeups
   and writes zero bytes over an observed idle window (TestBackend write log +
   instrumented timer arm count).
10. **Teardown**: cancelling `Run`'s ctx with 3 in-flight tasks (2 well-behaved, 1
    ignoring its ctx) returns within `WithTaskDrainTimeout`+ε with an error reporting
    `1 task(s) abandoned`, joined per the scaffold rule with any backend stop error.
11. **Queue diagnostics** (rev 1): driving lane B to a high pending count while the loop
    is stalled emits high-water-mark log entries via `WithLogger` (the test asserts the
    logger observed the threshold crossing); with `WithEventQueueLimit(100)`, the 101st
    pending program event panics with the runaway-producer message; the identical load
    without the option only grows memory and logs.

## 6. Questions for the reviewer

- **Q1.** Lane B (program events) is **unbounded** — never blocks, never drops, memory
  is the pressure valve, high-water logging the tripwire (§2.4). The conservative
  alternative is a large bound (say 64k) with a panic-on-overflow "you have a runaway
  producer" crash. Is unbounded-with-visibility acceptable, or do you want a hard
  ceiling as a last-resort bug detector?
  — **Lector r1:** unbounded is acceptable only with explicit high-water logging and
  tests, plus an optional hard ceiling for fail-fast apps — folded: mandatory high-water
  logging + `WithEventQueueLimit(n)` (§2.1, §2.4, §5.11).
- **Q2.** Inline `Update` relies on goroutine-id capture via `runtime.Stack` header
  parsing (§2.4) — stable, stdlib-only, but unprincipled. Since lane B never blocks,
  dropping the fast-path entirely (Update always enqueues; document that in-handler
  callers just mutate directly) loses only same-statement ordering. Keep the gid parse,
  or drop inline execution for purity?
  — **Lector r1:** drop gid parsing; always enqueue `Update`; same-handler code mutates
  directly — folded (§2.4, §3, §5.2).
- **Q3.** Default task-pool size is a flat 16 (§2.1). Should it instead scale
  (`max(4, runtime.GOMAXPROCS(0))`), given tasks are typically IO-bound where GOMAXPROCS
  is the wrong signal — or is a documented flat default the more predictable golib
  choice?
  — **Lector r1:** keep the flat 16 — predictable, and IO-bound work does not scale
  cleanly with GOMAXPROCS. Unchanged.
- **Q4.** Streaming is `TaskProgress` posted by the task itself via `TaskInfo(ctx)`
  (§2.8.3) rather than a first-class `App.GoStream`. Accept the convention-over-API
  call, or is a `yield func(any)` signature worth the second primitive for
  discoverability (widget authors in ADR-0007 will write this pattern often)?
  — **Lector r1:** keep the `TaskProgress` convention for v1; `GoStream` can be sugar
  later if widget code proves noisy. Unchanged.
- **Q5.** `PanicRepanic` (restore terminal, then propagate) is the default panic policy
  (§2.1/§2.2) — crash loudly with the original stack, golib fail-loud. lazygit-class
  production apps may prefer `PanicReturn` to show their own error UI on the way out.
  Right default?
  — **Lector r1:** `PanicRepanic` is the right default — restore-then-repanic gives
  golib's fail-loud behavior without leaving raw mode behind. Unchanged.
