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
// (/proc/<pid>/task/<tid>/children), stopping at the first live child found: the answer needs any,
// not a count. A child that has ended but is not reaped yet (a zombie, state Z: a job the shell
// has not waited for) is not live, and neither is one gone under the read. A pid with no /proc
// entry has exited: none.
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
		for _, child := range strings.Fields(string(b)) {
			if live(child) {
				return true, nil
			}
		}
	}
	return false, nil
}

// live reports whether process pid (as /proc names it) is alive: its stat readable and its state
// not Z (ended, not reaped yet).
func live(pid string) bool {
	b, err := os.ReadFile(filepath.Join("/proc", pid, "stat"))
	if err != nil {
		return false // gone under the read
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')') // the command may hold spaces and parentheses
	return i >= 0 && i+2 < len(s) && s[i+2] != 'Z'
}
