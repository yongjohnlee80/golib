package widget_test

import (
	"testing"
	"testing/fstest"

	"github.com/yongjohnlee80/golib/tui/widget"
)

// fileListSwappingSource is a FileList listing /a whose owner, told of /c, replaces the source (async
// or not) and notes it. events records the owner's reports.
func fileListSwappingSource(async bool) (l *widget.FileList, events *[]string, moved *bool) {
	events, moved = new([]string), new(bool)
	l = widget.NewFileList(
		widget.WithFileListSource(widget.FileSource{FS: fstest.MapFS{"a/one.txt": {}, "c/old.txt": {}}, Root: "/"}),
		widget.WithFileListDir("/a"),
		widget.WithOnCursor(func(p string, _ bool) { *events = append(*events, "cursor "+p) }),
		widget.WithOnDir(func(dir string) {
			if dir == "/c" && !*moved {
				*moved = true
				l.SetSource(widget.FileSource{FS: fstest.MapFS{"a/new.txt": {}}, Root: "/", Async: async})
				*events = append(*events, "source replaced")
			}
		}),
	)
	*events = nil
	return l, events, moved
}

// An owner that replaces the source from its onDir hook hears nothing more of the folder it left,
// whether the new source is read at once or later: the replacement, not the new rows' arrival, ends
// the old listing (C1). Unmounted, so an async source is listed only once mounted; the synchronous
// replacement is the control.
func TestAFileListOwnerReplacingTheSourceHearsNothingOfTheOldListing(t *testing.T) {
	for _, async := range []bool{false, true} {
		name := map[bool]string{false: "sync control", true: "async, unmounted"}[async]
		t.Run(name, func(t *testing.T) {
			l, events, moved := fileListSwappingSource(async)
			l.SetDir("/c")
			if !*moved {
				t.Fatal("the source-replacing hook never ran")
			}
			if (*events)[len(*events)-1] != "source replaced" {
				t.Errorf("the old listing went on reporting after SetSource returned: %q", *events)
			}
		})
	}
}

// The same, mounted: the async source's read is a task, still in flight when SetSource returns, and
// the old listing must already have stopped.
func TestAMountedFileListOwnerReplacingTheSourceHearsNothingOfTheOldListing(t *testing.T) {
	l, events, moved := fileListSwappingSource(true)
	h := startApp(t, l, 40, 10)
	defer h.stop()
	var got []string
	h.onLoop(func() {
		*events = nil
		l.SetDir("/c")
		got = append(got, *events...) // before the async read can land
	})
	if !*moved {
		t.Fatal("the source-replacing hook never ran")
	}
	if got[len(got)-1] != "source replaced" {
		t.Errorf("the old listing went on reporting after SetSource returned: %q", got)
	}
}
