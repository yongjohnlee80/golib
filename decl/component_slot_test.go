package decl_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// component_slot_test.go covers named children slots: `property alias footer:
// foot.data` in a component file, and a use site setting it to one object,
// which lands under foot.

// picker routes a use's children to list and its footer and header objects to
// foot and head, each after the object's own children.
var picker = map[string]string{"Picker.qml": `Flex {
 default property alias cols: list.data
 property alias footer: foot.data
 property alias header: head.data
 Box { id: head }
 Box { id: list
  Text { text: "own col" } }
 Box { id: foot
  Text { text: "own foot" } }
}`}

// expanded parses doc and expands it against comps, as Mount does before
// judging it.
func expanded(t *testing.T, comps map[string]string, doc string) (*qml.SpecNode, error) {
	t.Helper()
	types := map[string]*qml.SpecNode{}
	for file, src := range comps {
		tree, err := qml.QML{}.Parse([]byte(src))
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		types[strings.TrimSuffix(file, ".qml")] = tree.Root
	}
	tree, err := qml.QML{}.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("doc: %v", err)
	}
	return decl.ExpandComponents(tree.Root, types, nil)
}

// texts lists the text of each Text directly under the node whose id has the
// prefix id (a scoped id is id@key), in order, or nil when there is no such node.
func texts(n *qml.SpecNode, id string) []string {
	if n.ID == id || strings.HasPrefix(n.ID, id+"@") {
		out := []string{}
		for _, c := range n.Children {
			for _, p := range c.Props {
				if p.Name == "text" {
					out = append(out, p.Value.Raw)
				}
			}
			if c.Type != "Text" {
				out = append(out, c.Type+texts1(c))
			}
		}
		return out
	}
	for _, c := range n.Children {
		if got := texts(c, id); got != nil {
			return got
		}
	}
	return nil
}

// texts1 renders a non-Text child's own texts, "[a b]".
func texts1(n *qml.SpecNode) string {
	var out []string
	for _, c := range n.Children {
		for _, p := range c.Props {
			if p.Name == "text" {
				out = append(out, p.Value.Raw)
			}
		}
	}
	return "[" + strings.Join(out, " ") + "]"
}

// TestASlotsObjectLandsUnderItsTarget: the footer object goes under foot,
// after foot's own child; the header under head; the children under list; and
// a second use that sets no slot leaves foot and head as the file wrote them.
func TestASlotsObjectLandsUnderItsTarget(t *testing.T) {
	root, err := expanded(t, picker, `Flex {
 Picker {
  Text { text: "c1" }
  footer: Row {
   Text { text: "f1" }
   Text { text: "f2" }
  }
  header: Text { text: "h1" }
 }
 Picker { }
}`)
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	first, second := root.Children[0], root.Children[1]
	for id, want := range map[string]string{"list": "own col c1", "foot": "own foot Row[f1 f2]", "head": "h1"} {
		if got := strings.Join(texts(first, id), " "); got != want {
			t.Errorf("first use's %s holds %q, want %q", id, got, want)
		}
	}
	for id, want := range map[string]string{"list": "own col", "foot": "own foot", "head": ""} {
		if got := strings.Join(texts(second, id), " "); got != want {
			t.Errorf("second use's %s holds %q, want %q (an unset slot is empty)", id, got, want)
		}
	}
	for _, p := range first.Props {
		if p.Name == "footer" || p.Name == "header" {
			t.Errorf("the slot %q was left on the root as a property", p.Name)
		}
	}
}

// TestASlotOnTheRootAppendsToItsChildren, as the default alias does there.
func TestASlotOnTheRootAppendsToItsChildren(t *testing.T) {
	root, err := expanded(t, map[string]string{"Bar.qml": "Box {\n id: bar\n property alias extra: bar.data\n Text { text: \"own\" }\n}"},
		"Flex {\n Bar { extra: Text { text: \"x\" } }\n}")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if got := strings.Join(texts(root.Children[0], "bar"), " "); got != "own x" {
		t.Fatalf("the root holds %q, want \"own x\"", got)
	}
}

// TestAComponentInASlotExpands, and a slot's object reaches an inner
// component's slot through a declared property of the outer one.
func TestAComponentInASlotExpands(t *testing.T) {
	comps := map[string]string{
		"Picker.qml": picker["Picker.qml"],
		"Badge.qml":  "Text { text: \"badge\" }",
		"Outer.qml":  "Box {\n required property var foot\n Picker { footer: foot }\n}",
	}
	root, err := expanded(t, comps, "Flex {\n Picker { footer: Badge { } }\n Outer { foot: Row { Text { text: \"deep\" } } }\n}")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	if got := strings.Join(texts(root.Children[0], "foot"), " "); got != "own foot badge" {
		t.Errorf("a component in a slot: foot holds %q, want \"own foot badge\"", got)
	}
	if got := strings.Join(texts(root.Children[1], "foot"), " "); got != "own foot Row[deep]" {
		t.Errorf("through Outer's property: foot holds %q, want \"own foot Row[deep]\"", got)
	}
}

// TestASlotsKeyIsItsOwn: a slot's object and the default children are keyed
// apart, so a component inside each gets ids that do not collide.
func TestASlotsKeyIsItsOwn(t *testing.T) {
	comps := map[string]string{"Picker.qml": picker["Picker.qml"], "Named.qml": "Box {\n Text { id: inner }\n}"}
	root, err := expanded(t, comps, "Flex {\n Picker {\n  Named { }\n  footer: Named { }\n }\n}")
	if err != nil {
		t.Fatalf("expand: %v", err)
	}
	ids := map[string]int{}
	var walk func(n *qml.SpecNode)
	walk = func(n *qml.SpecNode) {
		if strings.HasPrefix(n.ID, "inner@") {
			ids[n.ID]++
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if len(ids) != 2 {
		t.Fatalf("inner ids %v, want two distinct", ids)
	}
	for id := range ids {
		if strings.Contains(id, "+footer.") {
			return
		}
	}
	t.Fatalf("no inner id is keyed under the footer slot: %v", ids)
}

// TestAReloadPatchesASlotsObject: a changed slot object is patched in place,
// not rebuilt.
func TestAReloadPatchesASlotsObject(t *testing.T) {
	tr, rec, _ := componentTree(t, map[string]string{"Picker.qml": picker["Picker.qml"]})
	doc := func(text string) qml.SpecTree {
		return qmlDoc(t, "import app.ui\nFlex {\n Picker { footer: Text { text: \""+text+"\" } }\n}")
	}
	if err := tr.Mount(doc("f1")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	rec.trace = nil
	if _, err := tr.Reconcile(doc("f2")); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if trace := traced(rec); !strings.Contains(trace, "text=string(f2)") || strings.Contains(trace, "create") {
		t.Fatalf("a reload rebuilt the slot's object rather than patching it:\n%s", trace)
	}
}

// TestASlotIsRefusedWhatItCannotTake: the use site's value must be one object;
// the component file may fill an object from one alias only, and only an id's.
func TestASlotIsRefusedWhatItCannotTake(t *testing.T) {
	for name, c := range map[string]struct{ doc, want string }{
		"a string":    {"Flex {\n Picker { footer: \"x\" }\n}", `"footer" is a slot, which takes one object`},
		"a reference": {"Flex {\n Picker { footer: App.rows }\n}", `"footer" is a slot, which takes one object`},
		"a slot set twice": {"Flex {\n Picker {\n  footer: Flex { }\n  footer: Flex { }\n }\n}",
			`"footer" is set twice by this use (at 4:`},
		"a slot set twice, invalid first": {"Flex {\n Picker {\n  footer: \"x\"\n  footer: Flex { }\n }\n}",
			`"footer" is set twice by this use (at 4:`},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := expanded(t, picker, c.doc)
			if !errors.Is(err, decl.ErrComponent) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want ErrComponent saying %q", err, c.want)
			}
		})
	}
	for name, c := range map[string]struct{ src, want string }{
		"two aliases to one target": {"Box {\n default property alias a: x.data\n property alias b: x.data\n Flex { id: x }\n}",
			`"a" and "b" both fill x`},
		"two slots to one target": {"Box {\n property alias a: x.data\n property alias b: x.data\n Flex { id: x }\n}",
			`"a" and "b" both fill x`},
		"a slot to no id": {"Box {\n property alias foot: nowhere.data\n}", `the slot "foot" is nowhere.data`},
		"a slot named as an id": {"Box {\n property alias foot: foot.data\n Flex { id: foot }\n}",
			`"foot" is declared and is also an id`},
	} {
		t.Run(name, func(t *testing.T) {
			tr, _, _ := componentTree(t, map[string]string{"Bad.qml": c.src})
			err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Bad { }\n}"))
			if !errors.Is(err, decl.ErrComponent) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want ErrComponent saying %q", err, c.want)
			}
		})
	}
	// Qt's list of objects is refused where it is written, with the advice.
	_, err := qml.QML{}.Parse([]byte("Picker {\n footer: [Row { }, Text { }]\n}"))
	var se parse.SyntaxError
	if !errors.As(err, &se) || !strings.Contains(se.Want, "wrap them in one Flex") || se.Pos.Line != 2 {
		t.Fatalf("a list of objects: err = %v, want a syntax error at line 2 advising one Flex", err)
	}
	// An ordinary array is still an expression.
	if _, err := (qml.QML{}).Parse([]byte("Picker {\n footer: [a, \"b\", 3]\n}")); err != nil {
		t.Fatalf("an ordinary array: %v", err)
	}
}

// TestAComponentWithSlotsLoads when nothing uses its slots.
func TestAComponentWithSlotsLoads(t *testing.T) {
	tr, _, _ := componentTree(t, picker)
	if err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Picker { }\n}")); err != nil {
		t.Fatalf("mount: %v", err)
	}
}
