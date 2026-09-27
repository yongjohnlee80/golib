//go:build darwin

package local

import "syscall"

func timesNs(st *syscall.Stat_t) (mtime, ctime int64) {
	return st.Mtimespec.Nano(), st.Ctimespec.Nano()
}
