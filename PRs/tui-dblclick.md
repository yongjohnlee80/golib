# tui: double-click is activation, synthesised once at the event boundary

Closes item 2 of the autodb `--web-ui` follow-up ticket. Johno: *"let's implement double mouse click as ENTER."*

ENTER is not one behaviour — it means whatever the focused widget makes it mean — so the primitive is **activation**, and each widget keeps its own answer.

## What changed

- **`MouseEvent.Count`** — the press ordinal. 1 for a single press, 2 for the second press of a double, and **0 on every non-press kind**, so nothing can read a count off motion, wheel or release and believe it.
- **Synthesised in `App.dispatch`**, before hit-testing and before ADR-0010 §2.1's focus step, so a double-click that also moves focus still carries `Count == 2`. `term/decoder.go` and `web/input.go` are **unchanged**: a click count is behaviour derived from timing and position, not decode shape, so it belongs at the one boundary every producer already flows through (§2.5, and criterion 18's layering). A third producer inherits it for free.
- **`WithDoubleClickWindow`**, default 400ms. A run continues only on the same button, the same **cell**, and inside the window.
- **`Tree` publishes `ActivateEvent` on a double-click** — the same channel ENTER already uses, so a host that already handles activation gains double-click without changing. It fires for **branches** as well as leaves and does **not** also toggle expansion.
- **`List` converges on the boundary count**, replacing its own detection.

## Two judgement calls worth reviewing

**Same cell, not "near".** A terminal row is one cell tall, so a one-cell drift is a *different row*. Tolerating it would activate the row the user did not click, which is worse than requiring a steady hand.

**The Tree does not expand on double-click.** It cannot know whether a branch is activatable: autodb's `tbl:` nodes are branches whose children are columns and whose activation is a query scaffold, so a Tree that guessed would scaffold *and* expand. Hosts wanting folder-opens-on-double-click do it where the node grammar is known.

## `List` already had this, and that is why it changed

`List` timed its own double-clicks with a package constant and a direct `time.Now()`. That duplicated behaviour the boundary now owns, could not be configured, and could not be tested without sleeping. Keying it on the synthesised `Count` makes `List` and `Tree` agree by construction rather than by two implementations that happen to match.

`TestListMouse` and `TestTableBodyDoubleClickActivates` are **unchanged and still pass** — and now **fail** when synthesis is disabled, which is what proves the convergence is wired rather than parallel.

## Tests — criteria 19-28

Same-cell / window / button / release rules, `Count` 0 on non-press kinds, survival of the focus step, Tree branch-activation without expansion, single click publishing nothing, and **a behavioural bridge per real producer**: real SGR bytes through `term`, a real client report through `web`, each proving `Count == 2` without either producer computing it.

All verified to fail with synthesis disabled.

Two test defects found and fixed while writing them, both vacuous-pass:

- `h.sync()` only runs an `Update` callback while injected events travel a different path, so every assertion was reading an empty slice;
- the non-press assertion passed while **no** non-press event had arrived — it now requires all three.

## Checks

gofmt clean, vet clean, module green under `-race`.

## Release

Patch bump on the existing line — **v0.5.3** — once reviewed. autodb consumes it for item 2; no autodb code change is needed, because its explorer already subscribes to `ActivateEvent`.
