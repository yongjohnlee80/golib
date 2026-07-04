package ingestor

import (
	"sync"

	"github.com/yongjohnlee80/golib/threadsafe"
)

// memState is the single atomically-updated state of a MemoryLoader: the
// buffer and the running total move together so no observer can see one
// updated without the other.
type memState[T any] struct {
	buf   []T
	total uint64
}

// MemoryLoader temporarily stores and manages a buffered set of data in memory.
// It is the base implementation embedded by CSV and JSON ingestors.
type MemoryLoader[T any] struct {
	desc  string
	wg    sync.WaitGroup
	state threadsafe.Value[memState[T]]
}

// NewMemoryLoader initializes a new MemoryLoader with the given description.
func NewMemoryLoader[T any](description string) *MemoryLoader[T] {
	return &MemoryLoader[T]{
		desc:  description,
		state: threadsafe.NewSynchronizedValue(memState[T]{}),
	}
}

// Commit appends the provided items to the internal buffer.
func (ml *MemoryLoader[T]) Commit(items ...T) error {
	ml.state.Update(func(s memState[T]) memState[T] {
		s.buf = append(s.buf, items...)
		s.total += uint64(len(items))
		return s
	})
	return nil
}

// Len returns the number of elements currently stored in the buffer.
func (ml *MemoryLoader[T]) Len() uint64 {
	return uint64(len(ml.state.Get().buf))
}

// Total returns the total count of committed items.
func (ml *MemoryLoader[T]) Total() uint64 {
	return ml.state.Get().total
}

// Shift removes and returns the first n elements from the buffer.
// If n is greater than the buffer length, it returns all elements.
func (ml *MemoryLoader[T]) Shift(n uint64) []T {
	if n == 0 {
		return nil
	}

	var temp []T
	ml.state.Update(func(s memState[T]) memState[T] {
		n = min(n, uint64(len(s.buf)))
		if n == 0 {
			return s
		}
		temp = make([]T, n)
		copy(temp, s.buf[:n])
		s.buf = s.buf[n:]
		return s
	})

	return temp
}

// Flush drains all buffered data and returns it to the caller.
func (ml *MemoryLoader[T]) Flush() ([]T, error) {
	var temp []T
	ml.state.Update(func(s memState[T]) memState[T] {
		temp = s.buf
		s.buf = nil
		return s
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
