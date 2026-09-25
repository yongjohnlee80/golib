package decl

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// CHECK — qmllint for a program: every document it can load, judged before it
// ships.
//
//	func TestTheQMLIsSound(t *testing.T) {
//	    if err := tuidecl.Check(programOptions()...); err != nil {
//	        t.Fatal(err)
//	    }
//	}
//
// [NewProgram] already refuses a broken layout, so a test that builds the
// program catches one. What it cannot catch is a document the layout does not
// load: an offered module is read only when an import names it, so the theme
// the layout does not import and the dialog no screen uses yet are never
// parsed — and break the day someone switches to them.
//
// Qt's qmllint checks each file in the context of its imports. Check does the
// same for a program, with the options [NewProgram] takes, and mounts:
//
//	the layout          as written
//	each alternative    the layout with one import replaced by an offered
//	                    module that brings the same name into scope — the
//	                    other theme, say: switching is one import line, and
//	                    this is that line switched
//	each unused         a component no document of the program uses yet,
//	component           inside the layout's root type, under the layout's
//	                    imports — where its names would resolve once used
//
// and loads every other offered module, so its own rules still hold. Nothing
// runs: providers subscribe and are released, and no App is built.
//
// Every problem is reported, each labelled with what was mounted.
func Check(opts ...ProgramOption) error {
	c, err := configure(opts)
	if err != nil {
		return err
	}
	spec, err := qml.QML{File: c.layoutFile}.Parse(c.layout)
	if err != nil {
		return err
	}
	var errs []error
	report := func(label string, err error) {
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", label, err))
		}
	}
	report(c.layoutFile, mountAndRelease(c, spec))

	imported := map[string]bool{}
	for _, im := range spec.Imports {
		imported[im.Module] = true
	}
	offers := make(map[string]decl.ModuleContents, len(c.offers))
	for _, o := range c.offers {
		contents, err := o.load()
		if err != nil {
			report("module "+o.name, err)
			continue
		}
		offers[o.name] = contents
	}

	used := usedTypes(spec.Root)
	for _, o := range c.offers {
		contents, ok := offers[o.name]
		if !ok {
			continue
		}
		if !imported[o.name] {
			for _, alt := range alternativesTo(o.name, contents, spec, offers) {
				variant := spec
				variant.Imports = append([]qml.SpecImport(nil), spec.Imports...)
				variant.Imports[alt].Module = o.name
				report(fmt.Sprintf("%s with import %s in place of %s", c.layoutFile, o.name,
					spec.Imports[alt].Module), mountAndRelease(c, variant))
			}
			continue
		}
		for _, name := range sortedKeys(contents.Components) {
			if used[name] {
				continue
			}
			report(fmt.Sprintf("component %s of %s, which %s does not use", name, o.name, c.layoutFile),
				mountAndRelease(c, hosting(spec, name)))
		}
	}
	return errors.Join(errs...)
}

// mountAndRelease mounts spec the way NewProgram would, then tears it down.
func mountAndRelease(c programConfig, spec qml.SpecTree) error {
	p, err := mount(c, spec)
	if err != nil {
		return err
	}
	return p.tree.Destroy()
}

// alternativesTo is the imports of spec that module could replace: offered
// modules the document imports that bring one of the same names into scope.
func alternativesTo(module string, contents decl.ModuleContents, spec qml.SpecTree,
	offers map[string]decl.ModuleContents) []int {
	names := scopeNames(contents)
	var out []int
	for i, im := range spec.Imports {
		other, ok := offers[im.Module]
		if !ok || im.Module == module {
			continue
		}
		for n := range scopeNames(other) {
			if names[n] {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

// scopeNames is every name a module brings into scope: its singletons and its
// component types.
func scopeNames(c decl.ModuleContents) map[string]bool {
	out := map[string]bool{}
	for _, e := range c.Exports {
		out[e] = true
	}
	for n := range c.Components {
		out[n] = true
	}
	return out
}

// usedTypes is every type name the document writes, unqualified: `D.QuitDialog`
// counts as QuitDialog.
func usedTypes(root *qml.SpecNode) map[string]bool {
	out := map[string]bool{}
	var walk func(*qml.SpecNode)
	walk = func(sn *qml.SpecNode) {
		if sn == nil {
			return
		}
		t := sn.Type
		if i := strings.LastIndexByte(t, '.'); i >= 0 {
			t = t[i+1:]
		}
		out[t] = true
		for _, ch := range sn.Children {
			walk(ch)
		}
	}
	walk(root)
	return out
}

// hosting is a document that uses one component and nothing else: the
// layout's imports, and its root type holding the component. The root type
// matters — a Dialog opens on its Window — and the root's own properties do
// not, so they are left out.
func hosting(layout qml.SpecTree, component string) qml.SpecTree {
	pos := layout.Root.Pos
	return qml.SpecTree{
		Imports: layout.Imports,
		Root: &qml.SpecNode{
			Type:     layout.Root.Type,
			Pos:      pos,
			Children: []*qml.SpecNode{{Type: component, Pos: pos}},
		},
	}
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
