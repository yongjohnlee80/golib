//go:build linux || darwin

package local

import (
	"cmp"
	"context"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/logger"
	"github.com/yongjohnlee80/golib/vfs"
)

var (
	errClosed    = errs.Sentinel(errs.ErrClosed, "local: closed")
	errIsDir     = errs.Sentinel(errs.ErrInvalidArgument, "local: is a directory")
	errIsSymlink = errs.Sentinel(errs.ErrUnsupported, "local: writing through a symlink")
)

// FS is a [vfs.FS] over one directory on local disk. Every name is resolved through [os.Root], so no
// name — "..", absolute, or a symlink pointing outside — reaches outside the root. It is safe for
// concurrent use.
//
// It implements [vfs.ConditionalWriter], [vfs.ExclusiveCreator] and [vfs.NoReplaceRenamer], and on
// Linux [vfs.Watcher]. Conditions are exact among callers of one FS; a write by another process that
// lands between the check and the commit is overwritten.
type FS struct {
	root    *os.Root
	absRoot string // absolute path of the root at New, for messages only
	log     logger.Logger
	locks   pathLocks

	mu     sync.RWMutex // held for reading by every operation, for writing by Close
	closed bool
	done   chan struct{} // closed by Close; ends every watch
}

type config struct {
	log logger.Logger
}

// Option configures [New].
type Option func(*config)

// WithLogger sets the logger for the driver's diagnostics. The default discards everything.
func WithLogger(l logger.Logger) Option { return func(c *config) { c.log = l } }

// New opens dir as the root of a filesystem. dir must be an existing directory.
func New(dir string, opts ...Option) (*FS, error) {
	cfg := config{log: logger.Nop{}}
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	if cfg.log == nil {
		cfg.log = logger.Nop{}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: dir, Err: err}
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	return &FS{root: root, absRoot: abs, log: cfg.log, done: make(chan struct{})}, nil
}

var (
	_ vfs.FS                = (*FS)(nil)
	_ vfs.ConditionalWriter = (*FS)(nil)
	_ vfs.ExclusiveCreator  = (*FS)(nil)
	_ vfs.NoReplaceRenamer  = (*FS)(nil)
)

// enter checks ctx, the name and the closed state, and holds the read lock. The caller calls f.leave.
func (f *FS) enter(ctx context.Context, op, name string, mutable bool) error {
	if err := ctx.Err(); err != nil {
		return &fs.PathError{Op: op, Path: name, Err: err}
	}
	check := vfs.CheckName
	if mutable {
		check = vfs.CheckMutable
	}
	if err := check(op, name); err != nil {
		return err
	}
	f.mu.RLock()
	if f.closed {
		f.mu.RUnlock()
		return &fs.PathError{Op: op, Path: name, Err: errClosed}
	}
	return nil
}

func (f *FS) leave() { f.mu.RUnlock() }

// info converts the lstat of name into a vfs.FileInfo describing the entry itself.
func info(name string, fi os.FileInfo) vfs.FileInfo {
	base := "."
	if name != "." {
		_, base = vfs.Split(name)
	}
	out := vfs.FileInfo{
		Path:    name,
		Name:    base,
		ModTime: fi.ModTime(),
		Mode:    fi.Mode(),
		Version: versionOf(fi),
	}
	if fi.Mode().IsRegular() {
		out.Size = fi.Size()
	}
	return out
}

// Stat returns the entry at name itself; a symlink is reported, not followed.
func (f *FS) Stat(ctx context.Context, name string) (vfs.FileInfo, error) {
	if err := f.enter(ctx, "stat", name, false); err != nil {
		return vfs.FileInfo{}, err
	}
	defer f.leave()
	fi, err := f.lstat(name)
	if err != nil {
		return vfs.FileInfo{}, err
	}
	return info(name, fi), nil
}

// lstat is Stat without following a final symlink. The caller is inside enter/leave.
func (f *FS) lstat(name string) (os.FileInfo, error) { return f.root.Lstat(name) }

// ReadDir lists the directory at name, sorted by Name, without the driver's temporary files. Each entry
// is reported itself, as Stat would.
func (f *FS) ReadDir(ctx context.Context, name string) ([]vfs.FileInfo, error) {
	if err := f.enter(ctx, "readdir", name, false); err != nil {
		return nil, err
	}
	defer f.leave()
	d, err := f.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	entries, err := d.ReadDir(-1)
	if err != nil {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: err}
	}
	out := make([]vfs.FileInfo, 0, len(entries))
	for _, e := range entries {
		if vfs.IsTemp(e.Name()) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			if os.IsNotExist(err) {
				continue // removed between the listing and the stat
			}
			return nil, &fs.PathError{Op: "readdir", Path: vfs.Join(name, e.Name()), Err: err}
		}
		out = append(out, info(vfs.Join(name, e.Name()), fi))
	}
	slices.SortFunc(out, func(a, b vfs.FileInfo) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

// Open streams the file at name from offset.
func (f *FS) Open(ctx context.Context, name string, offset int64) (io.ReadCloser, error) {
	if err := f.enter(ctx, "open", name, false); err != nil {
		return nil, err
	}
	defer f.leave()
	if offset < 0 {
		return nil, &fs.PathError{Op: "open", Path: name,
			Err: errs.Wrap(errs.ErrInvalidArgument, "negative offset %d", offset)}
	}
	file, err := f.root.Open(name)
	if err != nil {
		return nil, err
	}
	st, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if st.IsDir() {
		file.Close()
		return nil, &fs.PathError{Op: "open", Path: name, Err: errIsDir}
	}
	if offset > 0 {
		if _, err := file.Seek(offset, io.SeekStart); err != nil {
			file.Close()
			return nil, err
		}
	}
	return file, nil
}

// MkdirAll creates name and any missing parents.
func (f *FS) MkdirAll(ctx context.Context, name string) error {
	if err := f.enter(ctx, "mkdir", name, false); err != nil {
		return err
	}
	defer f.leave()
	if name == "." {
		return nil
	}
	return f.root.MkdirAll(name, 0o755)
}

// Remove deletes the file, symlink or empty directory at name.
func (f *FS) Remove(ctx context.Context, name string) error { return f.remove(ctx, name, false, "") }

// RemoveIf is Remove that removes only if the entry's current version is want.
func (f *FS) RemoveIf(ctx context.Context, name string, want vfs.Version) error {
	return f.remove(ctx, name, true, want)
}

func (f *FS) remove(ctx context.Context, name string, ifVersion bool, want vfs.Version) error {
	if err := f.enter(ctx, "remove", name, true); err != nil {
		return err
	}
	defer f.leave()
	unlock := f.locks.lock(name)
	defer unlock()
	if ifVersion {
		if err := f.checkVersion(name, want); err != nil {
			return err
		}
	}
	return f.root.Remove(name) // unlinkat: a symlink goes, its target stays
}

// checkVersion fails with a *vfs.ConflictError unless name's current version is want.
func (f *FS) checkVersion(name string, want vfs.Version) error {
	fi, err := f.lstat(name)
	if err != nil {
		if os.IsNotExist(err) {
			return &vfs.ConflictError{Path: name, Want: want}
		}
		return err
	}
	cur := info(name, fi)
	if cur.Version != want {
		return &vfs.ConflictError{Path: name, Want: want, Current: cur}
	}
	return nil
}

// RemoveAll deletes name and everything under it. A missing name is not an error.
func (f *FS) RemoveAll(ctx context.Context, name string) error {
	if err := f.enter(ctx, "removeall", name, true); err != nil {
		return err
	}
	defer f.leave()
	unlock := f.locks.lock(name)
	defer unlock()
	return f.root.RemoveAll(name)
}

// Close releases the root and ends every watch. It waits for operations in flight; later calls fail
// with errs.ErrClosed.
func (f *FS) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true
	close(f.done)
	return f.root.Close()
}
