//go:build unix

package sshkey

import (
	"os/exec"
	"syscall"
)

// isolateProcessGroup puts the child in a process group of its own and makes
// cancellation kill that whole group.
//
// Without this, `ssh-keygen` dies on timeout but anything it spawned survives —
// and since Wait blocks on the stderr pipe a descendant still holds, the process
// AND the goroutine both outlive the deadline. The new group id equals the
// child's pid, so signalling -pgid can never reach this process.
func isolateProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// SIGKILL, not SIGTERM: a verification has no state worth flushing, and
		// a descendant that ignores SIGTERM is exactly the case being defended
		// against.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill() // group gone already, or never formed
		}
		return nil
	}
}

// supportsProcessGroups reports that this platform can bound a subprocess tree.
func supportsProcessGroups() error { return nil }
