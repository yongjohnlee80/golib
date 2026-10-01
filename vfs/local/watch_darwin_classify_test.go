//go:build linux || darwin

package local

import (
	"path"
	"slices"
	"testing"

	"github.com/yongjohnlee80/golib/vfs"
)

// The flags FSEvents actually delivered, recorded on macOS 26 arm64: an append still carries
// ItemCreated; a chmod of a directory made before the
// stream started does too.
const (
	flagsCreateFile  = 0x00019100 // IsFile|Modified|InodeMetaMod|Created
	flagsAppend      = 0x00019100 // the same: Created is still set
	flagsRenamedFile = 0x00010800 // IsFile|Renamed
	flagsSaveTarget  = 0x00019900 // IsFile|Modified|InodeMetaMod|Renamed|Created
	flagsDirMovedIn  = 0x00020800 // IsDir|Renamed
	flagsCreateGone  = 0x00018300 // IsFile|Modified|Removed|Created
	flagsDirChmod    = 0x0002c100 // IsDir|XattrMod|ChangeOwner|Created
	flagsRootRemoved = 0x00028300 // IsDir|Modified|Removed|Created
)

func TestFSEventsClassification(t *testing.T) {
	const real = "/private/var/root"
	cases := []struct {
		name      string
		dir       string
		recursive bool
		skip      func(string) bool
		batch     []fsevent
		present   map[string]presence // absent when not listed
		want      []vfs.Event
		end       bool
	}{
		{name: "create", batch: []fsevent{{real + "/a.md", flagsCreateFile}},
			present: map[string]presence{"a.md": presentFile},
			want:    []vfs.Event{{Path: "a.md", Op: vfs.OpCreate}}},
		{name: "append still flagged Created is a create, which consumers treat as a write",
			batch:   []fsevent{{real + "/a.md", flagsAppend}},
			present: map[string]presence{"a.md": presentFile},
			want:    []vfs.Event{{Path: "a.md", Op: vfs.OpCreate}}},
		{name: "a plain modification is a write", batch: []fsevent{{real + "/a.md", 0x00011000}},
			present: map[string]presence{"a.md": presentFile},
			want:    []vfs.Event{{Path: "a.md", Op: vfs.OpWrite}}},
		{name: "atomic save: the temp is never named, the target once",
			batch:   []fsevent{{real + "/.vfs-tmp-1", flagsSaveTarget}, {real + "/a.md", flagsRenamedFile}, {real + "/a.md", flagsSaveTarget}},
			present: map[string]presence{"a.md": presentFile},
			want:    []vfs.Event{{Path: "a.md", Op: vfs.OpCreate}}},
		{name: "created then removed inside the latency is a remove", batch: []fsevent{{real + "/d.md", flagsCreateGone}},
			want: []vfs.Event{{Path: "d.md", Op: vfs.OpRemove}}},
		{name: "a populated directory moved in: create, then its subtree overflowed", recursive: true,
			batch:   []fsevent{{real + "/sub", flagsDirMovedIn}},
			present: map[string]presence{"sub": presentDir},
			want:    []vfs.Event{{Path: "sub", Op: vfs.OpCreate}, {Path: "sub", Op: vfs.OpOverflow}}},
		{name: "a directory moved away is a remove", recursive: true, batch: []fsevent{{real + "/sub", flagsDirMovedIn}},
			want: []vfs.Event{{Path: "sub", Op: vfs.OpRemove}}},
		{name: "a skipped directory moved in is a create and never an overflow", recursive: true,
			skip:    skipNodeModulesDir,
			batch:   []fsevent{{real + "/node_modules", flagsDirMovedIn}},
			present: map[string]presence{"node_modules": presentDir},
			want:    []vfs.Event{{Path: "node_modules", Op: vfs.OpCreate}}},
		{name: "nothing inside a skipped directory", recursive: true, skip: skipNodeModulesDir,
			batch:   []fsevent{{real + "/node_modules/pkg/README.md", flagsCreateFile}, {real + "/x/node_modules/a.js", flagsCreateFile}},
			present: map[string]presence{"node_modules/pkg/README.md": presentFile, "x/node_modules/a.js": presentFile}},
		{name: "a MustScanSubDirs inside a skipped directory is dropped", recursive: true, skip: skipNodeModulesDir,
			batch: []fsevent{{real + "/node_modules/pkg", fseMustScanSubDirs}}},
		{name: "a chmod of an existing directory (Created still set) is a create plus overflow", recursive: true,
			batch:   []fsevent{{real + "/docs", flagsDirChmod}},
			present: map[string]presence{"docs": presentDir},
			want:    []vfs.Event{{Path: "docs", Op: vfs.OpCreate}, {Path: "docs", Op: vfs.OpOverflow}}},
		{name: "a directory's attribute change without Created or Renamed is nothing", recursive: true,
			batch:   []fsevent{{real + "/docs", 0x00024000}},
			present: map[string]presence{"docs": presentDir}},
		{name: "not recursive: a directory appearing is not overflowed, and deeper paths are not reported",
			batch:   []fsevent{{real + "/sub", flagsDirMovedIn}, {real + "/sub/deep.md", flagsCreateFile}},
			present: map[string]presence{"sub": presentDir, "sub/deep.md": presentFile},
			want:    []vfs.Event{{Path: "sub", Op: vfs.OpCreate}}},
		{name: "a watch of docs reports docs/… paths", dir: "docs", recursive: true,
			batch:   []fsevent{{real + "/intro.md", flagsCreateFile}},
			present: map[string]presence{"docs/intro.md": presentFile},
			want:    []vfs.Event{{Path: "docs/intro.md", Op: vfs.OpCreate}}},
		{name: "a path outside the watched directory is ignored", batch: []fsevent{{"/private/var/rootless/a.md", flagsCreateFile}, {"/elsewhere/a.md", flagsCreateFile}}},
		{name: "MustScanSubDirs under the root overflows that subtree", recursive: true,
			batch: []fsevent{{real + "/sub", fseMustScanSubDirs}},
			want:  []vfs.Event{{Path: "sub", Op: vfs.OpOverflow}}},
		{name: "a kernel drop overflows everything", recursive: true,
			batch: []fsevent{{real + "/sub", fseMustScanSubDirs | fseKernelDropped}},
			want:  []vfs.Event{{Path: "", Op: vfs.OpOverflow}}},
		{name: "event ids wrapped overflows everything", batch: []fsevent{{real, fseEventIdsWrapped}},
			want: []vfs.Event{{Path: "", Op: vfs.OpOverflow}}},
		{name: "a volume mounted under the root overflows it", recursive: true, batch: []fsevent{{real + "/vol", fseMount}},
			want: []vfs.Event{{Path: "vol", Op: vfs.OpOverflow}}},
		{name: "the root removed: a final overflow and the end", recursive: true,
			batch:   []fsevent{{real + "/a.md", flagsCreateFile}, {real, fseRootChanged}, {real, flagsRootRemoved}},
			present: map[string]presence{"a.md": presentFile},
			want:    []vfs.Event{{Path: "a.md", Op: vfs.OpCreate}, {Path: "", Op: vfs.OpOverflow}}, end: true},
		{name: "events on the root itself are not reported", batch: []fsevent{{real, flagsDirChmod}, {real, fseHistoryDone}}},
		{name: "an lstat that fails otherwise is a write", batch: []fsevent{{real + "/locked.md", 0x00011000}},
			present: map[string]presence{"locked.md": unknown},
			want:    []vfs.Event{{Path: "locked.md", Op: vfs.OpWrite}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := tc.dir
			if dir == "" {
				dir = "."
			}
			m := fseMapper{realDir: real, dir: dir, recursive: tc.recursive, cfg: vfs.WatchConfig{Recursive: tc.recursive, Skip: tc.skip},
				stat: func(rel string) presence {
					if p, ok := tc.present[rel]; ok {
						return p
					}
					return absent
				}}
			got, end := m.mapBatch(tc.batch)
			if !slices.Equal(got, tc.want) || end != tc.end {
				t.Fatalf("mapBatch = %v (end %v), want %v (end %v)", got, end, tc.want, tc.end)
			}
		})
	}
}

func skipNodeModulesDir(dir string) bool { return path.Base(dir) == "node_modules" }
