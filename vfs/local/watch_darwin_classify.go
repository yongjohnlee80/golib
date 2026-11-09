//go:build linux || darwin

package local

import (
	"strings"

	"github.com/yongjohnlee80/golib/vfs"
)

// FSEvents' flags (CoreServices FSEvents.h), as plain numbers so the classification below is pure Go:
// it builds and is tested on Linux as well as on a Mac.
const (
	fseMustScanSubDirs  = 0x00000001
	fseUserDropped      = 0x00000002
	fseKernelDropped    = 0x00000004
	fseEventIdsWrapped  = 0x00000008
	fseHistoryDone      = 0x00000010
	fseRootChanged      = 0x00000020
	fseMount            = 0x00000040
	fseUnmount          = 0x00000080
	fseItemCreated      = 0x00000100
	fseItemRemoved      = 0x00000200
	fseItemInodeMetaMod = 0x00000400
	fseItemRenamed      = 0x00000800
	fseItemModified     = 0x00001000
	fseItemFinderInfo   = 0x00002000
	fseItemChangeOwner  = 0x00004000
	fseItemXattrMod     = 0x00008000

	fseItemChange = fseItemCreated | fseItemRemoved | fseItemInodeMetaMod | fseItemRenamed |
		fseItemModified | fseItemFinderInfo | fseItemChangeOwner | fseItemXattrMod
)

// fsevent is one FSEvents record: an absolute path, as FSEvents reports it, and its flags.
type fsevent struct {
	path  string
	flags uint32
}

// fseMapper turns FSEvents records into vfs events for one watch. FSEvents' item flags accumulate per
// path — an append to a recently created file still carries ItemCreated — so they say what happened at
// some point, not what is there now: presence is decided by stat, at handling time.
type fseMapper struct {
	realDir   string // the watched directory's canonical path (F_GETPATH): FSEvents' prefix
	dir       string // the watched directory, root-relative
	recursive bool
	cfg       vfs.WatchConfig
	stat      func(rel string) presence
}

// mapBatch maps one callback's records, in order. end reports that the watch must end (its root
// changed): the events then hold a final OpOverflow{""}. A path reported twice with the same
// outcome in one batch — an atomic save's rename and write — is sent once.
func (m *fseMapper) mapBatch(batch []fsevent) (out []vfs.Event, end bool) {
	seen := map[vfs.Event]bool{}
	add := func(ev vfs.Event) {
		if !seen[ev] {
			seen[ev] = true
			out = append(out, ev)
		}
	}
	for _, e := range batch {
		if e.flags&fseRootChanged != 0 {
			add(vfs.Event{Path: "", Op: vfs.OpOverflow})
			return out, true
		}
		if e.flags&fseEventIdsWrapped != 0 {
			add(vfs.Event{Path: "", Op: vfs.OpOverflow})
			continue
		}
		rel, ok := m.relative(e.path)
		if e.flags&fseMustScanSubDirs != 0 {
			if !ok || rel == m.dir || e.flags&(fseUserDropped|fseKernelDropped) != 0 {
				add(vfs.Event{Path: "", Op: vfs.OpOverflow})
			} else if !m.hidden(rel, true) {
				add(vfs.Event{Path: rel, Op: vfs.OpOverflow})
			}
			continue
		}
		if !ok || rel == m.dir || m.hidden(rel, false) {
			continue
		}
		if e.flags&(fseMount|fseUnmount) != 0 {
			if !m.cfg.Skipped(m.dir, rel, true) {
				add(vfs.Event{Path: rel, Op: vfs.OpOverflow})
			}
			continue
		}
		if e.flags&fseItemChange == 0 {
			continue // HistoryDone, OwnEvent and the like
		}
		created := e.flags&(fseItemCreated|fseItemRenamed) != 0
		for _, ev := range presenceEvents(m.dir, rel, m.stat(rel), created, m.recursive, m.cfg) {
			add(ev)
		}
	}
	return out, false
}

// relative maps an FSEvents path to a root-relative one: stripped of the watched directory's canonical
// path and joined back onto dir, so a watch of "docs" reports "docs/intro.md", as on Linux. ok is false
// for a path outside the watched directory. The watched directory itself maps to dir.
func (m *fseMapper) relative(abs string) (string, bool) {
	if abs == m.realDir {
		return m.dir, true
	}
	rest, ok := strings.CutPrefix(abs, m.realDir+"/")
	if !ok || rest == "" {
		return "", false
	}
	return vfs.Join(m.dir, rest), true
}

// hidden reports a path the watch never reports (watchHidden); isDir also tests rel itself.
func (m *fseMapper) hidden(rel string, isDir bool) bool {
	return watchHidden(m.dir, rel, isDir, m.recursive, m.cfg)
}
