package decl

import (
	"fmt"
	"os"
	"path"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// component_unchanged_internal_test.go holds expand to ADR 1791761080's first
// promise: a document whose components declare nothing expands to the same
// tree it did before components could declare anything. expandBefore is
// expand and merge as they were at golib 890f002, frozen here as the
// reference; any difference in the expanded tree — a key, an order, a nil
// slice made empty — fails.

func expandBefore(root *qml.SpecNode, types map[string]*qml.SpecNode) (*qml.SpecNode, error) {
	var walk func(sn *qml.SpecNode, using []string, key string) (*qml.SpecNode, error)
	children := func(kids []*qml.SpecNode, using []string, key string, tag string) ([]*qml.SpecNode, error) {
		out := make([]*qml.SpecNode, len(kids))
		seen := map[string]int{}
		for i, c := range kids {
			ck := c.ID
			if ck == "" {
				ck = key + "/" + tag + c.Type + "#" + strconv.Itoa(seen[c.Type])
				seen[c.Type]++
			}
			ec, err := walk(c, using, ck)
			if err != nil {
				return nil, err
			}
			out[i] = ec
		}
		return out, nil
	}
	walk = func(sn *qml.SpecNode, using []string, key string) (*qml.SpecNode, error) {
		out := *sn
		if def, ok := types[sn.Type]; ok {
			for _, u := range using {
				if u == sn.Type {
					return nil, fmt.Errorf("%w: %s contains itself", ErrComponent, sn.Type)
				}
			}
			inner, err := walk(scopeIDs(def, key, sn.ID), append(using, sn.Type), key)
			if err != nil {
				return nil, err
			}
			out = mergeBefore(*inner, sn)
			n := len(inner.Children)
			extra, err := children(out.Children[n:], using, key, "+")
			if err != nil {
				return nil, err
			}
			copy(out.Children[n:], extra)
			return &out, nil
		}
		kids, err := children(sn.Children, using, key, "")
		if err != nil {
			return nil, err
		}
		out.Children = kids
		return &out, nil
	}
	return walk(root, nil, "")
}

func mergeBefore(comp qml.SpecNode, use *qml.SpecNode) qml.SpecNode {
	out := comp
	if use.ID != "" {
		out.ID = use.ID
	}
	overridden := map[string]bool{}
	for _, p := range use.Props {
		overridden[p.Name] = true
	}
	out.Props = nil
	for _, p := range comp.Props {
		if !overridden[p.Name] {
			out.Props = append(out.Props, p)
		}
	}
	out.Props = append(out.Props, use.Props...)
	out.Handlers = append(append([]qml.SpecHandler(nil), comp.Handlers...), use.Handlers...)
	out.Children = append(append([]*qml.SpecNode(nil), comp.Children...), use.Children...)
	return out
}

// unchangedCorpus: components and documents exercising every rule expand
// had — ids inside and at the root, named and unnamed uses, a use site's
// properties, handlers and children, nesting three deep, a qualified use.
var unchangedCorpus = struct {
	components map[string]string
	documents  []string
}{
	components: map[string]string{
		"Card": "Box {\n id: me\n title: \"default\"\n hint: field.text\n" +
			" Text { id: field; text: me.title; onClicked: { if (field.ok) me.close(); else App.go(field.text) } }\n" +
			" Text { text: \"two\" }\n onOpened: App.open(me.title, field.text)\n}",
		"Pair":  "Flex {\n Card { id: left; title: \"l\" }\n Card { }\n Text { id: note; text: left.title }\n}",
		"Outer": "Flex {\n id: outer\n Pair { Text { id: added; text: outer.x } }\n onDone: added.go()\n}",
		"Plain": "Text { text: \"plain\" }",
	},
	documents: []string{
		"Flex {\n Card { id: card; title: \"mine\"\n  Text { text: \"added\" }\n  Text { id: named } }\n}",
		"Flex {\n Card { }\n Card { onClicked: second() }\n Plain { }\n Plain { id: p }\n}",
		"Flex {\n Pair { id: pair }\n Pair { Card { } }\n}",
		"Flex {\n Outer { }\n Outer { id: o; title: \"t\"; Plain { } }\n Text { }\n}",
		"Window {\n UI.Card { id: q }\n UI.Plain { }\n}",
	},
}

func TestADocumentDeclaringNothingExpandsAsBefore(t *testing.T) {
	types := map[string]*qml.SpecNode{}
	for name, src := range unchangedCorpus.components {
		root, err := parseComponent(name+".qml", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		types[name] = root
		types["UI."+name] = root
	}
	docs := append([]string(nil), unchangedCorpus.documents...)

	// golib's own example program: its layout and its four dialog files.
	dir := "../tui/examples/editor-qml"
	entries, err := os.ReadDir(path.Join(dir, "dialogs"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		src, err := os.ReadFile(path.Join(dir, "dialogs", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		root, err := parseComponent(e.Name(), src)
		if err != nil {
			t.Fatal(err)
		}
		types[strings.TrimSuffix(e.Name(), ".qml")] = root
	}
	layout, err := os.ReadFile(path.Join(dir, "editor.qml"))
	if err != nil {
		t.Fatal(err)
	}
	docs = append(docs, string(layout))

	for i, src := range docs {
		spec, err := qml.QML{File: fmt.Sprintf("doc%d.qml", i)}.Parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		want, err := expandBefore(spec.Root, types)
		if err != nil {
			t.Fatal(err)
		}
		got, err := expand(spec.Root, types, nil)
		if err != nil {
			t.Fatalf("doc %d: %v", i, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("doc %d expands differently from before:\n got %s\nwant %s", i, dumpSpec(got), dumpSpec(want))
		}
	}
}

// dumpSpec renders a tree's ids, types and children for a failure message.
func dumpSpec(sn *qml.SpecNode) string {
	var b strings.Builder
	var walk func(sn *qml.SpecNode)
	walk = func(sn *qml.SpecNode) {
		b.WriteString(sn.Type + "#" + sn.ID + "[")
		for _, p := range sn.Props {
			b.WriteString(p.Name + "=" + p.Value.Raw + ";")
		}
		for _, c := range sn.Children {
			walk(c)
		}
		b.WriteString("]")
	}
	walk(sn)
	return b.String()
}
