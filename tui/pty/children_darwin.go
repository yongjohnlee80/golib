package pty

import "golang.org/x/sys/unix"

// children counts pid's live child processes, from the system's process table (kern.proc.all),
// by parent. A pid that has exited has none.
func children(pid int) (int, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range procs {
		if int(procs[i].Eproc.Ppid) == pid && int(procs[i].Proc.P_pid) != pid {
			n++
		}
	}
	return n, nil
}
