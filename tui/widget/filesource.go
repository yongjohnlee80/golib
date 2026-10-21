package widget

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yongjohnlee80/golib/tui"
	"github.com/yongjohnlee80/golib/vfs"
)

// The FILE SOURCE the file widgets read: the filesystem, how its paths are written for a person,
// what a path names, and reading it on the loop or off it.

// FileSource is a filesystem the file widgets list, and how its paths are
// written for a person.
//
// PLATFORM-INDEPENDENT: the widgets reach files only through io/fs, so a
// FileSource over the local disk, an embedded tree, a zip or a remote store
// (an SFTP or object-store fs.FS) lists and previews the same way. Root is
// what a path is written under — "/" locally, "sftp://host/" remotely — and
// every path the widgets report or accept is in that ROOTED form, so a host
// hears an ordinary absolute path from a local dialog.
//
// An fs.FS is read on the UI loop, as the local disk always has been, unless
// Async says to read it off the loop. A source that may be slow (a daemon's, a
// remote store) is a [vfs.FS] in VFS instead, which the widgets read OFF the
// loop, with the reading task's context: a listing or a preview that a newer
// one supersedes, or that its dialog's closing ends, is stopped rather than
// only dropped.
type FileSource struct {
	FS fs.FS
	// VFS, when set, is read instead of FS, off the loop: VFS wins when both
	// are set. Its symlinks are listed as files, since vfs reports a link
	// without following it.
	VFS vfs.FS
	// Async reads FS off the loop, as a VFS source is read: a slow mount (NFS,
	// sshfs) no longer holds the loop, and links are followed as on the loop.
	// An fs.FS read takes no context, so a superseded one cannot be stopped:
	// it runs to its end on a worker and its answer is dropped.
	Async bool
	Root  string
	// Home is the rooted folder ~ stands for in a typed path; "" when the source has none, and
	// then a typed ~ is refused rather than guessed.
	Home string
}

// LocalFiles is the local filesystem, rooted at "/", ~ its user's home folder.
func LocalFiles() FileSource {
	home, _ := os.UserHomeDir()
	return FileSource{FS: os.DirFS("/"), Root: "/", Home: home}
}

// Rooted writes an fs path as a person reads it: "home/me" → "/home/me".
func (s FileSource) Rooted(p string) string {
	if p == "." || p == "" {
		return s.Root
	}
	return s.Root + p
}

// fsPath reads a rooted path back into an fs path, reporting false for one
// outside this source. A relative path is taken as relative to the root.
func (s FileSource) fsPath(rooted string) (string, bool) {
	p := filepath.ToSlash(rooted)
	if s.Root != "" && strings.HasPrefix(p, s.Root) {
		p = strings.TrimPrefix(p, s.Root)
	} else if strings.HasPrefix(p, "/") {
		return "", false
	}
	p = path.Clean("/" + p)[1:]
	if p == "" {
		p = "."
	}
	return p, fs.ValidPath(p)
}

// or returns the source, or the local filesystem when it has none, read off
// the loop when the source said Async.
func (s FileSource) or() FileSource {
	if s.FS == nil && s.VFS == nil {
		local := LocalFiles()
		local.Async = s.Async
		if s.Home != "" {
			local.Home = s.Home
		}
		return local
	}
	return s
}

// async reports whether the source is read off the loop: a VFS one, or an
// Async fs.FS.
func (s FileSource) async() bool { return s.VFS != nil || s.Async }

// isDir reports whether an fs path is a folder, following a link to one in an
// fs.FS, with ctx bounding a VFS read: what [FileSource.kind] says, the one
// place a path is asked about.
func (s FileSource) isDir(ctx context.Context, p string) bool { return s.kind(ctx, p) == pathFolder }

// readDir lists a folder, its folders and its files each sorted without regard
// to case, with ctx bounding a VFS read.
func (s FileSource) readDir(ctx context.Context, dir string) (dirs, files []fileEntry, err error) {
	if s.VFS != nil {
		infos, err := s.VFS.ReadDir(ctx, dir)
		for _, fi := range infos {
			if fi.IsDir() {
				dirs = append(dirs, fileEntry{name: fi.Name, dir: true})
			} else {
				files = append(files, fileEntry{name: fi.Name})
			}
		}
		sortEntries(dirs)
		sortEntries(files)
		return dirs, files, err
	}
	ds, err := fs.ReadDir(s.FS, dir)
	for _, d := range ds {
		// A LINK to a folder is a folder to the user; asking the source
		// follows it, which DirEntry.IsDir does not.
		if d.IsDir() || (d.Type()&fs.ModeSymlink != 0 && s.isDir(ctx, path.Join(dir, d.Name()))) {
			dirs = append(dirs, fileEntry{name: d.Name(), dir: true})
		} else {
			files = append(files, fileEntry{name: d.Name()})
		}
	}
	sortEntries(dirs)
	sortEntries(files)
	return dirs, files, err
}

func sortEntries(s []fileEntry) {
	sort.Slice(s, func(i, j int) bool { return strings.ToLower(s[i].name) < strings.ToLower(s[j].name) })
}

// open opens a file to read, with ctx bounding a VFS read.
func (s FileSource) open(ctx context.Context, p string) (io.ReadCloser, error) {
	if s.VFS != nil {
		return s.VFS.Open(ctx, p, 0)
	}
	return s.FS.Open(p)
}

// pathKind is what a path names in a source.
type pathKind int

const (
	pathMissing    pathKind = iota // nothing there
	pathFile                       // a file (a link followed, for an fs.FS)
	pathFolder                     // a folder (a link to one, for an fs.FS)
	pathUnreadable                 // there, perhaps, but the source refused to say (permission)
)

// kind reports what an fs path names, following a link in an fs.FS, with ctx bounding a VFS read.
func (s FileSource) kind(ctx context.Context, p string) pathKind {
	var fi fs.FileInfo
	var err error
	if s.VFS != nil {
		var vi interface{ IsDir() bool }
		vi, err = s.VFS.Stat(ctx, p)
		if err == nil {
			return kindOf(vi.IsDir())
		}
	} else {
		fi, err = fs.Stat(s.FS, p)
		if err == nil {
			return kindOf(fi.IsDir())
		}
	}
	if errors.Is(err, fs.ErrNotExist) {
		return pathMissing
	}
	return pathUnreadable
}

func kindOf(folder bool) pathKind {
	if folder {
		return pathFolder
	}
	return pathFile
}

// goRead runs read off the loop as one of the owner's group's tasks: a newer
// one in the group cancels it, as does its owner's unmount, and so does the
// returned stop, which its owner calls when something not run as a task (a
// folder row, an fs.FS source) takes over.
func goRead(ctx *tui.Context, group string, read func(context.Context) (any, error)) (tui.TaskID, context.CancelFunc) {
	own, stop := context.WithCancel(context.Background())
	id := ctx.Go(func(task context.Context) (any, error) {
		c, cancel := context.WithCancel(task)
		defer cancel()
		defer context.AfterFunc(own, cancel)()
		return read(c)
	}, tui.Exclusive(group))
	return id, stop
}
