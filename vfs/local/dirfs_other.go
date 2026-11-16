//go:build !windows

package local

import "os"

// openShared opens path to read. A Unix open never keeps another program from replacing or
// deleting the file.
func openShared(path string) (*os.File, error) { return os.Open(path) }
