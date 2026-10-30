package markdown

// DefaultMaxNesting is how deeply containers and inline constructs nest by default (MaxNesting).
const DefaultMaxNesting = 100

// MaxNesting bounds how deeply a document nests (default DefaultMaxNesting): block quotes and list
// items, and emphasis, strikethrough, links and images within a line. A marker that would open a
// level past it stays text, in the deepest level allowed: a block quote's '>' or a list marker is
// read as the line's content, and an emphasis or a bracket pair that would hold more is left
// unmatched. Every walk of a document, in this package and out of it (a renderer, a highlighter, an
// indexer), recurses at most that deep, where unbounded input would nest as deep as it is long and
// overflow the stack, which is fatal. n <= 0 keeps the default.
func MaxNesting(n int) Option {
	return func(c *config) { c.maxNesting = n }
}

// containerDepth is how deep a block started under n lands, less one: how many containers hold
// it, the document's root not counted. A paragraph or a table there is closed by the start, and a
// list takes an item as its own child or is closed for a new one, so none of them is a level the
// new block sits under; they are skipped.
func containerDepth(n *Node) int {
	for n != nil && (n.Kind == KindParagraph || n.Kind == KindTable || n.Kind == KindList) {
		n = n.Parent
	}
	d := 0
	for ; n != nil && n.Parent != nil; n = n.Parent {
		d++
	}
	return d
}

// heightOf is the tallest inline height among the siblings from n up to stop.
func heightOf(n, stop *Node) int {
	h := 0
	for ; n != nil && n != stop; n = n.Next {
		h = max(h, int(n.height))
	}
	return h
}
