package tui

// Keyer is an optional capability interface discovered at runtime via type assertion
// on the outer component value (similar to Focusable).
//
// Keyed containers (such as dynamic lists, table rows, and tab bars) consult Keyer
// to preserve a component across element reordering: matching keys preserve the
// existing mount, allowing repositioning via Container.Move rather than a disruptive
// Remove+Add cycle. As a result, the component retains its NodeID, lifetime Context,
// in-flight background tasks, timer registrations, and active focus state without
// re-running Init.
//
// IDENTITY CONTRACT: The returned key MUST be non-nil, comparable (as it serves as
// an internal map key), and stable across the component's mounted lifetime. A component
// that does not implement Keyer relies on positional identity (its index in the container).
// AsKey is a helper that statically asserts comparability and returns the value as an any.
type Keyer interface {
	// Key returns this component's stable identity within a keyed container.
	Key() any
}

// AsKey statically asserts that the provided value is comparable, returning it as an any.
func AsKey[K comparable](v K) any {
	return v
}
