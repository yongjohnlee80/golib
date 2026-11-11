//go:build windows

package local

// SCRATCH (never merged): whether Windows refuses to rename a directory with an open handle beneath
// it, without the driver; and which handles the watch cell holds under its parent at its rename.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/yongjohnlee80/golib/vfs"

	"golang.org/x/sys/windows"
)

func openChildDir(t *testing.T, dir string) windows.Handle {
	t.Helper()
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateFile(p, windows.FILE_LIST_DIRECTORY|windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestScratchNativeAncestorRename(t *testing.T) {
	for _, pending := range []bool{false, true} {
		parent := filepath.Join(t.TempDir(), "parent")
		child := filepath.Join(parent, "child")
		if err := os.MkdirAll(child, 0o755); err != nil {
			t.Fatal(err)
		}
		h := openChildDir(t, child)
		if pending {
			ev, _ := windows.CreateEvent(nil, 1, 0, nil)
			ov := windows.Overlapped{HEvent: ev}
			buf := make([]byte, 64<<10)
			err := windows.ReadDirectoryChanges(h, &buf[0], uint32(len(buf)), true, windows.FILE_NOTIFY_CHANGE_FILE_NAME|windows.FILE_NOTIFY_CHANGE_DIR_NAME, nil, &ov, 0)
			t.Logf("pending=%v ReadDirectoryChanges issued: %v", pending, err)
			defer windows.CloseHandle(ev)
		}
		err := os.Rename(parent, parent+"-moved")
		t.Logf("SCRATCH pending=%v: the parent renamed with the child's handle open: %v", pending, err)
		if err == nil {
			_ = os.Rename(parent+"-moved", parent)
		}
		_ = windows.CancelIoEx(h, nil)
		windows.CloseHandle(h)
		err = os.Rename(parent, parent+"-moved2")
		t.Logf("SCRATCH pending=%v: the parent renamed with the handle closed: %v", pending, err)
	}
}

func TestScratchWatchCellHandles(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.MkdirAll(filepath.Join(parent, "w"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := newPerOp(t, parent)
	events, _ := startWatch(t, f, "w")
	mustWrite(t, f, "w/a.md", "1")
	await(t, events, "w/a.md", vfs.OpCreate)
	err := os.Rename(parent, parent+"-moved")
	t.Logf("SCRATCH the watch running: the parent renamed: %v", err)
	if tool := os.Getenv("HANDLE_EXE"); tool != "" {
		out, herr := exec.Command(tool, "-accepteula", "-nobanner", "-p", strconv.Itoa(os.Getpid()), parent).CombinedOutput()
		t.Logf("SCRATCH handles under %s (%v):\n%s", parent, herr, out)
	}
}
