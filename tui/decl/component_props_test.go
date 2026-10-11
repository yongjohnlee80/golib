package decl_test

import (
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/controls"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
)

// component_props_test.go: a component's own properties on a running program
// — a parametric Dialog whose handler runs with the value its use gave — and
// Check judging a component that requires them though nothing uses it yet.

// picker is a Picker-like Dialog: what differs between its uses is declared,
// the use site's columns go into its table, and its handlers call App with a
// declared name.
const picker = `Dialog {
    id: picker
    required property string name
    required property var model
    property string label: "Choose"
    property int widthPercent: 80
    required property bool wrapped
    required property color accent
    required property url place
    default property alias columns: list.data
    title: label
    maxWidthPercent: widthPercent
    Flex {
        direction: Tui.Vertical
        Text { text: picker.label; palette.windowText: accent }
        TextField { placeholderText: place }
        CheckBox { text: "wrap"; checked: wrapped }
        TableView { id: list; model: model; onActivated: App.picked(name, index) }
    }
    onAccepted: App.picked(picker.name, list.currentIndex)
}`

// checkPicker runs Check on a layout and a ui/ directory holding picker as
// Picker.qml, with extra files beside it.
func checkPicker(t *testing.T, layoutBody string, extra map[string]string) error {
	t.Helper()
	files := fstest.MapFS{
		"main.qml": {Data: []byte("import tui 1.0\nimport demo 1.0\nimport demo.ui 1.0\n" +
			"Window {\n Text { text: \"a\" }\n" + layoutBody + "\n}")},
		"ui/Picker.qml": {Data: []byte(picker)},
	}
	for name, src := range extra {
		files[name] = &fstest.MapFile{Data: []byte(src)}
	}
	return tuidecl.Check(
		tuidecl.Layout(files, "main.qml"),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": tableRows(2)}),
		tuidecl.Components(files, "ui", "demo.ui", "1.0"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.picked": func([]qml.SpecValue) error { return nil }}))
}

// TestCheckExpandsAnUnusedComponentItCannotMount: a Picker requiring a model,
// a colour and a url cannot be mounted bare — no placeholder builds as every
// model a use might give — so Check expands it with marked placeholders and
// passes it, unless expansion itself fails.
func TestCheckExpandsAnUnusedComponentItCannotMount(t *testing.T) {
	if err := checkPicker(t, "", nil); err != nil {
		t.Fatalf("Check refused an unused Picker requiring a model: %v", err)
	}
	// An expansion error is still found: a call where an expression goes.
	broken := strings.Replace(picker, `property string label: "Choose"`,
		"property string label: App.hint()", 1)
	broken = strings.Replace(broken, "onAccepted: App.picked(picker.name, list.currentIndex)",
		"onAccepted: App.picked(label, list.currentIndex)", 1)
	err := checkPicker(t, "", map[string]string{"ui/Picker.qml": broken})
	if err == nil || !strings.Contains(err.Error(), "component Picker of demo.ui") ||
		!strings.Contains(err.Error(), `"label" is the call App.hint(…)`) {
		t.Fatalf("Check passed an unused Picker whose expansion fails: %v", err)
	}
	// What expansion cannot judge — a property the widget lacks — waits for a use.
	misspelt := strings.Replace(picker, "checked: wrapped", "chekked: wrapped", 1)
	if err := checkPicker(t, "", map[string]string{"ui/Picker.qml": misspelt}); err != nil {
		t.Fatalf("expansion-only checking judged a widget property: %v", err)
	}
	// Used, it is mounted with the use's values: a model that is no model fails
	// there, placed at the use.
	err = checkPicker(t, ` Picker { name: "files"; model: "rows"; wrapped: true; accent: "red"; place: "" }`, nil)
	if err == nil || !strings.Contains(err.Error(), "want a model") || !strings.Contains(err.Error(), "main.qml:6:") {
		t.Fatalf("a used Picker given no model: %v", err)
	}
}

// TestCheckMountsAnUnusedComponentRequiringOnlyScalars with a typed zero for
// each, so its file is judged as a mount judges it.
func TestCheckMountsAnUnusedComponentRequiringOnlyScalars(t *testing.T) {
	const field = "Flex {\n required property string name\n required property int width0\n" +
		" required property bool on\n Text { text: name }\n CheckBox { text: \"x\"; checked: on }\n}"
	if err := checkPicker(t, "", map[string]string{"ui/Field.qml": field}); err != nil {
		t.Fatalf("Check refused an unused component requiring scalars: %v", err)
	}
	err := checkPicker(t, "", map[string]string{"ui/Field.qml": strings.Replace(field, "checked: on", "chekked: on", 1)})
	if err == nil || !strings.Contains(err.Error(), "component Field of demo.ui") || !strings.Contains(err.Error(), "chekked") {
		t.Fatalf("Check passed a broken component requiring scalars: %v", err)
	}
}

// ask is a parametric Dialog: what it asks, and the name its answer is filed
// under, come from its use.
const ask = `Dialog {
    id: ask
    required property string name
    property string label: "Choose"
    title: label
    standardButtons: Dialog.Ok
    defaultButton: Dialog.Ok
    Flex { direction: Tui.Vertical
        Text { text: ask.label }
        TextField { } }
    onAccepted: App.picked(name, label)
}`

// TestAParametricDialogRunsItsHandlerWithTheUsesValue: two uses of one
// Dialog, each answering with its own values — the default where it set none.
func TestAParametricDialogRunsItsHandlerWithTheUsesValue(t *testing.T) {
	var mu sync.Mutex
	var answers []string
	got := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), answers...)
	}
	s := decltest.Run(t, 40, 12,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nimport demo.ui 1.0\n"+
			"Window {\n Text { text: \"under\" }\n Ask { id: files; name: \"files\"; label: \"Open a file\" }\n"+
			" Ask { id: heads; name: \"headings\" } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Components(fstest.MapFS{"ui/Ask.qml": {Data: []byte(ask)}}, "ui", "demo.ui", "1.0"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.picked": func(args []qml.SpecValue) error {
			mu.Lock()
			defer mu.Unlock()
			answers = append(answers, args[0].Raw+": "+args[1].Raw)
			return nil
		}}))
	s.WaitForText(t, "under")
	for i, c := range []struct{ id, title, want string }{
		{"files", "Open a file", "files: Open a file"},
		{"heads", "Choose", "headings: Choose"},
	} {
		onScreenLoop(t, s, func() {
			if err := s.Program.Call(c.id, "open"); err != nil {
				t.Error(err)
			}
		})
		s.WaitForText(t, "┌ "+c.title+" ")
		s.Keys(t, decltest.Rune('x'), tui.KeyEvent{Kind: tui.KeyPress, Code: tui.KeyEnter})
		s.WaitFor(t, c.want, func(string) bool { return len(got()) == i+1 })
		if a := got()[i]; a != c.want {
			t.Errorf("%s answered %q, want %q", c.id, a, c.want)
		}
	}
}

// TestAnEditedComponentWithDeclarationsReachesTheScreen: a hot reload of the
// component file re-expands each use with its own value, and an edit that
// leaves a use without a required value is refused, keeping the screen.
func TestAnEditedComponentWithDeclarationsReachesTheScreen(t *testing.T) {
	d := newHotDir(t, hotLayout("Card { caption: \"first use\" }\nCard { }"))
	d.write("ui/Card.qml", "Text {\n property string caption: \"default v1\"\n text: caption\n}")
	s := d.run()
	s.WaitFor(t, "both uses", func(sc string) bool {
		return strings.Contains(sc, "first use") && strings.Contains(sc, "default v1")
	})
	d.write("ui/Card.qml", "Text {\n property string caption: \"default v2\"\n text: caption\n}")
	s.WaitFor(t, "the new default", func(sc string) bool {
		return strings.Contains(sc, "first use") && strings.Contains(sc, "default v2")
	})
	_, refused := d.counts()
	d.write("ui/Card.qml", "Text {\n required property string caption\n text: caption\n}")
	s.WaitFor(t, "the refusal", func(string) bool { _, r := d.counts(); return r > refused })
	if sc := s.String(); !strings.Contains(sc, "default v2") {
		t.Fatalf("a refused component edit changed the screen:\n%s", sc)
	}
	d.mu.Lock()
	last := d.refusals[len(d.refusals)-1]
	d.mu.Unlock()
	if !strings.Contains(last.Error(), `required property "caption" is not set`) {
		t.Errorf("refused with %v, want the unset caption named", last)
	}
}

// TestAPickersColumnsLandInItsTable: the use site's TableViewColumns are its
// default alias's — the table's — so its rows show through them.
func TestAPickersColumnsLandInItsTable(t *testing.T) {
	s := decltest.Run(t, 60, 16,
		tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nimport demo 1.0\nimport demo.ui 1.0\n"+
			"Window {\n Text { text: \"under\" }\n"+
			" Picker { id: p; name: \"files\"; label: \"Files\"; model: App.rows; wrapped: true\n"+
			"  accent: \"red\"; place: \"folder\"\n"+
			"  TableViewColumn { role: \"name\"; title: \"FILECOL\" } } }")),
		tuidecl.Singleton("demo", "1.0", "App"),
		tuidecl.Sources(map[string]any{"App.rows": tableRows(3)}),
		tuidecl.Components(fstest.MapFS{"ui/Picker.qml": {Data: []byte(picker)}}, "ui", "demo.ui", "1.0"),
		tuidecl.Types(controls.Types()...),
		tuidecl.Handlers(map[string]decl.HandlerFunc{"App.picked": func([]qml.SpecValue) error { return nil }}))
	s.WaitForText(t, "under")
	onScreenLoop(t, s, func() {
		if err := s.Program.Call("p", "open"); err != nil {
			t.Error(err)
		}
	})
	s.WaitFor(t, "the table's column and rows", func(sc string) bool {
		return strings.Contains(sc, "┌ Files ") && strings.Contains(sc, "FILECOL") && strings.Contains(sc, "row01")
	})
}
