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
