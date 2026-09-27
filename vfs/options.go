package vfs

import "io/fs"

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
}

// Recursive watches the whole tree under dir, including directories created later.
func Recursive() WatchOption { return func(c *WatchConfig) { c.Recursive = true } }

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
