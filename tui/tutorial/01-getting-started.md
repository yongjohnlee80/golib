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
app := tui.NewApp(newRoot(cancel), tui.WithBackend(backend))
return app.Run(ctx)
```

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

Next: [the root controller](02-the-root-controller.md) — the one pattern
you cannot skip.
