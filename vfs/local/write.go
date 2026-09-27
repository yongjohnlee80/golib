//go:build linux || darwin

package local

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/yongjohnlee80/golib/errs"
	"github.com/yongjohnlee80/golib/vfs"
)

// testHook, when set by this package's tests, is called at "precommit" and "postcommit"; a non-nil
// return is treated as a failure at that point. Production code never sets it.
var testHook func(point string) error

func hook(point string) error {
	if testHook == nil {
		return nil
	}
	return testHook(point)
}

func tempName(base string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return vfs.TempPrefix + base + "-" + hex.EncodeToString(b[:])
}

// cond is the condition a write commits under: none, the entry's version, or its absence.
type cond struct {
	ifVersion bool
	want      vfs.Version
	exclusive bool
}

// WriteFile replaces the file at name with r's content, atomically. The temporary file is created
// through the parent directory's pinned fd and committed with one rename on that same fd; see the
// package documentation for the error contract around the commit point.
func (f *FS) WriteFile(ctx context.Context, name string, r io.Reader, opts ...vfs.WriteOption) (vfs.FileInfo, error) {
	return f.write(ctx, name, r, cond{}, opts)
}

// WriteFileIf is WriteFile that commits only if the entry's current version is want.
func (f *FS) WriteFileIf(ctx context.Context, name string, r io.Reader, want vfs.Version, opts ...vfs.WriteOption) (vfs.FileInfo, error) {
	return f.write(ctx, name, r, cond{ifVersion: true, want: want}, opts)
}

// CreateExclusive writes name only if nothing exists there. The commit is one no-replace rename, so
// the file appears whole or not at all, even against other processes.
func (f *FS) CreateExclusive(ctx context.Context, name string, r io.Reader, opts ...vfs.WriteOption) (vfs.FileInfo, error) {
	return f.write(ctx, name, r, cond{exclusive: true}, opts)
}

func (f *FS) write(ctx context.Context, name string, r io.Reader, c cond, opts []vfs.WriteOption) (vfs.FileInfo, error) {
	cfg := vfs.ResolveWrite(opts)
	if err := f.enter(ctx, "write", name, true); err != nil {
		return vfs.FileInfo{}, err
	}
	defer f.leave()
	unlock := f.locks.lock(name)
	defer unlock()

	// the condition, against the entry as it is now
	var cur os.FileInfo
	if fi, err := f.lstat(name); err == nil {
		cur = fi
	} else if !os.IsNotExist(err) {
		return vfs.FileInfo{}, err
	}
	if cur != nil {
		switch {
		case c.exclusive:
			return vfs.FileInfo{}, &vfs.ConflictError{Path: name, Current: info(name, cur)}
		case cur.Mode()&fs.ModeSymlink != 0:
			return vfs.FileInfo{}, &fs.PathError{Op: "write", Path: name, Err: errIsSymlink}
		case cur.IsDir():
			return vfs.FileInfo{}, &fs.PathError{Op: "write", Path: name, Err: errIsDir}
		case c.ifVersion && info(name, cur).Version != c.want:
			return vfs.FileInfo{}, &vfs.ConflictError{Path: name, Want: c.want, Current: info(name, cur)}
		}
	} else if c.ifVersion {
		return vfs.FileInfo{}, &vfs.ConflictError{Path: name, Want: c.want}
	}

	perm := fs.FileMode(0o644)
	switch {
	case cur != nil:
		perm = cur.Mode().Perm()
	case cfg.Perm != 0:
		perm = cfg.Perm
	}

	dir, base := vfs.Split(name)
	p, err := f.pin(dir)
	if err != nil {
		return vfs.FileInfo{}, err
	}
	defer p.close()

	tmp := tempName(base)
	t, err := p.createTemp(tmp, perm)
	if err != nil {
		return vfs.FileInfo{}, &fs.PathError{Op: "write", Path: name, Err: err}
	}
	committed := false
	defer func() {
		if !committed {
			_ = p.unlink(tmp)
		}
	}()
	if err := fill(ctx, t, r, perm); err != nil {
		return vfs.FileInfo{}, &fs.PathError{Op: "write", Path: name, Err: err}
	}
	if err := hook("precommit"); err != nil {
		return vfs.FileInfo{}, &fs.PathError{Op: "write", Path: name, Err: err}
	}

	// COMMIT POINT — one rename on the pinned parent fd
	if c.exclusive {
		err = p.renameNoReplace(tmp, p, base)
	} else {
		err = p.rename(tmp, p, base)
	}
	if err != nil {
		return vfs.FileInfo{}, f.commitFailed("write", name, err, c.exclusive)
	}
	committed = true

	return f.afterCommit(name, p)
}

// fill copies r into t, fixes the mode (the umask applies at create), syncs and closes t.
func fill(ctx context.Context, t *os.File, r io.Reader, perm fs.FileMode) error {
	_, err := io.Copy(t, ctxReader{ctx, r})
	if err == nil {
		err = t.Chmod(perm)
	}
	if err == nil {
		err = t.Sync()
	}
	if cerr := t.Close(); err == nil {
		err = cerr
	}
	return err
}

// commitFailed maps a failed commit rename. Only a no-replace commit gives EEXIST and the flag errnos
// their meaning — an existing target is a conflict, a filesystem without the flag is unsupported; a
// plain rename's errors are returned as they are.
func (f *FS) commitFailed(op, name string, err error, noReplace bool) error {
	switch {
	case noReplace && isExist(err):
		cur := vfs.FileInfo{}
		if fi, serr := f.lstat(name); serr == nil {
			cur = info(name, fi)
		}
		return &vfs.ConflictError{Path: name, Current: cur}
	case noReplace && isNoReplaceUnsupported(err):
		return &fs.PathError{Op: op, Path: name, Err: errs.WrapCause(errs.ErrUnsupported, err,
			"the filesystem under %s has no atomic no-replace rename", name)}
	}
	return &fs.PathError{Op: op, Path: name, Err: err}
}

// afterCommit runs the steps after the commit point; every failure is a *vfs.CommitError.
func (f *FS) afterCommit(name string, parents ...*parent) (vfs.FileInfo, error) {
	if err := hook("postcommit"); err != nil {
		return vfs.FileInfo{}, &vfs.CommitError{Path: name, Err: err}
	}
	for _, p := range parents {
		if err := p.sync(); err != nil {
			return vfs.FileInfo{}, &vfs.CommitError{Path: name, Err: err}
		}
	}
	fi, err := f.lstat(name)
	if err != nil {
		return vfs.FileInfo{}, &vfs.CommitError{Path: name, Err: err}
	}
	return info(name, fi), nil
}

// Rename moves from to to — a file, symlink or directory — with one rename on the two pinned parent
// fds, replacing an existing file at to.
func (f *FS) Rename(ctx context.Context, from, to string) error {
	return f.rename(ctx, from, to, false)
}

// RenameNoReplace is Rename that refuses to replace an existing entry at to.
func (f *FS) RenameNoReplace(ctx context.Context, from, to string) error {
	return f.rename(ctx, from, to, true)
}

func (f *FS) rename(ctx context.Context, from, to string, noReplace bool) error {
	if err := vfs.CheckMutable("rename", from); err != nil {
		return err
	}
	if err := f.enter(ctx, "rename", to, true); err != nil {
		return err
	}
	defer f.leave()
	// Refused here, not left to the kernel: its EINVAL for this case is indistinguishable from the
	// EINVAL a filesystem without RENAME_NOREPLACE returns.
	if strings.HasPrefix(to, from+"/") {
		return &fs.PathError{Op: "rename", Path: to,
			Err: errs.Wrap(errs.ErrInvalidArgument, "cannot move %q into itself", from)}
	}
	unlock := f.locks.lock(from, to)
	defer unlock()

	if _, err := f.lstat(from); err != nil {
		return err
	}
	fromDir, fromBase := vfs.Split(from)
	toDir, toBase := vfs.Split(to)
	src, err := f.pin(fromDir)
	if err != nil {
		return err
	}
	defer src.close()
	dst := src
	if toDir != fromDir {
		if dst, err = f.pin(toDir); err != nil {
			return err
		}
		defer dst.close()
	}
	if noReplace {
		err = src.renameNoReplace(fromBase, dst, toBase)
	} else {
		err = src.rename(fromBase, dst, toBase)
	}
	if err != nil {
		return f.commitFailed("rename", to, err, noReplace)
	}
	parents := []*parent{src}
	if dst != src {
		parents = append(parents, dst)
	}
	_, err = f.afterCommit(to, parents...)
	return err
}

// ctxReader aborts a copy once ctx ends.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
