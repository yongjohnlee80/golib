package threadsafe

// Value provides thread-safe access to a value of generic type T.
// It ensures safe concurrent access and modification of the underlying value.
//
// Usage:
//
//	v := NewSynchronizedValue(0)
//	v.Set(10)
//	v.Set(v.Get() + 1) // WRONG! Another goroutine may have changed the value between Get and Set: data corruption.
//
//	v.Update(func(v int) int { return v + 1 }) // OK
//
//	x := NewSynchronizedValue(map[string]bool{})
//	foo := x.Get()["foo"] // WRONG! Indexing may lead to race condition, if another goroutine modifies the map concurrently.
//
//	var foo bool
//	x.RDo(func(m map[string]bool) { foo = m["foo"] })     // OK (read under the lock)
//	x.Do(func(m *map[string]bool) { (*m)["bar"] = true }) // OK (mutation under the lock)
//
// See also: NewSynchronizedValue, NewMultiReadSyncValue, NewAtomicValue
type Value[T any] interface {
	// Get retrieves the current value for read-only purposes.
	// Do not attempt to modify the returned value with Set or Update methods.
	// If side effects are expected, use Update instead.
	// If indexing is required on the returned value such as map or slice, use
	// RDo (reads) or Do (mutation) so the access happens under the lock.
	Get() T

	// Set updates the value safely in a concurrent environment.
	// It replaces the current value with the provided one.
	// Use Update if the new value depends on the current value.
	Set(T)

	// Update atomically modifies the stored value using the provided function,
	// ensuring thread-safe read-modify-write access.
	Update(func(T) T)

	// Do runs fn with mutable access to the stored value under the
	// implementation's exclusive lock. Mutations through the pointer are
	// visible to subsequent readers. fn must not call back into this Value
	// (self-deadlock) and must not retain the pointer after returning.
	//
	// AtomicValue implements Do with a compare-and-swap retry loop, so there
	// fn may run more than once and must be free of side effects.
	Do(fn func(*T))

	// RDo runs fn with read access to the stored value under the
	// implementation's read (or exclusive) lock. fn must not mutate state
	// reachable from the value, must not call back into this Value, and must
	// not retain references after returning.
	RDo(fn func(T))
}
