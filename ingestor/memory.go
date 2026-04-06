package ingestor

import (
	"sync"

	"github.com/yongjohnlee80/golib/threadsafe"
)

// MemoryLoader temporarily stores and manages a buffered set of data in memory.
// It is the base implementation embedded by CSV and JSON ingestors.
type MemoryLoader[T any] struct {
	desc  string
	wg    sync.WaitGroup
	buf   threadsafe.Value[[]T]
	total threadsafe.Value[uint64]
}

// NewMemoryLoader initializes a new MemoryLoader with the given description.
func NewMemoryLoader[T any](description string) *MemoryLoader[T] {
	return &MemoryLoader[T]{
		desc:  description,
		buf:   threadsafe.NewSynchronizedValue([]T{}),
		total: threadsafe.NewSynchronizedValue(uint64(0)),
	}
}

// Commit appends the provided items to the internal buffer.
func (ml *MemoryLoader[T]) Commit(items ...T) error {
	ml.total.Update(func(x uint64) uint64 {
		return x + uint64(len(items))
	})
	ml.buf.Update(func(x []T) []T {
		return append(x, items...)
	})
	return nil
}

// Len returns the number of elements currently stored in the buffer.
func (ml *MemoryLoader[T]) Len() uint64 {
	return uint64(len(ml.buf.Get()))
}

// Total returns the total count of committed items.
func (ml *MemoryLoader[T]) Total() uint64 {
	return ml.total.Get()
}

// Shift removes and returns the first n elements from the buffer.
// If n is greater than the buffer length, it returns all elements.
func (ml *MemoryLoader[T]) Shift(n uint64) []T {
	if n == 0 || ml.Len() == 0 {
		return nil
	}

	var temp []T
	ml.buf.Update(func(x []T) []T {
		n = min(n, uint64(len(x)))
		temp = make([]T, n)
		copy(temp, x[:n])
		return x[n:]
	})

	return temp
}

// Flush drains all buffered data and returns it to the caller.
func (ml *MemoryLoader[T]) Flush() ([]T, error) {
	var temp []T
	ml.buf.Update(func(x []T) []T {
		temp = x
		return nil
	})
	return temp, nil
}

// Description retrieves the current description.
func (ml *MemoryLoader[T]) Description() string {
	return ml.desc
}

// SetDescription updates the description.
func (ml *MemoryLoader[T]) SetDescription(desc string) {
	ml.desc = desc
}
