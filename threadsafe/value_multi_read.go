package threadsafe

import (
	"sync"
)

// NewMultiReadSyncValue creates a new MultiReadSyncValue initialized with the
// given value. Multiple goroutines can read the value concurrently, improving
// performance for read-heavy workloads. Only one goroutine can write at a time.
func NewMultiReadSyncValue[T any](v T) *MultiReadSyncValue[T] {
	return &MultiReadSyncValue[T]{value: v}
}

// MultiReadSyncValue provides thread-safe access to a value using a
// read-write mutex. Multiple goroutines can read concurrently; writes are
// exclusive.
type MultiReadSyncValue[T any] struct {
	value T
	mu    sync.RWMutex
}

// Unlock releases the read lock previously acquired by Lock.
func (rw *MultiReadSyncValue[T]) Unlock() {
	rw.mu.RUnlock()
}

// Lock acquires a read lock and returns the current value. Multiple goroutines
// can hold a read lock simultaneously. Must be paired with Unlock.
func (rw *MultiReadSyncValue[T]) Lock() T {
	rw.mu.RLock()
	return rw.value
}

// Get returns the stored value in a thread-safe manner by acquiring and
// releasing a read lock.
func (rw *MultiReadSyncValue[T]) Get() T {
	rw.mu.RLock()
	defer rw.mu.RUnlock()
	return rw.value
}

// Set updates the stored value in a thread-safe manner by acquiring an
// exclusive write lock.
func (rw *MultiReadSyncValue[T]) Set(v T) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	rw.value = v
}

// Update atomically modifies the stored value using the provided function,
// ensuring thread-safe read-modify-write access with an exclusive write lock.
func (rw *MultiReadSyncValue[T]) Update(fn func(T) T) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	rw.value = fn(rw.value)
}
