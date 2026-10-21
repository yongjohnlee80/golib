package widget

import (
	"testing"
	"testing/fstest"
)

// A folder shown is reported to the owner, and an owner that answers it by showing another folder is
// told of that one last: the first folder's remaining reports are not made over the second's rows
// (C1). Without a move, the owner is told the folder once, the positive control.
func TestAFileListToldOfAFolderItsOwnerLeftReportsTheNewOneLast(t *testing.T) {
	fsys := fstest.MapFS{"a/x.txt": {}, "b/y.txt": {}}
	newList := func(onShow func(l *FileList)) (*FileList, *[]string) {
		l := NewFileList(WithFileListSource(FileSource{FS: fsys, Root: "/"}))
		told := new([]string)
		l.onDir = func(d string) { *told = append(*told, d) }
		l.onShow = func() { onShow(l) }
		return l, told
	}

	l, told := newList(func(*FileList) {})
	l.SetDir("/a")
	if len(*told) != 1 || (*told)[0] != "/a" || l.Dir() != "a" {
		t.Errorf("a plain move: told %v, in %q; want [/a], a", *told, l.Dir())
	}

	moved := false
	l, told = newList(func(l *FileList) {
		if !moved {
			moved = true
			l.SetDir("/b")
		}
	})
	l.SetDir("/a")
	if last := (*told)[len(*told)-1]; !moved || l.Dir() != "b" || last != "/b" {
		t.Errorf("the owner moved to /b while /a was shown: in %q, told %v; want the last report /b", l.Dir(), *told)
	}
}
