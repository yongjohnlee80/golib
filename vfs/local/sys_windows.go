//go:build windows

package local

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// parent is a directory held open while a write or a rename runs in it. Every create, rename and
// unlink goes through the FS's os.Root with the directory's root-relative path, so the jail resolves
// each name and refuses one that would leave the root. Windows refuses to rename or remove a
// directory while a handle is open on it, so the open handle keeps the directory where it is, as the
// fd does on Unix.
type parent struct {
	root    *os.Root
	absRoot string // the root's absolute path: a directory's no-replace rename is path-based
	dir     string // root-relative, OS separators; "." for the root
	d       *os.File
}

// pin opens dir through the root (so the jail resolves it) and keeps it open.
func (f *FS) pin(dir string) (*parent, error) {
	d, err := f.root.Open(dir)
	if err != nil {
		return nil, err
	}
	return &parent{root: f.root, absRoot: f.absRoot, dir: filepath.FromSlash(dir), d: d}, nil
}

func (p *parent) path(name string) string { return filepath.Join(p.dir, name) }

// createTemp creates name exclusively in p: O_EXCL refuses anything already there, a symlink planted
// at the temp name included.
func (p *parent) createTemp(name string, perm fs.FileMode) (*os.File, error) {
	return p.root.OpenFile(p.path(name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
}

// rename moves from in p to to in dst, replacing an existing file at to.
func (p *parent) rename(from string, dst *parent, to string) error {
	return p.root.Rename(p.path(from), dst.path(to))
}

func (p *parent) unlink(name string) error { return p.root.Remove(p.path(name)) }

// sync has nothing to flush: Windows can't flush a directory handle, and NTFS journals its metadata.
func (p *parent) sync() error { return nil }

func (p *parent) close() error { return p.d.Close() }

func isExist(err error) bool { return errors.Is(err, fs.ErrExist) }

// supportsNoReplace: every Windows filesystem refuses a hard link or a move onto an existing name.
func (p *parent) supportsNoReplace() bool { return true }

// renameNoReplace moves from in p to to in dst, failing with an existing-file error if to exists. A
// file is hard-linked at to and then unlinked at from: the link appears whole and at once, so a
// reader sees either no file at to or the whole of it. A directory has no hard link, so it moves by
// MoveFileEx without MOVEFILE_REPLACE_EXISTING, on its absolute path, which is atomic and refuses
// an existing target.
func (p *parent) renameNoReplace(from string, dst *parent, to string) error {
	fi, err := p.root.Lstat(p.path(from))
	if err != nil {
		return err
	}
	if fi.IsDir() {
		src, err := windows.UTF16PtrFromString(filepath.Join(p.absRoot, p.path(from)))
		if err != nil {
			return err
		}
		tgt, err := windows.UTF16PtrFromString(filepath.Join(dst.absRoot, dst.path(to)))
		if err != nil {
			return err
		}
		return windows.MoveFileEx(src, tgt, windows.MOVEFILE_WRITE_THROUGH)
	}
	if err := p.root.Link(p.path(from), dst.path(to)); err != nil {
		return err
	}
	return p.root.Remove(p.path(from))
}

// isNoReplaceUnsupported: the errors that only mean the filesystem has no hard links (FAT, some
// network shares).
func isNoReplaceUnsupported(err error) bool {
	return errors.Is(err, errors.ErrUnsupported) || errors.Is(err, windows.ERROR_INVALID_FUNCTION) ||
		errors.Is(err, windows.ERROR_NOT_SUPPORTED)
}

// isRenameInvalid: Windows has no EINVAL from a move into itself; commitFailed's probe is Unix's.
func isRenameInvalid(error) bool { return false }

// readFlags open a file for reading: Windows has no FIFOs to wait on.
const readFlags = os.O_RDONLY
