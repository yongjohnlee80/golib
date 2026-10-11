package decl

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// COMPONENTS — a screen made of several QML files.
//
//	// dialogs/QuitDialog.qml
//	Dialog {
//	    title: "Quit"
//	    standardButtons: Dialog.Yes | Dialog.No
//	    onAccepted: App.quit()
//	    Text { text: App.quitQuestion }
//	}
//
//	// editor.qml
//	import editor.dialogs 1.0
//	Window { QuitDialog { id: quitDialog } }
//
// A QML file defines a TYPE named for the file, and a document that imports
// the module the file belongs to may use it like any other. A module carries
// its component types in [ModuleContents.Components], so they arrive the way
// its values do: when an import names the module, and not before.
//
// A use is EXPANDED into a copy of the component's root before the document
// is judged, with Qt's rules for what the use site adds:
//
//	properties   the use site's replace the component's of the same name;
//	             a declared one is substituted instead (below)
//	handlers     both run, the component's first
//	children     the use site's follow the component's, under its
//	             default alias's object when it declares one
//	id           the use site's names the instance; ids inside are the
//	             component's own, one set per use (component_ids.go)
//
// NAMES RESOLVE IN THE USING DOCUMENT, as they do in one of Qt's inline
// components, so a component file imports nothing. That is not a shortcut: a
// component that imported a theme would pin it, and switching theme would stop
// being one line of the layout.
//
// A component's root may DECLARE PROPERTIES of its own — `property string
// label: "Find"`, `required property var model`, `default property alias
// columns: list.data` — which a use sets by name. They are parameters of the
// macro, not properties of anything at run time: substitute (component_props.go)
// replaces each reference to one inside the copy with the use site's value, or
// the default, before the copy's nested uses expand — but not a signal's
// parameter of the same name in its handler; merge leaves them off the root,
// and sends the use site's children to the default alias's object when there
// is one. A file's declarations are judged when it loads (checkDecls).

// ErrComponent reports a component type that cannot be used as written: a file
// the rules refuse, a name taken twice, a component that contains itself.
var ErrComponent = errors.New("decl: this component cannot be used")

// Vocabulary is an OPTIONAL capability an [Adapter] may implement to list the
// type names it builds, so a component named like one is refused rather than
// silently replacing it.
type Vocabulary interface {
	TypeNames() []string
}

// component is one loaded component type.
type component struct {
	module string
	root   *qml.SpecNode
}

// maxComponentDepth bounds how deeply components may nest in one another. A
// component that contains itself is caught by name before this; the bound is
// for a chain long enough to be a mistake nobody meant.
const maxComponentDepth = 32

// ComponentFiles returns a loader for a module of component types: every
// `.qml` file in dir of fsys, each a type named for its file.
//
// The files are read and parsed when the loader RUNS — when a document first
// imports the module — so a module of forty dialogs costs nothing until
// something imports it.
func ComponentFiles(fsys fs.FS, dir string) ModuleLoader {
	return func() (ModuleContents, error) {
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			return ModuleContents{}, err
		}
		out := ModuleContents{Components: map[string]*qml.SpecNode{}}
		for _, e := range entries {
			if e.IsDir() || path.Ext(e.Name()) != ".qml" {
				continue
			}
			file := path.Join(dir, e.Name())
			src, err := fs.ReadFile(fsys, file)
			if err != nil {
				return ModuleContents{}, err
			}
			root, err := parseComponent(file, src)
			if err != nil {
				return ModuleContents{}, err
			}
			out.Components[strings.TrimSuffix(e.Name(), ".qml")] = root
		}
		return out, nil
	}
}

// parseComponent reads one component file and checks what a component may
// not contain.
func parseComponent(file string, src []byte) (*qml.SpecNode, error) {
	spec, err := qml.QML{File: file}.Parse(src)
	if err != nil {
		return nil, err
	}
	if len(spec.Imports) > 0 {
		return nil, fmt.Errorf("%s: a component imports nothing — its names resolve in the "+
			"document that uses it (at %s)", file, spec.Imports[0].Pos)
	}
	// Its declarations are judged now, against the file, rather than at each
	// use (component_props.go).
	if err := checkDecls(file, spec.Root); err != nil {
		return nil, err
	}
	// Ids inside are the component's own, renamed for each use when it is
	// expanded (component_ids.go).
	return spec.Root, nil
}

// loadComponents registers a module's component types as it loads, and
// returns their names for the unload.
func (t *Tree) loadComponents(module string, comps map[string]*qml.SpecNode) ([]string, error) {
	names := make([]string, 0, len(comps))
	for n := range comps {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !isUpperName(n) || strings.Contains(n, ".") {
			return nil, fmt.Errorf("%w: %q must be a type name — one upper-case word",
				ErrComponent, n)
		}
		if prior, dup := t.components[n]; dup {
			return nil, fmt.Errorf("%w: %q is already a component of %q", ErrDuplicateExport, n, prior.module)
		}
		if t.vocabularyHas(n) {
			return nil, fmt.Errorf("%w: %q is already a type of the vocabulary, and a component "+
				"of that name would replace it", ErrComponent, n)
		}
	}
	if t.components == nil {
		t.components = map[string]component{}
	}
	for _, n := range names {
		t.components[n] = component{module: module, root: comps[n]}
	}
	return names, nil
}

// vocabularyHas reports whether the adapter builds a type of this name.
func (t *Tree) vocabularyHas(name string) bool {
	v, ok := t.adapter.(Vocabulary)
	if !ok {
		return false
	}
	for _, n := range v.TypeNames() {
		if n == name {
			return true
		}
	}
	return false
}

// componentTypes is the component types a document's imports bring into scope,
// by the name the document writes: `QuitDialog`, or `D.QuitDialog` after an
// `as D`.
func (t *Tree) componentTypes(im imports) map[string]*qml.SpecNode {
	out := map[string]*qml.SpecNode{}
	for name, c := range t.components {
		for _, mod := range im.modules {
			if mod == c.module {
				out[name] = c.root
			}
		}
		for q, mod := range im.byQualifier {
			if mod == c.module {
				out[q+"."+name] = c.root
			}
		}
	}
	return out
}

// expand returns the document with every component use replaced by its
// expansion. It builds a new tree and leaves the one it was given alone: the
// caller's document is the caller's.
//
// params names a signal's parameters by type and signal, as the adapter
// declares them (Tree.signalParams); nil for none.
func expand(root *qml.SpecNode, types map[string]*qml.SpecNode,
	params func(typeName, signal string) []string) (*qml.SpecNode, error) {
	ex := expansion{types: types, params: params}
	// Every node walked has a KEY, which names a component use for the ids
	// inside it (component_ids.go): a node's own id when it has one, else its
	// parent's key and its place among the siblings of its type. It is stable
	// under edits that do not touch the path to the node — a use added before
	// a named instance leaves that instance's inner ids alone, so a reload
	// patches its fields rather than rebuilding them.
	var walk func(sn *qml.SpecNode, using []string, key string) (*qml.SpecNode, error)
	children := func(kids []*qml.SpecNode, using []string, key string, tag string) ([]*qml.SpecNode, error) {
		out := make([]*qml.SpecNode, len(kids))
		seen := map[string]int{}
		for i, c := range kids {
			ck := c.ID
			if ck == "" {
				ck = key + "/" + tag + c.Type + "#" + strconv.Itoa(seen[c.Type])
				seen[c.Type]++
			}
			ec, err := walk(c, using, ck)
			if err != nil {
				return nil, err
			}
			out[i] = ec
		}
		return out, nil
	}
	walk = func(sn *qml.SpecNode, using []string, key string) (*qml.SpecNode, error) {
		out := *sn
		if def, ok := types[sn.Type]; ok {
			for _, u := range using {
				if u == sn.Type {
					return nil, fmt.Errorf("%w: %s contains itself, by way of %s (at %s)",
						ErrComponent, sn.Type, strings.Join(append(using, sn.Type), " → "), sn.Pos)
				}
			}
			if len(using) >= maxComponentDepth {
				return nil, fmt.Errorf("%w: components nest more than %d deep (at %s)",
					ErrComponent, maxComponentDepth, sn.Pos)
			}
			// The declared properties are bound BEFORE the copy's own nested
			// uses expand, so a value passed down to an inner component's
			// property arrives there already substituted (component_props.go).
			scoped, err := substitute(scopeIDs(def, key, sn.ID), sn, ex)
			if err != nil {
				return nil, err
			}
			inner, err := walk(scoped, append(using, sn.Type), key)
			if err != nil {
				return nil, err
			}
			// The use site's children were not expanded by the walk above;
			// they are keyed apart from the component's own ("+"), wherever
			// they land.
			extra, err := children(sn.Children, using, key, "+")
			if err != nil {
				return nil, err
			}
			use := withoutDeclared(sn, def)
			use.Children = extra
			out = merge(*inner, use, aliasTarget(def, key, sn.ID))
			return &out, nil
		}
		kids, err := children(sn.Children, using, key, "")
		if err != nil {
			return nil, err
		}
		out.Children = kids
		return &out, nil
	}
	// A component's root declares its properties; a document declares none,
	// and neither does a use of a component.
	if sn := firstDeclaring(root, true); sn != nil {
		return nil, fmt.Errorf("%w: a %s declares %q, and only a component file's root "+
			"declares properties (at %s)", ErrComponent, sn.Type, sn.Decls[0].Name, sn.Decls[0].Pos)
	}
	return walk(root, nil, "")
}

// ExpandComponents returns root with every component use in it expanded, as
// Mount expands a document before judging it, or the error expansion finds:
// a component that contains itself, a required property a use does not set,
// a value of the wrong kind, an object or a call where an expression goes.
// types are the component types in scope, by the name a document writes, and
// params the adapter's signal parameters by type and signal ([SignalParameters];
// nil for none), which shadow a declared property in their handlers as they
// do when the document is mounted. It builds nothing, resolves no name and
// leaves root alone.
//
// It is for a checker that cannot mount a use of a component —
// tuidecl.Check, for one requiring a value no placeholder can build — and
// still judges what expansion alone can.
func ExpandComponents(root *qml.SpecNode, types map[string]*qml.SpecNode,
	params func(typeName, signal string) []string) (*qml.SpecNode, error) {
	return expand(root, types, params)
}

// merge lays a use site over an expanded component, by Qt's rules. target is
// the scoped id of the object the component's default alias sends the use
// site's children to, "" for none: they go to the root, after its own.
func merge(comp qml.SpecNode, use *qml.SpecNode, target string) qml.SpecNode {
	out := comp
	// The use site names the instance. With no name of its own, the root keeps
	// the private one scopeIDs gave its own id, so the component can still
	// reach itself (`me.close()`).
	if use.ID != "" {
		out.ID = use.ID
	}
	overridden := map[string]bool{}
	for _, p := range use.Props {
		overridden[p.Name] = true
	}
	out.Props = nil
	for _, p := range comp.Props {
		if !overridden[p.Name] {
			out.Props = append(out.Props, p)
		}
	}
	out.Props = append(out.Props, use.Props...)
	out.Handlers = append(append([]qml.SpecHandler(nil), comp.Handlers...), use.Handlers...)
	if target != "" && target != comp.ID && len(use.Children) > 0 {
		// checkDecls made the target an id in the file, and scopeIDs renamed
		// it with every other, so it is found; the fallback below is the
		// root's, as with no alias at all.
		if kids, ok := appendUnder(comp.Children, target, use.Children); ok {
			out.Children = kids
			return out
		}
	}
	out.Children = append(append([]*qml.SpecNode(nil), comp.Children...), use.Children...)
	return out
}

// appendUnder returns a copy of kids with more appended to the children of the
// node whose id is id, copying each node on the way to it; false when no node
// below kids has that id.
func appendUnder(kids []*qml.SpecNode, id string, more []*qml.SpecNode) ([]*qml.SpecNode, bool) {
	for i, c := range kids {
		n := *c
		switch {
		case c.ID == id:
			n.Children = append(append([]*qml.SpecNode(nil), c.Children...), more...)
		default:
			sub, ok := appendUnder(c.Children, id, more)
			if !ok {
				continue
			}
			n.Children = sub
		}
		out := append([]*qml.SpecNode(nil), kids...)
		out[i] = &n
		return out, true
	}
	return nil, false
}
