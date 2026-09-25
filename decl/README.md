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

// What the host hands over is what the schema can reach. Injection IS
// authorisation; there is no second gate asking whether it meant it.
tree.DeclareModule(decl.Module{Name: "myapp.theme", Version: "1.0",
    Exports: []string{"Theme"}})
tree.Inject("Theme.heading", decl.SourceValue(str("Project Atlas")))
tree.Inject("save", decl.Handle(func([]qml.SpecValue) error { return save() }))

spec, err := qml.QML{}.Parse(src)
if err != nil { /* a schema is input; show the error and keep the old tree */ }

if err := tree.Mount(spec); err != nil { /* … */ }

// Later, when a widget's signal fires:
err = tree.Emit(nodeID, "clicked")
```

An adapter implements three methods — `Create`, `Apply`, `Destroy` — and
nothing in their signatures names a toolkit. Handlers are **not** among them:
the adapter used to turn a handler name into a function, which made it a second
name scope beside the engine's own and flattened `onClicked: save` to `"save"`,
erasing the difference from `save()` where nothing downstream could recover it.
A host injects its effects and the engine resolves them through the same typed
registry as every other name. A fourth capability,
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

A property is judged by the optional `Classifier` capability, which answers with
one of **three** kinds — and the third is the one a boolean could not carry:

| kind | meaning | response |
| --- | --- | --- |
| `PropRuntime` | there is a setter | applied in place |
| `PropConstructorOnly` | taken at construction, no setter | **rebuild** |
| `PropUnknown` | the adapter has no such property | **refused during planning; nothing is touched** |

"Unknown" and "constructor-only" are both un-appliable, which is exactly why one
flag looked sufficient. Their correct responses are opposite: rebuilding for a
*misspelled* property demolishes a working widget to construct one that refuses
it just the same — and the diagnostic blames the constructor for a name the
adapter never had.

The engine cannot work this out for itself. A builder reports a property
consumed only when the schema *declared* it, so a `Split` mounted with no
`orientation` consumes nothing; and nothing in the consumed set distinguishes a
constructor argument from a typo. An adapter therefore declares its
constructor-only properties (`WithConstructorProps`) alongside its setters.

`ClassifyProperty` takes a **schema type name, not a node** — and that is what
makes it useful. "Can a `Button` take a property called `nosuch`" is a question
about `Button`s, and the nodes a reload most needs vetted **do not exist yet**:
one the schema adds, or the replacement for one whose type changed. Planning
therefore checks every property of every subtree it is about to mount, so a
misspelling in a *new* node is refused before the node it replaces is destroyed.

`PropertyKind` is a closed set. An adapter returning something else is refused,
not treated as settable: a permissive default would turn a future fourth kind
into "apply it and hope".

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

## Modules — declared, offered, and component files

A module is imported before its names resolve, as in QML. A host **declares** a
module every document may use, or **offers** one it may not: an offered module's
loader runs the first time a document imports it — at Mount or at a Reconcile —
and never otherwise.

```go
tree.DeclareModule(decl.Module{Name: "editor", Version: "1.0", Exports: []string{"App"}})
tree.OfferModule("editor.theme.retro", "1.0", decl.ValueModule(retroQML))
tree.OfferModule("editor.dialogs", "1.0", decl.ComponentFiles(dialogFS, "dialogs"))
```

A loader returns `ModuleContents`: the singletons it exports, their values, and
its **component types**.

- **`ValueModule(src)`** is a module written as a QML document of literals —
  `Theme { menu { window: "white" } }` — whose root type is the export and whose
  properties are constants under it. A theme is one.
- **`ComponentFiles(fs, dir)`** is a directory of `.qml` files, each a type named
  for its file. A use is EXPANDED before the document is judged: the use site's
  properties replace the component's, handlers from both run, the use site's
  children follow, the use site gives the id. A component resolves names in the
  document that uses it, and so imports nothing. Ids it declares are its own —
  Qt's component scope: one set per use, unreachable from the document, and
  shadowing the document's; its root's id names the instance from inside. One
  that contains itself is refused, with the cycle spelled out.
- **`Repeater` / `Instantiator`** are expanded after components: one copy of
  the delegate per row of a host `Model`, `model.<role>` and `index` written in
  from the row, each copy keyed by the row's key. The tree follows the models
  and sources it read and, on a change, re-expands the document as written and
  reconciles it — on the scheduler, never inside an emission. A delegate that
  is a `DelegateChooser` is resolved per row first: the first `DelegateChoice`
  whose `roleValue` matches the row's `role` value by Qt's rule (as values,
  else as integers — a number rounded half away from zero, as QVariant does —
  else as strings; one without a roleValue matches all), and
  a row with no match has no copy. Every delegate template, including a choice
  no row selects and the delegate of an empty model, is checked for the
  placement rules whatever the rows are.
- **Reading an object's property by id in a handler** — `App.login(user.text)`
  — reads the live object when the handler runs, through the optional
  `PropertyReader` capability. Only in a handler: a binding over another
  object's property would need its change signal.

Loading goes through the same registration a declared module does, so every
rule holds — two loaded modules still may not export one name, and a document
importing two themes that both export `Theme` is refused. A load is undone when
the operation that caused it fails without building anything, and `Destroy`
unloads every loaded module with the registry its values lived in. An optional
`Vocabulary` capability lists the adapter's type names, so a component named
like one is refused rather than replacing it.

## Handlers: methods by id, and signal parameters

A handler may call a **method on a node the document declared**, by its id:

```qml
MenuItem { onTriggered: quitDialog.open() }
```

An adapter offers methods through the optional `Methods` capability
(`MethodsOf`, `Invoke`). The engine checks the call when the handler is
compiled, so a method the type lacks is refused at mount, naming the ones it has.
It resolves the id when the signal FIRES, so a reload that rebuilds the node
under the same id is followed. An id that spells an injected or imported name
is refused as ambiguous.

A handler may pass on **what its signal was raised with**:

```qml
FileDialog { onAccepted: App.openFile(selectedFile) }
```

An adapter names each signal's parameters through the optional
`SignalParameters` capability; an emitter takes them —
`emit(args ...qml.SpecValue)` — and `Tree.Emit` carries them. A parameter
shadows an injected name of the same spelling, as a handler's innermost scope.

## Values: the one operator

The evaluator runs names, calls and ONE operator: `|` over integers, which is
how Qt combines flags (`Dialog.Yes | Dialog.No`). A source in an operand makes
the value a binding. Every other operator is refused with `ErrExpressionValue`
— the document is valid QML, and what is missing is evaluator capability, which
the sentinel says.

## Signals raised mid-operation

A widget may raise a signal while the tree is mounting, reconciling or fanning
out a source change — an editor reports its mode when a bound keyset switches
it. Such a signal is **deferred**, not refused: queued, coalesced (one delivery
per node and signal, carrying the latest parameters), and delivered once the
outermost operation has committed. A handler that starts an operation of its own
drains the queue at that operation's end, while its own emission is still
running, so a feedback loop is caught by the ordinary cycle detector.

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
- **Mounting is refused while a signal is running.** A signal raised while a
  reconcile or a propagation is walking the tree is deferred until it commits.
- **A reload is planned before it is applied.** Every handler resolves —
  including those in subtrees the schema *adds*, whose identities are allocated
  during planning for exactly that reason — and every restructure and changed
  property is cleared with the adapter while the tree is untouched. A typo in a
  handler name leaves the screen exactly as it was.
- **An unchanged file changes nothing** — no setter runs, no binding is
  re-resolved, and the adapter is not called at all.
- **Three things cannot be pre-checked.** A **constructor**, since
  classification proves a property *exists* and only the builder knows whether
  it accepts this *value*. A **setter**, for the same reason one step later. And
  a **structural operation** — `CanRestructure` settles whether a node accepts
  child changes at all, but `Insert`, `Remove` and `Move` each fail at the
  moment they run.
- **The constructor is made harmless, not just reported** — *within one
  parent's child list*. Every replacement there is **built before anything it
  replaces is released**, so `direction: diagonal` costs you the half-built
  replacement and nothing else: the live screen is still there and the tree is
  **not** latched. If *discarding* that replacement fails — `Destroy` is allowed
  to — the failure is **joined onto** the constructor error rather than dropped.
  It has nowhere else to surface: the nodes are forgotten immediately after, so
  no later `Destroy` can retry or report them.
- **That guarantee is per-parent, not whole-tree.** A node's own properties are
  applied before its children's replacements are constructed, so a property
  change on one node followed by a refused constructor *deeper in the tree*
  leaves the applied value in place, and the tree **does** latch. Closing this
  would mean deferring every property application to a commit phase across the
  whole reconcile — a different design, and not one this engine makes.
- **The other two latch.** Once a setter has touched a live widget or a child
  has been detached, the tree is partially reconciled; `Destroy` clears it. A
  reconcile that fails while still *constructing* has changed nothing and is not
  latched, and the difference is tracked rather than assumed.

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
