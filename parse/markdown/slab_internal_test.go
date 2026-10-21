package markdown

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// A node never moves once placed, across many chunks.
func TestASlabNodeNeverMoves(t *testing.T) {
	var s nodeSlab
	nodes := make([]*Node, 10000)
	for i := range nodes {
		nodes[i] = s.new(Node{Kind: KindText, Span: Span{Start: i, End: i + 1}})
	}
	seen := map[*Node]bool{}
	for i, n := range nodes {
		if seen[n] || n.Span.Start != i || n.Span.End != i+1 {
			t.Fatalf("node %d moved, was overwritten or shares a slot: %+v", i, n.Span)
		}
		seen[n] = true
	}
	if got := cap(s.chunks[len(s.chunks)-1]); got != 1024 {
		t.Errorf("the last chunk holds %d nodes, want the 1024 a chunk grows to", got)
	}
}

// Parses made at once by one parser value share no node, and none keeps its slab after Parse.
func TestParsesAtOnceShareNoNode(t *testing.T) {
	parser := New(GFM(), Obsidian())
	docs := make([]*Document, 24)
	var wg sync.WaitGroup
	for i := range docs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			docs[i], _ = parser.Parse([]byte(fmt.Sprintf("# document %d\n\n", i) + strings.Repeat("*a* [[b]] #tag\n", 600)))
		}(i)
	}
	wg.Wait()
	owner := map[*Node]int{}
	for i, d := range docs {
		var visit func(*Node)
		visit = func(n *Node) {
			for ; n != nil; n = n.Next {
				if o, ok := owner[n]; ok {
					t.Fatalf("a node is shared by parses %d and %d", o, i)
				}
				owner[n] = i
				visit(n.FirstChild)
			}
		}
		visit(d.Root)
		if d.nodes.chunks != nil {
			t.Errorf("document %d keeps its slab after Parse", i)
		}
	}
}
