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
		for _, ev := range presenceEvents(m.dir, rel, m.stat(rel), created, m.recursive, m.cfg) {
			add(ev)
		}
	}
	return out
}

// hidden reports a path the watch never reports (watchHidden). A record never tests the path
// itself as a skipped directory: a skipped directory's own appearance, going and renaming are
// still reported.
func (m *winMapper) hidden(rel string) bool {
	return watchHidden(m.dir, rel, false, m.recursive, m.cfg)
}
