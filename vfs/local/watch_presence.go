//go:build linux || darwin || windows

package local

import (
	"strings"

	"github.com/yongjohnlee80/golib/vfs"
)

// presence is what an lstat at handling time found at a path: the FSEvents and ReadDirectoryChangesW
// watches decide what an event means by it, since what they report says what happened at some point,
// not what is there now.
type presence uint8

const (
	absent presence = iota
	presentFile
	presentDir
	unknown // lstat failed other than "does not exist": report a write and let the consumer re-read
)

// presenceEvents is what a change at rel means, by what an lstat finds there now; the FSEvents and
// ReadDirectoryChangesW mappers share it, each deciding created from its own records. A vanished
// path is OpRemove. A directory is reported only when it appeared (a modification of one is its
// children's, which report themselves): OpCreate, and, for a recursive watch, OpOverflow for its
// subtree, since a populated directory moved in reports only itself, unless SkipDirs leaves it out,
// when reading it is the walk the caller asked not to make. A file is OpCreate when it appeared,
// else OpWrite; a path lstat cannot read is OpWrite, for the consumer to re-read.
func presenceEvents(dir, rel string, p presence, created, recursive bool, cfg vfs.WatchConfig) []vfs.Event {
	switch p {
	case absent:
		return []vfs.Event{{Path: rel, Op: vfs.OpRemove}}
	case presentDir:
		if !created {
			return nil
		}
		if recursive && !cfg.Skipped(dir, rel, true) {
			return []vfs.Event{{Path: rel, Op: vfs.OpCreate}, {Path: rel, Op: vfs.OpOverflow}}
		}
		return []vfs.Event{{Path: rel, Op: vfs.OpCreate}}
	case presentFile:
		if created {
			return []vfs.Event{{Path: rel, Op: vfs.OpCreate}}
		}
		return []vfs.Event{{Path: rel, Op: vfs.OpWrite}}
	}
	return []vfs.Event{{Path: rel, Op: vfs.OpWrite}}
}

// watchHidden reports a path a watch of dir never reports: a temp name anywhere in it, anything
// under a SkipDirs directory (isDir also tests rel itself), or, for a watch that is not recursive,
// anything below dir's direct children.
func watchHidden(dir, rel string, isDir, recursive bool, cfg vfs.WatchConfig) bool {
	rest := rel
	if dir != "." && dir != "" {
		rest = strings.TrimPrefix(rel, dir+"/")
	}
	for name := range strings.SplitSeq(rest, "/") {
		if vfs.IsTemp(name) {
			return true
		}
	}
	if !recursive && strings.Contains(rest, "/") {
		return true
	}
	return cfg.Skipped(dir, rel, isDir)
}
