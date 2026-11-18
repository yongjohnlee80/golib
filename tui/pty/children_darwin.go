package pty

import "golang.org/x/sys/unix"

// sZomb is SZOMB in <sys/proc.h>, which x/sys doesn't define: a process ended, not reaped yet.
const sZomb = 5

// hasChildren reports whether pid has a live child process, from the system's process table
// (kern.proc.all) by parent, stopping at the first found. A child that has ended but is not reaped
// yet (a zombie: a job the shell has not waited for) is not live. A pid that has exited has none.
func hasChildren(pid int) (bool, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return false, err
	}
	for i := range procs {
		p := &procs[i]
		if int(p.Eproc.Ppid) == pid && int(p.Proc.P_pid) != pid && p.Proc.P_stat != sZomb {
			return true, nil
		}
	}
	return false, nil
}
