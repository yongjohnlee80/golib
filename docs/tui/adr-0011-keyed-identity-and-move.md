# ADR-0011 — golib/tui: Keyed Identity and Identity-Preserving Reorder (Move)

**Tags:** `type:adr` `status:proposed` `owner:shared` `repo:golib` `area:tui`
`kind:component-tree` `kind:lifecycle`

**Abstract:** Adds two small seams to the component tree: `Keyer`, an optional
capability interface letting a component declare a stable identity independent of
position, and `Container.Move`, an identity-preserving reorder primitive backed by
`App.moveWithin` — a splice that keeps a child's `NodeID`, context, in-flight
tasks, hooks, and focus intact where `Remove`+`Add` destroys them.

- **Status:** **Proposed** (2026-08-27)
- **Date:** 2026-08-27
- **Module:** github.com/yongjohnlee80/golib
- **Supersedes:** none
- **Related:** ADR-0004 (tree mechanics, mount/unmount cascade, `Container`; this
  ADR extends §2.4 with a third structural primitive), ADR-0005 (§2.8 addressed
  deliveries — a `NodeID`-keyed channel Move deliberately keeps routable),
  ADR-0007 (widget containers implementing `Container`).

## 1. Motivation

ADR-0004 gives the tree two structural mutations: mount and unmount. Identity is
the Go value (`App.byComp`); a remount of the same value is a NEW mount — fresh
`NodeID`, fresh derived context, fresh `Init`. Anything hung off the node is
destroyed on `Remove`: the `ctx.Go` task context is cancelled, `OnUnmount` hooks
run, addressed deliveries (`TaskResult`, `TickEvent`) dead-letter, and focus is
repaired away.

Reordering a container's children is the operation this pair cannot express.
"Move row 3 to row 1" via `Remove`+`Add` is an amputation: every child after the
insertion point pays a destroy/rebuild cycle it did not need, losing focus,
in-flight tasks, and subscriptions along the way. Sortable lists, manual
drag-reorder, and z-order changes all hit this. The struct fields survive (the Go
value persists) — the demo (`tui/examples/movesort`) makes the distinction
visible: `mounts` increments on re-`Init`, struct counters keep counting, but the
focused row loses focus and timers re-arm.

## 2. Decision

### 2.1 `Keyer` — declarable identity, container-consumed

```go
type Keyer interface { Key() any }
```

An optional capability interface, detected by assertion like `Focusable`. The
key MUST be non-nil, comparable (it serves as a map key in keyed containers),
and stable for the component's mounted lifetime. Absence = positional identity
(the index), which remains the correct default for append-only lists.

The key is declared on the component — not passed per-call — because tui fuses
config and state in one struct; there is no element/config object to annotate as
React/Flutter do. Containers that reconcile by key (a future keyed list widget)
consume `Keyer`; the framework itself never consults it.

### 2.2 `Container.Move` — sibling reorder without unmount

```go
type Container interface {
	Component
	Add(children ...Component)
	Remove(child Component)
	Move(child Component, to int)   // NEW
	Children() iter.Seq[Component]
}
```

Post-move-index semantics: the child ends up at index `to`. Bounds violations
panic; a child this container does not own is a silent no-op, mirroring
`Remove`. `Flex`, `Stack`, `Dock`, and `widget.Box` implement it (`Box` is
degenerate: single child, only index 0).

The three multi-child containers share their sibling-list machinery through
`MultiChild` (`container.go`), embedded anonymously — Go's analogue of
Flutter's `ContainerRenderObjectMixin`: `Init`, `Move`, and `Children` promote
verbatim; `Add` and `Remove` are shadowed as a PAIR by each container, because
only the container knows how to record and clean up per-child metadata, held
in a side table keyed by the child (`weights`, `layers`, `edges`). The zero
value of `MultiChild` is usable. Side-table caveats that bit once: a zero-
value map lookup can alias a real enum value (`DockTop == 0`); unpinned-vs-
pinned must be decided by PRESENCE of the map entry, not its value.

### 2.3 `App.moveWithin` — the splice (framework)

`moveWithin(parent, child, to)` is the single new tree primitive, placed beside
`mount`/`unmountTree` in `tree.go` and exposed as `Context.Move`. It splices
`parent.children` and nothing else. By construction it preserves:

- the `NodeID` — addressed deliveries stay routable (ADR-0005 §2.8);
- the derived context — in-flight `ctx.Go` tasks and timers keep running;
- `OnUnmount` hooks — none fire;
- focus and scope-stack membership — no repair pass is needed, since the node
  stays registered in `a.nodes` and a dangling focus ID is impossible;
- `Init` — not re-run.

It reuses mount's invariant grammar: illegal inside Layout/Render, panics on an
unmounted component and on a child of a different container.

### 2.4 Scope exclusions (v1)

- **No reparenting.** Moving a node to a different container remains
  Remove+Add (fresh mount). Cross-parent moves change the derived-ctx chain and
  focus-scope membership mid-flight — Flutter's `GlobalKey` complexity — and are
  banned with a panic until designed properly.
- **No framework-level reconciliation.** No keyed diffing in the core; key-aware
  containers compute their own Move/Add/Remove sequences. `Keyer` and `Move`
  are orthogonal: either works without the other.

## 3. Consequences

- Widening `Container` breaks hand-rolled test doubles (`lateContent` in
  `vimkeys_test.go` gained a no-op `Move`) — the expected tax of growing a
  capability interface; in-repo implementers were updated.
- `TestMovePreservesIdentity` pins the value proposition: same `NodeID`, ctx
  not cancelled, `Init` count 1, document order actually changed, plus the
  panic contract (`TestMovePanics`).
- `tui/examples/movesort` demonstrates both paths: `s`/arrow keys reorder via
  `Move` (counters keep climbing, focus follows the row); `R` reorders via
  Remove+Add (`mounts` increments, focus is repaired away).
