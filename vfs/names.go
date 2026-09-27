package vfs

import (
	"io/fs"
	"path"
	"strings"
)

// TempPrefix starts the name of every temporary file a driver creates while committing a write. Drivers
// hide such entries from ReadDir, Walk and watch events.
const TempPrefix = ".vfs-tmp-"

// IsTemp reports whether base is a driver's temporary file name.
func IsTemp(base string) bool { return strings.HasPrefix(base, TempPrefix) }

// CheckName validates name for op, returning a *fs.PathError wrapping ErrInvalidName when it is not a
// valid path (see [io/fs.ValidPath]) or names a driver temporary file. Drivers call it before any I/O.
func CheckName(op, name string) error {
	if !fs.ValidPath(name) || IsTemp(path.Base(name)) {
		return &fs.PathError{Op: op, Path: name, Err: ErrInvalidName}
	}
	return nil
}

// CheckMutable is [CheckName] that also rejects the root ("."), which cannot be written, removed or
// renamed.
func CheckMutable(op, name string) error {
	if err := CheckName(op, name); err != nil {
		return err
	}
	if name == "." {
		return &fs.PathError{Op: op, Path: name, Err: ErrInvalidName}
	}
	return nil
}

// Split returns name's parent directory and base name, with "." as the root's parent spelling:
// Split("a/b.md") = ("a", "b.md"); Split("b.md") = (".", "b.md").
func Split(name string) (dir, base string) {
	dir, base = path.Split(name)
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		dir = "."
	}
	return dir, base
}

// Join joins a directory and a base name; Join(".", "b.md") = "b.md".
func Join(dir, base string) string {
	if dir == "." || dir == "" {
		return base
	}
	return dir + "/" + base
}
