//go:build unix

package sshkey

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A wedged verifier must not hold an attach open — and must not LEAVE ANYTHING
// BEHIND.
//
// This is the regression test for a bug that shipped in the first cut:
// exec.CommandContext's default cancellation kills only ssh-keygen, and
// WaitDelay only closes our end of the pipes. Neither reaps a descendant, so the
// call returned in ~1.1s while the grandchild kept running — repeated timeouts
// would accumulate processes indefinitely. The fix is an owned process group
// killed as a group; the assertion below is that the grandchild is gone, not
// merely that the call returned.
func TestOpenSSH_TimeoutKillsTheWholeTree(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no POSIX shell to build a stub with")
	}
	dir := t.TempDir()
	signers := filepath.Join(dir, "signers")
	if err := os.WriteFile(signers, []byte("x ssh-ed25519 AAAA\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pidFile := filepath.Join(dir, "grandchild.pid")

	// The stub spawns a GRANDCHILD and waits on it, so killing only the direct
	// child leaves the grandchild running — which is exactly the bug.
	stub := filepath.Join(dir, "hang")
	script := "#!" + sh + "\nsleep 30 &\necho $! > " + pidFile + "\nwait\n"
	if err := os.WriteFile(stub, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	const timeout = 200 * time.Millisecond
	v, err := NewOpenSSH(signers, Binary(stub), VerifyTimeout(timeout))
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	err = v.VerifySignature(context.Background(), []byte("m"), []byte("sig"), testNS, "alice")
	elapsed := time.Since(start)

	if !errors.Is(err, ErrVerifierUnavailable) {
		t.Errorf("err = %v, want ErrVerifierUnavailable on timeout", err)
	}
	if errors.Is(err, ErrBadSignature) {
		t.Error("a timeout is not a rejected signature")
	}
	// Tight bound: the deadline plus the WaitDelay, plus slack for CI noise.
	// Anything materially larger means the deadline is not what ended the call.
	if want := timeout + waitDelay + 2*time.Second; elapsed > want {
		t.Errorf("took %v, want under %v — the timeout is not bounding the call", elapsed, want)
	}

	pid := grandchildPID(t, pidFile)
	if pid == 0 {
		// Not a skip: without the pid there is nothing proving the fix works, so
		// a green run here would be a false pass on the property under test.
		t.Fatal("the stub never recorded a grandchild pid — the cleanup assertion " +
			"could not run, so this test proves nothing")
	}
	// Poll: the group kill is delivered before Run returns, but reaping is
	// asynchronous.
	deadline := time.Now().Add(3 * time.Second)
	for {
		// ESRCH specifically. Treating ANY error as "gone" would let EPERM — a
		// pid recycled into a process we may not signal — read as success.
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return // gone, which is the point of the test
		}
		if err != nil {
			t.Fatalf("kill(%d, 0) = %v, want ESRCH: cannot tell whether the "+
				"grandchild died", pid, err)
		}
		if time.Now().After(deadline) {
			// Do not leak it into the rest of the suite regardless.
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("grandchild pid %d survived the timeout: cancellation is not "+
				"reaching the process group, so repeated timeouts accumulate processes", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func grandchildPID(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	return 0
}

// The child must land in its own process group, or the group kill above would
// signal this process's group instead of the child's.
func TestIsolateProcessGroup(t *testing.T) {
	t.Parallel()
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary")
	}
	// CommandContext, because isolateProcessGroup sets Cancel and os/exec
	// refuses a non-nil Cancel on a plain Command.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(ctx, sleep, "5")
	isolateProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		_ = cmd.Wait()
	}()

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatal(err)
	}
	if pgid != cmd.Process.Pid {
		t.Errorf("pgid = %d, child pid = %d: the child is not in its own group, so "+
			"killing -pgid would signal the wrong processes", pgid, cmd.Process.Pid)
	}
	if pgid == syscall.Getpgrp() {
		t.Fatal("the child shares THIS process's group — a group kill would signal us")
	}
}
