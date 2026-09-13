# 10 — Architectural Observations & Discussion

During the refactoring of `golib/tui`, its widget suite, and the tutorial
documentation, several architectural tension points and design deficits were
identified.

This chapter documents these observations to serve as an architectural backlog
for future design reviews, RFCs, and refactoring rounds.

---

## 1. Lane B Head-of-Line Blocking vs. Lane A Input Drops

### The Deficit
The `App` runtime separates events into two lanes:
- **Lane A (Input)**: Hardware terminal events (keys, mouse, resize) delivered over an unbuffered handoff channel (`a.input`) fed by an intake-owned bounded FIFO slice queue (`cfg.inputQueueSize`, default 256).
- **Lane B (Program)**: Application updates (`App.Update`), task results (`TaskResult`), and bus deliveries (`Bus.Publish`) queued on a mutex-protected slice queue (`programQueue`) backed by a 1-capacity wake channel and an optional ceiling (`WithEventQueueLimit`, default 0 = unlimited).

In `App.loop`, the runtime selects between lanes. However, when Lane B is selected, `drainProgramLane` captures the pending batch and executes **every single item** before returning to the `select`:

```go
func (a *App) drainProgramLane() {
    batch := a.queue.drain()
    for _, item := range batch {
        // Execute closures and deliver bus events
    }
}
```

If a background task or external goroutine pushes a massive burst of closures or events via `App.Update`, the loop goroutine spends continuous wall-clock time executing Lane B work. While the loop is occupied:
1. The terminal intake goroutine continues reading hardware input from `Backend.Events()`.
2. The bounded Lane A intake queue fills up.
3. Once full, `intakeEnqueue` begins dropping input events (`inputDrops`).

### Discussion & Potential Directions
- **Time-Sliced / Quota Draining**: Instead of exhausting the entire Lane B batch unconditionally, drain up to $N$ program items (or $M$ milliseconds), then perform a non-blocking check on Lane A (`select { case ev := <-a.input: ... default: }`).
- **Input Preemption Priority**: Hardware keystrokes (especially quit keys and navigation) should ideally take priority over asynchronous background batch updates.

---

## 2. Pre-Mount BufferView Write Rejection

### The Deficit
`BufferView.Writer()` provides an `io.Writer` interface via `*bufWriter` designed for logging and stream capture. However, writes made before the `BufferView` is mounted into an active component tree (`Init(ctx)`) or after it is unmounted fail immediately with `widget.ErrClosed`:

```go
func (v *BufferView) Writer() io.Writer { return v.wr }
```

In typical applications, logging sinks are initialized during application startup—before `app.Run(ctx)` is called. As a consequence, critical initialization logs, configuration dumps, and early connection errors fail to write unless the application developer manually defers writes or creates an intermediate staging buffer (the "deferred writer" pattern).

### Discussion & Potential Directions
- **Internal Pre-Mount Ring Buffer**: `BufferView` could maintain an internal ring buffer (e.g. holding the last 256 lines) when unmounted. Upon `Init(ctx)`, the view automatically flushes this pre-mount backlog into the active cell buffer, making `logView.Writer()` safe to use immediately at application initialization.

---

## 3. Modal Focus Seeding vs. Asynchronous Data Race

### The Deficit
When a modal `Float` is displayed (`f.Show()`), the `floatLayer` immediately attempts to seed focus to the first focusable child inside its content tree.

However:
1. At the time `f.Show()` is called, the float has not undergone its first layout pass.
2. If the modal displays dynamic data (such as a `Table` or `List` whose rows are loaded asynchronously via `ctx.Go`), the content is often empty or unmounted at `Show()` time.
3. The initial focus walk finds zero focusable elements and terminates, leaving focus stranded on the `floatLayer` backdrop.
4. When the async data finally arrives and populates the table, the table remains unfocused, and user keystrokes (arrows, Enter) do nothing until a manual `ctx.FocusComponent` is triggered.

### Discussion & Potential Directions
- **Post-Layout / Child-Mount Focus Re-evaluation**: `floatLayer` could register a post-layout hook: if focus currently rests on the modal layer itself and a newly mounted or laid-out child accepts focus, automatically transfer focus to that child.

---

## 4. Event Bus Reflection & Heap Allocations

### The Deficit
The `tui.Bus` provides a decoupled pub/sub event system:

```go
bus.Publish(event)
```

Internally, `Bus` uses `reflect.TypeOf(event)` to look up registered subscriber callbacks. Furthermore, wrapping events into interface values (`any`) and enqueuing them onto the program queue requires heap allocations.

In high-throughput scenarios (e.g. high-frequency telemetry, cursor movement tracking, or real-time terminal streaming), continuous bus publishing induces GC pressure and latency spikes.

### Discussion & Potential Directions
- **Direct Component Channels**: For performance-critical data streams (such as streaming log chunks or real-time metrics), prefer direct channel subscriptions or dedicated observer interfaces over generic reflection-based bus publishing.

---

## 5. Editor Key Chord Resolution: Ad-hoc State vs. Declarative Trie

### The Deficit
The `widget.Editor` supports modular keysets (`KeysetVim`, `KeysetNano`, `KeysetStandard`), customizable chords, and `ActUnbound` bubbling.

However, composite multi-key sequences in Vim mode (such as double-key prefix actions `ActDeletePrefix` for `dd`, `ActYankPrefix` for `yy`, `ActGoPrefix` for `gg`, and insert escape chords like `jk`) are currently tracked using manual struct fields (`pendingAct`, `pendingChord`, `pendingCount`, `pendingRune`, `chordCancel`).

As new motions and operator-pending combinations are added, managing transitions, arming chords, counts, and cancellation timers via mutable struct flags becomes error-prone and difficult to test exhaustively.

### Discussion & Potential Directions
- **Declarative Key Trie Router**: Replace ad-hoc pending flags with a generic Trie-based keymap router. Each incoming keystroke traverses nodes in the Trie; leaf nodes execute actions, intermediate nodes transition state and start cancellation timers, and unmatched chords bubble immediately.
- This Trie router could be promoted to `tui/` core, allowing any widget or modal dialog to easily implement multi-stroke keybindings (such as leader keys, `which-key` menus, or Emacs-style `Ctrl-X Ctrl-S` sequences).

---

## Summary of Action Items for Future Refactoring

| Item | Area | Priority | Impact |
|---|---|---|---|
| **Lane Interleaving** | `tui/app.go` | Medium | Eliminates input lag and drops during heavy `App.Update` batches. |
| **Pre-mount Buffer** | `widget/bufferview.go` | High | Eliminates lost startup logs and the need for deferredWriter workarounds. |
| **Auto-Focus Reseed** | `widget/float.go` | Medium | Prevents stranded focus in async modals. |
| **Trie Key Router** | `tui/widget/editor.go` | Low / Design | Unifies multi-chord parsing across Editor and future modal widgets. |
