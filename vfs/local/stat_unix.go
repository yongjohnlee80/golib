//go:build linux || darwin

package local

import (
	"io/fs"
	"os"
)

func (f *FS) lstat(name string) (os.FileInfo, error) {
	r, release, err := f.useRoot()
	if err != nil {
		return nil, err
	}
	defer release()
	return r.Lstat(name)
}

// fileStat is file's Stat: its Sys is the Stat_t versionOf reads.
func fileStat(file *os.File) (os.FileInfo, error) { return file.Stat() }

// entryInfo is a listed entry's FileInfo, as Stat would give it: the entry's own, whose Sys is the
// Stat_t versionOf reads.
func (f *FS) entryInfo(_ string, e fs.DirEntry) (os.FileInfo, error) { return e.Info() }
