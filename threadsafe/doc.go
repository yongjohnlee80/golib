// Package threadsafe provides generic, thread-safe value containers for Go.
//
// Three implementations are available, all satisfying the [Value] interface so
// they can be used interchangeably:
//
//   - [SynchronizedValue]: a mutex for exclusive access (simple, safe default)
//   - [MultiReadSyncValue]: a read-write mutex for concurrent reads (read-heavy workloads)
//   - [AtomicValue]: lock-free reads over an atomic snapshot pointer (read-dominated hot paths)
//
// Prefer the read-modify-write helpers [Value.Update], [Value.Do], and
// [Value.RDo] over a Get/Set pair: the latter is a lost-update race, and
// indexing a reference value returned by Get races concurrent writers. See the
// [Value] documentation for the closure discipline these methods require.
package threadsafe
