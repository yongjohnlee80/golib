package decl

import (
	"fmt"
	"github.com/yongjohnlee80/golib/parse/qml"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/tui"
)

// Adapter implements decl.Restructurer for the nodes that can support it.
//
// "The ones that can" is the whole point. tui.Container is the toolkit's own
// statement about which widgets accept child changes after construction, and it
// is not universal: widget.Split takes both of its children as required
// constructor arguments and implements no Add, Remove or Move at all. Asking
// the component rather than assuming is what lets the engine rebuild a Split
// and splice a Flex in the same reload.
var (
	_ decl.Restructurer = (*Adapter)(nil)
	_ decl.Classifier   = (*Adapter)(nil)
	_ decl.Constants    = (*Adapter)(nil)
)

// CanRestructure reports whether the node's component accepts child changes.
//
// A node that was never built, or whose component is not a tui.Container,
// answers no — and the engine rebuilds it rather than attempting an edit that
// would fail halfway.
func (a *Adapter) CanRestructure(node decl.NodeID) bool {
	b, ok := a.nodes[node]
	if !ok {
		return false
	}
	_, isContainer := b.comp.(tui.Container)
	return isContainer
}

// InsertChild places child among parent's children at index at.
//
// tui.Container has no insert-at-index: Add appends. So this appends and then
// moves, which is safe because Move is an identity-preserving splice — the
// child never unmounts between the two calls.
//
// The index is checked BEFORE anything is attached. Refusing afterwards would
// leave the child added but unplaced, which is a worse state than either doing
// the work or declining it. Appending to the end needs no Move at all, and
// skipping it matters: Move reorders a live container rather than politely
// noticing there is nothing to do.
func (a *Adapter) InsertChild(parent, child decl.NodeID, at int) error {
	c, comp, err := a.containerAndChild("insert", parent, child)
	if err != nil {
		return err
	}
	n := countChildren(c)
	if at < 0 || at > n {
		return fmt.Errorf("insert: index %d is out of range for %d children of node %d", at, n, parent)
	}
	c.Add(comp)
	if at < n {
		c.Move(comp, at)
	}
	return nil
}

// RemoveChild detaches child from parent, unmounting its subtree.
func (a *Adapter) RemoveChild(parent, child decl.NodeID) error {
	c, comp, err := a.containerAndChild("remove", parent, child)
	if err != nil {
		return err
	}
	c.Remove(comp)
	return nil
}

// MoveChild relocates child to index to without unmounting it, which is what
// preserves its NodeID, context, in-flight tasks, hooks and focus.
//
// The index is bound-checked here rather than trusted, because tui.Container's
// Move PANICS on an out-of-range index — a fatal error, not a returned one. An
// engine bug would otherwise take the whole program down instead of surfacing
// as the refusal this seam is shaped to carry.
func (a *Adapter) MoveChild(parent, child decl.NodeID, to int) error {
	c, comp, err := a.containerAndChild("move", parent, child)
	if err != nil {
		return err
	}
	if n := countChildren(c); to < 0 || to >= n {
		return fmt.Errorf("move: index %d is out of range for %d children of node %d", to, n, parent)
	}
	c.Move(comp, to)
	return nil
}

// containerAndChild resolves both ends of a structural operation, or says
// precisely which end was missing.
func (a *Adapter) containerAndChild(op string, parent, child decl.NodeID) (tui.Container, tui.Component, error) {
	pb, ok := a.nodes[parent]
	if !ok {
		return nil, nil, fmt.Errorf("%s: node %d has no component", op, parent)
	}
	c, ok := pb.comp.(tui.Container)
	if !ok {
		return nil, nil, fmt.Errorf("%s: %q (node %d) is not a container", op, pb.typ, parent)
	}
	cb, ok := a.nodes[child]
	if !ok {
		return nil, nil, fmt.Errorf("%s: child node %d has no component", op, child)
	}
	return c, cb.comp, nil
}

func countChildren(c tui.Container) int {
	n := 0
	for range c.Children() {
		n++
	}
	return n
}

// ClassifyProperty implements decl.Classifier.
//
// Two tables, three answers. A property with a setter is runtime-settable; one
// the type declared as constructor-only is a rebuild; anything else the adapter
// simply does not have, and saying so is what stops a typo from demolishing a
// working widget to build one that would refuse it just the same.
//
// It is keyed by the schema TYPE, not by a mounted node. An earlier version took
// a NodeID and used it for nothing but looking up that node's type — so it could
// not answer for a node the schema ADDS, or for the replacement of one whose
// type changed, which are precisely the nodes a reload is about to build.
func (a *Adapter) ClassifyProperty(typeName, prop string) decl.PropertyKind {
	if _, ok := a.setters[typeName][prop]; ok {
		return decl.PropRuntime
	}
	if a.ctorProps[typeName][prop] {
		return decl.PropConstructorOnly
	}
	return decl.PropUnknown
}

// Constants implements decl.Constants: the toolkit's own vocabulary, written
// the way QML writes an enum.
//
// `orientation: Tui.Horizontal` rather than a quoted string or a bare word. Qt
// spells these `Qt.Horizontal`; the mechanism is identical, and a qualified
// name is a CONSTANT — resolved once at planning, never tracked — which is why
// it costs the reactive graph nothing.
func (a *Adapter) Constants() map[string]qml.SpecValue {
	str := func(s string) qml.SpecValue {
		return qml.SpecValue{Kind: qml.SpecValueString, Raw: s}
	}
	return map[string]qml.SpecValue{
		"Tui.Horizontal": str("horizontal"),
		"Tui.Vertical":   str("vertical"),
	}
}

// TuiModuleVersion is the version `import tui <v>` must ask for, when it asks
// for one at all.
const TuiModuleVersion = "1.0"

// Modules implements decl.Modules.
//
// This is what makes `import tui 1.0` mean something. Without it the import
// line would parse, resolve to nothing, and a document that forgot it would
// work anyway — which is the difference between a module system and a comment.
//
// The module EXPORTS a singleton rather than being a name itself, which is how
// QML works: `import tui 1.0` brings `Tui` into scope, and the document writes
// `Tui.Horizontal`. An earlier version bound the module name, producing
// `Tui.Horizontal` — a spelling no QML runtime accepts, because a singleton is
// a type and a type is capitalised.
func (a *Adapter) Modules() []decl.Module {
	return []decl.Module{{
		Name:    "tui",
		Version: TuiModuleVersion,
		Exports: []string{"Tui"},
	}}
}
