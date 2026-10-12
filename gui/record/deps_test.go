package record

import (
	"os/exec"
	"strings"
	"testing"
)

// The record package must stay Gio-free: a terminal build records the same file a
// windowed one does without linking the toolkit, and this is the proof. The gui
// module's other packages are free to import gioui.org; this one never is.
func TestTheRecordPackageStaysGioFree(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Fatalf("go list -deps .: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "gioui.org") {
			t.Fatalf("the record package depends on %s: it must stay Gio-free so a terminal build records the same file without cgo", line)
		}
	}
}
