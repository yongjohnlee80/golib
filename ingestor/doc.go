// Package ingestor provides generic, thread-safe data ingestion pipelines
// that buffer items in memory and flush them to various backends.
//
// Built-in backends:
//   - [MemoryLoader]: in-memory buffer (base for other ingestors)
//   - [CSV]: writes batches to CSV files
//   - [JSON]: writes batches to JSON files
//
// All ingestors implement the [Ingestor] interface and are safe for
// concurrent use from multiple goroutines.
package ingestor
