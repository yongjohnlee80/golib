package decl

import (
	"fmt"

	"github.com/yongjohnlee80/golib/decl"
	"github.com/yongjohnlee80/golib/parse/qml"
)

// UNIVERSAL — what every item has, whatever its type, as Qt's Item has it: the palette roles
// (propagate.go), `visible` (visible.go), `toolTipText` (tooltip.go), `focus` (takeFocus),
// `keyScope` (read only) and `forceActiveFocus()` (focus.go).
//
// They are the adapter's, not any type's, so every adapter hook that answers for a property or a
// method asks these tables: ClassifyProperty, Apply, Resettable and Reset, ReadablesOf and
// ReadProperty, MethodsOf and Invoke. A new every-item property is one entry here.

// universalProp is what the adapter does with one universal property. A nil hook means "not this":
// a property with no set is not applied at run time, one with no reset cannot be reset, and one with
// no read is not readable.
type universalProp struct {
	// kind is ClassifyProperty's answer for a type with no setter of its own for the property.
	kind decl.PropertyKind
	// set applies a value to a built node (Apply), before the type's own setters are asked.
	set func(a *Adapter, b built, id decl.NodeID, prop string, v qml.SpecValue) error
	// reset returns a node to what one without the property has, when a reload removes it (Reset).
	reset func(a *Adapter, id decl.NodeID, prop string) error
	// read is the property's value (ReadProperty), for a type with no getter of its own for it.
	read func(a *Adapter, b built) (qml.SpecValue, error)
}

// focusProp is every item's `focus`: where the keyboard starts, taken at construction (takeFocus).
const focusProp = "focus"

// universalProps are the universal properties by name. The palette roles are a family, not names:
// see paletteProp.
var universalProps = map[string]universalProp{
	visibleProp: {kind: decl.PropRuntime,
		set: func(_ *Adapter, b built, _ decl.NodeID, _ string, v qml.SpecValue) error {
			return setVisible(b.typ, b.comp, v)
		},
		// removed from the document: shown again, as an item with no `visible` is
		reset: func(a *Adapter, id decl.NodeID, _ string) error {
			b, ok := a.nodes[id]
			if !ok {
				return fmt.Errorf("visible: node %d has no component", id)
			}
			return setVisible(b.typ, b.comp, boolValue(true))
		}},
	toolTipProp: {kind: decl.PropRuntime,
		set: func(_ *Adapter, b built, _ decl.NodeID, _ string, v qml.SpecValue) error {
			return setToolTip(b.typ, b.comp, v)
		},
		// removed from the document: no tooltip, as an item with no toolTipText has
		reset: func(a *Adapter, id decl.NodeID, _ string) error {
			b, ok := a.nodes[id]
			if !ok {
				return fmt.Errorf("%s: node %d has no component", toolTipProp, id)
			}
			return setToolTip(b.typ, b.comp, strValue(""))
		}},
	// read only: a type that writes its scope (FocusScope, Dialog, Drawer) declares keyScope itself
	keyScopeProp: {read: readKeyScope},
	focusProp:    {kind: decl.PropConstructorOnly},
}

// paletteProp is the entry of a palette or syntax role: universal, because the roles propagate to
// the children whether or not a type wears them. A misspelt role is still the palette's, so Apply
// refuses it by name (roleOf), but it is no property: ClassifyProperty does not know it, and it
// cannot be reset.
func paletteProp(prop string) universalProp {
	u := universalProp{set: func(a *Adapter, _ built, id decl.NodeID, prop string, v qml.SpecValue) error {
		return a.setRole(id, prop, v)
	}}
	if _, known := paletteRoles[prop]; known {
		u.kind, u.reset = decl.PropRuntime, (*Adapter).resetRole
	}
	return u
}

// universalOf is a universal property's entry, and whether prop is one.
func universalOf(prop string) (universalProp, bool) {
	if u, ok := universalProps[prop]; ok {
		return u, true
	}
	if isPaletteProp(prop) {
		return paletteProp(prop), true
	}
	return universalProp{}, false
}

// atBuild is what Create takes out of a declaration's properties before its builder sees them: the
// universal properties read at construction.
type atBuild struct {
	// focus is `focus: true`: the node is its subtree's focus nominee.
	focus bool
	// declaredFocus is whether `focus` was written at all, true or false.
	declaredFocus bool
	// palette is the node's own roles, which propagate to its children (propagate.go).
	palette palette
	// roles are the role properties it declared.
	roles []string
}

// takeUniversal takes the universal properties read at construction out of props: focus
// (takeFocus) and the palette roles (takePalette).
func takeUniversal(props []qml.SpecProp) ([]qml.SpecProp, atBuild, error) {
	own, focus, declared, err := takeFocus(props)
	if err != nil {
		return nil, atBuild{}, err
	}
	pal, own, roles, err := takePalette(own)
	if err != nil {
		return nil, atBuild{}, err
	}
	return own, atBuild{focus: focus, declaredFocus: declared, palette: pal, roles: roles}, nil
}

// consumed are the properties Create reports consumed for these: the roles, and focus when it was
// declared. A false focus is consumed too: the engine would otherwise apply it, and nothing sets
// focus.
func (t atBuild) consumed() []string {
	if t.declaredFocus {
		return append(t.roles, focusProp)
	}
	return t.roles
}

// keyScopeProp is every item's readable key-map scope: its own, or the nearest enclosing one's, read
// live from the App (tui.App.KeyScopeOf), so an Editor's follows its mode. "" before the App runs.
const keyScopeProp = "keyScope"

func readKeyScope(a *Adapter, b built) (qml.SpecValue, error) {
	scope := ""
	if a.app != nil {
		scope = string(a.app.KeyScopeOf(b.comp))
	}
	return strValue(scope), nil
}

// universalMethods are the methods every item has, by name. A type's own method of the name wins.
var universalMethods = map[string]func(a *Adapter, b built, args []qml.SpecValue) error{
	forceActiveFocus: (*Adapter).forceActiveFocus,
}
