//go:build windows

package local

import (
	"fmt"
	"os"
	"syscall"

	"github.com/yongjohnlee80/golib/vfs"
)

// versionOf derives an entry's version from its attributes: size, last-write time and creation time
// (ns). It is a heuristic, not a content hash, as on Unix. An atomic replace gives the entry the
// temp file's creation time; an in-place write moves the last-write time. Windows exposes no inode
// through os.FileInfo, so the version is made from times alone.
func versionOf(fi os.FileInfo) vfs.Version {
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return vfs.Version(fmt.Sprintf("l0:%x:%x", fi.Size(), fi.ModTime().UnixNano()))
	}
	return vfs.Version(fmt.Sprintf("w1:%x:%x:%x", fi.Size(), d.LastWriteTime.Nanoseconds(), d.CreationTime.Nanoseconds()))
}
