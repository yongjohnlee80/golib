package pty

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// children counts pid's live child processes, from each of its threads' children file
// (/proc/<pid>/task/<tid>/children). A pid with no /proc entry has exited: none.
func children(pid int) (int, error) {
	tasks, err := filepath.Glob(filepath.Join("/proc", strconv.Itoa(pid), "task", "*", "children"))
	if err != nil {
		return 0, err
	}
	if len(tasks) == 0 {
		if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); errors.Is(err, fs.ErrNotExist) {
			return 0, nil // exited
		}
		return 0, errors.New("pty: no children file to read")
	}
	n := 0
	for _, f := range tasks {
		b, err := os.ReadFile(f)
		if errors.Is(err, fs.ErrNotExist) {
			continue // the thread or the process ended under the read
		}
		if err != nil {
			return 0, err
		}
		n += len(strings.Fields(string(b)))
	}
	return n, nil
}
