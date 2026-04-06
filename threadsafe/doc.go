// Package threadsafe provides generic, thread-safe value containers for Go.
//
// Two implementations are available:
//
//   - [SynchronizedValue]: uses a mutex for exclusive access (simple, safe default)
//   - [MultiReadSyncValue]: uses a read-write mutex for concurrent reads (better for read-heavy workloads)
//
// Both implement the [Value] interface, so they can be used interchangeably.
package threadsafe
