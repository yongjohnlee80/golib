package decl_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// focus_nominee_test.go: `focus: true` gives the keyboard to the view it is
// written on when the screen starts — INTO a view whose focusable part is the
// widget inside it, as Qt gives an Item with focus the focus of its scope.

func TestFocusTrueOnAViewGivesItTheKeyboard(t *testing.T) {
	for name, view := range map[string]string{
		"ListView":  `ListView { focus: true; model: App.people; textRole: "name"; onActivated: App.use(index) }`,
		"TableView": `TableView { focus: true; model: App.people; onActivated: App.use(index) }`,
	} {
		m, rec := people(), &recorder{}
		m.SetColumns(tuidecl.Column{Role: "name", Title: "NAME"})
		s := decltest.Run(t, 30, 6,
			tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nWindow {\n Button { text: \"first\" }\n "+view+" }")),
			tuidecl.Singleton("demo", "1.0", "App"),
			tuidecl.Sources(map[string]any{"App.people": m}),
			tuidecl.Handlers(map[string]decl.HandlerFunc{"App.use": rec.handler}))
		s.WaitForText(t, "bob")
		// No Tab: the view has the keyboard from the start, though a Button
		// comes before it in the document.
		s.Keys(t, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyDown}, tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
		s.WaitFor(t, name+" activated", func(string) bool { return len(rec.all()) == 1 })
		if got := rec.all()[0].Raw; got != "1" {
			t.Errorf("%s: activated(%s), want its second row, 1", name, got)
		}
	}
}
