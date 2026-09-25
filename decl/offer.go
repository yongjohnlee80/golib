package decl

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// OFFERED MODULES — loaded when a document imports them, and not before.
//
// A declared module is registered whole, up front. That is right for a small
// vocabulary and wrong for a large application: a program with forty dialogs
// and three themes should not parse, validate and inject all of them to show
// the one screen a document asks for. QML's own engine does not either — it
// finds a module on its import path when an import line names it.
//
// An offered module is that. The host says the module EXISTS and how to
// produce it; the engine runs the loader the first time a document it mounts
// or reconciles imports the module, and never otherwise. The import line is
// the whole selection mechanism:
//
//	import editor.theme.retro 1.0     ← only this theme is loaded
//
// Loading goes through the same registration a declared module does, so every
// rule holds unchanged. In particular two loaded modules still may not export
// one name: a document importing two themes that both export Theme is refused
// at the second import, which is exactly the ambiguity it is.

// ModuleContents is what a loader produces.
//
// A struct rather than a pair of results, so a module can come to carry more —
// component types defined in QML, say — as a new field that existing loaders
// simply leave empty.
type ModuleContents struct {
	// Exports are the singleton names the module brings into scope. The same
	// rule as [Module.Exports]: each names a type and is upper-case.
	Exports []string
	// Values are the module's names, keyed as a document writes them after an
	// unqualified import: "Theme.menu.window". Every key must sit under one of
	// Exports — a module brings ITS singletons into scope, not arbitrary globals.
	Values map[string]Injected
	// Components are the module's component types, by type name: a QML file
	// each, usually, loaded by [ComponentFiles]. See component.go.
	Components map[string]*qml.SpecNode
}

// ModuleLoader produces an offered module's contents. It runs at most once per
// load: on the first Mount or Reconcile whose document imports the module.
type ModuleLoader func() (ModuleContents, error)

// ErrModuleLoad reports an offered module whose loader failed, or produced
// contents the engine refuses. It is reported at the import that asked for it.
var ErrModuleLoad = errors.New("decl: the module could not be loaded")

// offer is one module the host offered and the engine has not necessarily
// loaded.
type offer struct {
	version string
	load    ModuleLoader
}

// loaded is one offered module the engine did load, and every registry key the
// load wrote — what undoing it must remove.
type loaded struct {
	keys       []string
	components []string
}

// OfferModule makes a module importable without loading it, before Mount.
func (t *Tree) OfferModule(name, version string, load ModuleLoader) error {
	const op = "offer module"
	if t.ph != phaseIdle || t.root != NoNode {
		return SchemaError{Op: op, Detail: name, Err: fmt.Errorf(
			"%w: modules are offered before Mount, so an import can be checked "+
				"against a fixed set", ErrPhase)}
	}
	if name == "" || load == nil {
		return SchemaError{Op: op, Detail: name, Err: fmt.Errorf(
			"%w: an offered module needs a name and a loader", ErrUndefinedModule)}
	}
	if err := t.nameFree(op, name); err != nil {
		return err
	}
	if t.offered == nil {
		t.offered = map[string]offer{}
	}
	t.offered[name] = offer{version: version, load: load}
	return nil
}

// nameFree refuses a module name that is already declared or offered. Both
// registration paths ask it, so one name cannot mean two modules depending on
// which way it came in.
func (t *Tree) nameFree(op, name string) error {
	if prior, dup := t.modules[name]; dup {
		return SchemaError{Op: op, Detail: name, Err: fmt.Errorf(
			"%w: %q is already declared at version %q", ErrDuplicateDecl, name, prior.Version)}
	}
	if prior, dup := t.offered[name]; dup {
		return SchemaError{Op: op, Detail: name, Err: fmt.Errorf(
			"%w: %q is already offered at version %q", ErrDuplicateDecl, name, prior.version)}
	}
	return nil
}

// loadImported loads every offered module a document imports that is not
// loaded yet, and returns how to undo exactly those loads.
//
// It runs BEFORE the document is vetted, because vetting resolves the imports
// and an offered module has nothing to resolve against until it is loaded. The
// undo is what keeps that ordering honest: a document refused after its
// modules were loaded must leave the tree as it found it, so the caller undoes
// on any failure that built nothing.
func (t *Tree) loadImported(spec qml.SpecTree) (undo func(), err error) {
	var done []string
	undo = func() {
		for _, name := range done {
			t.unload(name)
		}
	}
	for _, im := range spec.Imports {
		o, ok := t.offered[im.Module]
		if !ok || t.isLoaded(im.Module) {
			continue
		}
		// A version the document cannot have is refused before the loader
		// runs: loading a module only to be told it was the wrong one would
		// report the load's problems instead of the import's.
		if err := checkVersion(im, o.version); err != nil {
			undo()
			return nil, err
		}
		if err := t.load(im, o); err != nil {
			undo()
			return nil, err
		}
		done = append(done, im.Module)
	}
	return undo, nil
}

// load runs one offered module's loader and registers what it produced.
func (t *Tree) load(im qml.SpecImport, o offer) error {
	fail := func(err error) error {
		return SchemaError{Op: "import", Detail: im.Module, Pos: im.Pos, Err: err}
	}
	c, err := o.load()
	if err != nil {
		return fail(fmt.Errorf("%w: %v", ErrModuleLoad, err))
	}
	for key := range c.Values {
		if !underExport(key, c.Exports) {
			return fail(fmt.Errorf("%w: %q is not under any of its exports (%s)",
				ErrModuleLoad, key, strings.Join(c.Exports, ", ")))
		}
	}
	if err := t.registerModule("import", Module{
		Name: im.Module, Version: o.version, Exports: c.Exports,
	}); err != nil {
		return err
	}
	if t.loads == nil {
		t.loads = map[string]*loaded{}
	}
	rec := &loaded{}
	t.loads[im.Module] = rec
	// Sorted, so a module that fails part-way fails on the same key every run.
	keys := make([]string, 0, len(c.Values))
	for k := range c.Values {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		created, err := t.register("import", k, c.Values[k])
		rec.keys = append(rec.keys, created...)
		if err != nil {
			t.unload(im.Module)
			return fail(fmt.Errorf("%w: %v", ErrModuleLoad, err))
		}
	}
	names, err := t.loadComponents(im.Module, c.Components)
	if err != nil {
		t.unload(im.Module)
		return fail(err)
	}
	rec.components = names
	return nil
}

func (t *Tree) isLoaded(name string) bool {
	_, ok := t.loads[name]
	return ok
}

// unload forgets one loaded module: its registration and every key its load
// wrote. The module stays OFFERED, so a later document can import it again.
func (t *Tree) unload(name string) {
	rec, ok := t.loads[name]
	if !ok {
		return
	}
	for _, k := range rec.keys {
		delete(t.injected, k)
		delete(t.sources, k)
	}
	for _, n := range rec.components {
		delete(t.components, n)
	}
	delete(t.modules, name)
	delete(t.loads, name)
}

// unloadAll forgets every loaded module, for Destroy: the values a load wrote
// went with the registry, so the modules claiming them must go too.
func (t *Tree) unloadAll() {
	for name := range t.loads {
		t.unload(name)
	}
}

// underExport reports whether a value key sits under one of the exports.
func underExport(key string, exports []string) bool {
	for _, e := range exports {
		if strings.HasPrefix(key, e+".") {
			return true
		}
	}
	return false
}

// checkVersion compares an import's version with a module's, when both state
// one. An import that names no version takes what it is given, which is what
// makes adding a version to a module later a compatible change.
func checkVersion(im qml.SpecImport, version string) error {
	if im.Version == "" || version == "" || im.Version == version {
		return nil
	}
	return SchemaError{Op: "import", Detail: im.Module, Pos: im.Pos, Err: fmt.Errorf(
		"%w: %q is version %q here, and this document asks for %q",
		ErrUndefinedModule, im.Module, version, im.Version)}
}
