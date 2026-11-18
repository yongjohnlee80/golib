package pty

import "golang.org/x/sys/unix"

// hasChildren reports whether pid has a live child process, from the system's process table
// (kern.proc.all) by parent, stopping at the first found. A pid that has exited has none.
func hasChildren(pid int) (bool, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return false, err
	}
	for i := range procs {
		if int(procs[i].Eproc.Ppid) == pid && int(procs[i].Proc.P_pid) != pid {
			return true, nil
		}
	}
	return false, nil
}
