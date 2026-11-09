//go:build linux || windows

package local

import (
	"encoding/binary"
	"strings"
	"unicode/utf16"

	"github.com/yongjohnlee80/golib/vfs"
)

// ReadDirectoryChangesW's actions (FILE_NOTIFY_INFORMATION.Action), as plain numbers so the mapping
// below is pure Go: it builds and is tested on Linux as well as on Windows.
const (
	winActionAdded      = 1 // FILE_ACTION_ADDED
	winActionRemoved    = 2 // FILE_ACTION_REMOVED
	winActionModified   = 3 // FILE_ACTION_MODIFIED
	winActionRenamedOld = 4 // FILE_ACTION_RENAMED_OLD_NAME
	winActionRenamedNew = 5 // FILE_ACTION_RENAMED_NEW_NAME
)

// winRecord is one FILE_NOTIFY_INFORMATION record: what happened, and to which name, relative to the
// watched directory with Windows' separators.
type winRecord struct {
	action uint32
	name   string
}

// parseNotify reads the FILE_NOTIFY_INFORMATION records in buf, each a NextEntryOffset, an Action, a
// FileNameLength in bytes and that many bytes of UTF-16 name; a record cut short by buf ends it.
func parseNotify(buf []byte) []winRecord {
	var out []winRecord
	for off := 0; off+12 <= len(buf); {
		next := binary.LittleEndian.Uint32(buf[off:])
		action := binary.LittleEndian.Uint32(buf[off+4:])
		n := int(binary.LittleEndian.Uint32(buf[off+8:]))
		if n%2 != 0 || off+12+n > len(buf) {
			break
		}
		u := make([]uint16, n/2)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(buf[off+12+2*i:])
		}
		out = append(out, winRecord{action: action, name: string(utf16.Decode(u))})
		if next == 0 {
			break
		}
		off += int(next)
	}
	return out
}

// winMapper turns ReadDirectoryChangesW records into vfs events for one watch. As with FSEvents,
// presence is decided by stat at handling time: a record says what happened to a name, and the
// name may have changed again since.
type winMapper struct {
	dir       string // the watched directory, root-relative
	recursive bool
	cfg       vfs.WatchConfig
	stat      func(rel string) presence
}

// mapBatch maps one completed read's records, in order. A path reported twice with the same outcome
// in one batch (a file's size and its time, say) is sent once.
func (m *winMapper) mapBatch(batch []winRecord) []vfs.Event {
	var out []vfs.Event
	seen := map[vfs.Event]bool{}
	add := func(ev vfs.Event) {
		if !seen[ev] {
			seen[ev] = true
			out = append(out, ev)
		}
	}
	for _, r := range batch {
		if r.name == "" {
			continue
		}
		rel := vfs.Join(m.dir, strings.ReplaceAll(r.name, `\`, "/"))
		if m.hidden(rel) {
			continue
		}
		// a name that appeared, or that went and is there again, is a new entry; a modified one is a write
		created := r.action != winActionModified
		switch m.stat(rel) {
		case absent:
			add(vfs.Event{Path: rel, Op: vfs.OpRemove})
		case presentDir:
			if !created {
				continue // a directory's own modification is its children's, which report themselves
			}
			add(vfs.Event{Path: rel, Op: vfs.OpCreate})
			// a populated directory moved in reports only itself, so its subtree must be read, unless
			// SkipDirs leaves it out, when reading it is the walk the caller asked not to make
			if m.recursive && !m.cfg.Skipped(m.dir, rel, true) {
				add(vfs.Event{Path: rel, Op: vfs.OpOverflow})
			}
		case presentFile:
			if created {
				add(vfs.Event{Path: rel, Op: vfs.OpCreate})
			} else {
				add(vfs.Event{Path: rel, Op: vfs.OpWrite})
			}
		case unknown:
			add(vfs.Event{Path: rel, Op: vfs.OpWrite})
		}
	}
	return out
}

// hidden reports a path the watch never reports: a temp name anywhere in it, anything under a
// SkipDirs directory, or, for a watch that is not recursive, anything below dir's direct children
// (which ReadDirectoryChangesW does not report then either). A skipped directory's own appearance,
// going and renaming are still reported.
func (m *winMapper) hidden(rel string) bool {
	rest := rel
	if m.dir != "." && m.dir != "" {
		rest = strings.TrimPrefix(rel, m.dir+"/")
	}
	for name := range strings.SplitSeq(rest, "/") {
		if vfs.IsTemp(name) {
			return true
		}
	}
	if !m.recursive && strings.Contains(rest, "/") {
		return true
	}
	return m.cfg.Skipped(m.dir, rel, false)
}
