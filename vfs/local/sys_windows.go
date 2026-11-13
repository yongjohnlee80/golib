//go:build windows

package local

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// parent is a directory pinned by its handle. A create goes through the FS's os.Root with the
// directory's root-relative path, so the jail resolves the name; a rename and a no-replace rename
// open the entry relative to the parent's handle and move it relative to the destination's handle
// (renameAt), so no path is looked up again: a directory renamed, or a junction swapped in on its
// path, after it was pinned can't redirect the move, as the fd keeps a Unix write in place.
type parent struct {
	root *os.Root // the operation's: held from New, or with RootPerOperation the operation's own
	dir  string   // root-relative, OS separators; "." for the root
	d    *os.File
}

// pin opens dir through the root (so the jail resolves it) and keeps its handle.
func (f *FS) pin(r *os.Root, dir string) (*parent, error) {
	d, err := r.Open(dir)
	if err != nil {
		return nil, err
	}
	return &parent{root: r, dir: filepath.FromSlash(dir), d: d}, nil
}

func (p *parent) handle() windows.Handle { return windows.Handle(p.d.Fd()) }

func (p *parent) path(name string) string { return filepath.Join(p.dir, name) }

// createTemp creates name exclusively in p: O_EXCL refuses anything already there, a symlink planted
// at the temp name included.
func (p *parent) createTemp(name string, perm fs.FileMode) (*os.File, error) {
	return p.root.OpenFile(p.path(name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
}

// rename moves from in p to to in dst, replacing an existing file at to. A file another handle
// holds open without delete-sharing can't be replaced while it's held: an editor, a virus
// scanner, the search indexer, or any reader through os.Open (Go opens without FILE_SHARE_DELETE)
// refuses it with a sharing violation, or access denied. Such a hold is brief, so the replace is
// tried again for up to replaceBusyWait, as Go's own toolchain does (cmd/go/internal/robustio),
// and then the last refusal is answered whole.
func (p *parent) rename(from string, dst *parent, to string) error {
	_ = whileBusy // SCRATCH (never merged): the mutant without the wait
	return renameAt(p.handle(), from, dst.handle(), to, true)
}

// replaceBusyWait bounds how long a replace waits out a target held open (rename).
const replaceBusyWait = time.Second

// whileBusy runs op, and runs it again, backing off from 1 ms to 64 ms, while it fails because
// the file is held (isBusy) and replaceBusyWait hasn't run out; its last error is op's own.
func whileBusy(op func() error) error {
	deadline := time.Now().Add(replaceBusyWait)
	wait := time.Millisecond
	for {
		err := op()
		if err == nil || !isBusy(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(wait)
		wait = min(2*wait, 64*time.Millisecond)
	}
}

// isBusy: the refusals of a file another handle holds open without delete-sharing.
func isBusy(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_ACCESS_DENIED)
}

func (p *parent) unlink(name string) error { return p.root.Remove(p.path(name)) }

// sync has nothing to flush: Windows can't flush a directory handle, and NTFS journals its metadata.
func (p *parent) sync() error { return nil }

func (p *parent) close() error { return p.d.Close() }

func isExist(err error) bool { return errors.Is(err, fs.ErrExist) }

// supportsNoReplace: every Windows filesystem refuses a rename onto an existing name when replace
// isn't asked for.
func (p *parent) supportsNoReplace() bool { return true }

// renameNoReplace moves from in p to to in dst in one atomic step, failing with an existing-file
// error if to exists.
func (p *parent) renameNoReplace(from string, dst *parent, to string) error {
	return renameAt(p.handle(), from, dst.handle(), to, false)
}

// renameAt is renameat(2) on Windows, as Go's own os.Root.Rename does it: the entry is opened
// relative to srcDir's handle, not following a reparse point at the name, and renamed by
// NtSetInformationFile relative to dstDir's handle. The move is atomic, and with replace false it
// fails with STATUS_OBJECT_NAME_COLLISION (an existing-file error) when dst exists.
func renameAt(srcDir windows.Handle, src string, dstDir windows.Handle, dst string, replace bool) error {
	name, err := windows.NewNTUnicodeString(src)
	if err != nil {
		return err
	}
	oa := &windows.OBJECT_ATTRIBUTES{RootDirectory: srcDir, ObjectName: name, Attributes: windows.OBJ_CASE_INSENSITIVE}
	oa.Length = uint32(unsafe.Sizeof(*oa))
	var h windows.Handle
	var iosb windows.IO_STATUS_BLOCK
	if err := windows.NtCreateFile(&h, windows.SYNCHRONIZE|windows.DELETE, oa, &iosb, nil, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, windows.FILE_OPEN,
		windows.FILE_OPEN_REPARSE_POINT|windows.FILE_OPEN_FOR_BACKUP_INTENT|windows.FILE_SYNCHRONOUS_IO_NONALERT, 0, 0); err != nil {
		return ntErr(err)
	}
	defer windows.CloseHandle(h)
	p16, err := windows.UTF16FromString(dst)
	if err != nil {
		return err
	}
	// FILE_RENAME_INFORMATION_EX's layout; FILE_RENAME_INFORMATION's is the same, its BOOLEAN
	// ReplaceIfExists in the first byte of Flags
	var info struct {
		Flags          uint32
		RootDirectory  windows.Handle
		FileNameLength uint32
		FileName       [windows.MAX_PATH]uint16
	}
	n := len(p16) - 1 // without the NUL
	if n > len(info.FileName) {
		return syscall.ENAMETOOLONG
	}
	copy(info.FileName[:], p16[:n])
	info.RootDirectory, info.FileNameLength = dstDir, uint32(n*2)
	info.Flags = windows.FILE_RENAME_POSIX_SEMANTICS
	if replace {
		info.Flags |= windows.FILE_RENAME_REPLACE_IF_EXISTS
	}
	const fileRenameInformationEx = 65
	err = windows.NtSetInformationFile(h, &iosb, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), fileRenameInformationEx)
	if err == nil {
		return nil
	}
	// a filesystem without POSIX rename semantics (FAT), or a Windows without the Ex class: the plain
	// class, still atomic, still refusing an existing name when replace is false
	if !errors.Is(err, windows.STATUS_INVALID_INFO_CLASS) && !errors.Is(err, windows.STATUS_INVALID_PARAMETER) &&
		!errors.Is(err, windows.STATUS_NOT_SUPPORTED) {
		return ntErr(err)
	}
	info.Flags = 0
	if replace {
		info.Flags = 1
	}
	return ntErr(windows.NtSetInformationFile(h, &iosb, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), windows.FileRenameInformation))
}

// ntErr is an NTSTATUS as the Win32 error it stands for, so errors.Is(err, fs.ErrExist) and
// fs.ErrNotExist read it as os's errors do.
func ntErr(err error) error {
	var st windows.NTStatus
	if errors.As(err, &st) {
		return st.Errno()
	}
	return err
}

// isNoReplaceUnsupported: the errors that only mean the filesystem can't rename by handle.
func isNoReplaceUnsupported(err error) bool {
	return errors.Is(err, errors.ErrUnsupported) || errors.Is(err, windows.ERROR_INVALID_FUNCTION) ||
		errors.Is(err, windows.ERROR_NOT_SUPPORTED)
}

// isRenameInvalid: Windows has no EINVAL from a move into itself; commitFailed's probe is Unix's.
func isRenameInvalid(error) bool { return false }

// readFlags open a file for reading: Windows has no FIFOs to wait on.
const readFlags = os.O_RDONLY
