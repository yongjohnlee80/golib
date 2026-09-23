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

An adapter implements five methods — `Create`, `Apply`, `Attach`,
`ResolveHandler`, `Destroy` — and nothing in their signatures names a toolkit.

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

- **Order is readable off the file.** Create, then properties in document order,
  then handlers, then children left to right. No map decides any of it.
- **Node IDs are never reused**, so a callback that outlived its node cannot name
  a different one.
- **Teardown is reverse mount order**, so a child is released before its parent.
- **Signals run synchronously**, in document order.
- **A signal that re-enters itself is refused immediately**, by identity — not
  after a depth counter notices. A separate cap catches long *acyclic* chains.
- **A handler error stops the emission**; later handlers do not run.
- **Writes made before that error stay committed.** There is no rollback, and
  pretending otherwise would mean pretending the adapter's setters are
  reversible.
- **Mounting is refused while a signal is running.**

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
