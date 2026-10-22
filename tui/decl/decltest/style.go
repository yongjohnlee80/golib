package decltest

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/tui"
	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// CheckStyle fails t for each type of s that a document could tell from the standard type it
// replaces: a type that is not standard or not Replaceable, and any difference in the QML API
// (constructor properties, setters, methods, getters, signals and their parameters, enums).
// A style that passes takes every document the standard vocabulary takes.
//
// It also mounts each type s replaces, in that type's minimal declaration (minimals) under s, in a
// Window whose palette is not empty, and fails t for each hook that refuses or panics on the widget
// the style's Build makes: a getter read (ReadProperty), Restyle as the palette reaches it, and
// Destroyed as the tree goes. A style that kept a standard hook copied from StandardType while its
// Build makes another widget fails here, at the type and the hook. Setters and methods are not
// called: they need values, and they refuse a foreign widget as an error a document sees.
func CheckStyle(t testing.TB, s tuidecl.Style) {
	t.Helper()
	for _, typ := range s.Types {
		std, ok := tuidecl.StandardContract(typ.Name)
		if !ok {
			t.Errorf("style %q: %q is not a standard type", s.Name, typ.Name)
			continue
		}
		if !tuidecl.Replaceable(typ.Name) {
			t.Errorf("style %q: %q cannot be replaced by a style", s.Name, typ.Name)
			continue
		}
		for _, d := range contractDiff(std, typ.Contract()) {
			t.Errorf("style %q, %s: %s", s.Name, typ.Name, d)
		}
		checkHooks(t, s, typ)
	}
}

// checkHooks mounts typ's minimal declaration under s, reads each of its getters, and destroys the
// tree, failing t for each refusal or panic.
func checkHooks(t testing.TB, s tuidecl.Style, typ tuidecl.Type) {
	t.Helper()
	m, ok := minimals[typ.Name]
	if !ok {
		t.Errorf("style %q, %s: decltest has no minimal declaration of it to mount", s.Name, typ.Name)
		return
	}
	stage := "mounting it"
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("style %q, %s: %s panicked (its Build, Restyle or Destroyed on the style's widget): %v", s.Name, typ.Name, stage, r)
		}
	}()
	p, err := mountMinimal(typ.Name, m, tuidecl.WithStyle(s))
	if err != nil {
		t.Errorf("style %q, %s: its minimal declaration does not mount under the style: %v", s.Name, typ.Name, err)
		return
	}
	node, ok := p.Tree().NodeByID("subject")
	if !ok {
		t.Errorf("style %q, %s: the minimal declaration's subject is gone", s.Name, typ.Name)
		return
	}
	stage = "reading its getters"
	for _, g := range typ.Contract().Getters {
		if _, err := p.Adapter().ReadProperty(node, g); err != nil {
			t.Errorf("style %q, %s: getter %s refuses the style's widget: %v", s.Name, typ.Name, g, err)
		}
	}
	stage = "destroying it"
	if err := p.Tree().Destroy(); err != nil {
		t.Errorf("style %q, %s: destroying it: %v", s.Name, typ.Name, err)
	}
}

// mountMinimal builds a Program of typ's minimal declaration, with extra options (a style), on a
// test backend it never runs.
func mountMinimal(typ string, m minimal, extra ...tuidecl.ProgramOption) (*tuidecl.Program, error) {
	opts := []tuidecl.ProgramOption{
		tuidecl.LayoutSource("minimal.qml", minimalDocument(typ, m)),
		tuidecl.AppOptions(tui.WithBackend(tui.NewTestBackend(60, 20))),
	}
	if m.opts != nil {
		opts = append(opts, m.opts()...)
	}
	return tuidecl.NewProgram(append(opts, extra...)...)
}

// contractDiff says how got differs from want, one line per difference.
func contractDiff(want, got tuidecl.Contract) []string {
	var out []string
	list := func(what string, w, g []string) {
		if missing := minus(w, g); len(missing) > 0 {
			out = append(out, fmt.Sprintf("%s missing: %s", what, strings.Join(missing, ", ")))
		}
		if extra := minus(g, w); len(extra) > 0 {
			out = append(out, fmt.Sprintf("%s not in the standard type: %s", what, strings.Join(extra, ", ")))
		}
	}
	list("constructor properties", want.Ctor, got.Ctor)
	list("setters", want.Setters, got.Setters)
	list("methods", want.Methods, got.Methods)
	list("getters", want.Getters, got.Getters)
	list("enums", want.Enums, got.Enums)
	var ws, gs []string
	for name, params := range want.Signals {
		ws = append(ws, name+"("+strings.Join(params, ", ")+")")
	}
	for name, params := range got.Signals {
		gs = append(gs, name+"("+strings.Join(params, ", ")+")")
	}
	list("signals", ws, gs)
	return out
}

// minus is the strings of a not in b, sorted.
func minus(a, b []string) []string {
	var out []string
	for _, s := range a {
		if !slices.Contains(b, s) {
			out = append(out, s)
		}
	}
	slices.Sort(out)
	return out
}
