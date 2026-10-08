# graph/layout

A layered (Sugiyama) layout for directed graphs: the drawing flowcharts use. The caller measures
each node; `Layered` says where each node's centre goes, the polyline each edge follows, where
each edge's label sits, and the box around each group. Standard library only, no drawing.

```go
import "github.com/yongjohnlee80/golib/graph/layout"

res, err := layout.Layered(ctx, layout.Input{
	Nodes:  []layout.Size{{W: 80, H: 40}, {W: 120, H: 40}, {W: 80, H: 40}},
	Edges:  []layout.Edge{{From: 0, To: 1}, {From: 0, To: 2, Label: layout.Size{W: 30, H: 12}}, {From: 1, To: 2}},
	Groups: [][]int{{1, 2}},
	Dir:    layout.LR,
}, layout.Limits{})
```

## How it lays out

1. **Cycles** are broken by reversing the back edges of a depth-first search in input order. A
   reversed edge is still drawn from its `From` to its `To`.
2. **Ranks** come from the longest path, then each node moves toward the side holding more of its
   edges, as far as no edge drops below its `MinLen`. That shortens edges.
3. **Long edges** are split by a dummy node on every rank they cross. When any edge has a label,
   every `MinLen` doubles and `RankSep` halves, and a labelled edge's middle dummy takes the
   label's size, so the label has room between the ranks.
4. **Order** within each rank comes from barycentre sweeps, down then up, eight rounds, keeping
   the order with the fewest crossings. A group's members stay together on every rank, between
   the group's own border dummies.
5. **x** comes from Brandes and Köpf's four alignments (upper or lower medians, scanned from
   either side), shifted onto the narrowest and balanced. **y** comes from the ranks' heights.

`TB` is computed, and `BT`, `LR` and `RL` are the same drawing turned, with each node's width
and height swapped on the way in for the sideways ones.

## Limits

Every input the work grows with has a limit (`Limits`; a zero field takes `DefaultLimits`'s):
nodes, edges, ranks, the dummy nodes long edges need, and the counted steps of work. A graph past
one is refused with `ErrTooLarge` before the pass that would grow past it. `ctx` is checked
between passes and every 1024 steps, so a caller can give up early or fall back to another
renderer. An `Input` that is not a graph (an edge to a missing node, a negative size, groups that
overlap without nesting) is `ErrInvalid`.

## Guarantees

- The same `Input` gives the same `Result`.
- Nodes never overlap; an edge starts on its `From` node's border and ends on its `To` node's.
- A group's box holds its members and no other node's centre. Nested groups grow by `GroupPad`
  per level. Vertically a box reaches `GroupPad` per level into the gap between ranks, so a
  deeply nested group wants a larger `RankSep`.

## Trees

`Tree` lays out a tree: `TreeInput` holds each node's size and its parent (`-1` for the root).
Each subtree takes a band of its own breadth, a parent sits centred over its children, and each
child sits `LevelSep` past its parent's far side, the way `Dir` grows. `BothWays` splits the
root's children, in their order, into two sides of near-equal breadth, growing in `Dir` and the
opposite way, as a mind map grows from its centre. `TreeResult` gives each node's centre and a
line from its parent's border to its own. It is deterministic and limited like `Layered`
(`MaxNodes`, `MaxRanks` as the deepest level, `MaxWork`).
