//go:build linux || windows

package local

import (
	"encoding/binary"
	"path"
	"slices"
	"testing"
	"unicode/utf16"

	"github.com/yongjohnlee80/golib/vfs"
)

// notifyBuf lays records out as ReadDirectoryChangesW does: each DWORD-aligned, NextEntryOffset 0 on
// the last.
func notifyBuf(recs ...winRecord) []byte {
	var buf []byte
	for i, r := range recs {
		name := utf16.Encode([]rune(r.name))
		size := 12 + 2*len(name)
		size = (size + 3) &^ 3
		rec := make([]byte, size)
		if i < len(recs)-1 {
			binary.LittleEndian.PutUint32(rec, uint32(size))
		}
		binary.LittleEndian.PutUint32(rec[4:], r.action)
		binary.LittleEndian.PutUint32(rec[8:], uint32(2*len(name)))
		for j, c := range name {
			binary.LittleEndian.PutUint16(rec[12+2*j:], c)
		}
		buf = append(buf, rec...)
	}
	return buf
}

// FILE_NOTIFY_INFORMATION records are read whole, in order, names decoded from UTF-16 (a name
// outside the BMP included); a record its buffer cuts short ends the list.
func TestParseNotifyReadsEachRecord(t *testing.T) {
	recs := []winRecord{{winActionAdded, `a.md`}, {winActionRenamedOld, `sub\old name.md`}, {winActionRenamedNew, `sub\새 🙂.md`}}
	buf := notifyBuf(recs...)
	if got := parseNotify(buf); !slices.Equal(got, recs) {
		t.Fatalf("parseNotify = %v, want %v", got, recs)
	}
	if got := parseNotify(buf[:len(buf)-4]); !slices.Equal(got, recs[:2]) {
		t.Fatalf("parseNotify of a buffer cut inside its last record = %v, want the first two whole", got)
	}
	if got := parseNotify(nil); got != nil {
		t.Fatalf("parseNotify(nil) = %v, want none", got)
	}
}

func TestWindowsRecordClassification(t *testing.T) {
	nodeModules := func(d string) bool { return path.Base(d) == "node_modules" }
	cases := []struct {
		name      string
		dir       string
		recursive bool
		skip      func(string) bool
		batch     []winRecord
		present   map[string]presence // absent when not listed
		want      []vfs.Event
	}{
		{name: "a file added", batch: []winRecord{{winActionAdded, `a.md`}},
			present: map[string]presence{"a.md": presentFile},
			want:    []vfs.Event{{Path: "a.md", Op: vfs.OpCreate}}},
		{name: "a file modified twice in one read is one write", batch: []winRecord{{winActionModified, `a.md`}, {winActionModified, `a.md`}},
			present: map[string]presence{"a.md": presentFile},
			want:    []vfs.Event{{Path: "a.md", Op: vfs.OpWrite}}},
		{name: "a file removed", batch: []winRecord{{winActionRemoved, `a.md`}},
			want: []vfs.Event{{Path: "a.md", Op: vfs.OpRemove}}},
		{name: "a rename within", batch: []winRecord{{winActionRenamedOld, `a.md`}, {winActionRenamedNew, `b.md`}},
			present: map[string]presence{"b.md": presentFile},
			want:    []vfs.Event{{Path: "a.md", Op: vfs.OpRemove}, {Path: "b.md", Op: vfs.OpCreate}}},
		{name: "an editor's save: the temp is never reported, the target is a create",
			batch:   []winRecord{{winActionAdded, `.vfs-tmp-1`}, {winActionModified, `.vfs-tmp-1`}, {winActionRemoved, `a.md`}, {winActionRenamedOld, `.vfs-tmp-1`}, {winActionRenamedNew, `a.md`}},
			present: map[string]presence{"a.md": presentFile},
			want:    []vfs.Event{{Path: "a.md", Op: vfs.OpCreate}}},
		{name: "a modified name since removed is a remove", batch: []winRecord{{winActionModified, `a.md`}},
			want: []vfs.Event{{Path: "a.md", Op: vfs.OpRemove}}},
		{name: "a directory's own modification is not reported", recursive: true, batch: []winRecord{{winActionModified, `sub`}},
			present: map[string]presence{"sub": presentDir}},
		{name: "a directory moved in, recursive: create and its subtree's overflow", recursive: true,
			batch:   []winRecord{{winActionRenamedNew, `sub`}},
			present: map[string]presence{"sub": presentDir},
			want:    []vfs.Event{{Path: "sub", Op: vfs.OpCreate}, {Path: "sub", Op: vfs.OpOverflow}}},
		{name: "a directory added, not recursive: create alone", batch: []winRecord{{winActionAdded, `sub`}},
			present: map[string]presence{"sub": presentDir},
			want:    []vfs.Event{{Path: "sub", Op: vfs.OpCreate}}},
		{name: "not recursive: nothing below the direct children", batch: []winRecord{{winActionAdded, `sub\deep.md`}},
			present: map[string]presence{"sub/deep.md": presentFile}},
		{name: "a skipped directory moved in: create, no overflow, nothing inside", recursive: true, skip: nodeModules,
			batch:   []winRecord{{winActionRenamedNew, `node_modules`}, {winActionAdded, `node_modules\pkg\x.md`}},
			present: map[string]presence{"node_modules": presentDir, "node_modules/pkg/x.md": presentFile},
			want:    []vfs.Event{{Path: "node_modules", Op: vfs.OpCreate}}},
		{name: "a watch of a subdirectory reports root-relative paths", dir: "docs/guide", recursive: true,
			batch:   []winRecord{{winActionAdded, `part\intro.md`}},
			present: map[string]presence{"docs/guide/part/intro.md": presentFile},
			want:    []vfs.Event{{Path: "docs/guide/part/intro.md", Op: vfs.OpCreate}}},
		{name: "a name lstat cannot read is a write", batch: []winRecord{{winActionAdded, `locked.md`}},
			present: map[string]presence{"locked.md": unknown},
			want:    []vfs.Event{{Path: "locked.md", Op: vfs.OpWrite}}},
		{name: "a name removed and back in one read is there: a create", batch: []winRecord{{winActionRemoved, `a.md`}, {winActionAdded, `a.md`}},
			present: map[string]presence{"a.md": presentFile},
			want:    []vfs.Event{{Path: "a.md", Op: vfs.OpCreate}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := c.dir
			if dir == "" {
				dir = "."
			}
			m := winMapper{dir: dir, recursive: c.recursive, cfg: vfs.WatchConfig{Recursive: c.recursive, Skip: c.skip},
				stat: func(rel string) presence { return c.present[rel] }}
			if got := m.mapBatch(c.batch); !slices.Equal(got, c.want) {
				t.Fatalf("mapBatch = %v, want %v", got, c.want)
			}
		})
	}
}
