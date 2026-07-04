package ingestor

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// writer is the shared batching engine behind the CSV and JSON ingestors:
// it drains full batches off the embedded MemoryLoader, writes each batch to
// its own file on a background goroutine, and aggregates write errors until
// the next flush.
//
// mu serializes commit/flush state transitions (buffer drain + WaitGroup
// bookkeeping) so a Flush can never race a concurrent Commit into dropping a
// batch or calling wg.Add during wg.Wait. File writes themselves still run
// concurrently in the background.
type writer[T any] struct {
	loader    *MemoryLoader[T]
	cfg       config
	timestamp int64
	batchSize uint64
	ext       string
	write     func(w *writer[T], name string, rows []T) error

	fileCount atomic.Uint64
	sem       chan struct{} // bounds concurrent background writes

	mu sync.Mutex // serializes commit/flush transitions

	errsMu sync.Mutex
	errs   []error
}

// filename returns the name for the next batch file:
// <description>-<unix-timestamp>-<NNN>.<ext>, safe for any filesystem.
func (w *writer[T]) filename() string {
	n := w.fileCount.Add(1)
	return fmt.Sprintf("%s-%d-%03d.%s",
		sanitizeDescription(w.loader.Description()), w.timestamp, n, w.ext)
}

func (w *writer[T]) recordErr(err error) {
	w.errsMu.Lock()
	w.errs = append(w.errs, err)
	w.errsMu.Unlock()
}

// commit buffers items and spawns a background write for every full batch.
// Background writes are bounded by the maxWriters semaphore: once the cap is
// reached, commit blocks until a writer frees up (backpressure, not unbounded
// goroutine growth).
func (w *writer[T]) commit(ctx context.Context, items ...T) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.loader.Commit(ctx, items...); err != nil {
		return err
	}

	for w.loader.Len() >= w.batchSize {
		rows := w.loader.Shift(w.batchSize)
		name := w.filename()

		select {
		case w.sem <- struct{}{}:
		case <-ctx.Done():
			// The rows are already drained from the buffer; write them
			// synchronously rather than lose them.
			if err := w.write(w, name, rows); err != nil {
				w.recordErr(err)
			}
			return ctx.Err()
		}
		w.loader.wg.Go(func() {
			defer func() { <-w.sem }()
			if err := w.write(w, name, rows); err != nil {
				w.recordErr(err)
			}
		})
	}
	return nil
}

// flush drains the remaining buffer, waits for background writes, writes the
// remainder synchronously, and returns the drained rows plus any accumulated
// write errors as a *BatchErrors.
func (w *writer[T]) flush(ctx context.Context) ([]T, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	rows, err := w.loader.Flush(ctx)
	if err != nil {
		return nil, err
	}

	w.loader.wg.Wait()

	if len(rows) > 0 {
		if writeErr := w.write(w, w.filename(), rows); writeErr != nil {
			w.recordErr(writeErr)
		}
	}

	w.errsMu.Lock()
	errs := w.errs
	w.errs = nil
	w.errsMu.Unlock()

	if len(errs) > 0 {
		return rows, &BatchErrors{Errors: errs}
	}
	return rows, nil
}

// close drains and writes any remaining buffered data, waits for background
// writes, and discards the flushed rows.
func (w *writer[T]) close() error {
	_, err := w.flush(context.Background())
	return err
}
