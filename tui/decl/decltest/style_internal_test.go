package decltest

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
	"github.com/yongjohnlee80/golib/tui/widget"
)

// recordingTB records CheckStyle's failures instead of failing the test.
type recordingTB struct {
	testing.TB
	errs []string
}

func (r *recordingTB) Helper()                   {}
func (r *recordingTB) Errorf(f string, a ...any) { r.errs = append(r.errs, fmt.Sprintf(f, a...)) }

// CheckStyle's hook step names each way a style's replacement fails: no minimal declaration to
// mount it in, a Build that refuses or panics, a getter that refuses the style's widget, and a
// Destroyed that panics. Each failure names the style, the type and the step.
func TestCheckStyleNamesEachWayAReplacementFails(t *testing.T) {
	copyOf := func(name string) tuidecl.Type {
		typ, ok := tuidecl.StandardType(name)
		if !ok {
			t.Fatalf("no standard %s", name)
		}
		return typ
	}
	text := func(tuidecl.Build) (tui.Component, []string, error) { return widget.NewText("styled"), nil, nil }

	refusing := copyOf("ListView")
	refusing.Build = func(tuidecl.Build) (tui.Component, []string, error) { return nil, nil, errors.New("no list") }
	panicking := copyOf("ListView")
	panicking.Build = func(tuidecl.Build) (tui.Component, []string, error) { panic("build") }
	foreign := copyOf("ListView")
	foreign.Build = text
	dying := copyOf("Text")
	dying.Build, dying.Destroyed = text, func(tui.Component) { panic("destroyed") }
	for _, c := range []struct {
		typ  tuidecl.Type
		want string
	}{
		{refusing, `style "odd", ListView: its minimal declaration does not mount under the style`},
		{panicking, `style "odd", ListView: mounting it panicked`},
		{foreign, `style "odd", ListView: getter currentIndex refuses the style's widget`},
		{dying, `style "odd", Text: destroying it`},
	} {
		r := &recordingTB{}
		CheckStyle(r, tuidecl.Style{Name: "odd", Types: []tuidecl.Type{c.typ}})
		if !strings.Contains(strings.Join(r.errs, "\n"), c.want) {
			t.Errorf("want %q; CheckStyle said %q", c.want, r.errs)
		}
	}

	missing := copyOf("Text")
	delete(missing.Setters, "text")
	for _, c := range []struct {
		typ  tuidecl.Type
		want string
	}{
		{tuidecl.Type{Name: "Gizmo"}, `style "odd": "Gizmo" is not a standard type`},
		{tuidecl.Type{Name: "Flex"}, `style "odd": "Flex" cannot be replaced by a style`},
		{missing, `style "odd", Text: setters missing: text`},
	} {
		r := &recordingTB{}
		CheckStyle(r, tuidecl.Style{Name: "odd", Types: []tuidecl.Type{c.typ}})
		if !strings.Contains(strings.Join(r.errs, "\n"), c.want) {
			t.Errorf("want %q; CheckStyle said %q", c.want, r.errs)
		}
	}

	kept := minimals["Text"]
	defer func() { minimals["Text"] = kept }()
	for _, c := range []struct {
		entry *minimal // nil: no entry
		want  string
	}{
		{nil, `style "odd", Text: decltest has no minimal declaration of it to mount`},
		{&minimal{body: `Text { text: "x" }`}, `style "odd", Text: the minimal declaration's subject is gone`},
		{&minimal{body: `Text { id: subject; text: "x" }`, opts: func() []tuidecl.ProgramOption {
			return []tuidecl.ProgramOption{tuidecl.Providers(failingCancel{})}
		}}, `style "odd", Text: destroying it: `},
	} {
		delete(minimals, "Text")
		if c.entry != nil {
			minimals["Text"] = *c.entry
		}
		r := &recordingTB{}
		CheckStyle(r, tuidecl.Style{Name: "odd", Types: []tuidecl.Type{copyOf("Text")}})
		if !strings.Contains(strings.Join(r.errs, "\n"), c.want) {
			t.Errorf("want %q; CheckStyle said %q", c.want, r.errs)
		}
	}
}

// failingCancel is a provider whose subscription fails to cancel, so the tree's Destroy errs.
type failingCancel struct{}

func (failingCancel) Subscribe(fn func(decl.Update)) ([]string, func() error, error) {
	fn(decl.Update{Version: 1, Values: map[string]qml.SpecValue{"App.x": {Kind: qml.SpecValueString, Raw: "x"}}})
	return []string{"App.x"}, func() error { return errors.New("cancel failed") }, nil
}
