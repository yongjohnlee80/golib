package memfs_test

import (
	"testing"

	"github.com/yongjohnlee80/golib/vfs"
	"github.com/yongjohnlee80/golib/vfs/memfs"
	"github.com/yongjohnlee80/golib/vfs/vfstest"
)

func TestConformance(t *testing.T) {
	vfstest.TestFS(t, func(*testing.T) vfs.FS { return memfs.New() })
}
