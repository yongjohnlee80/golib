//go:build darwin && !cgo

package local

// A macOS build without cgo cannot reach FSEvents, so *FS does not implement vfs.Watcher here and
// callers take vfs.Poll, as for any driver without a watch.
