//go:build darwin || windows

package local

import (
	"errors"
	"io/fs"
)

// The FSEvents and ReadDirectoryChangesW watches decide what an event means by what is at its path
// when it is handled (watch_presence.go); the inotify watch needs no such probe.

// presence lstats rel through the jail: a probe of its own, through a root of its own.
func (f *FS) presence(rel string) presence {
	r, release, err := f.useRoot()
	if err != nil {
		return unknown
	}
	defer release()
	st, err := f.lstat(r, rel)
	switch {
	case err == nil && st.IsDir():
		return presentDir
	case err == nil:
		return presentFile
	case errors.Is(err, fs.ErrNotExist):
		return absent
	}
	return unknown
}
