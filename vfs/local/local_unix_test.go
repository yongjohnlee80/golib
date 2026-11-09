//go:build linux || darwin

// The cells for POSIX behaviour alone: a FIFO refused without waiting for a writer, and the
// no-replace flag's EINVAL probe.

package local

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/yongjohnlee80/golib/errs"
)

// TestRenameIntoItselfThroughLink: a move into itself the lexical check cannot see (through an in-root
// symlink alias) fails as the kernel's EINVAL, not as an unsupported no-replace flag, and the probe that
// decided it leaves no file behind.
func TestRenameIntoItselfThroughLink(t *testing.T) {
	f, dir := newLocal(t)
	_ = f.MkdirAll(bg, "d")
	_ = os.Symlink("d", filepath.Join(dir, "link"))
	for name, run := range map[string]func() error{
		"Rename":          func() error { return f.Rename(bg, "d", "link/sub") },
		"RenameNoReplace": func() error { return f.RenameNoReplace(bg, "d", "link/sub") },
	} {
		err := run()
		if !errors.Is(err, unix.EINVAL) || errors.Is(err, errs.ErrUnsupported) {
			t.Fatalf("%s through an alias: err = %v, want EINVAL and not ErrUnsupported", name, err)
		}
	}
	if st, err := f.Stat(bg, "d"); err != nil || !st.IsDir() {
		t.Fatalf("d after the refused moves: %+v, %v", st, err)
	}
	if tt := temps(t, dir); len(tt) != 0 {
		t.Fatalf("the probe left files behind: %v", tt)
	}
}

// TestOpenRefusesAFileThatIsNotRegular: a FIFO is refused at once, not after a writer comes (an open
// for reading waits for one), and a regular file beside it still opens. The answer is waited for
// in a goroutine, so a blocking Open fails the cell instead of hanging the package.
func TestOpenRefusesAFileThatIsNotRegular(t *testing.T) {
	f, dir := newLocal(t)
	if err := unix.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, f, "file", "data")
	done := make(chan error, 1)
	go func() {
		rc, err := f.Open(bg, "pipe", 0)
		if err == nil {
			rc.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("Open of a FIFO: %v, want ErrInvalidArgument", err)
		}
	case <-time.After(5 * time.Second):
		// unblock the open, so the goroutine and the package can finish
		if w, err := os.OpenFile(filepath.Join(dir, "pipe"), os.O_WRONLY|unix.O_NONBLOCK, 0); err == nil {
			w.Close()
		}
		t.Fatal("Open of a FIFO waited for a writer")
	}
	rc, err := f.Open(bg, "file", 0)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if b, err := io.ReadAll(rc); err != nil || string(b) != "data" {
		t.Errorf("the regular file read %q, %v", b, err)
	}
}
