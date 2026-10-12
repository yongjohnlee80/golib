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
	_, arranges := b.comp.(rearranger)
	return isContainer || arranges
}

// rearranger is a node whose children are not a Container's but can still
// change in place: the Window, which sorts them into keys, menus, dialogs and
// what its dock lays out. It is handed the whole new child list, in order,
// each time one changes.
type rearranger interface {
	arrange(children []tui.Component, attached []map[string]qml.SpecValue, nominee tui.Component) error
}

// rearrange hands a rearranger its children as the adapter now records them.
func (a *Adapter) rearrange(parent decl.NodeID, r rearranger) error {
	kids := a.kids[parent]
	comps := make([]tui.Component, 0, len(kids))
	attached := make([]map[string]qml.SpecValue, 0, len(kids))
	var nominee tui.Component
	for _, k := range kids {
		b := a.nodes[k]
		comps = append(comps, b.comp)
		attached = append(attached, b.attached)
		if nominee == nil {
			nominee = b.nominee
		}
	}
	return r.arrange(comps, attached, nominee)
}

// arrangerAndChild resolves a structural operation on a rearranger.
func (a *Adapter) arrangerAndChild(parent, child decl.NodeID) (rearranger, bool) {
	pb, ok := a.nodes[parent]
	if !ok {
		return nil, false
	}
	r, ok := pb.comp.(rearranger)
	if !ok {
		return nil, false
	}
	_, ok = a.nodes[child]
	return r, ok
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
	if r, ok := a.arrangerAndChild(parent, child); ok {
		if at < 0 || at > len(a.kids[parent]) {
			return fmt.Errorf("insert: index %d is out of range for %d children of node %d", at, len(a.kids[parent]), parent)
		}
		a.kids[parent] = insertKid(a.kids[parent], at, child)
		a.paletteAdopt(parent, child)
		return a.rearrange(parent, r)
	}
	c, comp, err := a.containerAndChild("insert", parent, child)
	if err != nil {
		return err
	}
	n := countChildren(c)
	if at < 0 || at > n {
		return fmt.Errorf("insert: index %d is out of range for %d children of node %d", at, n, parent)
	}
	if adopt := a.adopters[a.nodes[parent].typ]; adopt != nil {
		// The child is placed as its attached properties say, as it would
		// have been had the parent been built with it.
		if err := adopt(c, comp, a.nodes[child].attached); err != nil {
			return fmt.Errorf("insert: %w", err)
		}
	} else {
		c.Add(comp)
	}
	if at < n {
		c.Move(comp, at)
	}
	a.kids[parent] = insertKid(a.kids[parent], at, child)
	a.paletteAdopt(parent, child)
	return nil
}

// withAdopt sets how a built-in container takes a child inserted by a reload.
func withAdopt(typeName string, fn adopter) Option {
	return func(a *Adapter) { a.adopters[typeName] = fn }
}

// RemoveChild detaches child from parent, unmounting its subtree.
func (a *Adapter) RemoveChild(parent, child decl.NodeID) error {
	if r, ok := a.arrangerAndChild(parent, child); ok {
		a.kids[parent] = removeKid(a.kids[parent], child)
		a.paletteRelease(child)
		return a.rearrange(parent, r)
	}
	c, comp, err := a.containerAndChild("remove", parent, child)
	if err != nil {
		return err
	}
	c.Remove(comp)
	a.kids[parent] = removeKid(a.kids[parent], child)
	a.paletteRelease(child)
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
	if r, ok := a.arrangerAndChild(parent, child); ok {
		if n := len(a.kids[parent]); to < 0 || to >= n {
			return fmt.Errorf("move: index %d is out of range for %d children of node %d", to, n, parent)
		}
		a.kids[parent] = insertKid(removeKid(a.kids[parent], child), to, child)
		return a.rearrange(parent, r)
	}
	c, comp, err := a.containerAndChild("move", parent, child)
	if err != nil {
		return err
	}
	if n := countChildren(c); to < 0 || to >= n {
		return fmt.Errorf("move: index %d is out of range for %d children of node %d", to, n, parent)
	}
	c.Move(comp, to)
	a.kids[parent] = insertKid(removeKid(a.kids[parent], child), to, child)
	return nil
}

// insertKid places id at index at, which the caller has checked is in range.
func insertKid(kids []decl.NodeID, at int, id decl.NodeID) []decl.NodeID {
	out := make([]decl.NodeID, 0, len(kids)+1)
	out = append(out, kids[:at]...)
	out = append(out, id)
	return append(out, kids[at:]...)
}

// removeKid splices id out IN PLACE. A copy per removal made tearing down a
// node of n children quadratic in allocation (measured: 57% of a 1000-node
// mount-and-destroy's memory); the adapter holds the only reference to each
// list, so splicing its backing array is safe.
func removeKid(kids []decl.NodeID, id decl.NodeID) []decl.NodeID {
	for i, k := range kids {
		if k == id {
			return append(kids[:i], kids[i+1:]...)
		}
	}
	return kids
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
	if _, ok := paletteRoles[prop]; ok {
		return decl.PropRuntime
	}
	// `visible` is every type's, as Qt's Item.visible is — see setVisible.
	if prop == visibleProp {
		return decl.PropRuntime
	}
	if a.ctorProps[typeName][prop] {
		return decl.PropConstructorOnly
	}
	// An attached property is read by the PARENT at the parent's construction,
	// which from this node's side is a constructor-only property: there is no
	// setter to apply a change through. Whether this parent honours it is a
	// separate question, answered by CheckAttached during planning.
	if a.isAttached(prop) {
		return decl.PropConstructorOnly
	}
	// `focus` is the adapter's, for every type — see takeFocus.
	if prop == "focus" {
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
	// Derived from the enums the vocabulary accepts, so a document can name
	// exactly the values a builder takes — no more, and never a spelling the
	// builder would then refuse.
	out := tuiConstants(tuiEnums, tuiFlags)
	for _, e := range a.enums {
		for _, n := range e.Values {
			out[e.Scope+"."+n] = strValue(enumConstant(e.Scope, n))
		}
	}
	return out
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
		Exports: a.exports(),
	}}
}

// tuiExports are the singletons `import tui` brings into scope: Tui for the
// enums, and one per flag set, named for the type it belongs to.
func tuiExports() []string {
	out := []string{"Tui"}
	for _, f := range tuiFlags {
		out = append(out, f.singleton)
	}
	return out
}
