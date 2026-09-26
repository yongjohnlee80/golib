package qml

import "testing"

func TestObjectPropertyKeepsItsTemplateSeparateFromChildren(t *testing.T) {
	spec, err := (QML{File: "table.qml"}).Parse([]byte(`TableView {
    model: App.rows
    delegate: Text { text: model.display; color: Theme.warning }
    TableViewColumn { role: "value" }
}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(spec.Root.Props) != 2 || len(spec.Root.Children) != 1 {
		t.Fatalf("object property got lost among children: %#v", spec.Root)
	}
	d := spec.Root.Props[1].Value
	if d.Kind != SpecValueTemplate || d.Template.Type != "Text" || len(d.Template.Props) != 2 || d.Pos.File != "table.qml" {
		t.Fatalf("object property lost its node or source position: %#v", d)
	}
}

func TestTemplateRequiresAClosingBrace(t *testing.T) {
	if _, err := (QML{File: "bad.qml"}).Parse([]byte(`TableView { delegate: Text { text: "hello" `)); err == nil {
		t.Fatal("an incomplete object property was accepted")
	}
}
