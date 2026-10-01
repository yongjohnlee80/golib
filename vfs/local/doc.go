//go:build linux || darwin

// Package local is the [github.com/yongjohnlee80/golib/vfs.FS] driver for a directory on local disk,
// for Linux and macOS.
//
// Every name is resolved through [os.Root], and every write creates its temporary file and commits it
// relative to the parent directory's pinned fd, so nothing reaches outside the root. Replacement is one
// renameat; CreateExclusive and RenameNoReplace commit with renameat2(RENAME_NOREPLACE) on Linux and
// renameatx_np(RENAME_EXCL) on macOS, and return errs.ErrUnsupported on a filesystem that refuses the
// flag.
//
// Versions are a metadata heuristic (device, inode, size, mtime and ctime of the entry's lstat).
// Conditions are exact among callers of one FS, not against other processes. The FS also implements
// [github.com/yongjohnlee80/golib/vfs.Watcher]: with inotify on Linux, and with FSEvents on macOS when
// built with cgo (a cgo-less macOS build polls).
package local
