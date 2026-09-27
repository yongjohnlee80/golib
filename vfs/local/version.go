//go:build linux || darwin

package local

import (
	"fmt"
	"os"
	"syscall"

	"github.com/yongjohnlee80/golib/vfs"
)

// versionOf derives an entry's version from its lstat: device, inode, size, mtime ns and ctime ns. It
// is a heuristic, not a content hash — Stat stays O(1). An atomic replace changes the inode; an
// in-place write moves mtime and ctime, and ctime cannot be set by user tools. Limits: the
// filesystem's timestamp granularity, inode reuse after delete + create, and a foreign write racing
// the lstat.
func versionOf(fi os.FileInfo) vfs.Version {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return vfs.Version(fmt.Sprintf("l0:%x:%x", fi.Size(), fi.ModTime().UnixNano()))
	}
	m, c := timesNs(st)
	return vfs.Version(fmt.Sprintf("l1:%x:%x:%x:%x:%x", st.Dev, st.Ino, st.Size, m, c))
}
