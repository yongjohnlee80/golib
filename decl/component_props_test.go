package decl_test

import (
	"errors"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// component_props_test.go covers a component's own properties: declared in
// its file, judged when the file loads, and substituted at each use with the
// use site's value or the default. As in component_test.go, every claim about
// a value is read off the adapter's trace — what was BUILT.

// TestAComponentsDeclarationsAreJudgedWhenItLoads: each mistake is the FILE's,
// reported against it whatever uses it.
func TestAComponentsDeclarationsAreJudgedWhenItLoads(t *testing.T) {
	for name, c := range map[string]struct{ src, want string }{
		"a name declared twice": {"Box {\n property string a: \"x\"\n property int a: 1\n}",
			`"a" is declared twice (at ui/Bad.qml:3:`},
		"a name that is an id": {"Box {\n property string field\n Text { id: field }\n}",
			`"field" is declared and is also an id`},
		"the root's id": {"Box {\n id: me\n property string me\n}",
			`"me" is declared and is also an id`},
		"an alias to no id": {"Box {\n default property alias cols: list.data\n}",
			`"list" is no id in the file`},
		"two default aliases": {"Box {\n default property alias a: x.data\n default property alias b: x.data\n Flex { id: x }\n}",
			`"a" and "b" are both default aliases`},
		"a declaration below the root": {"Box {\n Text { property string a }\n}",
			`a Text inside the component declares "a"`},
		"a declaration in a template": {"Box {\n delegate: Text { property string a }\n}",
			`a Text inside the component declares "a"`},
		"an optional property with no default": {"Box {\n property string label\n}",
			"give label a default, or make it required"},
		"a default of the wrong kind": {"Box {\n property int n: \"x\"\n}",
			`"n" is declared int, and its default is a string`},
	} {
		t.Run(name, func(t *testing.T) {
			tr, _, _ := componentTree(t, map[string]string{"Bad.qml": c.src})
			err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Bad { }\n}"))
			if !errors.Is(err, decl.ErrComponent) || !errors.Is(err, decl.ErrModuleLoad) ||
				!strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want ErrComponent from the load, saying %q", err, c.want)
			}
		})
	}
	// And a sound one loads.
	tr, _, _ := componentTree(t, map[string]string{"Good.qml": "Box {\n property int n: 3\n" +
		" required property string a\n default property alias c: x.data\n Flex { id: x }\n}"})
	if err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Good { a: \"y\" }\n}")); err != nil {
		t.Fatalf("a sound component was refused: %v", err)
	}
}

// labelled is a component using its declared label in every place a
// reference can be: a property's whole value, through the root's id, a call's
// argument, and a handler's statements.
var labelled = map[string]string{"Card.qml": `Box {
 id: me
 property string label: "dflt"
 title: label
 Text { text: me.label }
 Text { text: join(label, "!") }
 onClicked: got(label, me.label)
}`}

// mountLabelled mounts doc against labelled, with join and got injected; got
// records the arguments each run saw.
func mountLabelled(t *testing.T, doc string) (*decl.Tree, *recorder, *[]string) {
	t.Helper()
	tr, rec, _ := componentTree(t, labelled)
	ran := &[]string{}
	if err := tr.DeclareFunc("join", func(a []qml.SpecValue) (qml.SpecValue, error) {
		return qml.SpecValue{Kind: qml.SpecValueString, Raw: a[0].Raw + a[1].Raw}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := tr.Inject("got", decl.Handle(func(a []qml.SpecValue) error {
		*ran = append(*ran, a[0].Raw+","+a[1].Raw)
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := tr.Mount(qmlDoc(t, doc)); err != nil {
		t.Fatalf("mount: %v", err)
	}
	return tr, rec, ran
}

// TestADeclaredPropertyTakesTheUseSitesValueEverywhere — and the default
// where the use sets none. Two uses of one component keep their values apart.
func TestADeclaredPropertyTakesTheUseSitesValueEverywhere(t *testing.T) {
	tr, rec, ran := mountLabelled(t, "import app.ui\nFlex {\n Card { id: a; label: \"mine\" }\n Card { id: b }\n}")
	trace := traced(rec)
	for _, want := range []string{
		"apply 2 title=string(mine)", "apply 3 text=string(mine)", "apply 4 text=string(mine!)",
		"apply 5 title=string(dflt)", "apply 6 text=string(dflt)", "apply 7 text=string(dflt!)",
	} {
		if !strings.Contains(trace, want) {
			t.Errorf("trace lacks %q:\n%s", want, trace)
		}
	}
	if strings.Contains(trace, "label=") {
		t.Errorf("a declared property was applied to the root as well:\n%s", trace)
	}
	for _, id := range []string{"a", "b"} {
		n, _ := tr.NodeByID(id)
		if err := rec.emitters[n]["clicked"](); err != nil {
			t.Fatalf("emit %s: %v", id, err)
		}
	}
	if got := strings.Join(*ran, " "); got != "mine,mine dflt,dflt" {
		t.Errorf("the handlers ran with %q, want each use's own label: mine,mine dflt,dflt", got)
	}
}

// TestAReloadSubstitutesTheNewValue: the use site's value is the binding, so
// changing it patches the property where the component uses it.
func TestAReloadSubstitutesTheNewValue(t *testing.T) {
	tr, rec, _ := mountLabelled(t, "import app.ui\nFlex {\n Card { id: a; label: \"one\" }\n}")
	rec.trace = nil
	if _, err := tr.Reconcile(qmlDoc(t, "import app.ui\nFlex {\n Card { id: a; label: \"two\" }\n}")); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	trace := traced(rec)
	for _, want := range []string{"title=string(two)", "text=string(two)", "text=string(two!)"} {
		if !strings.Contains(trace, want) {
			t.Errorf("trace lacks %q:\n%s", want, trace)
		}
	}
	if strings.Contains(trace, "create") || strings.Contains(trace, "destroy") {
		t.Errorf("a new value rebuilt the component:\n%s", trace)
	}
}

// TestARequiredPropertyTheUseDoesNotSetIsRefused, naming it and the use.
func TestARequiredPropertyTheUseDoesNotSetIsRefused(t *testing.T) {
	tr, _, _ := componentTree(t, map[string]string{"Card.qml": "Box {\n required property string label\n title: label\n}"})
	err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Text { }\n Card { }\n}"))
	if !errors.Is(err, decl.ErrComponent) || !strings.Contains(err.Error(), `required property "label" is not set`) ||
		!strings.Contains(err.Error(), "(at 4:2)") {
		t.Fatalf("err = %v, want ErrComponent naming label and the use at 4:2", err)
	}
	if err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Card { label: \"x\" }\n}")); err != nil {
		t.Fatalf("set, it mounts: %v", err)
	}
}

// TestAUseSitesValueMustBeOfTheDeclaredKind, when it is a literal.
func TestAUseSitesValueMustBeOfTheDeclaredKind(t *testing.T) {
	for _, c := range []struct{ typ, value, got string }{
		{"string", "3", "number"}, {"int", `"3"`, "string"}, {"real", "true", "bool"},
		{"double", `"x"`, "string"}, {"bool", "1", "number"},
	} {
		tr, _, _ := componentTree(t, map[string]string{"Card.qml": "Box {\n required property " + c.typ + " p\n}"})
		err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Card { p: "+c.value+" }\n}"))
		want := `"p" is declared ` + c.typ + ", and this use gives it a " + c.got
		if !errors.Is(err, decl.ErrComponent) || !strings.Contains(err.Error(), want) {
			t.Errorf("%s given %s: err = %v, want %q", c.typ, c.value, err, want)
		}
	}
	// var takes anything, and a binding is the type's to judge where it lands.
	tr, _, _ := componentTree(t, map[string]string{"Card.qml": "Box {\n property var v: 1\n property string s: \"\"\n title: s\n}"})
	if err := tr.Mount(qmlDoc(t, "import tui 1.0\nimport app.ui\nFlex {\n Card { v: \"x\"; s: Tui.Vertical }\n}")); err != nil {
		t.Fatalf("var given a string, string given a binding: %v", err)
	}
}

// TestAnObjectCannotBecomeAnExpression: a template is a whole property value,
// never part of a handler.
func TestAnObjectCannotBecomeAnExpression(t *testing.T) {
	tr, _, _ := componentTree(t, map[string]string{"Card.qml": "Box {\n required property var item\n onClicked: got(item)\n}"})
	err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Card { item: Text { } }\n}"))
	if !errors.Is(err, decl.ErrComponent) || !strings.Contains(err.Error(), `"item" is a object template`) {
		t.Fatalf("err = %v, want the template refused in a handler", err)
	}
}

// TestADefaultAliasTakesTheUsesChildren: they land under the target, after
// its own, and a reload patches them rather than rebuilding.
func TestADefaultAliasTakesTheUsesChildren(t *testing.T) {
	tr, rec, _ := componentTree(t, map[string]string{"Table.qml": "Flex {\n default property alias cols: list.data\n" +
		" Text { id: head }\n Box { id: list\n  Text { text: \"own\" } }\n}"})
	doc := func(col string) qml.SpecTree {
		return qmlDoc(t, "import app.ui\nFlex {\n Table { id: t\n  Text { text: \""+col+"\" }\n  Text { id: named } }\n}")
	}
	if err := tr.Mount(doc("c1")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	trace := traced(rec)
	if !strings.Contains(trace, "create 4 Box children=[5 6 7]") || !strings.Contains(trace, "create 2 Flex children=[3 4]") {
		t.Fatalf("the use's children are not the Box's, after its own:\n%s", trace)
	}
	if !strings.Contains(trace, "apply 6 text=string(c1)") {
		t.Fatalf("the first column is not node 6:\n%s", trace)
	}
	rec.trace = nil
	if _, err := tr.Reconcile(doc("c2")); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if trace := traced(rec); !strings.Contains(trace, "apply 6 text=string(c2)") || strings.Contains(trace, "create") {
		t.Fatalf("a reload rebuilt the column rather than patching it:\n%s", trace)
	}
}

// TestAParameterPassesThroughANestedComponent: Outer's own property set as
// Inner's; Inner sees the value the document gave Outer. Outer's names do not
// reach into another file: Plain's `label` is the document's, as it would be
// were Plain used anywhere else.
func TestAParameterPassesThroughANestedComponent(t *testing.T) {
	tr, rec, _ := componentTree(t, map[string]string{
		"Inner.qml": "Box {\n required property string caption\n title: caption\n}",
		"Plain.qml": "Text { text: label }",
		"Outer.qml": "Flex {\n property string label: \"outer default\"\n Inner { caption: label }\n Plain { }\n}",
	})
	if err := tr.DeclareSource("label", sv("the document's")); err != nil {
		t.Fatal(err)
	}
	if err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Outer { label: \"from the document\" }\n Outer { }\n}")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	trace := traced(rec)
	for _, want := range []string{"title=string(from the document)", "title=string(outer default)",
		"text=string(the document's)"} {
		if !strings.Contains(trace, want) {
			t.Errorf("trace lacks %q:\n%s", want, trace)
		}
	}
	if strings.Contains(trace, "text=string(outer") || strings.Contains(trace, "text=string(from") {
		t.Errorf("Outer's label reached into Plain's file:\n%s", trace)
	}
}

// TestOnlyAComponentsRootDeclares: a document declares nothing, and neither
// does a use.
func TestOnlyAComponentsRootDeclares(t *testing.T) {
	tr, _, _ := componentTree(t, map[string]string{"Card.qml": "Box { }"})
	for _, doc := range []string{
		"import app.ui\nFlex {\n property string x\n}",
		"import app.ui\nFlex {\n Card { property string x }\n}",
	} {
		err := tr.Mount(qmlDoc(t, doc))
		if !errors.Is(err, decl.ErrComponent) || !strings.Contains(err.Error(), `declares "x", and only a component file's root`) {
			t.Errorf("%q: err = %v, want the declaration refused", doc, err)
		}
	}
}

// TestADeclaredNameShadowsASourceOnlyInsideTheComponent: inside, the
// declaration wins over a host source of the same spelling; outside, the
// source is what it was.
func TestADeclaredNameShadowsASourceOnlyInsideTheComponent(t *testing.T) {
	tr, rec, _ := componentTree(t, map[string]string{"Card.qml": "Box {\n property string label: \"declared\"\n title: label\n}"})
	if err := tr.DeclareSource("label", sv("the host's")); err != nil {
		t.Fatal(err)
	}
	if err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Card { }\n Text { text: label }\n}")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	trace := traced(rec)
	if !strings.Contains(trace, "title=string(declared)") || !strings.Contains(trace, "text=string(the host's)") {
		t.Fatalf("want the declaration inside and the source outside:\n%s", trace)
	}
}

// TestTheRootIdIsTheScopedOne: `me.label` reaches the declaration whether
// the use names the instance (me becomes its id) or not (me@key).
func TestTheRootIdIsTheScopedOne(t *testing.T) {
	_, rec, _ := mountLabelled(t, "import app.ui\nFlex {\n Card { id: named; label: \"n\" }\n Card { label: \"anon\" }\n}")
	trace := traced(rec)
	for _, want := range []string{"apply 3 text=string(n)", "apply 6 text=string(anon)"} {
		if !strings.Contains(trace, want) {
			t.Errorf("trace lacks %q:\n%s", want, trace)
		}
	}
}

// TestAnInsertedValueIsTerminal: what the use site gives means what it means
// in the using document — never substituted again inside the component.
func TestAnInsertedValueIsTerminal(t *testing.T) {
	tr, rec, _ := componentTree(t, map[string]string{"Card.qml": "Box {\n property string label: \"l\"\n" +
		" property string caption: \"c\"\n title: label\n Text { text: caption }\n}"})
	for name, v := range map[string]string{"caption": "the document's caption", "label": "the document's label"} {
		if err := tr.DeclareSource(name, sv(v)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Card { label: caption; caption: label }\n}")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	trace := traced(rec)
	if !strings.Contains(trace, "title=string(the document's caption)") ||
		!strings.Contains(trace, "text=string(the document's label)") {
		t.Fatalf("a use site's value was substituted again inside the component:\n%s", trace)
	}
}

// TestACallValueIsRefusedInAHandler, and is the binding it is as a property's
// whole value.
func TestACallValueIsRefusedInAHandler(t *testing.T) {
	tr, rec, _ := componentTree(t, map[string]string{
		"Card.qml": "Box {\n required property string label\n title: label\n}",
		"Go.qml":   "Box {\n required property string label\n onClicked: got(label)\n}",
	})
	if err := tr.DeclareFunc("upper", func(a []qml.SpecValue) (qml.SpecValue, error) {
		return sv(strings.ToUpper(a[0].Raw)), nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := tr.Inject("got", decl.Handle(func([]qml.SpecValue) error { return nil })); err != nil {
		t.Fatal(err)
	}
	if err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Card { label: upper(\"x\") }\n}")); err != nil {
		t.Fatalf("a call as a property's whole value: %v", err)
	}
	if !strings.Contains(traced(rec), "title=string(X)") {
		t.Fatalf("the call was not bound:\n%s", traced(rec))
	}
	if err := tr.Destroy(); err != nil {
		t.Fatal(err)
	}
	err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Go { label: upper(\"x\") }\n}"))
	if !errors.Is(err, decl.ErrComponent) || !strings.Contains(err.Error(), `"label" is the call upper(…)`) ||
		!strings.Contains(err.Error(), "pass a value, or bind it where it is used") {
		t.Fatalf("err = %v, want the call refused in the handler", err)
	}
}

// TestADefaultAliasIsNotSetAsAValue: its children are written inside the use.
func TestADefaultAliasIsNotSetAsAValue(t *testing.T) {
	tr, _, _ := componentTree(t, map[string]string{"Table.qml": "Flex {\n default property alias cols: list.data\n Box { id: list }\n}"})
	err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Table { cols: 3 }\n}"))
	if !errors.Is(err, decl.ErrComponent) || !strings.Contains(err.Error(), `"cols" is its default alias`) {
		t.Fatalf("err = %v, want the alias refused as a value", err)
	}
}

// TestAValueModuleDeclaresNothing: a theme is data, and a declaration in one
// would otherwise be dropped without a word.
func TestAValueModuleDeclaresNothing(t *testing.T) {
	_, err := decl.ValueModule([]byte("Theme {\n property color accent: \"red\"\n}"))()
	if err == nil || !strings.Contains(err.Error(), "a value module declares no properties: write accent:") {
		t.Fatalf("err = %v, want the declaration refused, naming accent", err)
	}
}

// TestASignalParameterShadowsADeclaredName in its handler, as it shadows an
// injected one: Picker's `chosen` is raised with (path, count). A declared
// name the signal does not have is still substituted, and `me.path` through
// the root id still means the property. A handler on a nested component use
// binds on the type that component expands to, and its parameters shadow too.
func TestASignalParameterShadowsADeclaredName(t *testing.T) {
	fsys := fstest.MapFS{
		"ui/Chooser.qml": {Data: []byte("Picker {\n id: me\n property string path: \"declared\"\n" +
			" property string label: \"lbl\"\n onChosen: pick(path, label, me.path)\n}")},
		"ui/Bare.qml": {Data: []byte("Picker { }")},
		"ui/Outer.qml": {Data: []byte("Flex {\n property string path: \"outer's\"\n" +
			" Bare { id: inner; onChosen: pick(path, count) }\n}")},
	}
	a := paramAdapter{newRecorder()}
	tr := decl.New(a)
	if err := tr.OfferModule("app.ui", "", decl.ComponentFiles(fsys, "ui")); err != nil {
		t.Fatal(err)
	}
	var calls []string
	if err := tr.Inject("pick", decl.Handle(func(args []qml.SpecValue) error {
		var got []string
		for _, v := range args {
			got = append(got, v.Raw)
		}
		calls = append(calls, strings.Join(got, ","))
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := tr.Mount(qmlDoc(t, "import app.ui\nFlex {\n Chooser { id: c }\n Outer { }\n}")); err != nil {
		t.Fatalf("mount: %v", err)
	}
	for _, id := range []string{"c", "inner@/Outer#0"} {
		n, ok := tr.NodeByID(id)
		if !ok {
			t.Fatalf("no node %q", id)
		}
		if err := a.emitters[n]["chosen"](sv("from-signal"), sv("2")); err != nil {
			t.Fatalf("emit %s: %v", id, err)
		}
	}
	if got := strings.Join(calls, " "); got != "from-signal,lbl,declared from-signal,2" {
		t.Fatalf("pick got %q, want the parameter over the property, the rest substituted: "+
			"from-signal,lbl,declared from-signal,2", got)
	}
}
