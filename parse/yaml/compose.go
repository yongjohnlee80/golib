package yaml

// composer builds documents from events. It keeps an explicit stack, so a deep document costs no
// Go stack, and binds each alias to the most recent node with its anchor in the same document.
type composer struct {
	stream  *Stream
	doc     *Document
	stack   []*Node
	pending []*Node // per open mapping: the key waiting for its value
	anchors map[string]*Node
}

func (c *composer) add(ev Event) error {
	switch ev.Kind {
	case EventDocumentStart:
		c.doc = &Document{Directives: ev.Directives, ExplicitStart: ev.Explicit, Span: ev.Span, stream: c.stream}
		c.anchors = map[string]*Node{}
	case EventDocumentEnd:
		c.doc.ExplicitEnd = ev.Explicit
		c.doc.Span.End = max(c.doc.Span.End, ev.Span.End)
		if c.doc.Root != nil {
			c.doc.Span.End = max(c.doc.Span.End, c.doc.Root.Span.End)
		}
		c.stream.Docs = append(c.stream.Docs, c.doc)
		c.doc = nil
	case EventScalar:
		c.place(&Node{Kind: KindScalar, Style: ev.Style, Tag: ev.Tag, Anchor: ev.Anchor, Value: ev.Value, Span: ev.Span})
	case EventAlias:
		target := c.anchors[ev.Alias]
		if target == nil {
			return &Error{Pos: c.stream.Position(ev.Span.Start), Msg: "an alias names an anchor not defined before it"}
		}
		c.place(&Node{Kind: KindAlias, Alias: ev.Alias, Target: target, Span: ev.Span})
	case EventSequenceStart, EventMappingStart:
		kind := KindSequence
		if ev.Kind == EventMappingStart {
			kind = KindMapping
		}
		n := &Node{Kind: kind, Style: ev.Style, Tag: ev.Tag, Anchor: ev.Anchor, Span: ev.Span}
		c.place(n)
		c.stack = append(c.stack, n)
		c.pending = append(c.pending, nil)
	case EventSequenceEnd, EventMappingEnd:
		n := c.stack[len(c.stack)-1]
		c.stack = c.stack[:len(c.stack)-1]
		c.pending = c.pending[:len(c.pending)-1]
		// a flow collection ends at its closing bracket; a block one at its last entry
		if n.Style == StyleFlow {
			n.Span.End = max(n.Span.End, ev.Span.End)
		}
		c.extend(n)
	}
	return nil
}

// place attaches n to the open collection, or makes it the document's root.
func (c *composer) place(n *Node) {
	if n.Anchor != "" {
		c.anchors[n.Anchor] = n // a later anchor of the same name shadows the earlier one
	}
	if len(c.stack) == 0 {
		c.doc.Root = n
		c.doc.Span.End = max(c.doc.Span.End, n.Span.End)
		return
	}
	parent := c.stack[len(c.stack)-1]
	top := len(c.pending) - 1
	switch parent.Kind {
	case KindSequence:
		parent.Items = append(parent.Items, n)
	case KindMapping:
		if c.pending[top] == nil {
			c.pending[top] = n
		} else {
			parent.Pairs = append(parent.Pairs, Pair{Key: c.pending[top], Value: n})
			c.pending[top] = nil
		}
	}
	c.extend(n)
}

// extend stretches the open collections to end at least where n does.
func (c *composer) extend(n *Node) {
	for i := len(c.stack) - 1; i >= 0 && c.stack[i].Span.End < n.Span.End; i-- {
		c.stack[i].Span.End = n.Span.End
	}
	if len(c.stack) == 0 && c.doc != nil && c.doc.Root == n {
		c.doc.Span.End = max(c.doc.Span.End, n.Span.End)
	}
}
