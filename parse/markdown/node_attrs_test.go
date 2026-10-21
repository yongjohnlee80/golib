package markdown_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
)

// Every accessor answers its zero value on a node that carries no values, and on a nil node, rather
// than panicking: the guard the moved fields need, since a field read could not fail on a real node.
// A setter on a nil node does nothing.
func TestANodeWithoutValuesAndANilNodeReadAsZero(t *testing.T) {
	text := &markdown.Node{Kind: markdown.KindText}
	var none *markdown.Node
	for name, n := range map[string]*markdown.Node{"a text node": text, "a nil node": none} {
		if n.Level() != 0 || n.Info() != nil || n.Fence() != 0 || n.List() != (markdown.ListData{}) ||
			n.Dest() != nil || n.Title() != nil || n.Label() != nil || n.HTML() != 0 ||
			n.Checked() != nil || n.Align() != nil || n.Target() != nil || n.Callout() != nil {
			t.Errorf("%s does not read as zero", name)
		}
	}
	none.SetDest([]byte("/u")) // must not panic
	if none.Dest() != nil {
		t.Error("a nil node kept a value")
	}
	text.SetDest([]byte("/u"))
	text.SetLevel(2)
	if string(text.Dest()) != "/u" || text.Level() != 2 {
		t.Errorf("set values read back %q, %d", text.Dest(), text.Level())
	}
	if (&markdown.Node{}).Dest() != nil {
		t.Error("a value set on one node reached another")
	}
}
