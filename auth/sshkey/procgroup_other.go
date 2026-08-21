//go:build !unix

package sshkey

import "os/exec"

// On platforms without POSIX process groups, cancellation reaches only
// ssh-keygen itself; a descendant it spawned can outlive the timeout. Windows
// would need a Job Object to match the unix behavior. Recorded rather than
// silently absent: a deployment there gets the timeout but not the cleanup.
func isolateProcessGroup(*exec.Cmd) {}

func reapProcessGroup(*exec.Cmd) {}
