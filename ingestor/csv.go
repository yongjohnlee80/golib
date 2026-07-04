package ingestor

import (
	"encoding/csv"
	"fmt"
	"reflect"
	"time"
)

const (
	// DefaultCSVBatchSize is the default number of rows per CSV file.
	DefaultCSVBatchSize = 1_000_000
)

// CSV provides functionalities for loading, buffering, and exporting
// data to CSV files. It embeds MemoryLoader to manage in-memory buffering.
// Commit and Flush are safe for concurrent use.
type CSV[T any] struct {
	*MemoryLoader[T]
	writer[T]
}

// NewCSV creates and returns a new CSV ingestor with the given description
// and batch size. If batchSize is 0, it defaults to DefaultCSVBatchSize.
// Options control where batch files are written (WithDir, WithOpener).
func NewCSV[T any](description string, batchSize uint64, opts ...Option) *CSV[T] {
	if batchSize == 0 {
		batchSize = DefaultCSVBatchSize
	}

	c := &CSV[T]{MemoryLoader: NewMemoryLoader[T](description)}
	c.writer = writer[T]{
		loader:    c.MemoryLoader,
		cfg:       newConfig(opts),
		timestamp: time.Now().Unix(),
		batchSize: batchSize,
		ext:       "csv",
		write:     writeCSV[T],
	}
	return c
}

// Commit buffers items and writes full batches to CSV files in the
// background. Write errors from background batches are collected and
// returned by Flush.
func (ml *CSV[T]) Commit(items ...T) error {
	return ml.writer.commit(items...)
}

// Flush transfers all buffered data from memory to a CSV file, waits for
// any background writes to complete, and returns the flushed data.
// If any background writes failed, errors are returned as a *BatchErrors.
func (ml *CSV[T]) Flush() ([]T, error) {
	return ml.writer.flush()
}

// CSVHeaderRow generates a CSV header row by extracting the exported field
// names from the provided struct or struct pointer sample. Unexported fields
// are skipped (their values cannot be read), matching the row encoding.
func CSVHeaderRow[T any](sample T) ([]string, error) {
	val := reflect.ValueOf(sample)
	if val.Kind() == reflect.Pointer {
		if val.IsNil() {
			return nil, fmt.Errorf("CSV expects non-nil struct pointer, got nil")
		}
		val = val.Elem()
	}

	if val.Kind() != reflect.Struct {
		return nil, fmt.Errorf("CSV expects struct or pointer to struct, got %v", val.Kind())
	}

	typ := val.Type()
	header := make([]string, 0, typ.NumField())
	for i := range typ.NumField() {
		if f := typ.Field(i); f.IsExported() {
			header = append(header, f.Name)
		}
	}
	return header, nil
}

// writeCSV encodes rows as CSV (header + one record per row) into w's target.
func writeCSV[T any](wr *writer[T], name string, rows []T) error {
	file, err := wr.cfg.open(name)
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
		if val.Kind() == reflect.Pointer {
			if val.IsNil() {
				continue
			}
			val = val.Elem()
		}

		record := make([]string, 0, val.NumField())
		for i := range val.NumField() {
			if !val.Type().Field(i).IsExported() {
				continue
			}
			record = append(record, fmt.Sprintf("%v", val.Field(i).Interface()))
		}

		if err = w.Write(record); err != nil {
			return err
		}
	}

	return nil
}
