//go:build linux || darwin || windows

// Package local is the [github.com/yongjohnlee80/golib/vfs.FS] driver for a directory on local disk,
// for Linux, macOS and Windows.
//
// Every name is resolved through [os.Root], and every write creates its temporary file and commits it
// relative to the parent directory's pinned fd, so nothing reaches outside the root. Replacement is one
// renameat; CreateExclusive and RenameNoReplace commit with renameat2(RENAME_NOREPLACE) on Linux and
// renameatx_np(RENAME_EXCL) on macOS, and return errs.ErrUnsupported on a filesystem that refuses the
// flag. On Windows the parent is pinned by its handle, and a rename moves the entry by handle
// (NtSetInformationFile with FILE_RENAME_INFORMATION_EX, relative to the destination's handle),
// replacing or not, atomically.
//
// Versions are a metadata heuristic (device, inode, size, mtime and ctime of the entry's lstat; on
// Windows the handle's volume, file index, size, last-write and change times).
// Conditions are exact among callers of one FS, not against other processes. The FS also implements
// [github.com/yongjohnlee80/golib/vfs.Watcher]: with inotify on Linux, and with FSEvents on macOS when
// built with cgo (a cgo-less macOS build polls). On Windows it has no Watch yet: a caller polls.
package local
