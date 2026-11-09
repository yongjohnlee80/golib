//go:build windows

package files

import (
	"io"
	"io/fs"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// localFiles is the local disk on Windows: every drive, its paths Windows' own.
func localFiles() FileSource {
	home, _ := os.UserHomeDir()
	return FileSource{FS: drivesFS{}, drives: true, Home: home}
}

// drivesFS is Windows' local disk as an fs.FS: "." lists the drives, and "C:/Users/me" is
// C:\Users\me (drivepaths.go). os.DirFS cannot be it, since it has one root and refuses a colon.
type drivesFS struct{}

var (
	_ fs.ReadDirFS = drivesFS{}
	_ fs.StatFS    = drivesFS{}
)

// osPath is name as Windows opens it, or the error an fs.FS gives for a name it does not hold.
func (drivesFS) osPath(op, name string) (string, error) {
	p, ok := drivePath(name)
	if !ok {
		err := fs.ErrNotExist
		if !fs.ValidPath(name) {
			err = fs.ErrInvalid
		}
		return "", &fs.PathError{Op: op, Path: name, Err: err}
	}
	return p, nil
}

func (d drivesFS) Open(name string) (fs.File, error) {
	if name == "." {
		return &driveList{entries: drives()}, nil
	}
	p, err := d.osPath("open", name)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (d drivesFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name == "." {
		return drives(), nil
	}
	p, err := d.osPath("readdir", name)
	if err != nil {
		return nil, err
	}
	return os.ReadDir(p)
}

func (d drivesFS) Stat(name string) (fs.FileInfo, error) {
	if name == "." {
		return driveInfo("."), nil
	}
	p, err := d.osPath("stat", name)
	if err != nil {
		return nil, err
	}
	return os.Stat(p)
}

// drives lists the drives Windows has (GetLogicalDrives), in letter order, each a folder.
func drives() []fs.DirEntry {
	mask, _ := windows.GetLogicalDrives()
	var out []fs.DirEntry
	for i := range 26 {
		if mask&(1<<i) != 0 {
			out = append(out, fs.FileInfoToDirEntry(driveInfo(string(rune('A'+i))+":")))
		}
	}
	return out
}

// driveInfo is a drive, or the list of them ("."), as a folder.
type driveInfo string

func (d driveInfo) Name() string     { return string(d) }
func (driveInfo) Size() int64        { return 0 }
func (driveInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (driveInfo) ModTime() time.Time { return time.Time{} }
func (driveInfo) IsDir() bool        { return true }
func (driveInfo) Sys() any           { return nil }

// driveList is "." opened: the drives, read as a folder.
type driveList struct {
	entries []fs.DirEntry
	read    int
}

func (l *driveList) Stat() (fs.FileInfo, error) { return driveInfo("."), nil }
func (l *driveList) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: ".", Err: fs.ErrInvalid}
}
func (l *driveList) Close() error { return nil }

func (l *driveList) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := l.entries[l.read:]
	if n <= 0 {
		l.read = len(l.entries)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	rest = rest[:min(n, len(rest))]
	l.read += len(rest)
	return rest, nil
}
