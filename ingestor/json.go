package ingestor

import (
	"encoding/json"
	"time"
)

const (
	// DefaultJSONBatchSize is the default number of items per JSON file.
	DefaultJSONBatchSize = 100_000
)

// JSON provides functionalities for loading, buffering, and exporting
// data to JSON files. It embeds MemoryLoader to manage in-memory buffering.
// Commit and Flush are safe for concurrent use.
type JSON[T any] struct {
	*MemoryLoader[T]
	writer[T]
}

// NewJSON creates and returns a new JSON ingestor with the given description
// and batch size. If batchSize is 0, it defaults to DefaultJSONBatchSize.
// Options control where batch files are written (WithDir, WithOpener).
func NewJSON[T any](description string, batchSize uint64, opts ...Option) *JSON[T] {
	if batchSize == 0 {
		batchSize = DefaultJSONBatchSize
	}

	j := &JSON[T]{MemoryLoader: NewMemoryLoader[T](description)}
	j.writer = writer[T]{
		loader:    j.MemoryLoader,
		cfg:       newConfig(opts),
		timestamp: time.Now().Unix(),
		batchSize: batchSize,
		ext:       "json",
		write:     writeJSON[T],
	}
	return j
}

// Commit buffers items and writes full batches to JSON files in the
// background. Write errors from background batches are collected and
// returned by Flush.
func (ml *JSON[T]) Commit(items ...T) error {
	return ml.writer.commit(items...)
}

// Flush transfers all buffered data from memory to a JSON file, waits for
// any background writes to complete, and returns the flushed data.
// If any background writes failed, errors are returned as a *BatchErrors.
func (ml *JSON[T]) Flush() ([]T, error) {
	return ml.writer.flush()
}

// writeJSON encodes rows as an indented JSON array into w's target.
func writeJSON[T any](wr *writer[T], name string, rows []T) error {
	file, err := wr.cfg.open(name)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}
