# decl — instantiate a declarative UI schema, without knowing the toolkit

`decl` turns a parsed schema into a live tree by telling an **adapter** what to
build. It owns identity, ordering and the signal contract. It owns no widgets,
no surfaces, and no opinion about redrawing.

## Install

```go
import "github.com/yongjohnlee80/golib/decl"
```

## The shape of it

```go
tree := decl.New(myAdapter)

spec, err := parse.QML{}.Parse(src)
if err != nil { /* a schema is input; show the error and keep the old tree */ }

if err := tree.Mount(spec); err != nil { /* … */ }

// Later, when a widget's signal fires:
err = tree.Emit(nodeID, "clicked")
```

An adapter implements four methods — `ResolveHandler`, `Create`, `Apply`,
`Destroy` — and nothing in their signatures names a toolkit. A fifth capability,
`Restructurer`, is **optional**: implement it and a reload can splice a node's
children in place; leave it out and every structural change degrades to a
rebuild, which is correct and merely lossy.

`Create` receives a **`Construction`**: the declared properties, the
already-built children, and one emitter per signal. It returns the property
names it **consumed**, and the engine applies only the rest — because a
constructor-consumed property may have no setter at all, or a setter that
assigns and invalidates unconditionally, so a replay is either impossible or a
second visible effect.

There is no `Attach`. Children arrive at construction, which is the only thing a
container requiring them as arguments can work with. Structural insert, remove
and move arrive separately, through `Restructurer` — with the reconciler that
needed them, rather than guessed at in advance.

## Reload

```go
res, err := tree.Reload(src)          // parse, then reconcile
switch {
case errors.Is(err, decl.ErrIncomplete):
    // A save in progress. Hold the current tree and wait; show nobody an error.
case err != nil:
    // A real mistake. Show it, and keep the last good tree on screen.
default:
    for _, rb := range res.Rebuilt {
        log.Println(rb)               // what lost its state, and which edit did it
    }
    if res.RootReplaced {
        // The root component is gone; mount the new one in its place.
    }
}
```

A reconcile patches what changed and leaves everything else alone. A node that
keeps its identity keeps everything the toolkit hung on it — scroll offset,
focus, half-typed input, in-flight tasks — because it is *moved*, never
re-created.

**Identity** is the declared `id`, else position among the remaining children.
A node the author named is never handed to an anonymous newcomer; the reverse is
allowed, so giving an existing node a name does not reset it. There is no rule
reading a component's own key: a key belongs to a mounted component, and the new
side of a reload is text.

**Some edits cannot be patched**, and each one is reported in `res.Rebuilt` with
the reason:

| edit | why it rebuilds |
| --- | --- |
| a node's type changed | it is a different thing |
| a **consumed** property changed | consuming it is how the adapter said there is no setter |
| a property was **removed** | there is no "unset", and the engine holds no default |
| a **new signal** appeared | some widgets take a callback only at construction |
| children changed on a node that cannot restructure | `widget.Split` has no `Add`, `Remove` or `Move` at all |

Everything else is free. Re-pointing an existing signal at a different handler
costs nothing: the emitter calls back into the engine, which reads the binding
at call time.

## What crosses the seam

An **`Application`**: *node N's property P now has value V*. Not "please
repaint".

That is the difference that makes a second toolkit possible. A repaint
notification would leave the adapter to infer what the engine meant, and would
invite it to invalidate on the engine's behalf. Real widgets already make that
call, and they make it differently — one setter schedules a relayout because the
value changes the widget's intrinsic size, another repaints only, another also
repairs focus. Handing over the **value** leaves that judgement where it can be
correct.

## What the engine promises

- **Order is readable off the file.** Handlers resolve, then children are built
  left to right, then the node itself, then its unconsumed properties in
  document order. No map decides any of it.
- **Construction is bottom-up; identity is top-down.** A parent is built after
  its children, because it may require them; node IDs are still allocated in
  schema order so diagnostics read the way the file does.
- **Node IDs are never reused**, so a callback that outlived its node cannot name
  a different one.
- **Teardown follows the tree's topology**, children before parents, and skips
  nodes whose construction never completed. It is *not* reverse creation order —
  those stopped being the same thing when construction went bottom-up.
- **A failed mount latches.** Until `Destroy`, a further `Mount` is refused, so
  two schemas cannot combine into one tree.
- **Signals run synchronously**, in document order.
- **A signal that re-enters itself is refused immediately**, by identity — not
  after a depth counter notices. A separate cap catches long *acyclic* chains.
- **A handler error stops the emission**; later handlers do not run.
- **Writes made before that error stay committed.** There is no rollback, and
  pretending otherwise would mean pretending the adapter's setters are
  reversible.
- **Mounting is refused while a signal is running**, and emitting is refused
  while a reconcile is walking the tree.
- **A reload is planned before it is applied.** Handlers resolve and every
  restructure is cleared with the adapter while the tree is still untouched, so
  a typo in a handler name leaves the screen exactly as it was.
- **An unchanged file changes nothing** — no setter runs, no binding is
  re-resolved, and the adapter is not called at all.
- **What cannot be pre-checked is a setter.** A failure there leaves the tree
  partially reconciled and latches it, exactly as a failed mount does.

## What it does not promise

`decl` guarantees its own graph. It cannot police what a resolved host function
does — that closure belongs to the host program and may touch the toolkit
directly. Keeping that safe is the adapter's contract with its own toolkit, and
it is documented there rather than claimed here.

A `Tree` is not safe for concurrent use. It lives wherever its adapter's toolkit
requires, and an adapter whose toolkit owns state on one goroutine is
responsible for getting calls there.

## Licence

See the repository's [LICENSE](../LICENSE).
