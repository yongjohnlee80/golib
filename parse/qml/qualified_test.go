package qml_test

import (
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// TestAQualifiedTypeIsANodeAndAGroupIsNot: the LAST segment decides, as in QML.
func TestAQualifiedTypeIsANodeAndAGroupIsNot(t *testing.T) {
	spec, err := qml.QML{}.Parse([]byte("T.Window {\n UI.Card { }\n palette { window: \"x\" }\n}"))
	if err != nil {
		t.Fatal(err)
	}
	if spec.Root.Type != "T.Window" {
		t.Errorf("root type = %q, want T.Window", spec.Root.Type)
	}
	if len(spec.Root.Children) != 1 || spec.Root.Children[0].Type != "UI.Card" {
		t.Errorf("children = %+v, want one UI.Card", spec.Root.Children)
	}
	if len(spec.Root.Props) != 1 || spec.Root.Props[0].Name != "palette.window" {
		t.Errorf("props = %+v, want palette.window", spec.Root.Props)
	}
	if _, err := (qml.QML{}).Parse([]byte("T.window { }")); err == nil {
		t.Error("a lower-case qualified root was accepted as a type")
	}
}

// TestAFileNameReachesEveryPosition, the JavaScript inside included: the
// expression parser reads through the same scanner.
func TestAFileNameReachesEveryPosition(t *testing.T) {
	spec, err := qml.QML{File: "ui/Card.qml"}.Parse([]byte("Box {\n title: a + b\n onClicked: go()\n}"))
	if err != nil {
		t.Fatal(err)
	}
	p := spec.Root.Props[0].Value
	for what, pos := range map[string]string{
		"node": spec.Root.Pos.String(), "value": p.Pos.String(), "expression": p.Expr.Left.Pos.String(),
		"handler": spec.Root.Handlers[0].Pos.String(),
	} {
		if !strings.HasPrefix(pos, "ui/Card.qml:") {
			t.Errorf("the %s is at %q, want it in ui/Card.qml", what, pos)
		}
	}
	_, err = qml.QML{File: "ui/Card.qml"}.Parse([]byte("Box {"))
	if err == nil || !strings.Contains(err.Error(), "ui/Card.qml:1:5") {
		t.Errorf("a syntax error = %v, want it placed in ui/Card.qml", err)
	}
}
