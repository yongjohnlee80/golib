package decl

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/parse/qml"
	"github.com/yongjohnlee80/golib/tui"
)

// THE WIDGET CONTRACT, AND THE PRIMITIVES EVERY TYPE IS BUILT FROM.
//
// A type's whole contract lives in ONE Type value: its name, its builder,
// which properties it takes only at construction, and a setter for each it can
// take at runtime. Registration and the adapter's property tables are DERIVED
// from those values, so adding a type is one new value in a table — nothing
// that already exists is edited, and a type cannot be registered with a
// builder in one place and forgotten in the property tables in another.
//
// The primitives below are the three shapes every property takes — read a
// value, read it into a builder's local, apply it through a setter — written
// once. A new property is a line that names which shape it is.

// Type is one widget type's complete contract: everything QML can do with it,
// in ONE value. The standard vocabulary is a table of these, and a consumer's
// own widget is one more — added with [WithTypes], through the same code, so a
// custom widget binds, reloads and signals exactly as a built-in one does.
type Type struct {
	// Name is what a document writes: `Gauge { }`. Upper-case, as QML types are.
	Name string
	// Build constructs the widget from a declaration.
	Build Builder
	// Ctor are the properties Build consumes and no setter can change. A
	// reload that changes one rebuilds the node.
	Ctor []string
	// Setters are the properties that can change after construction — the ones
	// a document can bind to a source.
	Setters map[string]Setter
	// Methods are what a handler can call on a node of this type by its id:
	// `gauge.reset()`.
	Methods map[string]Method
	// Signals names each signal's parameters, in the order it is raised with
	// them: `accepted(selectedFile)`. A signal with none need not be listed.
	Signals map[string][]string
	// Destroyed runs when a node of this type is destroyed, with the widget
	// Build made: a reload dropped it, or the tree was torn down. For a widget
	// holding something to release — a process, a timer, a subscription.
	Destroyed func(tui.Component)

	// restyle is how a built-in type wears an effective palette. Unexported:
	// a consumer's type does not take palette roles yet; its subtree still
	// inherits through it.
	restyle restyler
}

// registerTypes adds each type's builder to the registry.
func registerTypes(r *Registry, types []Type) {
	for _, w := range types {
		Register(r, w.Name, w.Build)
	}
}

// typeOptions derives the adapter's property contract from the same values the
// registry was built from.
func typeOptions(types []Type) []Option {
	var opts []Option
	for _, w := range types {
		if len(w.Ctor) > 0 {
			opts = append(opts, WithConstructorProps(w.Name, w.Ctor...))
		}
		if len(w.Setters) > 0 {
			opts = append(opts, WithSetters(w.Name, w.Setters))
		}
		if len(w.Methods) > 0 {
			opts = append(opts, WithMethods(w.Name, w.Methods))
		}
		if len(w.Signals) > 0 {
			opts = append(opts, WithSignalParams(w.Name, w.Signals))
		}
		if w.Destroyed != nil {
			opts = append(opts, WithDestroyHook(w.Name, w.Destroyed))
		}
		if w.restyle != nil {
			opts = append(opts, withRestyle(w.Name, w.restyle))
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

// ---------------------------------------------------------------- methods

// Method runs one method a handler called on a node: `quitDialog.open()`.
type Method func(c tui.Component, args []qml.SpecValue) error

// method is the one shape of a method that takes no arguments: find the
// widget, refuse arguments it would ignore, run it.
func method[W any](what string, run func(W) error) Method {
	return func(c tui.Component, args []qml.SpecValue) error {
		w, ok := any(c).(W)
		if !ok {
			return fmt.Errorf("not %s", what)
		}
		if len(args) > 0 {
			return fmt.Errorf("takes no arguments, and was given %d", len(args))
		}
		return run(w)
	}
}

// ---------------------------------------------------------------- builders

// Field reads one constructor property into a builder's local. [ReadProps]
// takes a table of them; StringField and its siblings make one.
type Field func(qml.SpecValue) error

// field is the package's own spelling of Field.
type field = Field

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
			// The reader knows the value, not the property it was written
			// for — "want a bool, got string" alone does not say which.
			return nil, fmt.Errorf("%s: %w", p.Name, err)
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
	values map[string]T
}

// constantValue is what a `Tui.<Name>` constant resolves to: the name in lower
// case. It is the ONE place that relation is stated — both the constant table
// and every enum's reader go through it.
func constantValue(name string) string { return strings.ToLower(name) }

func (e enum[T]) read(v qml.SpecValue) (T, error) {
	var zero T
	if v.Kind != qml.SpecValueString {
		return zero, fmt.Errorf("must be written as a string, got %s (at %s)", v.Kind, v.Pos)
	}
	for name, val := range e.values {
		if constantValue(name) == v.Raw {
			return val, nil
		}
	}
	return zero, fmt.Errorf("must be %s, got %q (at %s)", e.spelling(), v.Raw, v.Pos)
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

// tuiConstants derives the Tui singleton's qualified names from every enum,
// and each flag set's under its own singleton.
func tuiConstants(enums []enumeration, sets []flagSet) map[string]qml.SpecValue {
	out := map[string]qml.SpecValue{}
	for _, e := range enums {
		for _, n := range e.names() {
			out["Tui."+n] = strValue(constantValue(n))
		}
	}
	for _, f := range sets {
		for n, bit := range f.values {
			out[f.singleton+"."+n] = qml.SpecValue{Kind: qml.SpecValueNumber, Raw: strconv.FormatInt(bit, 10)}
		}
	}
	return out
}

// ---------------------------------------------------------------- flags

// flagSet is a set of values a property combines with `|`, the way Qt writes
// them: `standardButtons: Dialog.Yes | Dialog.No`. They are qualified by their
// OWN singleton — the type they belong to, as in Qt — not by Tui.
type flagSet struct {
	// singleton is what a document writes before the dot: "Dialog".
	singleton string
	values    map[string]int64
}

// read returns the flags a value names, in the set's order, and refuses a bit
// the set does not have — a number from somewhere else is a mistake to name,
// not a set of buttons to guess at.
func (f flagSet) read(v qml.SpecValue) (int64, error) {
	if v.Kind != qml.SpecValueNumber {
		return 0, fmt.Errorf("must be %s flags, got %s (at %s)", f.singleton, v.Kind, v.Pos)
	}
	n, err := strconv.ParseInt(v.Raw, 0, 64)
	if err != nil {
		return 0, fmt.Errorf("must be %s flags, got %s (at %s)", f.singleton, v.Raw, v.Pos)
	}
	var known int64
	for _, bit := range f.values {
		known |= bit
	}
	if extra := n &^ known; extra != 0 {
		return 0, fmt.Errorf("%#x is not a %s flag (at %s)", extra, f.singleton, v.Pos)
	}
	return n, nil
}
