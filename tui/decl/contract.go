package decl

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
)

// THE WIDGET CONTRACT, AND THE PRIMITIVES EVERY TYPE IS BUILT FROM.
//
// A type's whole contract lives in ONE widgetType value: its name, its builder,
// which properties it takes only at construction, and a setter for each it can
// take at runtime. Registration and the adapter's property tables are DERIVED
// from those values, so adding a type is one new value in a table — nothing
// that already exists is edited, and a type cannot be registered with a
// builder in one place and forgotten in the property tables in another.
//
// The primitives below are the three shapes every property takes — read a
// value, read it into a builder's local, apply it through a setter — written
// once. A new property is a line that names which shape it is.

// widgetType is one widget type's complete contract.
type widgetType struct {
	name  string
	build Builder
	// ctor are the properties the builder consumes and no setter can change.
	ctor []string
	// setters are the properties that can change after construction.
	setters map[string]Setter
}

// registerTypes adds each type's builder to the registry.
func registerTypes(r *Registry, types []widgetType) {
	for _, w := range types {
		Register(r, w.name, w.build)
	}
}

// typeOptions derives the adapter's property contract from the same values the
// registry was built from.
func typeOptions(types []widgetType) []Option {
	var opts []Option
	for _, w := range types {
		if len(w.ctor) > 0 {
			opts = append(opts, WithConstructorProps(w.name, w.ctor...))
		}
		if len(w.setters) > 0 {
			opts = append(opts, WithSetters(w.name, w.setters))
		}
	}
	return opts
}

// ---------------------------------------------------------------- values

// reader turns one property value into a Go value, or says why it cannot.
type reader[V any] func(qml.SpecValue) (V, error)

func stringOf(v qml.SpecValue) (string, error) {
	if v.Kind != qml.SpecValueString {
		return "", fmt.Errorf("want a string, got %s (at %s)", v.Kind, v.Pos)
	}
	return v.Raw, nil
}

func boolOf(v qml.SpecValue) (bool, error) {
	if v.Kind != qml.SpecValueBool {
		return false, fmt.Errorf("want a bool, got %s (at %s)", v.Kind, v.Pos)
	}
	return v.Raw == "true", nil
}

// strValue is a string property value, for the adapter's own tables.
func strValue(s string) qml.SpecValue { return qml.SpecValue{Kind: qml.SpecValueString, Raw: s} }

// ---------------------------------------------------------------- setters

// setter is the one shape of a runtime property: find the widget, read the
// value, apply it. what names the widget for the diagnostic when the component
// is not the type the property belongs to.
func setter[W any, V any](what string, read reader[V], apply func(W, V)) Setter {
	return func(c tui.Component, v qml.SpecValue) error {
		w, ok := any(c).(W)
		if !ok {
			return fmt.Errorf("not %s", what)
		}
		val, err := read(v)
		if err != nil {
			return err
		}
		apply(w, val)
		return nil
	}
}

// ---------------------------------------------------------------- builders

// field reads one constructor property into a builder's local.
type field func(qml.SpecValue) error

// into makes a field that stores what read produces in dst.
func into[V any](dst *V, read reader[V]) field {
	return func(v qml.SpecValue) error {
		x, err := read(v)
		if err != nil {
			return err
		}
		*dst = x
		return nil
	}
}

// readProps reads a builder's constructor properties by name and reports the
// ones it read, which is what the builder must return as CONSUMED.
//
// Reporting what was consumed is not bookkeeping — the engine applies only the
// rest — and deriving it from the same table that did the reading means a
// builder cannot read a property and forget to say so. That exact omission
// once made the engine apply a label a second time.
func readProps(props []qml.SpecProp, fields map[string]field) ([]string, error) {
	var consumed []string
	for _, p := range props {
		f, ok := fields[p.Name]
		if !ok {
			continue
		}
		if err := f(p.Value); err != nil {
			return nil, err
		}
		consumed = append(consumed, p.Name)
	}
	return consumed, nil
}

// ---------------------------------------------------------------- enums

// enum is a closed set of symbolic values one property accepts, keyed by the
// name a document writes after `Tui.` — "Horizontal" for `Tui.Horizontal`.
//
// The Tui singleton's constants are DERIVED from every enum in [tuiEnums], so
// the spelling a document writes and the value a builder accepts come from the
// same entry and cannot drift apart. Adding a value is one entry.
type enum[T any] struct {
	// prop names the property, for diagnostics.
	prop   string
	values map[string]T
}

// constantValue is what a `Tui.<Name>` constant resolves to: the name in lower
// case. It is the ONE place that relation is stated — both the constant table
// and every enum's reader go through it.
func constantValue(name string) string { return strings.ToLower(name) }

func (e enum[T]) read(v qml.SpecValue) (T, error) {
	var zero T
	if v.Kind != qml.SpecValueString {
		return zero, fmt.Errorf("%s must be written as a string, got %s (at %s)", e.prop, v.Kind, v.Pos)
	}
	for name, val := range e.values {
		if constantValue(name) == v.Raw {
			return val, nil
		}
	}
	return zero, fmt.Errorf("%s must be %s, got %q (at %s)", e.prop, e.spelling(), v.Raw, v.Pos)
}

func (e enum[T]) names() []string {
	names := make([]string, 0, len(e.values))
	for n := range e.values {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (e enum[T]) spelling() string {
	names := e.names()
	for i, n := range names {
		names[i] = "Tui." + n
	}
	switch len(names) {
	case 0:
		return "nothing"
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

// enumeration is what the constant table needs from an enum, whatever its
// value type.
type enumeration interface{ names() []string }

// tuiConstants derives the Tui singleton's qualified names from every enum.
func tuiConstants(enums []enumeration) map[string]qml.SpecValue {
	out := map[string]qml.SpecValue{}
	for _, e := range enums {
		for _, n := range e.names() {
			out["Tui."+n] = strValue(constantValue(n))
		}
	}
	return out
}
