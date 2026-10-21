package tui

// Keyer is an optional capability interface discovered at runtime via type assertion
// on the outer component value (similar to Focusable).
//
// IDENTITY CONTRACT: The returned key MUST be non-nil, comparable (as it serves as
// an internal map key), and stable across the component's mounted lifetime.
//
// Deprecated: nothing in golib consults Keyer. A keyed container reconciles its
// children with [Reconcile], which takes the key as a function, so a component needs
// no method for it: pass func(c Component) any { return c.(Keyer).Key() } to keep
// using one. Keyer remains for source compatibility.
type Keyer interface {
	// Key returns this component's stable identity within a keyed container.
	Key() any
}

// AsKey statically asserts that the provided value is comparable, returning it as an any.
//
// Deprecated: it served [Keyer]. A [Reconcile] key function returns its key directly.
func AsKey[K comparable](v K) any {
	return v
}
