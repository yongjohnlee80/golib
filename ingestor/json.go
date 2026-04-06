package ingestor

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultJSONBatchSize is the default number of items per JSON file.
	DefaultJSONBatchSize = 100_000
)

// JSON provides functionalities for loading, buffering, and exporting
// data to JSON files. It embeds MemoryLoader to manage in-memory buffering.
type JSON[T any] struct {
	*MemoryLoader[T]

	timestamp int64
	fileCount atomic.Uint64
	batchSize uint64

	mu   sync.Mutex
	errs []error
}

// NewJSON creates and returns a new JSON ingestor with the given description
// and batch size. If batchSize is 0, it defaults to DefaultJSONBatchSize.
func NewJSON[T any](description string, batchSize uint64) *JSON[T] {
	if batchSize == 0 {
		batchSize = DefaultJSONBatchSize
	}

	return &JSON[T]{
		MemoryLoader: NewMemoryLoader[T](description),
		timestamp:    time.Now().Unix(),
		batchSize:    batchSize,
	}
}

// Commit writes buffered data to a JSON file when the batch size threshold
// is reached. Write errors from background batches are collected and returned
// by Flush.
func (ml *JSON[T]) Commit(items ...T) error {
	_ = ml.MemoryLoader.Commit(items...)

	limit := ml.batchSize
	for ml.Len() >= limit {
		rows := ml.Shift(limit)

		ml.wg.Add(1)
		go func() {
			defer ml.wg.Done()
			if err := ml.writeJSONFile(rows); err != nil {
				ml.mu.Lock()
				ml.errs = append(ml.errs, err)
				ml.mu.Unlock()
			}
		}()
	}
	return nil
}

// Flush transfers all buffered data from memory to a JSON file, waits for
// any background writes to complete, and returns the flushed data.
// If any background writes failed, errors are returned as a *BatchErrors.
func (ml *JSON[T]) Flush() ([]T, error) {
	rows, err := ml.MemoryLoader.Flush()
	if err != nil {
		return nil, err
	}

	ml.wg.Wait()

	if writeErr := ml.writeJSONFile(rows); writeErr != nil {
		ml.mu.Lock()
		ml.errs = append(ml.errs, writeErr)
		ml.mu.Unlock()
	}

	ml.mu.Lock()
	errs := ml.errs
	ml.errs = nil
	ml.mu.Unlock()

	if len(errs) > 0 {
		return rows, &BatchErrors{Errors: errs}
	}
	return rows, nil
}

func (ml *JSON[T]) writeJSONFile(rows []T) error {
	if len(rows) == 0 {
		return nil
	}

	n := ml.fileCount.Add(1)
	filename := fmt.Sprintf("./%s-%d (%d).json",
		strings.ReplaceAll(ml.Description(), "/", "-"),
		ml.timestamp,
		n,
	)

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	enc := json.NewEncoder(file)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}
