package vfs

import (
	"context"
	"io"
	"io/fs"
	"time"
)

// FS is a filesystem rooted at one directory, bucket or remote path. All methods are safe for concurrent
// use. It holds what every driver answers; optional abilities are separate interfaces
// ([ConditionalWriter], [ExclusiveCreator], [NoReplaceRenamer], [Watcher], [Copier]) found by type
// assertion. See the package documentation for names, atomicity and versions.
type FS interface {
	// Stat returns the entry at name itself: a symlink is reported as a symlink, not followed. Symlinks
	// in the intermediate elements of name are followed when they stay inside the root.
	Stat(ctx context.Context, name string) (FileInfo, error)

	// ReadDir lists the directory at name (following a symlink that stays inside the root), sorted by
	// Name. Each entry is reported itself, as Stat would; the driver's own temporary files are never
	// listed.
	ReadDir(ctx context.Context, name string) ([]FileInfo, error)

	// Open streams the file at name starting at offset. An offset at or past the end yields an empty
	// stream. The caller closes the returned reader.
	Open(ctx context.Context, name string, offset int64) (io.ReadCloser, error)

	// WriteFile replaces the file at name with r's content, creating it if needed (its parent directory
	// must exist). The replacement is atomic: readers see the old content or the new, never a mix.
	// Errors before the commit point leave the old state intact; errors after it are a *CommitError.
	// It returns the new entry, including its Version.
	WriteFile(ctx context.Context, name string, r io.Reader, opts ...WriteOption) (FileInfo, error)

	// MkdirAll creates the directory at name and any missing parents. It is idempotent.
	MkdirAll(ctx context.Context, name string) error

	// Remove deletes the file, symlink or empty directory at name. A symlink is removed, never its target.
	Remove(ctx context.Context, name string) error

	// RemoveAll deletes name and everything under it. A missing name is not an error. The root itself
	// cannot be removed.
	RemoveAll(ctx context.Context, name string) error

	// Rename moves from to to, replacing an existing file at to. Whether the move is atomic is part of
	// the driver's documented contract (local and memfs: atomic; an object store: copy then delete).
	Rename(ctx context.Context, from, to string) error

	// Close releases the driver. Every later call fails with an error wrapping
	// github.com/yongjohnlee80/golib/errs.ErrClosed.
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
