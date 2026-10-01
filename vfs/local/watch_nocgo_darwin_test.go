//go:build darwin && !cgo

package local

import (
	"testing"

	"github.com/yongjohnlee80/golib/vfs"
)

// TestNoWatcherWithoutCgo: a cgo-less macOS build does not offer a watch, so callers take vfs.Poll
// rather than a Watch that cannot work.
func TestNoWatcherWithoutCgo(t *testing.T) {
	if _, ok := any(&FS{}).(vfs.Watcher); ok {
		t.Fatal("*FS implements vfs.Watcher in a build without cgo")
	}
}
