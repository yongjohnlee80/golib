package exporter

import "context"

// Exporter is a generic interface for handling and processing data of type T.
type Exporter[T any] interface {
	// Commit adds one or more data items of type T for processing. A cancelled
	// ctx aborts before any state changes.
	Commit(ctx context.Context, items ...T) error

	// Flush finalizes and retrieves all committed data items of type T.
	// Implementations that perform background writes should block until
	// all pending writes complete before returning. A cancelled ctx aborts
	// before the buffer is drained.
	Flush(ctx context.Context) ([]T, error)

	// Total returns the total number of items committed to the exporter.
	Total() uint64

	// Close finalizes the exporter: it drains and writes any remaining
	// buffered data and waits for background writes, discarding the returned
	// rows. Use it as the terminal defer when the flushed data itself is not
	// needed.
	Close() error
}
