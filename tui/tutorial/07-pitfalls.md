# 7 — Pitfalls

Every entry below is a bug that actually happened while building real apps
on this package. Symptoms first — this page is meant to be greppable at
2am.

## "Only the top of the screen renders / one heading line"

A bare widget was passed as the app root. Widgets take their *preferred*
size; nothing forced fill. Use a root controller that `LayoutChild`s the
tree under the incoming constraints and `PlaceChild`s it over the full
rect. → [chapter 2](02-the-root-controller.md)

## "Ctrl-C doesn't quit"

Raw mode: `Ctrl-C` is `KeyEvent{'c', ModCtrl}`, not SIGINT. Handle it (and
`q`) in the root controller. → [chapter 2](02-the-root-controller.md)

## "Garbage characters paint over the UI"

Something wrote to stdout/stderr while the backend owned the screen —
usually a logger with a default stderr sink constructed before the TUI.
Route logs into a BufferView pane and treat "no sink available" as a hard
error, never a stderr fallback. → [chapter 1](01-getting-started.md)

## "My startup log lines never show in the log pane"

`BufferView.Writer()` DROPS writes made before the view mounts. Buffer
pre-mount writes and replay them at root Init (`deferredWriter` pattern).
→ [chapter 5](05-async-tasks.md)

## "The list shows stale data"

Views load once at mount and the world moves on. Add `ctx.Every(5s)` and
reload on `TickEvent`. → [chapter 5](05-async-tasks.md)

## "Keys do nothing inside my modal"

Modal focus seeding runs at `Show()`, before the float's first layout —
with async content there is nothing focusable yet, so the layer keeps
focus. Call `ctx.FocusComponent(...)` when the content's data arrives.
→ [chapter 6](06-floats-and-modals.md)

## "Pressing q in a detail view quit the whole app"

Unconsumed keys bubble past floats to the root. Track open floats at the
root and make `q` close the top float when one is open.
→ [chapter 6](06-floats-and-modals.md)

## "My ActivateEvent handler fires for the wrong list"

Bus events are app-wide. Always filter on `ev.Owner == myWidget.NodeID()`.
→ [chapter 4](04-events-focus-keys.md)

## "I can't select/copy text with the mouse"

Mouse reporting is on: the terminal sends drags to the app instead of
selecting. Offer copy from inside the app — `Context.CopyToClipboard`
(OSC 52) and BufferView's `y`. Terminal-side OSC 52 support varies (some
terminal/editor chains fragment or cap the payload); piping to a native
clipboard tool (`pbcopy`, `wl-copy`) alongside OSC 52 is the robust combo
for local apps.

## "The first frame is truncated at some row" (historical)

Fixed in the package (the frame writer now survives short writes on
non-blocking ttys), but the symptom is listed because it looked EXACTLY
like the root-controller bug above, and apps that re-render on a timer
masked it — a repaint healed the screen before anyone looked. If you see
partial frames on an old version: upgrade past v0.2.1.

## "My widget's cursor doesn't render where I expect in tests"

Rendering is asynchronous relative to `TestBackend.Inject`. Poll
`tb.String()` with a deadline; never assert immediately.
→ [chapter 1](01-getting-started.md)

## "Cells truncate exactly the value I assert on"

Table columns are fixed-width and truncate with an ellipsis. Size the
column for the longest real value ("invalid (1 errors)" is 18 cells), and
prefer the flex column (`Width: 0`) for free-length text.
→ [chapter 3](03-widgets.md)
