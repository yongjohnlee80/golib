package decl

import (
	"errors"
	"fmt"
	"maps"
	"slices"
)

// STYLES, as Qt Quick Controls has them: a set of types that replaces standard types by name, so
// a document writing `Editor { }` gets the style's Editor and the same document runs unchanged
// under any style. One style per Program, as Qt has one per application. A name the style does
// not define stays standard; a NEW type is still added with [Types], where a duplicate name is
// refused.

// Style replaces standard vocabulary types by name.
type Style struct {
	Name  string // for diagnostics: "native"
	Types []Type // each Name must be a standard type's, and that type Replaceable
}

var (
	// ErrUnknownStyleType is a style type whose name is not a standard type's.
	ErrUnknownStyleType = errors.New("tui/decl: a style type replaces a standard type, and this name is none")
	// ErrStyleTypeNotReplaceable is a style type for a standard type a public Type cannot stand
	// in for: a container that places its children by their attached properties.
	ErrStyleTypeNotReplaceable = errors.New("tui/decl: this standard type cannot be replaced by a style")
)

// WithStyle chooses the Program's style. It cannot be combined with [WithRegistry], which
// replaces the whole vocabulary a style substitutes into.
func WithStyle(s Style) ProgramOption {
	return func(c *programConfig) { c.style = &s }
}

// Replaceable reports whether a style may replace the standard type name. A container whose
// children are placed by their attached properties (Flex's Layout.fillHeight, say) is not: the
// hook that reads them is the vocabulary's own, and a public Type has no way to carry it.
func Replaceable(name string) bool {
	for _, t := range stdTypes() {
		if t.Name == name {
			return t.adopt == nil
		}
	}
	return false
}

// styledTypes is the standard vocabulary with s's types substituted by name, or why s cannot
// be used.
func styledTypes(s Style) ([]Type, error) {
	types := stdTypes()
	at := make(map[string]int, len(types))
	for i, t := range types {
		at[t.Name] = i
	}
	seen := map[string]bool{}
	for _, t := range s.Types {
		i, ok := at[t.Name]
		switch {
		case !ok:
			return nil, fmt.Errorf("%w: style %q, type %q", ErrUnknownStyleType, s.Name, t.Name)
		case types[i].adopt != nil:
			return nil, fmt.Errorf("%w: style %q, type %q", ErrStyleTypeNotReplaceable, s.Name, t.Name)
		case seen[t.Name]:
			return nil, fmt.Errorf("tui/decl: style %q replaces %q twice", s.Name, t.Name)
		case t.Build == nil:
			return nil, fmt.Errorf("tui/decl: style %q, type %q has no Build", s.Name, t.Name)
		}
		seen[t.Name] = true
		types[i] = t
	}
	return types, nil
}

// Contract is a type's QML API: what a document can write on it and call on it. Two types with
// equal contracts take the same documents. Every list is sorted.
type Contract struct {
	Ctor, Setters, Methods, Getters []string
	Signals                         map[string][]string // signals declared with parameters
	Enums                           []string            // "Scope.Value"
}

// Contract is t's QML API.
func (t Type) Contract() Contract {
	c := Contract{
		Ctor:    slices.Sorted(slices.Values(t.Ctor)),
		Setters: slices.Sorted(maps.Keys(t.Setters)),
		Methods: slices.Sorted(maps.Keys(t.Methods)),
		Getters: slices.Sorted(maps.Keys(t.Getters)),
		Signals: maps.Clone(t.Signals),
	}
	for _, e := range t.Enums {
		for _, v := range e.Values {
			c.Enums = append(c.Enums, enumConstant(e.Scope, v))
		}
	}
	slices.Sort(c.Enums)
	return c
}

// StandardContract is the standard type name's QML API, and whether there is such a type.
func StandardContract(name string) (Contract, bool) {
	for _, t := range stdTypes() {
		if t.Name == name {
			return t.Contract(), true
		}
	}
	return Contract{}, false
}

// StandardType is a copy of the standard type name, for a style to start from: its contract,
// builder and setters. The vocabulary's private palette and child hooks are left out, since they
// reach into the standard widget; a style's type sets Restyle for its own.
func StandardType(name string) (Type, bool) {
	for _, t := range stdTypes() {
		if t.Name == name {
			t.restyle, t.adopt = nil, nil
			t.Setters = maps.Clone(t.Setters)
			t.Methods = maps.Clone(t.Methods)
			t.Getters = maps.Clone(t.Getters)
			t.Signals = maps.Clone(t.Signals)
			t.Ctor = slices.Clone(t.Ctor)
			t.Enums = slices.Clone(t.Enums)
			return t, true
		}
	}
	return Type{}, false
}
