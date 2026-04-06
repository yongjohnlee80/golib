package ingestor

import (
	"encoding/csv"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// DefaultCSVBatchSize is the default number of rows per CSV file.
	DefaultCSVBatchSize = 1_000_000
)

// CSV provides functionalities for loading, buffering, and exporting
// data to CSV files. It embeds MemoryLoader to manage in-memory buffering.
type CSV[T any] struct {
	*MemoryLoader[T]

	timestamp int64
	fileCount atomic.Uint64
	batchSize uint64

	mu   sync.Mutex
	errs []error
}

// NewCSV creates and returns a new CSV ingestor with the given description
// and batch size. If batchSize is 0, it defaults to DefaultCSVBatchSize.
func NewCSV[T any](description string, batchSize uint64) *CSV[T] {
	if batchSize == 0 {
		batchSize = DefaultCSVBatchSize
	}

	return &CSV[T]{
		MemoryLoader: NewMemoryLoader[T](description),
		timestamp:    time.Now().Unix(),
		batchSize:    batchSize,
	}
}

// Commit writes buffered data to a CSV file when the batch size threshold is
// reached. Write errors from background batches are collected and returned
// by Flush.
func (ml *CSV[T]) Commit(items ...T) error {
	_ = ml.MemoryLoader.Commit(items...)

	limit := ml.batchSize
	for ml.Len() >= limit {
		rows := ml.Shift(limit)

		ml.wg.Add(1)
		go func() {
			defer ml.wg.Done()
			if err := ml.writeCSVFile(rows); err != nil {
				ml.mu.Lock()
				ml.errs = append(ml.errs, err)
				ml.mu.Unlock()
			}
		}()
	}
	return nil
}

// Flush transfers all buffered data from memory to a CSV file, waits for
// any background writes to complete, and returns the flushed data.
// If any background writes failed, errors are returned as a *BatchErrors.
func (ml *CSV[T]) Flush() ([]T, error) {
	rows, err := ml.MemoryLoader.Flush()
	if err != nil {
		return nil, err
	}

	ml.wg.Wait()

	if writeErr := ml.writeCSVFile(rows); writeErr != nil {
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

// CSVHeaderRow generates a CSV header row by extracting field names from the
// provided struct or struct pointer sample.
func CSVHeaderRow[T any](sample T) ([]string, error) {
	val := reflect.ValueOf(sample)
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return nil, fmt.Errorf("CSV expects non-nil struct pointer, got nil")
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("CSV expects struct or pointer to struct, got %v", val.Kind())
	}

	typ := val.Type()
	header := make([]string, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		header[i] = typ.Field(i).Name
	}
	return header, nil
}

func (ml *CSV[T]) writeCSVFile(rows []T) error {
	if len(rows) == 0 {
		return nil
	}

	n := ml.fileCount.Add(1)
	filename := fmt.Sprintf("./%s-%d (%d).csv",
		strings.ReplaceAll(ml.Description(), "/", "-"),
		ml.timestamp,
		n,
	)

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	w := csv.NewWriter(file)
	defer w.Flush()

	header, err := CSVHeaderRow(rows[0])
	if err != nil {
		return err
	}

	if err = w.Write(header); err != nil {
		return err
	}

	for _, row := range rows {
		val := reflect.ValueOf(row)
		if val.Kind() == reflect.Ptr {
			if val.IsNil() {
				continue
			}
			val = val.Elem()
		}

		var record []string
		for i := 0; i < val.NumField(); i++ {
			fieldVal := val.Field(i)
			if !fieldVal.CanInterface() {
				record = append(record, "")
				continue
			}
			record = append(record, fmt.Sprintf("%v", fieldVal.Interface()))
		}

		if err = w.Write(record); err != nil {
			return err
		}
	}

	return nil
}
