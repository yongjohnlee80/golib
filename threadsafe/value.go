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
//	m := x.Lock()
//	foo := m["foo"] // OK
//	x.Unlock()
//
// See also: NewSynchronizedValue, NewMultiReadSyncValue
type Value[T any] interface {
	// Get retrieves the current value for read-only purposes.
	// Do not attempt to modify the returned value with Set or Update methods.
	// If side effects are expected, use Update instead.
	// If indexing is required on the returned value such as map or slice, use
	// manual Lock/Unlock to avoid race conditions.
	Get() T

	// Set updates the value safely in a concurrent environment.
	// It replaces the current value with the provided one.
	// Use Update if the new value depends on the current value.
	Set(T)

	// Update atomically modifies the stored value using the provided function,
	// ensuring thread-safe read-modify-write access.
	Update(func(T) T)

	// Lock acquires a lock and returns the current value. The lock semantics
	// depend on the implementation:
	//   - SynchronizedValue: exclusive lock (blocks all other operations)
	//   - MultiReadSyncValue: read lock (blocks writes, allows concurrent reads)
	//
	// Must be paired with Unlock when finished.
	Lock() T

	// Unlock releases the lock previously acquired by Lock.
	Unlock()
}
