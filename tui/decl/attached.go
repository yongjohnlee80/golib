package decl

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// ATTACHED PROPERTIES.
//
// `Dock.edge: Tui.Top` is written on a child and read by its parent. QML tells
// an attached property from a grouped one by capitalisation — `Layout` names a
// type, `font` a sub-object — and so does this adapter.
//
// Two tables, kept apart on purpose: an ATTACHING SCHEMA says what `Dock.*`
// means, and a container says which schemas it HONOURS. Mixing them would mean
// every new container redeclaring every name it accepts, and two containers
// that honour one schema drifting apart on what its names are.

// attachingSchema is one attached vocabulary: `Dock` and the names under it.
type attachingSchema struct {
	names map[string]bool
}

// RegisterAttached declares an attaching schema and the members it has.
//
// Declaring the same schema twice is a programming mistake, for the same reason
// a duplicate type is: one declaration would silently win.
func RegisterAttached(r *Registry, schema string, names ...string) {
	if schema == "" || !unicode.IsUpper([]rune(schema)[0]) {
		panic("tui/decl.RegisterAttached: an attaching schema names a type and must be capitalised: " + schema)
	}
	if r.attached == nil {
		r.attached = map[string]attachingSchema{}
	}
	if _, dup := r.attached[schema]; dup {
		panic("tui/decl.RegisterAttached: " + schema + " is already declared")
	}
	s := attachingSchema{names: map[string]bool{}}
	for _, n := range names {
		s.names[n] = true
	}
	r.attached[schema] = s
}

// Honour declares that a container type reads an attaching schema from its
// children.
func Honour(r *Registry, container, schema string) {
	if _, ok := r.attached[schema]; !ok {
		panic("tui/decl.Honour: no attaching schema named " + schema + "; declare it with RegisterAttached first")
	}
	if r.honours == nil {
		r.honours = map[string]map[string]bool{}
	}
	if r.honours[container] == nil {
		r.honours[container] = map[string]bool{}
	}
	r.honours[container][schema] = true
}

// splitAttached reports the schema and member of a property written in the
// attached form, and false for anything else.
func splitAttached(prop string) (schema, member string, ok bool) {
	i := strings.IndexByte(prop, '.')
	if i <= 0 || i == len(prop)-1 {
		return "", "", false
	}
	if !unicode.IsUpper([]rune(prop)[0]) {
		return "", "", false // `font.bold` is grouped, not attached
	}
	return prop[:i], prop[i+1:], true
}

var _ decl.Attacher = (*Adapter)(nil)

// CheckAttached implements decl.Attacher.
func (a *Adapter) CheckAttached(parentType, childType, prop string) (bool, error) {
	schema, member, ok := splitAttached(prop)
	if !ok {
		return false, nil
	}
	s, known := a.reg.attached[schema]
	if !known {
		return true, fmt.Errorf("no attaching schema named %q; the adapter provides %s",
			schema, a.reg.attachedList())
	}
	if !s.names[member] {
		return true, fmt.Errorf("%q has no attached property %q", schema, member)
	}
	if parentType == "" {
		return true, fmt.Errorf("%s is written on the root node, which has no parent to read it", prop)
	}
	if !a.reg.honours[parentType][schema] {
		return true, fmt.Errorf("%s is set on a %s whose parent %s does not read %s.* properties",
			prop, childType, parentType, schema)
	}
	return true, nil
}

func (r *Registry) attachedList() string {
	if len(r.attached) == 0 {
		return "none"
	}
	names := make([]string, 0, len(r.attached))
	for n := range r.attached {
		names = append(names, n)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// isAttached reports whether a property is an attached one this adapter knows.
func (a *Adapter) isAttached(prop string) bool {
	schema, member, ok := splitAttached(prop)
	if !ok {
		return false
	}
	return a.reg.attached[schema].names[member]
}

// splitProps separates a node's attached properties from its own.
//
// The attached ones belong to the PARENT, so the node's builder never sees
// them, and they are reported as consumed so the engine does not try to apply
// them to a widget that has no such property.
func (a *Adapter) splitProps(props []qml.SpecProp) (own []qml.SpecProp, attached map[string]qml.SpecValue) {
	for _, p := range props {
		if a.isAttached(p.Name) {
			if attached == nil {
				attached = map[string]qml.SpecValue{}
			}
			attached[p.Name] = p.Value
			continue
		}
		own = append(own, p)
	}
	return own, attached
}

// Attached reports an attached property the i-th child set, and whether it set
// it. A container's builder calls this to read `Dock.edge` off its children.
func (b Build) Attached(i int, prop string) (qml.SpecValue, bool) {
	if i < 0 || i >= len(b.ChildAttached) {
		return qml.SpecValue{}, false
	}
	v, ok := b.ChildAttached[i][prop]
	return v, ok
}

// takeFocus removes QML's `focus` property from a node's own properties and
// reports its value.
//
// `focus: true` is understood for EVERY type, the way QML's Item.focus is: it
// says where the keyboard should start, which is a statement about the
// document rather than about any one widget. So no builder sees it, and the
// adapter carries the nominee up the tree to the container that owns the focus
// scope.
func takeFocus(props []qml.SpecProp) ([]qml.SpecProp, bool, error) {
	var own []qml.SpecProp
	focus := false
	for _, p := range props {
		if p.Name != "focus" {
			own = append(own, p)
			continue
		}
		on, err := boolOf(p.Value)
		if err != nil {
			return nil, false, err
		}
		focus = on
	}
	return own, focus, nil
}
