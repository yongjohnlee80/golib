//go:build linux || darwin

package local

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// parent is a directory pinned by fd. Every create, rename and unlink of a write happens relative to
// it with base names only, so a concurrent rename of the directory — or a symlink swapped in on its
// path — cannot split the temp file from its commit or move either outside the root.
type parent struct {
	d  *os.File
	fd int
}

// pin opens dir through the root (so the jail resolves it) and keeps its fd.
func (f *FS) pin(dir string) (*parent, error) {
	d, err := f.root.Open(dir)
	if err != nil {
		return nil, err
	}
	return &parent{d: d, fd: int(d.Fd())}, nil
}

// createTemp creates name exclusively in p; O_NOFOLLOW refuses a symlink planted at the temp name.
func (p *parent) createTemp(name string, perm fs.FileMode) (*os.File, error) {
	fd, err := unix.Openat(p.fd, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, uint32(perm))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), name), nil
}

// rename is renameat(2) from p to dst, replacing an existing file at to.
func (p *parent) rename(from string, dst *parent, to string) error {
	return unix.Renameat(p.fd, from, dst.fd, to)
}

func (p *parent) unlink(name string) error { return unix.Unlinkat(p.fd, name, 0) }

// sync flushes the directory entry changes made in p.
func (p *parent) sync() error { return p.d.Sync() }

func (p *parent) close() error { return p.d.Close() }

func isExist(err error) bool { return errors.Is(err, unix.EEXIST) }

// supportsNoReplace probes whether p's filesystem accepts the no-replace flag, by creating a temp and
// no-replace-renaming it to a fresh temp name. It decides what an EINVAL from a no-replace commit meant.
// A probe that cannot run claims nothing: it reports true, so the EINVAL is returned as it is.
func (p *parent) supportsNoReplace() bool {
	from, to := tempName("probe"), tempName("probe")
	t, err := p.createTemp(from, 0o600)
	if err != nil {
		return true
	}
	t.Close()
	if err := p.renameNoReplace(from, p, to); err != nil {
		_ = p.unlink(from)
		return !errors.Is(err, unix.EINVAL) && !isNoReplaceUnsupported(err)
	}
	_ = p.unlink(to)
	return true
}
