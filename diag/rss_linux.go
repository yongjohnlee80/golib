//go:build linux

package diag

import (
	"bytes"
	"os"
	"strconv"
)

// residentSize is the process's resident size: /proc/self/statm's second field, in pages.
func residentSize() uint64 {
	b, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	f := bytes.Fields(b)
	if len(f) < 2 {
		return 0
	}
	pages, err := strconv.ParseUint(string(f[1]), 10, 64)
	if err != nil {
		return 0
	}
	return pages * uint64(os.Getpagesize())
}
