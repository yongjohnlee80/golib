package vfs

import (
	"io/fs"
	"strings"
)

// WriteOption configures a write (WriteFile, WriteFileIf, CreateExclusive, Copy).
type WriteOption func(*WriteConfig)

// WriteConfig is the resolved form of a set of [WriteOption] values. Drivers read it through
// [ResolveWrite]; callers do not build it.
type WriteConfig struct {
	Perm fs.FileMode // mode for a newly created file; 0 means the driver default
}

// WithPerm sets the mode of a newly created file. An existing file keeps its mode.
func WithPerm(p fs.FileMode) WriteOption { return func(c *WriteConfig) { c.Perm = p.Perm() } }

// ResolveWrite applies opts in order.
func ResolveWrite(opts []WriteOption) WriteConfig {
	var cfg WriteConfig
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	return cfg
}

// WatchOption configures [Watcher.Watch] and [Poll].
type WatchOption func(*WatchConfig)

// WatchConfig is the resolved form of a set of [WatchOption] values.
type WatchConfig struct {
	Recursive bool
	// Skip reports a directory to leave out, with everything under it; nil skips nothing.
	Skip func(dir string) bool
}

// Recursive watches the whole tree under dir, including directories created later.
func Recursive() WatchOption { return func(c *WatchConfig) { c.Recursive = true } }

// SkipDirs leaves out every directory pred reports true for, with everything under it: [Poll] does
// not list it, and a [Watcher] neither watches it nor reports events in it. Events for the skipped
// directory itself — it appears, goes, is renamed — are still reported, so the caller learns it
// exists; but its appearance is never an OpOverflow, which would ask the caller to read the very
// subtree it skips.
//
// pred gets root-relative, slash-separated directory paths — never the watched dir itself, never a
// file — and must be cheap and safe for concurrent use: a Watcher may call it from its own goroutine.
func SkipDirs(pred func(dir string) bool) WatchOption {
	return func(c *WatchConfig) { c.Skip = pred }
}

// Skipped reports whether p lies in a directory cfg skips: p itself, when isDir, or any directory
// between dir (exclusive) and p. Drivers use it to filter what they report.
func (c WatchConfig) Skipped(dir, p string, isDir bool) bool {
	return skippedUnder(c.Skip, dir, p, isDir)
}

// WalkOption configures [Walk].
type WalkOption func(*WalkConfig)

// WalkConfig is the resolved form of a set of [WalkOption] values.
type WalkConfig struct {
	// Skip reports a directory Walk yields but does not descend; nil descends everything.
	Skip func(dir string) bool
}

// WalkSkipDirs makes [Walk] yield each directory pred reports true for — so the caller sees it
// exists — without descending it. pred gets root-relative, slash-separated directory paths.
func WalkSkipDirs(pred func(dir string) bool) WalkOption {
	return func(c *WalkConfig) { c.Skip = pred }
}

// ResolveWalk applies opts in order.
func ResolveWalk(opts []WalkOption) WalkConfig {
	var cfg WalkConfig
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	return cfg
}

// skippedUnder is SkipDirs' rule: p, when it is a directory, or any directory strictly between dir
// and p, is one skip reports.
func skippedUnder(skip func(string) bool, dir, p string, isDir bool) bool {
	if skip == nil || p == dir {
		return false
	}
	rel := p
	if dir != "." && dir != "" {
		rel = strings.TrimPrefix(p, dir+"/")
	}
	parts := strings.Split(rel, "/")
	n := len(parts)
	if !isDir {
		n-- // a file is never skipped itself, only by a directory above it
	}
	base := dir
	for i := 0; i < n; i++ {
		base = Join(base, parts[i])
		if skip(base) {
			return true
		}
	}
	return false
}

// ResolveWatch applies opts in order.
func ResolveWatch(opts []WatchOption) WatchConfig {
	var cfg WatchConfig
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}
	return cfg
}
