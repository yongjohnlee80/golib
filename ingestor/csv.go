package ingestor

import (
	"context"
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

// NewCSV creates and returns a new CSV ingestor with the given description.
// Options configure the batch size (WithBatchSize, default DefaultCSVBatchSize),
// where batch files are written (WithDir, WithOpener), and the background-write
// cap (WithMaxWriters).
func NewCSV[T any](description string, opts ...Option) *CSV[T] {
	cfg := newConfig(opts)
	if cfg.batchSize == 0 {
		cfg.batchSize = DefaultCSVBatchSize
	}

	c := &CSV[T]{MemoryLoader: NewMemoryLoader[T](description)}
	c.writer = writer[T]{
		loader:    c.MemoryLoader,
		cfg:       cfg,
		sem:       make(chan struct{}, cfg.maxWriters),
		timestamp: time.Now().Unix(),
		batchSize: cfg.batchSize,
		ext:       "csv",
		write:     writeCSV[T],
	}
	return c
}

// Commit buffers items and writes full batches to CSV files in the
// background. Write errors from background batches are collected and
// returned by Flush.
func (ml *CSV[T]) Commit(ctx context.Context, items ...T) error {
	return ml.writer.commit(ctx, items...)
}

// Flush transfers all buffered data from memory to a CSV file, waits for
// any background writes to complete, and returns the flushed data.
// If any background writes failed, errors are returned as a *BatchErrors.
func (ml *CSV[T]) Flush(ctx context.Context) ([]T, error) {
	return ml.writer.flush(ctx)
}

// Close drains and writes any remaining buffered data, discarding the rows.
func (ml *CSV[T]) Close() error { return ml.writer.close() }

// compile-time: *CSV satisfies Ingestor.
var _ Ingestor[int] = (*CSV[int])(nil)

// CSVHeaderRow generates a CSV header row from the provided struct or struct
// pointer sample. A `csv:"name"` tag overrides the column name and `csv:"-"`
// omits the field; untagged exported fields use the Go field name. Unexported
// fields are skipped (their values cannot be read), matching the row encoding.
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
		if name, ok := csvColumn(typ.Field(i)); ok {
			header = append(header, name)
		}
	}
	return header, nil
}

// csvColumn resolves a struct field's CSV column name: the `csv` tag when
// present ("-" omits the field), the field name otherwise. Unexported fields
// report ok=false.
func csvColumn(f reflect.StructField) (string, bool) {
	if !f.IsExported() {
		return "", false
	}
	tag, ok := f.Tag.Lookup("csv")
	if !ok {
		return f.Name, true
	}
	if tag == "-" || tag == "" {
		return "", false
	}
	return tag, true
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
			if _, ok := csvColumn(val.Type().Field(i)); !ok {
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
