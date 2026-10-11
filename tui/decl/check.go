package decl

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse"
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
// same for a program, with the options [NewProgram] takes. Each offered module
// has a CONTEXT — the imports a document using it would have:
//
//	imported      the layout's own imports
//	not imported  the layout's imports, with every offered import it clashes
//	              with replaced by it — or, clashing with none, added to them
//
// Two modules that bring one name into scope can never be imported together —
// the engine refuses the second — so a module sharing a name with an imported
// one can only ever be used INSTEAD of it. That makes it an alternative, and
// switching to it is exactly that edit of the import lines. Check then mounts:
//
//	the layout          as written
//	each alternative    the layout under the alternative's context: the other
//	                    theme, in place of the imported one
//	each component      every component the layout does not use, under its
//	nothing uses        module's context: alone, and inside a Window — sound
//	                    if either accepts it, since nothing says yet where it
//	                    will be used
//
// Every other offered module is loaded, so its own rules still hold. Nothing
// runs: providers subscribe and are released, and no App is built.
//
// An unused component that REQUIRES properties is mounted with a typed zero
// for each — "", 0, false — when they are all strings, numbers and bools. One
// requiring a var, a list, a url or a colour cannot be: no placeholder builds
// as every model, colour or path a use might give. Check EXPANDS it instead,
// each such property a marked placeholder, and passes it unless expansion
// fails. That still judges its declarations, and the substitution refusals —
// a required property unset, a literal of the wrong kind, a call or an object
// where an expression goes — but NOT its property values against the widgets'
// types, nor its handlers against their signals and methods: that is
// mounting, and it happens where the component is used.
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
		imports, replaced := contextFor(o, spec.Imports, offers)
		if len(replaced) > 0 {
			variant := spec
			variant.Imports = imports
			report(fmt.Sprintf("%s with import %s in place of %s", c.layoutFile, o.name,
				strings.Join(replaced, ", ")), mountAndRelease(c, variant))
		}
		for _, name := range sortedKeys(contents.Components) {
			// A component the layout uses was judged by the layout's mount —
			// or, for an alternative, by the variant's just above.
			if used[name] && (isImported(o.name, spec.Imports) || len(replaced) > 0) {
				continue
			}
			label := fmt.Sprintf("component %s of %s, which %s does not use", name, o.name, c.layoutFile)
			if root := contents.Components[name]; !buildable(root) {
				report(label, expandOnly(spec, imports, offers, name, root))
				continue
			}
			report(label, mountAnywhere(c, hostings(c, spec, imports, name, contents.Components[name])))
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

// contextFor is the imports a document using o would have, and the imports o
// replaced to get there. See [Check].
func contextFor(o offered, imports []qml.SpecImport, offers map[string]decl.ModuleContents) ([]qml.SpecImport, []string) {
	if isImported(o.name, imports) {
		return imports, nil
	}
	names := scopeNames(offers[o.name])
	own := qml.SpecImport{Module: o.name, Path: strings.Split(o.name, "."), Version: o.version}
	var out []qml.SpecImport
	var replaced []string
	for _, im := range imports {
		other, isOffer := offers[im.Module]
		if isOffer && shares(names, scopeNames(other)) {
			if len(replaced) == 0 {
				// In the replaced import's place, under its qualifier: a layout
				// writing `T.Theme` still reaches the alternative's Theme.
				own.Alias, own.Pos = im.Alias, im.Pos
				out = append(out, own)
			}
			replaced = append(replaced, im.Module)
			continue
		}
		out = append(out, im)
	}
	if len(replaced) == 0 {
		out = append(out, own)
	}
	return out, replaced
}

func isImported(module string, imports []qml.SpecImport) bool {
	for _, im := range imports {
		if im.Module == module {
			return true
		}
	}
	return false
}

func shares(a, b map[string]bool) bool {
	for n := range b {
		if a[n] {
			return true
		}
	}
	return false
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

// hostType is the root an unused component is also tried in: a Window, where
// a docked bar or a Dialog lives.
const hostType = "Window"

// placement is one document an unused component is tried in.
type placement struct {
	label string
	spec  qml.SpecTree
}

// hostings are the documents an unused component is tried in, under the given
// imports: ALONE, as the root, and — when the vocabulary has one — inside a
// Window. Nothing says where a component will be used, and no one host fits
// every component: `Dock.edge` needs a parent to read it, and a Menu belongs in
// a MenuBar, never directly in a Window. The layout's own root is not reused:
// its construction rules — a Split's two children — are the layout's.
//
// The use is bare but for a typed zero for each REQUIRED property root
// declares: a use without one is refused, and that would be the mount's
// doing, not the component's. Only a buildable component comes here.
func hostings(c programConfig, layout qml.SpecTree, imports []qml.SpecImport, component string,
	root *qml.SpecNode) []placement {
	pos := layout.Root.Pos
	var given []qml.SpecProp
	if root != nil {
		for _, d := range root.Decls {
			if d.Required {
				given = append(given, qml.SpecProp{Name: d.Name, Path: []string{d.Name},
					Value: zeroOf(d.Type, pos), Pos: pos})
			}
		}
	}
	node := func() *qml.SpecNode { return &qml.SpecNode{Type: component, Props: given, Pos: pos} }
	out := []placement{{"alone", qml.SpecTree{Imports: imports, Root: node()}}}
	reg := c.registry
	if reg == nil {
		reg = StdRegistry()
	}
	if _, ok := reg.builders[hostType]; ok {
		out = append(out, placement{"in a " + hostType, qml.SpecTree{Imports: imports,
			Root: &qml.SpecNode{Type: hostType, Pos: pos, Children: []*qml.SpecNode{node()}}}})
	}
	return out
}

// zeroOf is the placeholder a buildable component's required property is
// mounted with: its type's zero.
func zeroOf(typ string, at parse.Position) qml.SpecValue {
	switch typ {
	case "int", "real", "double":
		return qml.SpecValue{Kind: qml.SpecValueNumber, Raw: "0", Pos: at}
	case "bool":
		return qml.SpecValue{Kind: qml.SpecValueBool, Raw: "false", Pos: at}
	}
	return qml.SpecValue{Kind: qml.SpecValueString, Pos: at}
}

// buildable reports whether a component's required properties are all of a
// type whose zero every widget property of that type accepts.
func buildable(root *qml.SpecNode) bool {
	if root == nil {
		return true
	}
	for _, d := range root.Decls {
		switch d.Type {
		case "string", "int", "real", "double", "bool":
		default:
			if d.Required {
				return false
			}
		}
	}
	return true
}

// expandOnly judges an unbuildable component by expanding one use of it, in
// the context of imports, with a MARKED placeholder for each required
// property: a reference spelled as no document can write one, so it can
// stand anywhere a value can and is never mistaken for a name. Scalars get
// their zero, as a mount would give them.
func expandOnly(layout qml.SpecTree, imports []qml.SpecImport, offers map[string]decl.ModuleContents,
	component string, root *qml.SpecNode) error {
	pos := layout.Root.Pos
	use := &qml.SpecNode{Type: component, Pos: pos}
	for _, d := range root.Decls {
		if !d.Required {
			continue
		}
		v := zeroOf(d.Type, pos)
		if !buildable(&qml.SpecNode{Decls: []qml.SpecDecl{d}}) {
			mark := "<required " + d.Type + " " + d.Name + ">"
			v = qml.SpecValue{Kind: qml.SpecValueRef, Raw: mark, Path: []string{mark}, Pos: pos}
		}
		use.Props = append(use.Props, qml.SpecProp{Name: d.Name, Path: []string{d.Name}, Value: v, Pos: pos})
	}
	types := map[string]*qml.SpecNode{component: root}
	for _, im := range imports {
		for name, r := range offers[im.Module].Components {
			if im.Alias != "" {
				name = im.Alias + "." + name
			}
			if _, ok := types[name]; !ok {
				types[name] = r
			}
		}
	}
	_, err := decl.ExpandComponents(use, types)
	return err
}

// mountAnywhere judges a component sound when ANY placement accepts it. When
// none does, every placement's refusal is reported: which one is the
// component's fault is for its author to see, not for Check to guess.
func mountAnywhere(c programConfig, places []placement) error {
	var errs []error
	for _, pl := range places {
		err := mountAndRelease(c, pl.spec)
		if err == nil {
			return nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", pl.label, err))
	}
	return errors.Join(errs...)
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
