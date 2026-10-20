// Package fdctl runs code on an *os.File's descriptor without changing the file.
//
// os.File.Fd is the obvious way to reach a descriptor, and the wrong one for a file the runtime
// poller holds (a tty opened with os.OpenFile, a pty master): Fd reverts the file to blocking mode,
// after which a read ignores its deadline and Close cannot interrupt it. Control reaches the
// descriptor through SyscallConn instead, which leaves the file as it is.
package fdctl

import "os"

// Control runs fn on f's descriptor and returns fn's error, or the error reaching the descriptor.
// The descriptor is valid only while fn runs: f cannot be closed under it, and fn must not keep it.
func Control(f *os.File, fn func(fd int) error) error {
	rc, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var fnErr error
	if err := rc.Control(func(fd uintptr) { fnErr = fn(int(fd)) }); err != nil {
		return err
	}
	return fnErr
}
