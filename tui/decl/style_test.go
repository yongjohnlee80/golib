package decl_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/decl/decltest"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// recTB records CheckStyle's errors instead of failing the test.
type recTB struct {
	testing.TB
	errs []string
}

func (r *recTB) Helper()                   {}
func (r *recTB) Errorf(f string, a ...any) { r.errs = append(r.errs, fmt.Sprintf(f, a...)) }
func (r *recTB) has(sub string) bool       { return strings.Contains(strings.Join(r.errs, "\n"), sub) }

func editorCopy(t *testing.T) tuidecl.Type {
	t.Helper()
	e, ok := tuidecl.StandardType("Editor")
	if !ok {
		t.Fatal("no standard Editor")
	}
	return e
}

func TestCheckStylePassesACopyAndNamesADifference(t *testing.T) {
	decltest.CheckStyle(t, tuidecl.Style{Name: "copy", Types: []tuidecl.Type{editorCopy(t)}})

	missing := editorCopy(t)
	delete(missing.Setters, "readOnly")
	extra := editorCopy(t)
	extra.Setters["shiny"] = extra.Setters["wrap"]
	extra.Signals = map[string][]string{"sparkled": {"how"}}
	for _, c := range []struct {
		typ  tuidecl.Type
		want string
	}{
		{missing, "setters missing: readOnly"},
		{extra, "setters not in the standard type: shiny"},
		{extra, "signals not in the standard type: sparkled(how)"},
	} {
		r := &recTB{}
		decltest.CheckStyle(r, tuidecl.Style{Name: "odd", Types: []tuidecl.Type{c.typ}})
		if !r.has(c.want) {
			t.Errorf("CheckStyle did not say %q; said %q", c.want, r.errs)
		}
	}
	r := &recTB{}
	decltest.CheckStyle(r, tuidecl.Style{Name: "odd", Types: []tuidecl.Type{{Name: "Flex"}, {Name: "Gizmo"}}})
	if !r.has("cannot be replaced") || !r.has("not a standard type") {
		t.Errorf("CheckStyle on Flex and Gizmo said %q", r.errs)
	}
}

func TestWithStyleRefuses(t *testing.T) {
	src := tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nText { text: \"x\" }"))
	build := func(tuidecl.Build) (tui.Component, []string, error) { return widget.NewText("styled"), nil, nil }
	for _, c := range []struct {
		style tuidecl.Style
		want  error
	}{
		{tuidecl.Style{Name: "s", Types: []tuidecl.Type{{Name: "Gizmo", Build: build}}}, tuidecl.ErrUnknownStyleType},
		{tuidecl.Style{Name: "s", Types: []tuidecl.Type{{Name: "Flex", Build: build}}}, tuidecl.ErrStyleTypeNotReplaceable},
	} {
		if _, err := tuidecl.NewProgram(src, tuidecl.WithStyle(c.style)); !errors.Is(err, c.want) {
			t.Errorf("style %v: %v, want %v", c.style.Types[0].Name, err, c.want)
		}
	}
	if _, err := tuidecl.NewProgram(src, tuidecl.WithStyle(tuidecl.Style{Name: "s"}), tuidecl.WithRegistry(tuidecl.StdRegistry())); err == nil {
		t.Error("WithStyle and WithRegistry together were accepted")
	}
	if tuidecl.Replaceable("Flex") || !tuidecl.Replaceable("Editor") || tuidecl.Replaceable("Gizmo") {
		t.Errorf("Replaceable: Flex %v, Editor %v, Gizmo %v", tuidecl.Replaceable("Flex"), tuidecl.Replaceable("Editor"), tuidecl.Replaceable("Gizmo"))
	}
}

// A document writing Editor gets the style's Editor; one without the style, the standard one.
func TestAStylesEditorIsTheDocumentsEditor(t *testing.T) {
	styled := editorCopy(t)
	styled.Build = func(tuidecl.Build) (tui.Component, []string, error) {
		return widget.NewText("the styled editor"), []string{"text", "wrap"}, nil
	}
	styled.Setters = map[string]tuidecl.Setter{}
	for name := range editorCopy(t).Setters {
		styled.Setters[name] = func(tui.Component, qml.SpecValue) error { return nil }
	}
	doc := tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow { Editor { readOnly: true } }"))
	s := decltest.Run(t, 40, 6, doc, tuidecl.WithStyle(tuidecl.Style{Name: "test", Types: []tuidecl.Type{styled}}))
	s.WaitForText(t, "the styled editor")
}

// A type another part of the vocabulary reads by its concrete type cannot be replaced, even by a
// contract-equal copy: the copy would pass CheckStyle, and then an Editor would refuse its
// SyntaxHighlighter child, a TabView its Tab, a Dialog its shortcuts and button box, a Menu its
// rows, a TableView its columns, a DialogButtonBox its Buttons, a FocusScope or a Drawer would hold
// keys no check sees, and TerminalTabsOf would not find its tabs.
func TestATypeReadPrivatelyIsNotReplaceable(t *testing.T) {
	src := tuidecl.LayoutSource("main.qml", []byte("import tui 1.0\nWindow { Editor { SyntaxHighlighter { definition: \"QML\" } } }"))
	for _, name := range []string{"SyntaxHighlighter", "Tab", "Shortcut", "DialogButtonBox", "Dialog", "FileDialog", "FolderDialog", "TableView", "Window", "Menu", "MenuItem", "MenuSeparator", "MenuBar", "TableViewColumn",
		"Button", "FocusScope", "Drawer", "TerminalTabs"} {
		copied, ok := tuidecl.StandardType(name)
		if !ok {
			t.Errorf("%s is not a standard type", name)
			continue
		}
		if tuidecl.Replaceable(name) {
			t.Errorf("Replaceable(%q) = true", name)
		}
		_, err := tuidecl.NewProgram(src, tuidecl.WithStyle(tuidecl.Style{Name: "copy", Types: []tuidecl.Type{copied}}))
		if !errors.Is(err, tuidecl.ErrStyleTypeNotReplaceable) {
			t.Errorf("a style replacing %s with a copy: %v, want ErrStyleTypeNotReplaceable", name, err)
		}
	}
	// the Editor itself stays replaceable, and the document runs with its highlighter
	editor, _ := tuidecl.StandardType("Editor")
	if _, err := tuidecl.NewProgram(src, tuidecl.WithStyle(tuidecl.Style{Name: "copy", Types: []tuidecl.Type{editor}}),
		tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(40, 10)))); err != nil {
		t.Errorf("a style replacing Editor with a copy: %v", err)
	}
}

// WindowMod stays replaceable: what reads it, a WindowButtons inside it, finds its window through the
// WindowBehavior capability, which the standard WindowMod has and a style's widget can.
func TestAWindowModIsReachedThroughItsBehavior(t *testing.T) {
	if !tuidecl.Replaceable("WindowMod") {
		t.Error(`Replaceable("WindowMod") = false`)
	}
	var mod tui.Component = widget.NewWindowMod(widget.NewText("x"))
	b, ok := mod.(interface{ WindowBehavior() *widget.WindowCore })
	if !ok {
		t.Fatal("a WindowMod has no WindowBehavior")
	}
	if b.WindowBehavior() != mod.(*widget.WindowMod).Core() {
		t.Error("a WindowMod's WindowBehavior is not its Core")
	}
}

// A style that keeps a standard getter, copied from StandardType, while its Build makes another
// widget fails CheckStyle at the type and the getter: the getter refuses the style's widget, which
// a document would meet only reading it. Before, CheckStyle compared contracts and passed it.
func TestCheckStyleNamesACopiedGetterThatRefusesTheStylesWidget(t *testing.T) {
	list, ok := tuidecl.StandardType("ListView")
	if !ok {
		t.Fatal("no standard ListView")
	}
	list.Build = func(tuidecl.Build) (tui.Component, []string, error) { return widget.NewText("list"), nil, nil }
	r := &recTB{}
	decltest.CheckStyle(r, tuidecl.Style{Name: "odd", Types: []tuidecl.Type{list}})
	if !r.has(`style "odd", ListView: getter currentIndex refuses the style's widget`) {
		t.Errorf("CheckStyle did not name the copied getter; said %q", r.errs)
	}
}
