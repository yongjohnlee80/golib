package decl

import (
	"errors"
	"fmt"
	"strings"

	"github.com/yongjohnlee80/golib/parse"
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
}

// Modules is an OPTIONAL capability an [Adapter] may implement to say which
// modules a schema may import.
//
// It is separate from [Constants] because the two answer different questions.
// Constants says what `tui.Horizontal` MEANS; this says that `tui` is a module
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
)

// DeclareModule registers an importable module, before Mount.
func (t *Tree) DeclareModule(m Module) error {
	if t.ph != phaseIdle || t.root != NoNode {
		return SchemaError{Op: "declare module", Detail: m.Name, Err: fmt.Errorf(
			"%w: modules are declared before Mount, so an import can be checked "+
				"against a fixed set", ErrPhase)}
	}
	if m.Name == "" {
		return SchemaError{Op: "declare module",
			Err: fmt.Errorf("%w: a module needs a name", ErrUndefinedModule)}
	}
	if t.modules == nil {
		t.modules = map[string]Module{}
	}
	if prior, dup := t.modules[m.Name]; dup {
		return SchemaError{Op: "declare module", Detail: m.Name, Err: fmt.Errorf(
			"%w: %q is already declared at version %q", ErrDuplicateDecl, m.Name, prior.Version)}
	}
	t.modules[m.Name] = m
	return nil
}

// resolveImports checks a schema's imports and returns the names they bind.
//
// It runs BEFORE any node is planned, so a document that imports something
// nonexistent is refused with the tree untouched — the same rule every other
// check in this engine follows, for the same reason.
func (t *Tree) resolveImports(spec parse.SpecTree) (map[string]string, error) {
	bound := map[string]string{}
	for _, im := range spec.Imports {
		if im.Module == "" {
			// A directory or file import. This engine has no filesystem and
			// says so, rather than accepting a line it will then ignore.
			return nil, SchemaError{Op: "import", Pos: im.Pos, Err: fmt.Errorf(
				"%w: this engine resolves named modules, not paths", ErrUndefinedModule)}
		}
		m, ok := t.modules[im.Module]
		if !ok {
			return nil, SchemaError{Op: "import", Detail: im.Module, Pos: im.Pos, Err: fmt.Errorf(
				"%w: %q; the host provides %s", ErrUndefinedModule, im.Module, t.moduleList())}
		}
		// The version is compared only when the document states one. An import
		// that names no version takes what it is given, which is what makes
		// adding a version to a module later a compatible change.
		if im.Version != "" && m.Version != "" && im.Version != m.Version {
			return nil, SchemaError{Op: "import", Detail: im.Module, Pos: im.Pos, Err: fmt.Errorf(
				"%w: %q is version %q here, and this document asks for %q",
				ErrUndefinedModule, im.Module, m.Version, im.Version)}
		}

		// An alias REPLACES the module name rather than adding to it, which is
		// QML's rule: after `import tui 1.0 as T`, `tui.Horizontal` no longer
		// resolves. Binding both would let a document compile that a real QML
		// runtime refuses.
		name := im.Module
		if im.Alias != "" {
			name = im.Alias
		}
		if prior, dup := bound[name]; dup {
			return nil, SchemaError{Op: "import", Detail: name, Pos: im.Pos, Err: fmt.Errorf(
				"%w: %q already names %q", ErrDuplicateImport, name, prior)}
		}
		bound[name] = im.Module
	}
	return bound, nil
}

func (t *Tree) moduleList() string {
	if len(t.modules) == 0 {
		return "no modules at all"
	}
	names := make([]string, 0, len(t.modules))
	for n := range t.modules {
		names = append(names, n)
	}
	// Deterministic, so a diagnostic is the same sentence on every run.
	sortStrings(names)
	return strings.Join(names, ", ")
}

// moduleOf reports the module a qualified name belongs to, and whether it has
// one at all. A name with no module is ambient — an injected object, which needs
// no import.
func (t *Tree) moduleOf(path []string) (string, bool) {
	for i := len(path) - 1; i >= 1; i-- {
		prefix := joinDots(path[:i])
		if _, ok := t.modules[prefix]; ok {
			return prefix, true
		}
	}
	return "", false
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
