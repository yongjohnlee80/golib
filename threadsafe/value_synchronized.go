package threadsafe

import (
	"sync"
)

// NewSynchronizedValue creates a new SynchronizedValue initialized with the
// given value. Only one goroutine can access the value at a time for read or
// write operations.
func NewSynchronizedValue[T any](v T) *SynchronizedValue[T] {
	return &SynchronizedValue[T]{value: v}
}

// SynchronizedValue is a generic type that provides thread-safe access to a
// value using an internal mutex.
// It allows only one goroutine to access the critical section at a time.
type SynchronizedValue[T any] struct {
	value T
	mu    sync.Mutex
}

// Do runs fn with mutable access to the stored value under the exclusive lock.
func (m *SynchronizedValue[T]) Do(fn func(*T)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&m.value)
}

// RDo runs fn with read access to the stored value under the exclusive lock
// (SynchronizedValue has no shared read mode).
func (m *SynchronizedValue[T]) RDo(fn func(T)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(m.value)
}

// Get retrieves the value in a thread-safe manner by acquiring and releasing
// the mutex.
func (m *SynchronizedValue[T]) Get() T {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.value
}

// Set updates the value in a thread-safe manner.
func (m *SynchronizedValue[T]) Set(v T) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.value = v
}

// Update atomically modifies the stored value using the provided function,
// ensuring thread-safe read-modify-write access.
func (m *SynchronizedValue[T]) Update(fn func(T) T) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.value = fn(m.value)
}
