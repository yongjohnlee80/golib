# 8 — Debugging: runtime tracing

## The bug you cannot reason about

Every hard tui bug sounds the same:

> The modal is on screen. I press Enter. Nothing happens.

You cannot see why, because everything that decides the outcome belongs
to the runtime, not to your component: which node holds focus, what was
mounted at the moment the key arrived, which node consumed it, whether a
modal trap is open. A component can only see the events it is handed —
never the ones it *should* have been handed.

This is where afternoons go. The failure mode is not "I could not find
the fix", it is "I could not test my hypothesis", so patches accumulate
at the consumer: make this widget focusable, rebuild that child earlier,
re-seed focus after layout. None of them fix a cause, and each one hides
the next symptom a little better.

## Turn the trace on

```go
app := tui.NewApp(root,
    tui.WithBackend(backend),
    tui.WithTrace(func(ev tui.TraceEvent) {
        fmt.Fprintf(traceFile, "%s node=%s prev=%s %s\n",
            ev.Kind, ev.Comp, ev.PrevComp, ev.Detail)
    }),
)
```

`TraceFunc` runs on the loop goroutine, in order. Keep it cheap — append
to a slice, write to a file, never block. Tracing is off unless you pass
`WithTrace`, and costs one nil check per emit site when off, so it is
safe to leave wired behind a `--trace` flag in a shipped binary.

What you get:

| Kind | Meaning |
|---|---|
| `focus` | focus moved: `Node` gained it, `Prev` lost it |
| `focus-repair` | focus re-homed after its node died or went invisible |
| `mount` / `unmount` | component tree lifecycle |
| `key` | a key event and the node that CONSUMED it — `Node` empty means **nobody did** |
| `scope` | a modal trap opened |

Components are named by Go type (`*widget.List[string]`), not by a bare
node id, because the id tells you nothing while reading.

## Reading a trace

This is a real one. A picker float opened, and Enter did not select:

```
key node=<unmounted> prev=<unmounted> (C)
unmount node=*widget.Float
key node=            prev=*widget.Editor (Enter)
scope node=*widget.floatLayer prev=*widget.Editor (open)
focus node=*app.connPicker prev=*widget.Editor
```

Read it bottom-up and the diagnosis is immediate: the float opened
**after** the Enter. Nothing consumed that key (`node=` is empty), so it
was never a focus bug, never a widget bug — the test pressed Enter too
early, having matched a *menu label* that happened to read the same as
the float's title.

Four patches had already been written against that symptom, including
one to the framework itself, before the trace was added. All were wrong.

## In tests

Buffer the trace and print it with the assertion. A failure message that
carries the last ~40 records tells you what the screen dump cannot:

```go
tr := &traceLog{}                    // append-only, mutex-guarded
app := tui.NewApp(root,
    tui.WithBackend(tb),
    tui.WithTrace(tr.add))
...
t.Fatalf("waiting for %s: %q never appeared.\nscreen:\n%s\n\ntrace:\n%s",
    what, want, tb.String(), tr.tail(40))
```

## Questions the trace answers directly

- **"My key does nothing."** Find the `key` record. Empty `Node` means
  nobody consumed it — check *when* it arrived relative to the `scope` /
  `mount` records. A `Node` you did not expect means someone upstream ate
  it (a widget switching on `Code` without checking `Mods` is the classic).
- **"My modal ignores Esc."** Look for the `key` record for Esc: if a
  child consumed it, that child has a bug (see chapter 6).
- **"Focus went somewhere strange."** `focus-repair` records say the
  runtime re-homed focus because the focused node was unmounted or
  hidden — usually a swap that unmounted the focused child.
- **"The widget is stale."** Compare `mount` / `unmount` ordering against
  when your data arrived.

## Rules that survived the session that produced this chapter

1. **Trace first.** One trace read beats three plausible hypotheses.
2. **A fix you cannot make fail is a guess.** Write the failing test
   first. If it passes *without* your change, your diagnosis is wrong —
   that is the check that caught a speculative framework patch here.
3. **Fix the cause, not the consumer.** If a widget must be worked around
   to be usable, the widget is wrong. Every workaround leaves the next
   consumer to rediscover the same bug.

→ Every defect from that session, with the remedy scored (source fix vs
symptom patch):
[`docs/tui/incident-register-2026-08-autodb-m6.md`](../../docs/tui/incident-register-2026-08-autodb-m6.md)
