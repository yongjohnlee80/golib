package pty

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// hasChildren reports whether pid has a live child process, from its threads' children files
// (/proc/<pid>/task/<tid>/children), stopping at the first child found: the answer needs any, not
// a count. A pid with no /proc entry has exited: none.
func hasChildren(pid int) (bool, error) {
	tasks, err := filepath.Glob(filepath.Join("/proc", strconv.Itoa(pid), "task", "*", "children"))
	if err != nil {
		return false, err
	}
	if len(tasks) == 0 {
		if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); errors.Is(err, fs.ErrNotExist) {
			return false, nil // exited
		}
		return false, errors.New("pty: no children file to read")
	}
	for _, f := range tasks {
		b, err := os.ReadFile(f)
		if errors.Is(err, fs.ErrNotExist) {
			continue // the thread or the process ended under the read
		}
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(string(b)) != "" {
			return true, nil
		}
	}
	return false, nil
}
