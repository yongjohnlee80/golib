package widget

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// A folder shown is reported to the owner, and an owner that answers its show or dir hook by showing
// another folder hears nothing more of the first once that move returns: the second folder's reports
// are the last (C1). Without a move, the owner is told the folder, the positive control. (The cursor
// report is show's last act, so a move from it has nothing left to cut short.)
func TestAFileListOwnerMovingFromAFolderHookHearsNothingOfTheFolderLeft(t *testing.T) {
	fsys := fstest.MapFS{"a/x.txt": {}, "b/w.txt": {}, "c/v.txt": {}}
	for _, hook := range []string{"", "show", "dir"} {
		name := hook
		if name == "" {
			name = "no move"
		}
		t.Run(name, func(t *testing.T) {
			l := NewFileList(WithFileListSource(FileSource{FS: fsys, Root: "/"}))
			l.SetDir("/a")
			var log []string
			moved := false
			move := func(at string) {
				if !moved && at == hook && l.Dir() == "c" {
					moved = true
					l.SetDir("/b")
					log = append(log, "moved")
				}
			}
			l.onCursor = func(p string, _ bool) { log = append(log, "cursor "+p) }
			l.onShow = func() { log = append(log, "show"); move("show") }
			l.onDir = func(d string) { log = append(log, "dir "+d); move("dir") }

			l.SetDir("/c")
			if hook == "" {
				if l.Dir() != "c" || !slices.Contains(log, "dir /c") || !strings.HasPrefix(log[len(log)-1], "cursor ") {
					t.Errorf("a plain move: in %q, told %q; want /c told, a cursor report last", l.Dir(), log)
				}
				return
			}
			if !moved || l.Dir() != "b" || log[len(log)-1] != "moved" {
				t.Errorf("the owner moved to /b from its %s hook while /c was shown: in %q, told %q; want nothing after the move", hook, l.Dir(), log)
			}
		})
	}
}

// SetCurrent naming the row already under the cursor still tells the owner, as a move would: the
// list reports no move then, so the FileList reports it itself.
func TestAFileListSetCurrentOnTheCurrentRowTellsTheOwner(t *testing.T) {
	l := NewFileList(WithFileListSource(FileSource{FS: fstest.MapFS{"a/x.txt": {}}, Root: "/"}))
	l.SetDir("/a")
	l.SetCurrent("x.txt")
	var told []string
	l.onCursor = func(p string, _ bool) { told = append(told, p) }
	if !l.SetCurrent("x.txt") || len(told) != 1 || told[0] != "a/x.txt" {
		t.Errorf("SetCurrent on the current row: told %q; want [a/x.txt] and true", told)
	}
}
