package collections

// Functional slice operations. The canonical forms mirror the shapes common
// across the Go ecosystem (and the stdlib slices package): element-only
// callbacks, with -Indexed variants when the position matters. Inspired by
// github.com/cyc-ttn/go-collections, whose accumulated-results callback
// signatures these replace.

// Map transforms a slice of S into a slice of T by applying fn to every
// element.
func Map[S, T any](source []S, fn func(S) T) []T {
	result := make([]T, 0, len(source))
	for _, s := range source {
		result = append(result, fn(s))
	}
	return result
}

// MapIndexed transforms a slice of S into a slice of T. fn receives each
// element and its index and reports the mapped value along with whether to
// include it, so it can map and filter in one pass.
func MapIndexed[S, T any](source []S, fn func(s S, idx int) (T, bool)) []T {
	result := make([]T, 0, len(source))
	for i, s := range source {
		if v, ok := fn(s, i); ok {
			result = append(result, v)
		}
	}
	return result
}

// Filter returns a new slice containing only the elements for which fn
// returns true.
func Filter[S any](source []S, fn func(S) bool) []S {
	result := make([]S, 0, len(source))
	for _, s := range source {
		if fn(s) {
			result = append(result, s)
		}
	}
	return result
}

// FilterIndexed returns a new slice containing only the elements for which fn
// returns true; fn also receives the element's index.
func FilterIndexed[S any](source []S, fn func(s S, idx int) bool) []S {
	result := make([]S, 0, len(source))
	for i, s := range source {
		if fn(s, i) {
			result = append(result, s)
		}
	}
	return result
}

// Reduce folds a slice of S into a single value of A, starting from init and
// applying fn left to right.
func Reduce[S, A any](source []S, init A, fn func(acc A, s S) A) A {
	acc := init
	for _, s := range source {
		acc = fn(acc, s)
	}
	return acc
}
