package memfs_test

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yongjohnlee80/golib/vfs"
	"github.com/yongjohnlee80/golib/vfs/memfs"
	"github.com/yongjohnlee80/golib/vfs/vfstest"
)

func TestConformance(t *testing.T) {
	vfstest.TestFS(t, func(*testing.T) vfs.FS { return memfs.New() })
}

// TestCloseEndsWatch: Close ends a watch whose ctx never ends — idle, and with the consumer stalled
// behind a full channel — closing the channel and leaving no goroutine behind.
func TestCloseEndsWatch(t *testing.T) {
	for _, stalled := range []bool{false, true} {
		t.Run(fmt.Sprint("stalled=", stalled), func(t *testing.T) {
			before := runtime.NumGoroutine()
			fsys := memfs.New()
			events, err := fsys.Watch(context.Background(), ".", vfs.Recursive())
			if err != nil {
				t.Fatal(err)
			}
			if stalled { // more than the channel holds, nobody reading
				for i := range 200 {
					if _, err := fsys.WriteFile(context.Background(), fmt.Sprint("f", i), strings.NewReader("x")); err != nil {
						t.Fatal(err)
					}
				}
				time.Sleep(20 * time.Millisecond) // let run block on the full channel
			}
			if err := fsys.Close(); err != nil {
				t.Fatal(err)
			}
			// Counted BEFORE reading anything: a read would unblock a stalled send and hide the defect.
			for end := time.Now().Add(5 * time.Second); runtime.NumGoroutine() > before; {
				if time.Now().After(end) {
					t.Fatalf("goroutines: %d before, %d after Close with nobody reading", before, runtime.NumGoroutine())
				}
				time.Sleep(10 * time.Millisecond)
			}
			deadline := time.After(5 * time.Second)
			for open := true; open; {
				select {
				case _, open = <-events:
				case <-deadline:
					t.Fatal("the event channel did not close after Close")
				}
			}
		})
	}
}
