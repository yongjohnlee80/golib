// Package exporter provides generic, thread-safe data export pipelines
// that buffer items in memory and flush them to various backends.
//
// Built-in backends:
//   - [MemoryLoader]: in-memory buffer (base for other exporters)
//   - [CSV]: writes batches to CSV files
//   - [JSON]: writes batches to JSON files
//
// All exporters implement the [Exporter] interface and are safe for
// concurrent use from multiple goroutines.
package exporter
