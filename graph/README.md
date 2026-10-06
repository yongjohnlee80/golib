# graph

A directed multigraph whose edges carry a kind: a document graph whose links are wikilinks, or
`supersedes` and `amends` relations, side by side. Plain data and algorithms, with no I/O and no
dependencies, so a server that answers a part of its graph and a client that draws it build the
same `Graph` from the same edges and walk it with the same code.

```bash
go get github.com/yongjohnlee80/golib/graph
```

```go
import "github.com/yongjohnlee80/golib/graph"
```

## Nodes and edges

An `Edge[N]` is `{Src, Dst, Kind}`. Two edges are the same edge when all three are equal, so a pair
of nodes may hold edges of several kinds. `Add` adds an edge and its two ends; `AddNode` adds a
node with no edges, which stays in the graph (a document nothing links to is still a document).

```go
g := graph.New[string]()
g.Add(graph.Edge[string]{Src: "adrs/0002.md", Dst: "adrs/0001.md", Kind: "supersedes"})
g.Add(graph.Edge[string]{Src: "notes/a.md", Dst: "adrs/0002.md", Kind: "wikilink"})
g.AddNode("notes/orphan.md")
g.Has("notes/orphan.md") // true
```

Nodes and edges keep the order they were added in, and every iteration (`Nodes`, `Edges`, `Out`,
`In`) follows it: the same additions give the same answers, so a test or a reply can compare them.

## Filters and directions

A `Filter` admits edges by kind; the zero `Filter` admits all of them. `Out(n, f)` yields the edges
leaving `n`, `In(n, f)` those reaching it, `Edges(f)` all of them.

```go
for e := range g.In("adrs/0001.md", graph.Filter{Kinds: []string{"supersedes"}}) {
	fmt.Println(e.Src, "supersedes it")
}
```

## Neighbourhoods

`Neighborhood(start, depth, dir, f)` is the part of the graph within `depth` steps of `start`,
walking the edges `f` admits in direction `graph.Out`, `graph.In` or `graph.Both`. It holds the
nodes reached (`start` included, even with no edge) and every admitted edge whose two ends were
both reached. The filter stops the walk itself, not only the answer: a node reached only through
an edge of another kind is not in it. A start that is not a node gives an empty graph.

```go
near := g.Neighborhood("adrs/0002.md", 1, graph.Both, graph.Filter{Kinds: []string{"supersedes"}})
for e := range near.Edges(graph.Filter{}) {
	fmt.Println(e.Src, e.Kind, e.Dst) // adrs/0002.md supersedes adrs/0001.md
}
```

A `Graph` is not safe for concurrent use.

## License

See [LICENSE](../LICENSE).
