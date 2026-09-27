package vfs

import (
	"context"
	"io/fs"
	"iter"

	"github.com/yongjohnlee80/golib/errs"
)

var errWalkNotDir = errs.Sentinel(errs.ErrInvalidArgument, "vfs: walk start is not a directory")

// Walk yields every entry under dir (not dir itself), depth-first, each directory before its contents,
// siblings in ReadDir order. It is built from ReadDir alone and descends an entry only when its IsDir
// reports true, so a symlink to a directory is yielded but never entered. A read error is yielded once, with the FileInfo of the directory
// that failed (Path set), and the walk stops; so does ctx ending. Stopping the loop early is fine.
//
// dir itself is checked the same way: if its entry is not a directory — a file, or a symlink even to a
// directory — Walk yields one error wrapping errs.ErrInvalidArgument and nothing else.
func Walk(ctx context.Context, fsys FS, dir string) iter.Seq2[FileInfo, error] {
	return func(yield func(FileInfo, error) bool) {
		start, err := fsys.Stat(ctx, dir)
		if err != nil {
			yield(FileInfo{Path: dir}, err)
			return
		}
		if !start.IsDir() {
			yield(start, &fs.PathError{Op: "walk", Path: dir, Err: errWalkNotDir})
			return
		}
		walk(ctx, fsys, dir, yield)
	}
}

// walk returns false once the caller stopped or an error was yielded.
func walk(ctx context.Context, fsys FS, dir string, yield func(FileInfo, error) bool) bool {
	if err := ctx.Err(); err != nil {
		yield(FileInfo{Path: dir}, err)
		return false
	}
	entries, err := fsys.ReadDir(ctx, dir)
	if err != nil {
		yield(FileInfo{Path: dir}, err)
		return false
	}
	for _, e := range entries {
		if !yield(e, nil) {
			return false
		}
		if e.IsDir() && !walk(ctx, fsys, e.Path, yield) {
			return false
		}
	}
	return true
}
