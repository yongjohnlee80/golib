package qml_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// TestANamedAliasIsASlot: `property alias footer: foot.data` is recorded as a
// slot on foot, beside the default alias, which is not one.
func TestANamedAliasIsASlot(t *testing.T) {
	tree, err := qml.QML{}.Parse([]byte("Dialog {\n default property alias cols: list.data\n property alias footer: foot.data\n" +
		" Box { id: list }\n Box { id: foot }\n}"))
	if err != nil {
		t.Fatal(err)
	}
	d := tree.Root.Decls
	if len(d) != 2 || d[0].Slot || d[0].Target != "list" || !d[1].Slot || d[1].Name != "footer" ||
		d[1].Target != "foot" || d[1].Type != "alias" || d[1].Pos.Line != 3 {
		t.Fatalf("decls = %+v, want the default alias on list and the slot footer on foot", d)
	}
}

// TestASlotIsRefusedWhatIsNotOne: an alias to anything but an object's data
// names the plain property, its whole path included; a required slot is
// refused, since an unset one is empty.
func TestASlotIsRefusedWhatIsNotOne(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"property alias text: field.text", "property <type> text, used where field.text is set"},
		{"property alias size: a.b.c", "used where a.b.c is set"},
		{"property alias rows: list.data.x", "used where list.data.x is set"},
		{"required property alias footer: foot.data", "property alias footer: foot.data without required"},
	} {
		_, err := qml.QML{}.Parse([]byte("Dialog {\n " + c.src + "\n}"))
		var se parse.SyntaxError
		if !errors.As(err, &se) || !strings.Contains(se.Want, c.want) || se.Pos.Line != 2 {
			t.Errorf("%s: err = %v, want a syntax error at line 2 wanting %q", c.src, err, c.want)
		}
	}
}
