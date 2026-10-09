package states

import "github.com/yongjohnlee80/golib/highlight"

type entry[T any] struct {
	value T
	key   string
	refs  int
}

// Store interns values without recycling identifiers. It is single-owner.
type Store[T any] struct {
	key    func(T) string
	add    func(T)
	drop   func(T)
	values map[highlight.State]*entry[T]
	keys   map[string]highlight.State
	queue  []highlight.State
	head   int
	next   highlight.State
}

// Option configures ownership of dependencies in a stored value.
type Option[T any] func(*Store[T])

// WithDependencies retains dependencies when a value is interned and releases
// them when it is collected, not when an individual external lease is dropped.
func WithDependencies[T any](add, drop func(T)) Option[T] {
	return func(s *Store[T]) { s.add, s.drop = add, drop }
}

// New constructs a store whose key describes the complete immutable value.
func New[T any](key func(T) string, opts ...Option[T]) *Store[T] {
	s := &Store[T]{key: key, values: map[highlight.State]*entry[T]{}, keys: map[string]highlight.State{}}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Intern returns the canonical live ID. Newly unleased values are queued too,
// so scratch/provisional outputs cannot escape collection.
func (s *Store[T]) Intern(value T) highlight.State {
	key := s.key(value)
	if id, ok := s.keys[key]; ok {
		return id
	}
	s.next++
	id := s.next
	s.values[id] = &entry[T]{value: value, key: key}
	s.keys[key] = id
	s.queue = append(s.queue, id)
	if s.add != nil {
		s.add(value)
	}
	return id
}

// Get returns an immutable value; zero represents its zero value.
func (s *Store[T]) Get(id highlight.State) (T, bool) {
	if id == 0 {
		var zero T
		return zero, true
	}
	if e, ok := s.values[id]; ok {
		return e.value, true
	}
	var zero T
	return zero, false
}

// Retain adds a root lease.
func (s *Store[T]) Retain(id highlight.State) {
	if e := s.values[id]; e != nil {
		e.refs++
	}
}

// Release drops a lease and queues reclamation without walking dependencies.
func (s *Store[T]) Release(id highlight.State) {
	if e := s.values[id]; e != nil && e.refs > 0 {
		e.refs--
		if e.refs == 0 {
			s.queue = append(s.queue, id)
		}
	}
}

// Collect implements highlight.StateStore.
func (s *Store[T]) Collect(budget int) bool {
	_, more := s.CollectNodes(budget)
	return more
}

// CollectNodes visits at most budget nodes, including dead/retained queue slots.
func (s *Store[T]) CollectNodes(budget int) (int, bool) {
	visited := 0
	for s.head < len(s.queue) && visited < budget {
		id := s.queue[s.head]
		s.head++
		visited++
		if e := s.values[id]; e != nil && e.refs == 0 {
			delete(s.values, id)
			delete(s.keys, e.key)
			if s.drop != nil {
				s.drop(e.value)
			}
		}
	}
	more := s.head < len(s.queue)
	if !more {
		s.queue, s.head = nil, 0
	}
	return visited, more
}

// Len is the number of currently retained tuples, for retention measurements.
func (s *Store[T]) Len() int { return len(s.values) }
