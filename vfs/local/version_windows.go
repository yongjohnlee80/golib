//go:build windows

package local

import (
	"fmt"
	"os"

	"github.com/yongjohnlee80/golib/vfs"
)

// versionOf derives an entry's version: its volume, file index (its inode, in effect), size,
// last-write time and change time (ns), read from its handle (lstat, stat_windows.go). It is a
// heuristic, not a content hash, as on Unix: an atomic replace changes the file index, an in-place
// write moves the change time even when the last-write time is set back. Without the handle's
// attributes, the version is the size and the last-write time alone.
func versionOf(fi os.FileInfo) vfs.Version {
	if w, ok := fi.(winInfo); ok && w.ok {
		return vfs.Version(fmt.Sprintf("w1:%x:%x:%x:%x:%x", w.volume, w.index, fi.Size(), fi.ModTime().UnixNano(), w.change))
	}
	return vfs.Version(fmt.Sprintf("l0:%x:%x", fi.Size(), fi.ModTime().UnixNano()))
}
