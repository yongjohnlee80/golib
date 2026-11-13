package merge

import (
	"github.com/yongjohnlee80/golib/diff"
)

// Side is one of the two texts merged, or None.
type Side uint8

const (
	// None is neither side: as Input.ResultSide, a result written to a third place.
	None Side = iota
	Left
	Right
)

// Resolution is how a change was resolved.
type Resolution uint8

const (
	// Unresolved holds the change's unresolved lines: the side's the result replaces
	// (Input.ResultSide); with None, the base's in a three-way merge, none in a two-way one.
	Unresolved Resolution = iota
	TookLeft
	TookRight
	// TookBoth holds the left's lines, then the right's.
	TookBoth
	// Dismissed keeps the lines the change held, as resolved.
	Dismissed
	// Edited is a change whose lines were edited by hand.
	Edited
)

// Change is one hunk that is not alike on every side: the hunk, its lines in the result, and how
// it is resolved.
type Change struct {
	Hunk   diff.Hunk
	Result diff.Span
	State  Resolution
}

// Conflict is whether the change is a conflict: changed on both sides, differently.
func (c Change) Conflict() bool { return c.Hunk.Kind == diff.Conflict }

// Edit is a change to the result's lines: the lines At replaced by Lines. An operation answers the
// edit it made, so a view replays it into its result editor as one step of the editor's undo.
type Edit struct {
	At    diff.Span
	Lines []string
}

// state is what a revision of the result restores: each hunk's span in the result and its
// resolution.
type state struct {
	spans   []diff.Span
	states  []Resolution
	touched bool
}

// Model is a merge's state: the hunks of its texts, the result, and how each change is resolved.
// It runs on one goroutine, its view's.
//
// The result is the view's to edit: each operation changes the model's result and answers the
// Edit it made, for the view to replay; a hand edit, an undo or a redo in the view comes back
// through Edited. The model keeps its state per revision of the result's text (Settle), so an undo
// back to a revision restores how each change was resolved then.
type Model struct {
	in                Input
	base, left, right diff.Text
	merge             diff.Merge
	unresolved        diff.Pick
	result            []string
	state
	rev   uint64
	saved map[uint64]state
	order []uint64 // the saved revisions, oldest first, for the bound
}

// maxSaved bounds the revisions whose state is kept: an undo further back than an editor keeps
// finds none, and the change it reaches reads as a hand edit.
const maxSaved = 4096

// save keeps the state as revision rev's, dropping the oldest past maxSaved.
func (m *Model) save(rev uint64) {
	if _, ok := m.saved[rev]; !ok {
		m.order = append(m.order, rev)
	}
	m.saved[rev] = m.copyState()
	for len(m.order) > maxSaved {
		delete(m.saved, m.order[0])
		m.order = m.order[1:]
	}
}

// NewModel is the merge of in's texts, every change unresolved. An unresolved change keeps the
// lines of the side the result replaces (Input.ResultSide), so applying with changes left keeps
// what that side had there: in a buffer's merge with its file, the user's own text. With None, a
// three-way merge keeps the base's lines and a two-way one none.
func NewModel(in Input, opts ...diff.Options) *Model {
	m := &Model{in: in, saved: map[uint64]state{}}
	m.left, m.right = diff.Split(in.Left), diff.Split(in.Right)
	if in.HasBase {
		m.base = diff.Split(in.Base)
		m.merge = diff.Merge3(m.base.Lines, m.left.Lines, m.right.Lines, opts...)
	} else {
		m.base = diff.Text{Lines: []string{}}
		m.merge = diff.Merge2(m.left.Lines, m.right.Lines, opts...)
	}
	switch {
	case in.ResultSide == Left:
		m.unresolved = diff.PickLeft
	case in.ResultSide == Right:
		m.unresolved = diff.PickRight
	case in.HasBase:
		m.unresolved = diff.PickBase
	default:
		m.unresolved = diff.PickNone
	}
	m.result = []string{}
	m.spans = make([]diff.Span, len(m.merge.Hunks))
	m.states = make([]Resolution, len(m.merge.Hunks))
	for i, h := range m.merge.Hunks {
		lines := m.unresolvedLines(h)
		m.spans[i] = diff.Span{From: len(m.result), To: len(m.result) + len(lines)}
		m.result = append(m.result, lines...)
	}
	return m
}

// unresolvedLines are a hunk's lines in the result before it is resolved.
func (m *Model) unresolvedLines(h diff.Hunk) []string {
	if h.Kind == diff.Same {
		if m.merge.HasBase {
			return m.base.Lines[h.Base.From:h.Base.To]
		}
		return m.left.Lines[h.Left.From:h.Left.To]
	}
	switch m.unresolved {
	case diff.PickBase:
		return m.base.Lines[h.Base.From:h.Base.To]
	case diff.PickLeft:
		return m.left.Lines[h.Left.From:h.Left.To]
	case diff.PickRight:
		return m.right.Lines[h.Right.From:h.Right.To]
	}
	return nil
}

// Input is the texts merged.
func (m *Model) Input() Input { return m.in }

// HasBase is whether it is a three-way merge; Coarse whether a bound was met diffing it.
func (m *Model) HasBase() bool { return m.merge.HasBase }
func (m *Model) Coarse() bool  { return m.merge.Coarse }

// Hunks are the merge's hunks, in order.
func (m *Model) Hunks() []diff.Hunk { return m.merge.Hunks }

// BaseLines, LeftLines and RightLines are each text's lines; ResultLines the result's, as it is now.
func (m *Model) BaseLines() []string   { return m.base.Lines }
func (m *Model) LeftLines() []string   { return m.left.Lines }
func (m *Model) RightLines() []string  { return m.right.Lines }
func (m *Model) ResultLines() []string { return m.result }

// Result is the result as text, written with the line endings and final newline of the text it
// replaces: the ResultSide, else the base, else the left.
func (m *Model) Result() string {
	switch {
	case m.in.ResultSide == Left:
		return m.left.Join(m.result)
	case m.in.ResultSide == Right:
		return m.right.Join(m.result)
	case m.merge.HasBase:
		return m.base.Join(m.result)
	}
	return m.left.Join(m.result)
}

// ResultSpans are each hunk's lines in the result, by the hunk's index: they cover the result
// whole, in order.
func (m *Model) ResultSpans() []diff.Span { return append([]diff.Span(nil), m.spans...) }

// Changes are the hunks not alike on every side, in order: a change's index is its place here.
func (m *Model) Changes() []Change {
	var out []Change
	for i, h := range m.merge.Hunks {
		if h.Kind != diff.Same {
			out = append(out, Change{Hunk: h, Result: m.spans[i], State: m.states[i]})
		}
	}
	return out
}

// hunk is the hunk index of change i, or -1.
func (m *Model) hunk(i int) int {
	if i < 0 {
		return -1
	}
	for h, hk := range m.merge.Hunks {
		if hk.Kind == diff.Same {
			continue
		}
		if i == 0 {
			return h
		}
		i--
	}
	return -1
}

// Counts are the changes and, of them, the conflicts still unresolved.
func (m *Model) Counts() (changes, conflicts int) {
	for i, h := range m.merge.Hunks {
		if h.Kind == diff.Same || m.states[i] != Unresolved {
			continue
		}
		changes++
		if h.Kind == diff.Conflict {
			conflicts++
		}
	}
	return changes, conflicts
}

// Touched is whether the result has been changed since the merge was made: by an operation or by
// hand. A view asks before loading again over it.
func (m *Model) Touched() bool { return m.touched }

// Take resolves change i with side s's lines. ok is false for no such change, or a side that is
// None.
func (m *Model) Take(i int, s Side) (Edit, bool) {
	h := m.hunk(i)
	if h < 0 || (s != Left && s != Right) {
		return Edit{}, false
	}
	hk := m.merge.Hunks[h]
	if s == Left {
		return m.replace(h, m.left.Lines[hk.Left.From:hk.Left.To], TookLeft), true
	}
	return m.replace(h, m.right.Lines[hk.Right.From:hk.Right.To], TookRight), true
}

// TakeBoth resolves change i with the left's lines, then the right's.
func (m *Model) TakeBoth(i int) (Edit, bool) {
	h := m.hunk(i)
	if h < 0 {
		return Edit{}, false
	}
	hk := m.merge.Hunks[h]
	lines := append(append([]string{}, m.left.Lines[hk.Left.From:hk.Left.To]...), m.right.Lines[hk.Right.From:hk.Right.To]...)
	return m.replace(h, lines, TookBoth), true
}

// Dismiss resolves change i as it is, its lines unchanged. It is no edit, so it is kept with the
// result's present revision.
func (m *Model) Dismiss(i int) bool {
	h := m.hunk(i)
	if h < 0 {
		return false
	}
	m.states[h] = Dismissed
	m.touched = true
	m.save(m.rev)
	return true
}

// AcceptLeft makes the result the left text whole, every change taken from the left; AcceptRight
// the right.
func (m *Model) AcceptLeft() Edit  { return m.accept(Left) }
func (m *Model) AcceptRight() Edit { return m.accept(Right) }

func (m *Model) accept(s Side) Edit {
	lines, took := m.left.Lines, TookLeft
	if s == Right {
		lines, took = m.right.Lines, TookRight
	}
	e := Edit{At: diff.Span{From: 0, To: len(m.result)}, Lines: append([]string{}, lines...)}
	for i, h := range m.merge.Hunks {
		if s == Left {
			m.spans[i] = h.Left
		} else {
			m.spans[i] = h.Right
		}
		if h.Kind != diff.Same {
			m.states[i] = took
		}
	}
	m.result = append([]string{}, lines...)
	m.touched = true
	return e
}

// MergeNonConflicting resolves every unresolved change that is not a conflict with its changed
// side, as one edit over the lines from the first to the last. ok is false when there is none.
func (m *Model) MergeNonConflicting() (Edit, bool) {
	from, to, before := -1, -1, len(m.result)
	for i, h := range m.merge.Hunks {
		if m.states[i] != Unresolved || h.Kind == diff.Same || h.Kind == diff.Conflict {
			continue
		}
		if from < 0 {
			from = m.spans[i].From
		}
		switch h.Kind {
		case diff.LeftOnly, diff.Both:
			m.replace(i, m.left.Lines[h.Left.From:h.Left.To], TookLeft)
		case diff.RightOnly:
			m.replace(i, m.right.Lines[h.Right.From:h.Right.To], TookRight)
		}
		to = m.spans[i].To
	}
	if from < 0 {
		return Edit{}, false
	}
	// The edit spans the lines replaced as they were before: to, less what the replacements added.
	lines := append([]string{}, m.result[from:to]...)
	return Edit{At: diff.Span{From: from, To: to - (len(m.result) - before)}, Lines: lines}, true
}

// replace puts lines in hunk h's span, resolving it as r, and answers the edit.
func (m *Model) replace(h int, lines []string, r Resolution) Edit {
	at := m.spans[h]
	e := Edit{At: at, Lines: append([]string{}, lines...)}
	out := make([]string, 0, len(m.result)-at.Len()+len(lines))
	out = append(append(append(out, m.result[:at.From]...), lines...), m.result[at.To:]...)
	m.result = out
	delta := len(lines) - at.Len()
	m.spans[h].To = at.From + len(lines)
	for k := h + 1; k < len(m.spans); k++ {
		m.spans[k].From += delta
		m.spans[k].To += delta
	}
	if m.merge.Hunks[h].Kind != diff.Same {
		m.states[h] = r
	}
	m.touched = true
	return e
}

func (m *Model) copyState() state {
	return state{
		spans:   append([]diff.Span(nil), m.spans...),
		states:  append([]Resolution(nil), m.states...),
		touched: m.touched,
	}
}

// Settle records the model's state as that of the result's revision rev: the view calls it once it
// has the result's text (at the start, and after replaying an operation's Edit), with its editor's
// revision of that text.
func (m *Model) Settle(rev uint64) {
	m.rev = rev
	m.save(rev)
}

// Edited tells the model the result's lines are now lines, at its editor's revision rev: a hand
// edit, an undo or a redo. A revision settled before restores the state it had, so an undo brings
// back how each change was resolved. A new one is a hand edit: the lines between the common head
// and tail of the old result and the new are the edit, every change it reaches is Edited, and the
// spans after it move.
//
// Lines put in where two hunks meet go to a change that meets the place: an empty change there
// first (lines both sides put in, or none kept), then a settled change (resolved, or holding no
// lines) ending there (a line added after its last), then one starting there. So what is typed at
// a change is the change's, and a later Take replaces it. A line put in beside a change still to
// resolve goes to the unchanged lines beside it: the change is not resolved by a line next to it.
func (m *Model) Edited(lines []string, rev uint64) {
	old := m.result
	m.result = append([]string{}, lines...)
	m.rev = rev
	if st, ok := m.saved[rev]; ok {
		m.spans = append(m.spans[:0], st.spans...)
		m.states = append(m.states[:0], st.states...)
		m.touched = st.touched
		return
	}
	head := 0
	for head < len(old) && head < len(lines) && old[head] == lines[head] {
		head++
	}
	tail := 0
	for tail < len(old)-head && tail < len(lines)-head && old[len(old)-1-tail] == lines[len(lines)-1-tail] {
		tail++
	}
	f, t, n := head, len(old)-tail, len(lines)-tail-head
	if f == t && n == 0 {
		m.save(rev) // the same lines: a revision with no change to them
		return
	}
	hs := len(m.spans)
	if hs == 0 {
		m.touched = true
		m.save(rev)
		return
	}
	// The boundaries between spans, B[0] the first's start, B[hs] the last's end.
	b := make([]int, hs+1)
	for i, sp := range m.spans {
		b[i] = sp.From
	}
	b[hs] = m.spans[hs-1].To
	// at is where boundary i goes. One before the edit stays, one after it moves by what it added,
	// one inside goes to the end of the lines put in. At an insert's place (f == t), the boundaries
	// up to the hunk taking the lines (into) stay, the rest move past them.
	into := -1
	if f == t {
		into = m.insertInto(b, f)
	}
	at := func(i int) int {
		switch {
		case b[i] < f, b[i] == f && (f != t || i <= into):
			return b[i]
		case b[i] >= t:
			return b[i] + n - (t - f)
		}
		return f + n
	}
	for i := range m.spans {
		was := m.spans[i]
		now := diff.Span{From: at(i), To: at(i + 1)}
		if i == hs-1 {
			now.To = len(lines)
		}
		reached := (was.From < t && f < was.To) || (n > 0 && now.From < f+n && f < now.To)
		if reached && m.merge.Hunks[i].Kind != diff.Same {
			m.states[i] = Edited
		}
		m.spans[i] = now
	}
	m.touched = true
	m.save(rev)
}

// insertInto is the hunk lines put in at boundary place f go to (Edited): an empty change there;
// else a settled change (resolved, or holding no lines) ending there, then one starting there;
// else the hunk the place is inside; else the first hunk meeting it that is not an open change. A
// line typed beside a conflict still to resolve is not the conflict's, so the conflicts counted
// stay the conflicts left.
func (m *Model) insertInto(b []int, f int) int {
	change := func(i int) bool { return m.merge.Hunks[i].Kind != diff.Same }
	settled := func(i int) bool { return m.states[i] != Unresolved || b[i] == b[i+1] }
	hs := len(b) - 1
	for _, pick := range []func(i int) bool{
		func(i int) bool { return change(i) && b[i] == f && b[i+1] == f },
		func(i int) bool { return change(i) && settled(i) && b[i] < f && b[i+1] == f },
		func(i int) bool { return change(i) && settled(i) && b[i] == f && b[i+1] > f },
		func(i int) bool { return b[i] < f && f < b[i+1] },
		func(i int) bool { return b[i] <= f && f <= b[i+1] && !(change(i) && !settled(i)) },
	} {
		for i := 0; i < hs; i++ {
			if pick(i) {
				return i
			}
		}
	}
	return hs - 1
}

// Change is the index of the change hunk h is, or -1 for a hunk alike on every side.
func (m *Model) Change(h int) int {
	if h < 0 || h >= len(m.merge.Hunks) || m.merge.Hunks[h].Kind == diff.Same {
		return -1
	}
	i := 0
	for k := 0; k < h; k++ {
		if m.merge.Hunks[k].Kind != diff.Same {
			i++
		}
	}
	return i
}
