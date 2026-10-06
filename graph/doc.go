// Package graph is a directed multigraph whose edges carry a kind: a document graph whose links
// are wikilinks, or "supersedes" and "amends" relations, side by side. It is plain data and
// algorithms, with no I/O, so a server that answers a part of its graph and a client that draws
// it build the same Graph from the same edges and walk it with the same code.
//
//	g := graph.New[string]()
//	g.Add(graph.Edge[string]{Src: "adrs/0002.md", Dst: "adrs/0001.md", Kind: "supersedes"})
//	g.Add(graph.Edge[string]{Src: "notes/a.md", Dst: "adrs/0002.md", Kind: "wikilink"})
//	g.AddNode("notes/orphan.md") // a node with no edges
//
//	near := g.Neighborhood("adrs/0002.md", 1, graph.Both, graph.Filter{Kinds: []string{"supersedes"}})
//	for e := range near.Edges(graph.Filter{}) {
//		fmt.Println(e.Src, e.Kind, e.Dst)
//	}
//
// Nodes and edges keep the order they were added in, and every iteration follows it, so the same
// additions give the same answers. A Graph is not safe for concurrent use.
package graph
