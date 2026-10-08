package widget

// viewMark is what a scrolling widget last kept its cursor in view for: the cursor and the
// viewport's size. Layout reveals the cursor only when either changed since: a layout pass comes
// for any reason (a status line's update, a toast), and one that always revealed the cursor
// snapped the wheel's scroll back to it.
type viewMark struct {
	cursor, size [2]int // a cursor's line and column (a row and 0); a viewport's height and width
	set          bool
}

// moved reports whether the cursor or the size changed since the last call, and records them.
func (m *viewMark) moved(cursor, size [2]int) bool {
	if m.set && m.cursor == cursor && m.size == size {
		return false
	}
	m.cursor, m.size, m.set = cursor, size, true
	return true
}
