package markdown_test

import (
	"reflect"
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

// nodeValue is one of the values a Node reads through a method: how to set it, how to read it, and a
// value that is not its zero.
type nodeValue struct {
	name string
	set  func(*markdown.Node)
	get  func(*markdown.Node) any
	want any
}

func nodeValues() []nodeValue {
	yes := true
	target := &markdown.Target{Page: []byte("p"), Heading: []byte("h")}
	callout := &markdown.Callout{Type: []byte("note"), Fold: '-'}
	list := markdown.ListData{Type: markdown.ListOrdered, Tight: true, Delimiter: ')', Start: 3}
	return []nodeValue{
		{"Level", func(n *markdown.Node) { n.SetLevel(4) }, func(n *markdown.Node) any { return n.Level() }, 4},
		{"Info", func(n *markdown.Node) { n.SetInfo([]byte("go")) }, func(n *markdown.Node) any { return n.Info() }, []byte("go")},
		{"Fence", func(n *markdown.Node) { n.SetFence('~') }, func(n *markdown.Node) any { return n.Fence() }, byte('~')},
		{"List", func(n *markdown.Node) { n.SetList(list) }, func(n *markdown.Node) any { return n.List() }, list},
		{"Dest", func(n *markdown.Node) { n.SetDest([]byte("/d")) }, func(n *markdown.Node) any { return n.Dest() }, []byte("/d")},
		{"Title", func(n *markdown.Node) { n.SetTitle([]byte("t")) }, func(n *markdown.Node) any { return n.Title() }, []byte("t")},
		{"Label", func(n *markdown.Node) { n.SetLabel([]byte("l")) }, func(n *markdown.Node) any { return n.Label() }, []byte("l")},
		{"HTML", func(n *markdown.Node) { n.SetHTML(6) }, func(n *markdown.Node) any { return n.HTML() }, 6},
		{"Checked", func(n *markdown.Node) { n.SetChecked(&yes) }, func(n *markdown.Node) any { return n.Checked() }, &yes},
		{"Align", func(n *markdown.Node) { n.SetAlign([]markdown.Align{markdown.AlignLeft, markdown.AlignRight}) },
			func(n *markdown.Node) any { return n.Align() }, []markdown.Align{markdown.AlignLeft, markdown.AlignRight}},
		{"Target", func(n *markdown.Node) { n.SetTarget(target) }, func(n *markdown.Node) any { return n.Target() }, target},
		{"Callout", func(n *markdown.Node) { n.SetCallout(callout) }, func(n *markdown.Node) any { return n.Callout() }, callout},
	}
}

// Each of the twelve values reads back what its setter set, on a node of any kind, alone or with the
// other eleven set beside it, and a value set on one node reaches no other. A setter on a nil node
// does nothing, and leaves no value that a node without values then reads.
func TestEveryNodeValueReadsBackWhatItsSetterSet(t *testing.T) {
	values := nodeValues()
	all := &markdown.Node{Kind: markdown.KindParagraph}
	for _, v := range values {
		alone := &markdown.Node{Kind: markdown.KindText}
		v.set(alone)
		if got := v.get(alone); !reflect.DeepEqual(got, v.want) {
			t.Errorf("%s set alone reads %v; want %v", v.name, got, v.want)
		}
		v.set(all)
		var none *markdown.Node
		v.set(none) // must not panic
		if got, zero := v.get(none), v.get(&markdown.Node{}); !reflect.DeepEqual(got, zero) {
			t.Errorf("a nil node reads %s %v after its setter; want the zero %v", v.name, got, zero)
		}
	}
	for _, v := range values {
		if got := v.get(all); !reflect.DeepEqual(got, v.want) {
			t.Errorf("%s set beside the other eleven reads %v; want %v", v.name, got, v.want)
		}
		if got := v.get(&markdown.Node{Kind: markdown.KindParagraph}); !reflect.ValueOf(got).IsZero() {
			t.Errorf("a node nothing was set on reads %s %v", v.name, got)
		}
	}
}

// A copy of a Node that carries values shares them: they hang off it by a pointer, so a setter on the
// copy changes what the original reads. The README says so; this cell holds it to that.
func TestACopyOfANodeSharesItsValues(t *testing.T) {
	orig := &markdown.Node{Kind: markdown.KindLink}
	orig.SetDest([]byte("/a"))
	cp := *orig
	cp.SetDest([]byte("/b"))
	if string(orig.Dest()) != "/b" {
		t.Errorf("the original reads %q after a set on its copy; want the shared /b", orig.Dest())
	}
}
