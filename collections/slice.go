package collections

// Functional slice operations inspired by github.com/cyc-ttn/go-collections.

// Map transforms a slice of S into a slice of T. The callback receives the
// accumulated results so far, the current element, and its index, and returns
// the mapped value along with a bool indicating whether to include it.
func Map[T any, S any](source []S, fn func(agg []T, s S, idx int) (T, bool)) []T {
	result := make([]T, 0, len(source))
	for i, s := range source {
		if v, ok := fn(result, s, i); ok {
			result = append(result, v)
		}
	}
	return result
}

// Filter returns a new slice containing only the elements for which fn
// returns true. The callback receives the accumulated results so far,
// the current element, and its index.
func Filter[S any](source []S, fn func(agg []S, s S, idx int) bool) []S {
	result := make([]S, 0, len(source))
	for i, s := range source {
		if fn(result, s, i) {
			result = append(result, s)
		}
	}
	return result
}

// Reduce folds a slice of S into a single value of T. The callback receives
// the accumulated results slice, the current accumulator, the current element,
// and its index, and returns the updated accumulator. The initial accumulator
// is the zero value of T.
func Reduce[T any, S any](source []S, fn func(agg []T, acc T, s S, idx int) T) T {
	var acc T
	agg := make([]T, 0, len(source))
	for i, s := range source {
		acc = fn(agg, acc, s, i)
		agg = append(agg, acc)
	}
	return acc
}
