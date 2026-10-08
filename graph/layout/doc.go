// Package layout places a directed graph's nodes in layers and routes its edges between them: a
// layered (Sugiyama) drawing, the family flowcharts are drawn in. It knows nothing of what the
// nodes are; the caller measures each one and gets back where its centre goes and the polyline
// each edge follows.
//
//	res, err := layout.Layered(ctx, layout.Input{
//		Nodes: []layout.Size{{W: 80, H: 40}, {W: 120, H: 40}, {W: 80, H: 40}},
//		Edges: []layout.Edge{{From: 0, To: 1}, {From: 0, To: 2}, {From: 1, To: 2}},
//		Dir:   layout.TB,
//	}, layout.Limits{})
//
// The drawing is computed in five passes:
//
//  1. Cycles are broken by reversing the back edges of a depth-first search in input order.
//  2. Each node gets a rank by longest path, and nodes are then pulled toward whichever side
//     holds more of their edges, which shortens edges without breaking any.
//  3. An edge spanning several ranks is split by a dummy node on each rank between. An edge
//     with a label gets the label's own dummy on its middle rank, so the label has room.
//  4. Each rank is ordered to reduce crossings by barycentre sweeps, down then up, keeping the
//     best order seen. The members of a group stay next to each other on every rank.
//  5. x positions come from Brandes and Köpf's four alignments, balanced; y positions from the
//     ranks' heights.
//
// Layered is deterministic: the same Input gives the same Result. Every input that the work
// grows with has a limit, and a graph past one is refused with ErrTooLarge before the pass that
// would grow past it, so a caller can fall back rather than wait.
//
// Tree lays out a tree (TreeInput: sizes and each node's parent) as a tidy tree: each subtree in
// a band of its own breadth, a parent centred over its children. BothWays splits the root's
// children into two sides growing opposite ways from it, as a mind map does.
package layout
