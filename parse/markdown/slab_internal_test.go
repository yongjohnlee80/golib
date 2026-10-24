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
	parser := New(WithGFM(), WithObsidian())
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

// release keeps the values of the nodes the tree holds and zeroes those of the nodes it let go, across
// many chunks of nodes and of values, where a live node's values and a dead one's lie side by side.
func TestReleaseKeepsALiveNodesValuesAndZeroesADeadOnes(t *testing.T) {
	var s nodeSlab
	root := s.new(Node{Kind: KindDocument})
	const n = 3000
	vals := make([]*nodeAttrs, n)
	for i := range n {
		node := s.newWith(Node{Kind: KindLink}, nodeAttrs{dest: []byte(fmt.Sprint(i)), level: i + 1})
		vals[i] = node.a
		if i%2 == 0 {
			root.AppendChild(node) // live; the odd ones are let go
		}
	}
	if len(s.attrs) < 3 {
		t.Fatalf("the values span %d chunks; the cell needs several", len(s.attrs))
	}
	s.release(root)
	for i, a := range vals {
		live := i%2 == 0
		switch {
		case live && (string(a.dest) != fmt.Sprint(i) || a.level != i+1):
			t.Fatalf("live node %d reads dest %q, level %d after release; want %d, %d", i, a.dest, a.level, i, i+1)
		case !live && (a.dest != nil || a.level != 0):
			t.Fatalf("dead node %d keeps dest %q, level %d after release; want them zeroed", i, a.dest, a.level)
		}
	}
	for c := root.FirstChild; c != nil; c = c.Next {
		if c.blk != nil {
			t.Fatal("a live node keeps its block state after release")
		}
	}
}
