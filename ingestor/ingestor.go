package ingestor

// Ingestor is a generic interface for handling and processing data of type T.
type Ingestor[T any] interface {
	// Commit adds one or more data items of type T for processing.
	Commit(items ...T) error

	// Flush finalizes and retrieves all committed data items of type T.
	// Implementations that perform background writes should block until
	// all pending writes complete before returning.
	Flush() ([]T, error)

	// Total returns the total number of items committed to the ingestor.
	Total() uint64
}
