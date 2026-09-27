//go:build linux

package local

import "syscall"

func timesNs(st *syscall.Stat_t) (mtime, ctime int64) { return st.Mtim.Nano(), st.Ctim.Nano() }
