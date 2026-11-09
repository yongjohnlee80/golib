//go:build linux || darwin

package local

import "os"

func (f *FS) lstat(name string) (os.FileInfo, error) { return f.root.Lstat(name) }

// fileStat is file's Stat: its Sys is the Stat_t versionOf reads.
func fileStat(file *os.File) (os.FileInfo, error) { return file.Stat() }
