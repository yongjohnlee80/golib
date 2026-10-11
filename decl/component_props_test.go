package decl_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
)

// component_props_test.go covers a component's own properties: declared in
// its file, judged when the file loads, and substituted at each use with the
// use site's value or the default. As in component_test.go, every claim about
// a value is read off the adapter's trace — what was BUILT.

// TestAComponentsDeclarationsAreJudgedWhenItLoads: each mistake is the FILE's,
// reported against it whatever uses it.
func TestAComponentsDeclarationsAreJudgedWhenItLoads(t *testing.T) {
	for name, c := range map[string]struct{ src, want string }{
		"a name declared twice": {"Box {\n property string a\n property int a\n}",
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
