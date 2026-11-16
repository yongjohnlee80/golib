package local

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// DirFS is os.DirFS(dir) whose files are opened so that another program may rename over them or
// delete them while they are open (openShared). A program that follows files a user edits, as a
// hot reload's poller does, must not make the user's editor fail its save: on Windows a file Go's
// os.Open holds can be neither replaced nor deleted.
func DirFS(dir string) fs.FS { return dirFS{dir: dir, sys: os.DirFS(dir)} }

type dirFS struct {
	dir string
	sys fs.FS // directories and stats: they hold no file a save replaces
}

var (
	_ fs.ReadFileFS = dirFS{}
	_ fs.ReadDirFS  = dirFS{}
	_ fs.StatFS     = dirFS{}
)

func (d dirFS) Open(name string) (fs.File, error) {
	st, err := fs.Stat(d.sys, name)
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		return d.sys.Open(name)
	}
	p, err := filepath.Localize(name)
	if err != nil || !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	f, err := openShared(filepath.Join(d.dir, p))
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: underlying(err)}
	}
	return f, nil
}

func (d dirFS) ReadFile(name string) ([]byte, error) {
	f, err := d.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func (d dirFS) ReadDir(name string) ([]fs.DirEntry, error) { return fs.ReadDir(d.sys, name) }

func (d dirFS) Stat(name string) (fs.FileInfo, error) { return fs.Stat(d.sys, name) }

// underlying is err without the *PathError the OS's open wrapped it in.
func underlying(err error) error {
	if pe, ok := err.(*fs.PathError); ok {
		return pe.Err
	}
	return err
}
