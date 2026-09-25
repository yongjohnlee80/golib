package decl

import (
	"fmt"
	"io/fs"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// ValueModule returns a loader for a module written as a QML document of
// constants — a theme, a palette, a table of strings:
//
//	Theme {
//	    menu { window: "white"; windowText: "black"; accent: "red" }
//	    status { window: "white"; windowText: "black" }
//	}
//
// The root's type is the module's one export, and every property becomes a
// constant under it: `Theme.menu.window`. Grouped properties are how QML
// writes nesting, so the document needs no syntax of its own.
//
// It is parsed when the loader RUNS, not when it is made — which is the point:
// a theme nobody imports is never read.
//
// Only literals are accepted. A value module is data; one whose values were
// bindings would need the tree it is being loaded into to evaluate them, and it
// is loaded before that tree has resolved its imports.
func ValueModule(src []byte) ModuleLoader {
	return func() (ModuleContents, error) { return valueModule("", src) }
}

// ValueFile is [ValueModule] for a file: read when the loader runs, and every
// diagnostic placed in it — "themes/mono.qml:3:5" — as a component file's is.
func ValueFile(fsys fs.FS, file string) ModuleLoader {
	return func() (ModuleContents, error) {
		src, err := fs.ReadFile(fsys, file)
		if err != nil {
			return ModuleContents{}, err
		}
		return valueModule(file, src)
	}
}

func valueModule(file string, src []byte) (ModuleContents, error) {
	spec, err := qml.QML{File: file}.Parse(src)
	if err != nil {
		return ModuleContents{}, err
	}
	root := spec.Root
	switch {
	case len(spec.Imports) > 0:
		return ModuleContents{}, fmt.Errorf("a value module imports nothing (at %s)", spec.Imports[0].Pos)
	case len(root.Children) > 0:
		return ModuleContents{}, fmt.Errorf("a value module holds properties, not objects (at %s)",
			root.Children[0].Pos)
	case len(root.Handlers) > 0:
		return ModuleContents{}, fmt.Errorf("a value module has no handlers (at %s)", root.Pos)
	case root.ID != "":
		return ModuleContents{}, fmt.Errorf("a value module has no id (at %s)", root.Pos)
	}
	values := make(map[string]Injected, len(root.Props))
	for _, p := range root.Props {
		if !isTerminal(p.Value) {
			return ModuleContents{}, fmt.Errorf("%s.%s must be a literal, got %s (at %s)",
				root.Type, p.Name, p.Value.Kind, p.Value.Pos)
		}
		name := root.Type + "." + p.Name
		if _, dup := values[name]; dup {
			return ModuleContents{}, fmt.Errorf("%s is written twice (at %s)", name, p.Value.Pos)
		}
		values[name] = Constant(p.Value)
	}
	return ModuleContents{Exports: []string{root.Type}, Values: values}, nil
}
