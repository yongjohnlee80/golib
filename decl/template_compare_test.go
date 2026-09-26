package decl

import (
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"
)

func templateProp(t *testing.T, source string) qml.SpecValue {
	t.Helper()
	spec, err := (qml.QML{File: "view.qml"}).Parse([]byte(source))
	if err != nil {
		t.Fatal(err)
	}
	return spec.Root.Props[0].Value
}

func TestObjectTemplateReloadComparisonIgnoresPositionsButNotMeaning(t *testing.T) {
	first := templateProp(t, `TableView { delegate: Text {
    text: model.display
    color: model.state === "raised" ? Theme.alert : Theme.normal
} }`)
	shifted := templateProp(t, `TableView {
    // moving source lines must not rebuild a live table
    delegate: Text { text: model.display; color: model.state === "raised" ? Theme.alert : Theme.normal }
}`)
	if !sameValue(first, shifted) {
		t.Fatal("position-only edit rebuilt the delegate")
	}
	for label, source := range map[string]string{
		"color branch": `TableView { delegate: Text { text: model.display; color: model.state === "raised" ? Theme.other : Theme.normal } }`,
		"other branch": `TableView { delegate: Text { text: model.display; color: model.state === "raised" ? Theme.alert : Theme.other } }`,
		"display":      `TableView { delegate: Text { text: model.other; color: model.state === "raised" ? Theme.alert : Theme.normal } }`,
		"type":         `TableView { delegate: Button { text: model.display; color: model.state === "raised" ? Theme.alert : Theme.normal } }`,
		"id":           `TableView { delegate: Text { id: newIdentity; text: model.display; color: model.state === "raised" ? Theme.alert : Theme.normal } }`,
		"handler":      `TableView { delegate: Text { text: model.display; color: model.state === "raised" ? Theme.alert : Theme.normal; onClicked: App.run() } }`,
		"child":        `TableView { delegate: Text { text: model.display; color: model.state === "raised" ? Theme.alert : Theme.normal; Text { text: "x" } } }`,
	} {
		t.Run(label, func(t *testing.T) {
			if sameValue(first, templateProp(t, source)) {
				t.Fatal("changed object property was treated as unchanged")
			}
		})
	}
}

func TestExpressionReloadComparisonIncludesNestedArgumentsAndLiterals(t *testing.T) {
	for _, pair := range [][2]string{
		{`Text { text: value("old") }`, `Text { text: value("new") }`},
		{"Text { text: `old${name}` }", "Text { text: `new${name}` }"},
		{`Text { text: ({name: "old"}) }`, `Text { text: ({name: "new"}) }`},
		{`Text { text: state === "raised" ? "red" : "plain" }`, `Text { text: state === "normal" ? "red" : "plain" }`},
	} {
		oldValue := templateProp(t, pair[0])
		newValue := templateProp(t, pair[1])
		if sameValue(oldValue, newValue) {
			t.Errorf("reload missed nested expression edit: %s -> %s", pair[0], pair[1])
		}
	}
}
