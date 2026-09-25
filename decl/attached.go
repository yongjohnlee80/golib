package decl

import (
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/parse/qml"
)

// Attacher is an OPTIONAL capability an [Adapter] implements to support QML
// ATTACHED PROPERTIES — `Layout.fillWidth: true`, `Dock.edge: Tui.Top`.
//
// An attached property is written on a CHILD and read by its PARENT. That is
// QML's idiom for "this child tells its container how to treat it", and it fits
// a toolkit whose containers own sizing: a child cannot place itself, but it can
// ask.
//
// Two different questions hide in one spelling, and the adapter answers both:
// what `Dock.edge` MEANS — which names the attaching schema declares — and which
// PARENTS honour it. Keeping them apart is what lets a new container opt into an
// existing attached schema without redeclaring it.
type Attacher interface {
	// CheckAttached judges one property of a node of childType whose parent is
	// parentType, or "" for the root.
	//
	// attached is false when prop is not an attached property at all — an
	// ordinary or grouped property, which is the rest of the engine's business.
	// When it is true, a nil error admits it and a non-nil error says why it
	// is refused: an unknown schema member, or a parent that does not honour
	// the schema.
	//
	// It must not have side effects: it is consulted during planning, before
	// anything is built.
	CheckAttached(parentType, childType, prop string) (attached bool, err error)
}

// ErrAttached reports an attached property the document cannot use where it
// wrote it.
var ErrAttached = errors.New("decl: this attached property cannot be used here")

// checkAttached walks the NEW document with each node's parent type in hand and
// judges every attached property against it.
//
// It runs over the whole tree before any planning, from both Mount and
// Reconcile. That placement is the point. An attached property is only
// meaningful relative to its parent, and the prospective parent is known here —
// during planning — for every node, including ones that do not exist yet and
// ones a reload is about to move under a different container. A check made at
// the parent's construction instead would run after the children were built,
// which is after the damage.
func (t *Tree) checkAttached(root *qml.SpecNode) error {
	at, ok := t.adapter.(Attacher)
	if !ok {
		return nil
	}
	return walkSpec(root, func(parentType string, sn *qml.SpecNode) error {
		for _, p := range sn.Props {
			attached, err := at.CheckAttached(parentType, sn.Type, p.Name)
			if !attached {
				continue
			}
			if err != nil {
				return SchemaError{Op: "attach", Detail: p.Name, Pos: p.Pos,
					Err: fmt.Errorf("%w: %w", ErrAttached, err)}
			}
			// An attached property is read by the parent AT CONSTRUCTION, so a
			// binding on one could never propagate: nothing re-reads it when the
			// source moves. Refused here rather than accepted and silently
			// frozen at its first value.
			if t.isBinding(p.Value) {
				return SchemaError{Op: "attach", Detail: p.Name, Pos: p.Value.Pos,
					Err: fmt.Errorf("%w: %q is read by the parent when it is built, so it "+
						"cannot be bound to a value that changes", ErrAttached, p.Name)}
			}
		}
		return nil
	})
}

// ErrDuplicateID reports two nodes in one document declaring the same `id`.
var ErrDuplicateID = errors.New("decl: this id is already declared in the document")

// checkIDs refuses a document in which two nodes share an `id`.
//
// QML requires an id to be unique within a document, and this engine depends
// on it twice over: [Tree.NodeByID] answers with ONE node, and a reload matches
// old nodes to new by id. With a duplicate, both would pick whichever node a
// map iteration reached first — a different answer from run to run, with no
// error anywhere.
func (t *Tree) checkIDs(root *qml.SpecNode) error {
	seen := map[string]*qml.SpecNode{}
	return walkSpec(root, func(_ string, sn *qml.SpecNode) error {
		if sn.ID == "" {
			return nil
		}
		if first, dup := seen[sn.ID]; dup {
			return SchemaError{Op: "id", Detail: sn.ID, Pos: sn.Pos, Err: fmt.Errorf(
				"%w: %q is already the id of the %s at %s", ErrDuplicateID, sn.ID, first.Type, first.Pos)}
		}
		// An id is a name a handler can call through, so one spelling an
		// injected or imported name would make `x.open()` mean whichever the
		// resolver happened to try first.
		if t.nameTaken(sn.ID) {
			return SchemaError{Op: "id", Detail: sn.ID, Pos: sn.Pos, Err: fmt.Errorf(
				"%w: the id %q is already a name the document can reach; rename the id",
				ErrAmbiguousName, sn.ID)}
		}
		seen[sn.ID] = sn
		return nil
	})
}

// walkSpec visits every node of a document in DOCUMENT ORDER with its parent's
// type in hand — "" for the root — and stops at the first error.
//
// It is the one traversal every whole-document check shares, so a check says
// only what it judges and never how to reach the nodes.
func walkSpec(root *qml.SpecNode, visit func(parentType string, sn *qml.SpecNode) error) error {
	var walk func(parentType string, sn *qml.SpecNode) error
	walk = func(parentType string, sn *qml.SpecNode) error {
		if err := visit(parentType, sn); err != nil {
			return err
		}
		for _, c := range sn.Children {
			if err := walk(sn.Type, c); err != nil {
				return err
			}
		}
		return nil
	}
	return walk("", root)
}

// documentCheck judges a whole new document before any of it is planned.
type documentCheck func(t *Tree, root *qml.SpecNode) error

// documentChecks are run, in order, on EVERY document the tree is asked to
// adopt — by Mount and by Reconcile alike.
//
// A new whole-document rule is an entry here. Neither entry point changes, and
// so neither can come to enforce a rule the other does not: two call sites that
// each listed the checks themselves are exactly how one of them ends up
// missing one.
var documentChecks = []documentCheck{
	(*Tree).checkIDs,
	(*Tree).checkAttached,
}

// vetDocument resolves a document's imports, expands its components and runs
// every document check, returning the imports and the EXPANDED document only
// if all of it passed. The caller builds the expanded one.
//
// It changes nothing: the caller adopts the imports once it has decided to go
// ahead, so a refused document leaves the tree resolving through the imports
// it had before.
func (t *Tree) vetDocument(spec qml.SpecTree) (imports, qml.SpecTree, error) {
	imported, err := t.resolveImports(spec)
	if err != nil {
		return imports{}, qml.SpecTree{}, err
	}
	// COMPONENTS FIRST: every check below judges the document as it will be
	// built, which is with each component use expanded.
	root, err := expand(spec.Root, t.componentTypes(imported))
	if err != nil {
		return imports{}, qml.SpecTree{}, SchemaError{Op: "component", Err: err}
	}
	// REPEATERS NEXT: each becomes its delegate once per row of its model.
	root, sc, err := t.expandRepeaters(root)
	if err != nil {
		return imports{}, qml.SpecTree{}, err
	}
	t.repNext = sc
	spec.Root = root
	// The checks resolve names, so they run against the NEW document's
	// imports, with the old set restored whatever they decide.
	prev := t.imported
	t.imported = imported
	defer func() { t.imported = prev }()
	for _, check := range documentChecks {
		if err := check(t, spec.Root); err != nil {
			return imports{}, qml.SpecTree{}, err
		}
	}
	imported.ids = documentIDs(spec.Root)
	return imported, spec, nil
}
