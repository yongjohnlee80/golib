package collections

// Set is a generic unordered collection of unique elements backed by a map.
type Set[T comparable] map[T]struct{}

// NewSet creates a Set pre-populated with the given values.
func NewSet[T comparable](values ...T) Set[T] {
	s := make(Set[T], len(values))
	for _, v := range values {
		s[v] = struct{}{}
	}
	return s
}

// Add inserts one or more values into the set.
func (s Set[T]) Add(values ...T) {
	for _, v := range values {
		s[v] = struct{}{}
	}
}

// Remove deletes one or more values from the set.
func (s Set[T]) Remove(values ...T) {
	for _, v := range values {
		delete(s, v)
	}
}

// Has reports whether v is in the set.
func (s Set[T]) Has(v T) bool {
	_, ok := s[v]
	return ok
}

// Len returns the number of elements.
func (s Set[T]) Len() int {
	return len(s)
}

// Values returns all elements as a slice in indeterminate order.
func (s Set[T]) Values() []T {
	out := make([]T, 0, len(s))
	for v := range s {
		out = append(out, v)
	}
	return out
}

// Clone returns a shallow copy.
func (s Set[T]) Clone() Set[T] {
	c := make(Set[T], len(s))
	for v := range s {
		c[v] = struct{}{}
	}
	return c
}

// Union returns a new set containing all elements from both sets.
func (s Set[T]) Union(other Set[T]) Set[T] {
	// Start from the larger set to minimize insertions.
	big, small := s, other
	if len(small) > len(big) {
		big, small = small, big
	}
	result := big.Clone()
	for v := range small {
		result[v] = struct{}{}
	}
	return result
}

// Intersect returns a new set containing only elements present in both sets.
func (s Set[T]) Intersect(other Set[T]) Set[T] {
	// Iterate the smaller set for fewer lookups.
	small, big := s, other
	if len(big) < len(small) {
		small, big = big, small
	}
	result := make(Set[T])
	for v := range small {
		if _, ok := big[v]; ok {
			result[v] = struct{}{}
		}
	}
	return result
}

// Diff returns a new set containing elements in s that are not in other.
func (s Set[T]) Diff(other Set[T]) Set[T] {
	result := make(Set[T])
	for v := range s {
		if _, ok := other[v]; !ok {
			result[v] = struct{}{}
		}
	}
	return result
}

// SymmetricDiff returns a new set containing elements in either set but not both.
func (s Set[T]) SymmetricDiff(other Set[T]) Set[T] {
	result := make(Set[T])
	for v := range s {
		if _, ok := other[v]; !ok {
			result[v] = struct{}{}
		}
	}
	for v := range other {
		if _, ok := s[v]; !ok {
			result[v] = struct{}{}
		}
	}
	return result
}

// SubsetOf reports whether every element in s is also in other.
func (s Set[T]) SubsetOf(other Set[T]) bool {
	if len(s) > len(other) {
		return false
	}
	for v := range s {
		if _, ok := other[v]; !ok {
			return false
		}
	}
	return true
}

// SupersetOf reports whether s contains every element in other.
func (s Set[T]) SupersetOf(other Set[T]) bool {
	return other.SubsetOf(s)
}

// Equal reports whether s and other contain exactly the same elements.
func (s Set[T]) Equal(other Set[T]) bool {
	return len(s) == len(other) && s.SubsetOf(other)
}
