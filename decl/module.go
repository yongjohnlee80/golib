package decl

import (
	"errors"
	"fmt"
	"github.com/yongjohnlee80/golib/parse/qml"
	"strings"
)

// Module is one importable namespace a host or adapter offers.
//
// It is DISTINCT from an injected namespace, and the difference is the whole
// point of this file. QML draws the same line: a module is imported before its
// names are reachable, while a context property the host installs is simply
// there. Collapsing the two would make `import` decoration — a statement an
// author writes, that nothing checks, that changes nothing when wrong.
type Module struct {
	// Name is the dotted module name as an import writes it: "tui".
	Name string
	// Version is what this module answers to, empty when it is unversioned. It
	// is compared as WRITTEN, because this engine has no version policy and
	// inventing one would be guessing on the host's behalf.
	Version string
	// Exports are the SINGLETON NAMES this module brings into scope, each of
	// which must begin with an upper-case letter because it names a type.
	//
	// A module is not itself a name. `import tui 1.0` does not make `tui`
	// writable any more than `import QtQuick` makes `QtQuick` writable: it
	// brings the module's singletons into scope under THEIR names, so a module
	// exporting Tui gives `Tui.Horizontal`. An earlier version bound the module
	// name itself, which produced `Tui.Horizontal` — a spelling no QML runtime
	// accepts, because a singleton is a type and a type is capitalised.
	Exports []string
}

// Modules is an OPTIONAL capability an [Adapter] may implement to say which
// modules a schema may import.
//
// It is separate from [Constants] because the two answer different questions.
// Constants says what `Tui.Horizontal` MEANS; this says that `tui` is a module
// and therefore has to be imported before anyone writes that.
type Modules interface {
	// Modules returns the importable modules this adapter provides.
	Modules() []Module
}

// Sentinels for the import layer.
var (
	// ErrUndefinedModule reports an import of something no host provides. It is
	// reported AT THE IMPORT, because that is the line to fix.
	ErrUndefinedModule = errors.New("decl: no such module")

	// ErrNotImported reports a qualified name whose module exists but was never
	// imported. It is deliberately not ErrNotInjected: the remedy is an import
	// line, not a change to what the host hands over.
	ErrNotImported = errors.New("decl: this module was not imported")

	// ErrDuplicateImport reports the same name bound by two imports.
	ErrDuplicateImport = errors.New("decl: this import name is already bound")

	// ErrDuplicateExport reports two modules exporting one singleton name.
	ErrDuplicateExport = errors.New("decl: two modules export this name")
)

// DeclareModule registers an importable module, before Mount.
func (t *Tree) DeclareModule(m Module) error {
	if t.ph != phaseIdle || t.root != NoNode {
		return SchemaError{Op: "declare module", Detail: m.Name, Err: fmt.Errorf(
			"%w: modules are declared before Mount, so an import can be checked "+
				"against a fixed set", ErrPhase)}
	}
	// Kept off an offered name here rather than in registerModule, because a
	// loading offered module registers under its OWN offered name.
	if err := t.nameFree("declare module", m.Name); err != nil {
		return err
	}
	return t.registerModule("declare module", m)
}

// registerModule is the ONE place a module enters the registry.
//
// Both ways in go through it: a host calling [Tree.DeclareModule], and an
// adapter's [Modules] read during [New]. An earlier version validated only the
// first and copied the adapter's straight into the map, so an adapter
// publishing two modules that export one name bypassed every rule below — and
// the check that would have caught it at import time had been removed as
// unreachable, on an invariant that held for one of the two paths.
//
// A rule enforced at one entry point is not enforced.
func (t *Tree) registerModule(op string, m Module) error {
	if m.Name == "" {
		return SchemaError{Op: op,
			Err: fmt.Errorf("%w: a module needs a name", ErrUndefinedModule)}
	}
	// An export names a SINGLETON, and a singleton is a type. QML capitalises
	// types, so a lower-case export is refused here rather than producing a
	// spelling no QML runtime accepts.
	for _, e := range m.Exports {
		if e == "" || !isUpperName(e) {
			return SchemaError{Op: op, Detail: m.Name, Err: fmt.Errorf(
				"%w: export %q must begin with an upper-case letter, because it "+
					"names a singleton and a singleton is a type", ErrUndefinedModule, e)}
		}
	}
	if t.modules == nil {
		t.modules = map[string]Module{}
	}
	if prior, dup := t.modules[m.Name]; dup {
		return SchemaError{Op: op, Detail: m.Name, Err: fmt.Errorf(
			"%w: %q is already declared at version %q", ErrDuplicateDecl, m.Name, prior.Version)}
	}

	// TWO MODULES MAY NOT EXPORT ONE NAME, and this engine says so at
	// REGISTRATION rather than pretending a qualifier would sort it out.
	//
	// A qualifier changes SPELLING, not IDENTITY. `import a.theme as A` and
	// `import b.theme as B` would give `A.Theme.surface` and `B.Theme.surface`,
	// and both resolve to the one registry entry named Theme.surface: the
	// document reads as though it distinguishes them and it does not.
	//
	// QML disambiguates because a module OWNS its singletons. Making export
	// identity module-owned here would mean a second injection and update API
	// for module-scoped values, which nothing in this product needs — so the
	// combination is refused, and the diagnostic asks for the rename that
	// actually works instead of a qualifier that only looks like it does.
	for _, e := range m.Exports {
		if owner, exists := t.providerOf(e); exists {
			return SchemaError{Op: op, Detail: m.Name, Err: fmt.Errorf(
				"%w: %q is already exported by %q, and a qualifier would change how "+
					"a document SPELLS them without making them different values; "+
					"rename one export", ErrDuplicateExport, e, owner)}
		}
	}
	t.modules[m.Name] = m
	return nil
}

// imports is what a document's import lines brought into scope.
type imports struct {
	// byName maps a name a document may write to the module that provides it:
	// "Tui" -> "tui" after a plain `import tui 1.0`.
	byName map[string]string
	// byQualifier maps a qualifier to its module: "T" -> "tui" after
	// `import tui 1.0 as T`, which makes the exports reachable as `T.Tui`.
	byQualifier map[string]string
	// ids maps each id the document declares to its node's type, so a handler
	// can call `quitDialog.open()`. They are in scope with the imports because
	// they are names the document brought into scope, resolved the same way.
	ids map[string]string
	// modules are the modules imported WITHOUT a qualifier, whose component
	// types are in scope by their own names.
	modules []string
}

// resolveImports checks a schema's imports and returns what they bind.
//
// It runs BEFORE any node is planned, so a document that imports something
// nonexistent is refused with the tree untouched — the same rule every other
// check in this engine follows, for the same reason.
func (t *Tree) resolveImports(spec qml.SpecTree) (imports, error) {
	out := imports{byName: map[string]string{}, byQualifier: map[string]string{}}
	for _, im := range spec.Imports {
		if im.Module == "" {
			// A directory or file import. This engine has no filesystem and
			// says so, rather than accepting a line it will then ignore.
			return imports{}, SchemaError{Op: "import", Pos: im.Pos, Err: fmt.Errorf(
				"%w: this engine resolves named modules, not paths", ErrUndefinedModule)}
		}
		m, ok := t.modules[im.Module]
		if !ok {
			return imports{}, SchemaError{Op: "import", Detail: im.Module, Pos: im.Pos, Err: fmt.Errorf(
				"%w: %q; the host provides %s", ErrUndefinedModule, im.Module, t.moduleList())}
		}
		if err := checkVersion(im, m.Version); err != nil {
			return imports{}, err
		}

		if im.Alias != "" {
			// A QUALIFIED import reaches the exports through the qualifier and
			// NOT by their own names, which is QML's rule. Binding both would
			// let a document compile here that a real QML runtime refuses.
			if prior, dup := out.byQualifier[im.Alias]; dup {
				return imports{}, SchemaError{Op: "import", Detail: im.Alias, Pos: im.Pos, Err: fmt.Errorf(
					"%w: %q already qualifies %q", ErrDuplicateImport, im.Alias, prior)}
			}
			out.byQualifier[im.Alias] = im.Module
			continue
		}
		// No cross-module clash is possible here: registerModule refuses two
		// modules exporting one name on BOTH registration paths, so the only
		// way a name is bound twice is the same module imported twice, which is
		// a no-op. That invariant is asserted by TestAnAdapterCannotPublish
		// TwoModulesExportingOneName, not merely assumed — assuming it, while
		// one of the two paths was unguarded, is how it was wrong before.
		for _, name := range m.Exports {
			out.byName[name] = im.Module
		}
		out.modules = append(out.modules, im.Module)
	}
	return out, nil
}

// exportsOf reports whether a module declares this export.
func (t *Tree) exportsOf(module, name string) bool {
	for _, e := range t.modules[module].Exports {
		if e == name {
			return true
		}
	}
	return false
}

// providerOf reports the module that exports a name, for a diagnostic that can
// tell "you did not import it" from "nobody has it".
func (t *Tree) providerOf(name string) (string, bool) {
	for _, m := range t.modules {
		for _, e := range m.Exports {
			if e == name {
				return m.Name, true
			}
		}
	}
	return "", false
}

func (t *Tree) moduleList() string {
	// Offered modules are listed whether loaded or not: to a document they are
	// all equally importable.
	seen := map[string]bool{}
	var names []string
	for n := range t.modules {
		seen[n] = true
		names = append(names, n)
	}
	for n := range t.offered {
		if !seen[n] {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return "no modules at all"
	}
	// Deterministic, so a diagnostic is the same sentence on every run.
	sortStrings(names)
	return strings.Join(names, ", ")
}

// isUpperName reports whether a name is written the way QML writes a type.
func isUpperName(s string) bool {
	if s == "" {
		return false
	}
	r := []rune(s)[0]
	return r >= 'A' && r <= 'Z'
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
