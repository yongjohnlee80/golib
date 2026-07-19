# 5 — Async: tasks, ticks and writers

The loop goroutine owns every component. The three sanctioned ways work
gets in from outside:

## ctx.Go — fetch off the loop, receive on the loop

```go
func (v *view) reload(ctx *tui.Context) {
    ctx.Go(func(c context.Context) (any, error) {
        rows, err := v.client.Releases(c, 200, 0)
        if err != nil {
            return nil, err
        }
        return rowsLoaded{rows}, nil // a TYPED result you discriminate on
    })
}

func (v *view) HandleEvent(ev tui.Event) bool {
    switch e := ev.(type) {
    case tui.TaskResult: // delivered to the node that called ctx.Go
        if rl, ok := e.Value.(rowsLoaded); ok {
            v.table.SetItems(rl.rows)
            return true
        }
        if e.Err != nil { // the task returned an error — handle it,
            return true   // don't let it vanish silently
        }
    }
    return false
}
```

The closure runs on a worker goroutine (do NOT touch widgets in it); the
result comes back as a `tui.TaskResult` event **to the component that
started the task**, on the loop goroutine, where mutation is safe. Use one
small typed struct per operation — a `switch` on the value type replaces a
zoo of callbacks. The task context dies with the component's mount, so an
unmounted view can't apply stale results.

## ctx.Every — auto-refresh

A TUI over live data goes stale the moment it renders: the store keeps
changing underneath it. Poll:

```go
func (v *view) Init(ctx *tui.Context) {
    ...
    v.reload(ctx)
    ctx.Every(5 * time.Second) // TickEvent every 5s until unmount
}

case tui.TickEvent:
    v.reload(v.ctx)
    return true
```

We shipped without this once — the UI showed a delivery "still failing"
that had long since succeeded, and the debugging session that followed was
entirely self-inflicted. `ctx.After(d)` is the one-shot variant.

## The BufferView writer — and the pre-mount trap

`bufferView.Writer()` is an `io.Writer` that is safe from any goroutine —
your logger's sink, an exec's output, etc. It has one sharp edge:

> **Writes made before the view mounts are DROPPED.** The writer binds to
> the app at mount time; until then it is closed.

The classic victim: a server that logs "listening on :2022" during startup,
*before* `app.Run` has mounted the tree. Those lines silently never appear.
The fix is a small buffering relay owned by your model:

```go
type deferredWriter struct {
    mu  sync.Mutex
    buf bytes.Buffer
    dst io.Writer // nil until attach
}

func (d *deferredWriter) Write(p []byte) (int, error) {
    d.mu.Lock(); defer d.mu.Unlock()
    if d.dst != nil { return d.dst.Write(p) }
    return d.buf.Write(p) // hold pre-mount lines
}

func (d *deferredWriter) attach(dst io.Writer) { // call from root Init
    d.mu.Lock(); defer d.mu.Unlock()
    if d.buf.Len() > 0 { _, _ = dst.Write(d.buf.Bytes()); d.buf.Reset() }
    d.dst = dst
}
```

Hand `deferredWriter` to the logger at construction; call
`attach(logView.Writer())` from your root's `Init`. Startup lines replay
into the pane the moment it exists. (Mirroring the same logger to a plain
file besides the pane is cheap and makes logs copy-pasteable and
crash-durable — recommended.)

## app.Update — everything else

From any goroutine, `app.Update(func(){ ... })` enqueues a closure onto the
loop. It is the escape hatch for integrations that don't fit ctx.Go — use
it sparingly.

Next: [floats and modals](06-floats-and-modals.md).
