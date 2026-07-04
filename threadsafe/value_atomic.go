package threadsafe

import "sync/atomic"

// NewAtomicValue creates a new AtomicValue initialized with the given value.
// Reads are lock-free; prefer it for read-dominated hot paths where even a
// read lock shows up in profiles.
func NewAtomicValue[T any](v T) *AtomicValue[T] {
	a := &AtomicValue[T]{}
	a.p.Store(&v)
	return a
}

// AtomicValue provides thread-safe access to a value via an atomic pointer to
// an immutable snapshot. Get and RDo never lock. Update and Do run their
// function in a compare-and-swap retry loop: under contention the function may
// execute more than once, so it must be pure (no side effects). Values read
// through Get/RDo are shared snapshots — treat them as immutable.
type AtomicValue[T any] struct {
	p atomic.Pointer[T]
}

// compile-time: *AtomicValue satisfies Value.
var _ Value[int] = (*AtomicValue[int])(nil)

// Get returns the current snapshot. Lock-free.
func (a *AtomicValue[T]) Get() T { return *a.p.Load() }

// Set replaces the value with a new snapshot.
func (a *AtomicValue[T]) Set(v T) { a.p.Store(&v) }

// Update atomically replaces the value with fn(current) using a
// compare-and-swap loop. fn may run more than once under contention and must
// be pure.
func (a *AtomicValue[T]) Update(fn func(T) T) {
	for {
		old := a.p.Load()
		next := fn(*old)
		if a.p.CompareAndSwap(old, &next) {
			return
		}
	}
}

// Do runs fn on a copy of the current value and installs the mutated copy with
// a compare-and-swap loop. fn may run more than once under contention and must
// be pure. Unlike the lock-based implementations, mutations apply to the copy,
// never in place — retained references to earlier snapshots are unaffected.
func (a *AtomicValue[T]) Do(fn func(*T)) {
	for {
		old := a.p.Load()
		next := *old
		fn(&next)
		if a.p.CompareAndSwap(old, &next) {
			return
		}
	}
}

// RDo runs fn with the current snapshot. Lock-free; fn must not mutate state
// reachable from the value.
func (a *AtomicValue[T]) RDo(fn func(T)) { fn(*a.p.Load()) }
