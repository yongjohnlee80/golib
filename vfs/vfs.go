package vfs

import (
	"context"
	"io"
	"io/fs"
	"time"
)

// FS represents a unified, portable virtual filesystem abstraction rooted at a single directory,
// bucket, or remote path.
//
// Concurrency and Lifecycle:
// All methods on FS are safe for concurrent use by multiple goroutines. Calling [FS.Close]
// puts the driver into a terminal closed state, terminates active driver background tasks
// (such as watches or network sessions), and causes subsequent operations to fail with an error
// wrapping [github.com/yongjohnlee80/golib/errs.ErrClosed]. Stored filesystem content (such as
// in-memory node structures) is not necessarily purged on Close and remains reachable until the
// instance itself is garbage collected.
//
// Core Responsibilities:
// FS defines the common denominator guaranteed by every driver:
//  1. Path Resolution: Validates unrooted, slash-separated names ([io/fs.ValidPath]); rejects
//     lexical escapes ("../") and internal driver temporary files ([TempPrefix]).
//  2. Atomic Mutations: [FS.WriteFile] replaces files atomically, guaranteeing readers observe
//     either the old content or the new, never a mix. Driver implementations employ suitable
//     mechanisms (e.g. temporary staging files and atomic rename on local disk, or locked node updates
//     in memory).
//  3. Directory Integrity: Guarantees directory tree traversal and manipulation ([FS.ReadDir],
//     [FS.MkdirAll], [FS.RemoveAll]) without exposing intermediate driver artifacts.
//
// Extended Driver Capabilities:
// Optional features beyond this baseline are factored into specialized interfaces discovered via
// Go type assertion:
//   - [ConditionalWriter]: Optimistic concurrency via version checking (WriteFileIf, RemoveIf).
//   - [ExclusiveCreator]: Atomic creation without replacement (CreateExclusive).
//   - [NoReplaceRenamer]: Atomic move without overwriting targets (RenameNoReplace).
//   - [Watcher]: Real-time filesystem mutation event streaming (Watch).
//   - [Copier]: Native file cloning or optimized duplication (Copy).
type FS interface {
	// Stat returns the entry metadata for name.
	//
	// Entry Semantics:
	// Stat returns metadata for the directory entry itself: a symlink is reported with [fs.ModeSymlink]
	// and is never followed to its target. Symlinks encountered in parent directory components of name
	// are followed provided they remain strictly confined within the driver's root.
	Stat(ctx context.Context, name string) (FileInfo, error)

	// ReadDir lists the directory at name, returning entries sorted lexicographically by base name.
	//
	// Invariants:
	// Each entry represents the immediate child itself (symlinks are not followed). Driver-internal
	// staging files (prefixed with [TempPrefix]) are filtered out and never returned.
	ReadDir(ctx context.Context, name string) ([]FileInfo, error)

	// Open returns an [io.ReadCloser] streaming the contents of the file at name starting from offset.
	//
	// Bounds and Cleanup:
	// Specifying an offset at or past the end of the file yields an empty stream returning [io.EOF].
	// The caller is responsible for calling Close on the returned reader.
	Open(ctx context.Context, name string, offset int64) (io.ReadCloser, error)

	// WriteFile atomically replaces or creates the file at name with data read from r.
	//
	// Atomicity and Commit Point:
	// The replacement is atomic across all drivers: readers observe either the complete previous
	// file or the complete new file, never a partial state. Drivers implement this through backend-appropriate
	// mechanisms (such as staging files with atomic rename in local disk, or mutex-protected updates in memory).
	// Any error occurring before the commit point aborts the mutation and preserves the existing state.
	// An error occurring after the commit point returns a [*CommitError] indicating the new version was committed.
	WriteFile(ctx context.Context, name string, r io.Reader, opts ...WriteOption) (FileInfo, error)

	// MkdirAll creates the directory specified by name along with any missing parent directories.
	// It is idempotent: calling MkdirAll on an existing directory returns nil without error.
	MkdirAll(ctx context.Context, name string) error

	// Remove deletes the file, symlink, or empty directory at name.
	// If name names a symlink, the link itself is removed, never its target.
	Remove(ctx context.Context, name string) error

	// RemoveAll recursively removes name and all entries contained within it.
	// A missing target is treated as a no-op returning nil. The root directory (".") cannot be removed.
	RemoveAll(ctx context.Context, name string) error

	// Rename moves or renames from to to, atomically replacing any existing file at to where supported
	// by the driver (e.g. local disk and memfs).
	Rename(ctx context.Context, from, to string) error

	// Close releases active resources (such as file descriptors, watches, or network connections)
	// and places the driver into a terminal closed state. Subsequent calls return an error
	// wrapping [github.com/yongjohnlee80/golib/errs.ErrClosed].
	Close() error
}

// FileInfo describes one directory entry — the entry itself, never a symlink's target. Stat, ReadDir and
// Walk all report entries this way, so the Version they return is the one conditional mutations compare.
type FileInfo struct {
	Path    string      // root-relative, slash-separated ("docs/a.md"); "." for the root
	Name    string      // base name
	Size    int64       // bytes of a regular file; 0 for directories and symlinks
	ModTime time.Time   // last modification time as the backend reports it
	Mode    fs.FileMode // type bits (fs.ModeDir, fs.ModeSymlink, …) and permission bits where the backend has them
	Version Version     // opaque change token; see [Version]
}

// IsDir reports whether the entry is a directory. A symlink to a directory is not.
func (fi FileInfo) IsDir() bool { return fi.Mode.IsDir() }

// IsRegular reports whether the entry is a regular file. A symlink to a file is not.
func (fi FileInfo) IsRegular() bool { return fi.Mode.IsRegular() }

// Version is an opaque, driver-defined token that changes on every mutation made through the driver.
// For changes made by other processes it is best-effort (local disk: a metadata heuristic) or exact
// (backends that version objects). Compare versions only for equality, and only within one driver.
type Version string
