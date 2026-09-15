# 1 — Getting started

## The three pieces

Every tui program is the same three pieces:

1. **A Backend** — owns the terminal: raw mode, the alternate screen, the
   capability probe, input decoding, frame flushing. `term.Open()` gives you
   the real one; `tui.NewTestBackend(w, h)` gives you an in-memory grid for
   tests.
2. **A root Component** — your controller (chapter 2).
3. **The App** — `tui.NewApp(root, tui.WithBackend(backend))`, then
   `app.Run(ctx)`. Run blocks until ctx is cancelled and restores the
   terminal on the way out, even on panic.

```go
backend, err := term.Open()
if err != nil {
    // stdout isn't a terminal (piped, CI, --headless). Don't fight it:
    // run your non-TUI path.
    return runHeadless(cfg)
}
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

app := tui.NewApp(newRoot(cancel),
    tui.WithBackend(backend),
    tui.WithTaskPoolSize(16),            // semaphore limit for active ctx.Go task execution (default 16)
    // WithEventQueueLimit(n) is omitted here to keep Lane B unlimited (the default).
    // Pass n >= 1 to enforce a fail-loud ceiling that panics if producers run away.
    tui.WithMinFrameInterval(16*time.Millisecond), // target ~60fps frame rate
    tui.WithDoubleClickWindow(400*time.Millisecond), // double-click detection window
)
return app.Run(ctx)
```

## Base App configuration options

`tui.NewApp` configures the central runtime before the loop starts:

| Option | Default | Purpose |
|---|---|---|
| `WithBackend(b)` | *Required* | Terminal driver (`term.Open()` for production, `NewTestBackend()` for tests). |
| `WithTheme(t)` | `DefaultTheme()` | Color palette and standard attribute mapping across all widgets. |
| `WithTaskPoolSize(n)` | `16` | Concurrency limit for active `ctx.Go` task execution (each submission spawns a goroutine; semaphore bounds concurrently executing task functions). |
| `WithEventQueueLimit(n)` | Omitted (`0` / unlimited) | Enforces a fail-loud capacity ceiling for the Lane B program queue (`n >= 1`). Exceeding the limit panics to catch runaway producers; Lane B never silently drops events. |
| `WithMinFrameInterval(d)` | `16ms` (~60fps) | Frame limiter coalescing multiple dirty updates into atomic frame flushes. Use `0` in tests for instant renders. |
| `WithDoubleClickWindow(d)` | `400ms` | Maximum elapsed time between clicks on the same cell to emit a double-click event. Set $\le 0$ to disable. |
| `WithTrace(fn)` | `nil` (off) | Synchronous event tracing callback (`TraceEvent`) for debugging focus, mounts, keys, semantic actions, and pointer capture (chapter 8). Zero allocation when disabled. |
| `WithLogger(l)` | Discard | Structured logging sink (`logger.Logger`) for runtime warnings and queue drops. |

## Synchronous startup & panic contract

When `app.Run(ctx)` is invoked:
1. **Device acquisition is synchronous**: Raw mode, alternate screen buffer, and VT modes are engaged immediately.
2. **Bounded capability probe**: The runtime probes terminal capabilities (Kitty keyboard protocol, truecolor support, synchronized output, mouse reporting). Unanswered probes time out safely after 250ms (leaving ambiguous capabilities like mouse at `TriUnknown` for optimistic fallback) rather than hanging startup.
3. **Panic guarantee**: Any panic on the event loop (in layout, rendering, or handlers) is caught by a top-level defer. **The terminal is restored completely before the panic is re-thrown**, ensuring a crash never leaves your user's shell in a corrupted or raw state.

## Shutdown: cancel the context

The app stops when the ctx you passed to `Run` is cancelled. The idiomatic
wiring is to hand `cancel` to your root component and call it from a quit
key. **In raw mode `Ctrl-C` does not deliver SIGINT** — it arrives as the
keystroke `'c'` with `ModCtrl`. If you don't handle it, your app is
un-quittable. Handle both:

```go
if e.Code == 'q' || (e.Code == 'c' && e.Mods&tui.ModCtrl != 0) {
    r.quit()
    return true
}
```

## Never print to stdout/stderr while the TUI runs

The backend owns the screen. Anything else writing to the terminal — a
logger defaulting to stderr, a stray `fmt.Println`, a library's debug
output — paints garbage over your frames and is maddening to diagnose.
Route logs into a `BufferView` pane (chapter 5) or a file. If your app
constructs a logger before the TUI, make the no-sink case a **hard error**,
not a silent stderr fallback.

## Testing with TestBackend

`TestBackend` runs the full pipeline (layout → render → flush) into an
in-memory grid you can assert on:

```go
tb := tui.NewTestBackend(100, 24)
app := tui.NewApp(root, tui.WithBackend(tb), tui.WithMinFrameInterval(0))
ctx, cancel := context.WithCancel(context.Background())
resc := make(chan error, 1)
go func() { resc <- app.Run(ctx) }()
defer func() { cancel(); <-resc }()

// drive it
_ = tb.Inject(tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyRight})

// assert on the rendered screen
waitFor(t, func() bool { return strings.Contains(tb.String(), "Releases") })
```

`tb.Inject` feeds events through the real routing; `tb.String()` is the
current screen; `tb.Clipboard()` records OSC 52 copies. Rendering is
asynchronous — always poll with a deadline rather than asserting
immediately after an Inject.

Mouse events are injected verbatim: `TestBackend` does not synthesize a
release or clamp captured coordinates. Supply the complete press/motion/release
sequence, or inject `tui.FocusEvent{Gained: false, Terminal: true}` to verify
that a backend loss cancels the App's held pointer capture.

Next: [the root controller](02-the-root-controller.md) — the one pattern
you cannot skip.
