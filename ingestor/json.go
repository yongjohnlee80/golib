package ingestor

import (
	"context"
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

// NewJSON creates and returns a new JSON ingestor with the given description.
// Options configure the batch size (WithBatchSize, default DefaultJSONBatchSize),
// where batch files are written (WithDir, WithOpener), and the background-write
// cap (WithMaxWriters).
func NewJSON[T any](description string, opts ...Option) *JSON[T] {
	cfg := newConfig(opts)
	if cfg.batchSize == 0 {
		cfg.batchSize = DefaultJSONBatchSize
	}

	j := &JSON[T]{MemoryLoader: NewMemoryLoader[T](description)}
	j.writer = writer[T]{
		loader:    j.MemoryLoader,
		cfg:       cfg,
		sem:       make(chan struct{}, cfg.maxWriters),
		timestamp: time.Now().Unix(),
		batchSize: cfg.batchSize,
		ext:       "json",
		write:     writeJSON[T],
	}
	return j
}

// Commit buffers items and writes full batches to JSON files in the
// background. Write errors from background batches are collected and
// returned by Flush.
func (ml *JSON[T]) Commit(ctx context.Context, items ...T) error {
	return ml.writer.commit(ctx, items...)
}

// Flush transfers all buffered data from memory to a JSON file, waits for
// any background writes to complete, and returns the flushed data.
// If any background writes failed, errors are returned as a *BatchErrors.
func (ml *JSON[T]) Flush(ctx context.Context) ([]T, error) {
	return ml.writer.flush(ctx)
}

// Close drains and writes any remaining buffered data, discarding the rows.
func (ml *JSON[T]) Close() error { return ml.writer.close() }

// compile-time: *JSON satisfies Ingestor.
var _ Ingestor[int] = (*JSON[int])(nil)

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
