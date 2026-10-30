//go:build unix

package diag

import (
	"fmt"
	"os"
	"syscall"
)

// noFollow refuses to open a symlink in the directory's place (a samples or lock file planted as
// a link to another file is refused, not written through).
const noFollow = syscall.O_NOFOLLOW

// checkDir refuses a directory another user could have prepared or can change: a symlink in its
// place, one owned by another user, or one that group or others can write (a 0755 one passes).
// Its files are opened by name within it, so whoever could write it could swap them.
func checkDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("diag: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("diag: %s is a symlink: pass the directory itself", dir)
	}
	if !fi.IsDir() {
		return fmt.Errorf("diag: %s is not a directory", dir)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("diag: %s belongs to another user (uid %d)", dir, st.Uid)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("diag: %s is writable by group or others (%v): chmod go-w it, or choose another directory", dir, fi.Mode().Perm())
	}
	return nil
}
