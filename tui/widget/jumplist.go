package widget

// JUMP LIST — Vim's jumplist (src/mark.c setpcmark, movemark, cleanup_jumplist), for Ctrl+O back and
// Ctrl+I forward. A jump records the place it leaves at the end; past the limit the oldest goes.
// Stepping first drops each entry a later one repeats (the same document and line), and the first
// step back from the end records the place it leaves, so Ctrl+I can come back to it.
//
// A step is proposed, then committed: Back and Forward change nothing and return a JumpStep, and
// Commit applies it only while the list is as it was when the step was proposed. A host whose way to
// another document is asynchronous (a question first, a read) commits when the document is shown,
// and leaves the list as it was when the open doesn't go ahead. Every change to the list makes its
// steps stale, so an older step never moves a list that has moved since.

// DefaultJumpLimit is how many entries a JumpList keeps unless told otherwise: Vim's JUMPLISTSIZE.
const DefaultJumpLimit = 100

// JumpPos is a place: a document (the host's key for it; "" for an editor on its own), a line and a
// column (grapheme clusters, from 0), and the host's view of the document there ("" for the
// editor's own view): what going back restores.
type JumpPos struct {
	Doc       string
	Line, Col int
	View      string
}

// JumpList is a jumplist. Its methods run on the owner's goroutine (the loop's).
type JumpList struct {
	entries []JumpPos
	idx     int // len(entries) when past the end, as after a jump
	limit   int
	gen     uint64
}

// NewJumpList is an empty list keeping limit entries (DefaultJumpLimit for 0 or less).
func NewJumpList(limit int) *JumpList {
	if limit <= 0 {
		limit = DefaultJumpLimit
	}
	return &JumpList{limit: limit}
}

// JumpStep is a proposed move along a list: To is where it goes.
type JumpStep struct {
	To      JumpPos
	list    *JumpList
	gen     uint64
	entries []JumpPos // the list once the step is committed
	idx     int
}

// Push records from as the place a jump leaves (Vim's setpcmark).
func (l *JumpList) Push(from JumpPos) {
	l.entries = append(l.entries, from)
	if len(l.entries) > l.limit {
		l.entries = append([]JumpPos(nil), l.entries[len(l.entries)-l.limit:]...)
	}
	l.idx = len(l.entries)
	l.gen++
}

// Back proposes n steps back from at, the place now (Vim's Ctrl+O); ok is false when there is
// nothing that far back.
func (l *JumpList) Back(at JumpPos, n int) (JumpStep, bool) { return l.step(at, -max(n, 1)) }

// Forward proposes n steps forward (Vim's Ctrl+I); ok is false when there is nothing that far on.
func (l *JumpList) Forward(at JumpPos, n int) (JumpStep, bool) { return l.step(at, max(n, 1)) }

// step is Vim's movemark over a copy: the cleanup, then, from the end, the place now recorded and
// stepped over, then count entries on.
func (l *JumpList) step(at JumpPos, count int) (JumpStep, bool) {
	entries, idx := cleanJumps(l.entries, l.idx)
	if len(entries) == 0 || idx+count < 0 || idx+count >= len(entries) {
		return JumpStep{}, false
	}
	if idx == len(entries) {
		entries = append(entries, at) // the place now, so Forward can come back to it
		if len(entries) > l.limit {
			entries = entries[len(entries)-l.limit:]
		}
		idx = len(entries) - 1 // stepped over
		if idx+count < 0 {
			return JumpStep{}, false
		}
	}
	idx += count
	return JumpStep{To: entries[idx], list: l, gen: l.gen, entries: entries, idx: idx}, true
}

// cleanJumps is Vim's cleanup_jumplist on a copy: an entry that a later one repeats (the same
// document and line) goes, the index following its entry.
func cleanJumps(entries []JumpPos, idx int) ([]JumpPos, int) {
	out := make([]JumpPos, 0, len(entries)+1)
	newIdx := -1
	for from, e := range entries {
		if idx == from {
			newIdx = len(out)
		}
		repeated := false
		for _, later := range entries[from+1:] {
			if later.Doc == e.Doc && later.Line == e.Line {
				repeated = true
				break
			}
		}
		if !repeated {
			out = append(out, e)
		}
	}
	if newIdx < 0 { // past the end
		newIdx = len(out)
	}
	return out, newIdx
}

// Commit applies step: the list becomes as the step left it. It reports false, changing nothing,
// when the step is another list's, or the list has changed since the step was proposed.
func (l *JumpList) Commit(step JumpStep) bool {
	if step.list != l || step.gen != l.gen {
		return false
	}
	l.entries, l.idx = step.entries, step.idx
	l.gen++
	return true
}

// Drop removes the entry step goes to, for a place that is gone for good (a document removed), the
// index staying where the list's was. It reports false, changing nothing, when the step is stale.
// A host then proposes a fresh step, as Vim skips an entry whose file is gone.
func (l *JumpList) Drop(step JumpStep) bool {
	if step.list != l || step.gen != l.gen {
		return false
	}
	// the list is as it was proposed from, so the step's index is its entry's in the cleaned list
	// (an entry the step would have recorded at the end comes after it)
	entries, idx := cleanJumps(l.entries, l.idx)
	entries = append(entries[:step.idx:step.idx], entries[step.idx+1:]...)
	if idx > step.idx {
		idx--
	}
	l.entries, l.idx = entries, idx
	l.gen++
	return true
}

// adjust moves doc's entries with f (an edit of that document), and makes the list's steps stale.
func (l *JumpList) adjust(doc string, f func(*JumpPos)) {
	changed := false
	for i := range l.entries {
		if l.entries[i].Doc == doc {
			before := l.entries[i]
			f(&l.entries[i])
			changed = changed || l.entries[i] != before
		}
	}
	if changed {
		l.gen++
	}
}

// Entries returns the list's entries, oldest first, and its index (len for past the end): what it
// holds, for a host's view of it.
func (l *JumpList) Entries() ([]JumpPos, int) {
	return append([]JumpPos(nil), l.entries...), l.idx
}
