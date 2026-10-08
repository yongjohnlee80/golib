package decltest

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tuidecl "github.com/yongjohnlee80/golib/tui/decl"
)

// CheckStyle fails t for each type of s that a document could tell from the standard type it
// replaces: a type that is not standard or not Replaceable, and any difference in the QML API
// (constructor properties, setters, methods, getters, signals and their parameters, enums).
// A style that passes takes every document the standard vocabulary takes.
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
	}
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
