// Package memfs is an in-memory [github.com/yongjohnlee80/golib/vfs.FS] for tests and for code that
// wants the vfs contract without a disk.
//
// It implements the core interface and every capability: ConditionalWriter, ExclusiveCreator,
// NoReplaceRenamer, Watcher and Copier. Every mutation happens under one lock, so renames and
// no-replace commits are atomic and write conditions are exact. Versions are a counter; each mutation
// gives the touched entries (and the parent directory) a new one, the way a disk moves a directory's
// modification time. Watch events are queued, never dropped, and arrive in mutation order.
package memfs
