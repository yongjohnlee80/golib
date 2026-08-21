//go:build !unix

package sshkey

import (
	"fmt"
	"os/exec"
)

// This platform has no POSIX process groups, so a timed-out `ssh-keygen` cannot
// be guaranteed to take its descendants with it — the exact resource leak the
// unix path exists to close would still be reachable here.
//
// Rather than ship a verifier that documents whole-tree cleanup and silently
// does not perform it, [NewOpenSSH] refuses to construct on this platform. A
// private comment would not be capability honesty; a typed construction error
// is.
//
// The alternative was an untested Windows Job Object implementation guarding a
// security property, which is worse than an honest refusal. Callers here use
// [NewPureGo], which forks nothing and therefore has nothing to leak.
func supportsProcessGroups() error {
	return fmt.Errorf("%w: the delegating verifier needs POSIX process groups to "+
		"bound a timed-out ssh-keygen and reap its descendants, which this platform "+
		"does not provide — use sshkey.NewPureGo instead", ErrUnsupportedPlatform)
}

func isolateProcessGroup(*exec.Cmd) {}
