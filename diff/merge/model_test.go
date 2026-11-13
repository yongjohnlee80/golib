package merge

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/diff"
)

var (
	buffer = Origin{Kind: Buffer, Location: "notes.md", Revision: "7"}
	disk   = Origin{Kind: File, Location: "/work/notes.md", Revision: "2026-11-13T10:00:00Z"}
	ours   = Origin{Kind: Git, Location: "notes.md", Revision: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"}
)

func text(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

// replay applies e to lines, as a view's editor does.
func replay(lines []string, e Edit) []string {
	out := append([]string{}, lines[:e.At.From]...)
	out = append(out, e.Lines...)
	return append(out, lines[e.At.To:]...)
}

// check is what holds after every operation: the spans partition the result, in order.
func check(t *testing.T, m *Model) {
	t.Helper()
	at := 0
	for i, h := range m.Hunks() {
		s := m.spans[i]
		if s.From != at || s.To < s.From {
			t.Fatalf("hunk %d's span %v does not follow %d", i, s, at)
		}
		_ = h
		at = s.To
	}
	if at != len(m.ResultLines()) {
		t.Fatalf("the spans end at %d, the result has %d lines", at, len(m.ResultLines()))
	}
}

func TestAThreeWayMergeStartsOnTheBaseAndResolvesEachChange(t *testing.T) {
	in := Input{
		HasBase: true,
		Base:    text("a", "b", "c", "d", "x", "e"),
		Left:    text("a", "B", "c", "d", "x", "E"),
		Right:   text("a", "b", "c", "D", "x", "e!"),
	}
	m := NewModel(in)
	check(t, m)
	if got := m.ResultLines(); !reflect.DeepEqual(got, diff.Split(in.Base).Lines) {
		t.Fatalf("the result starts as %q, want the base", got)
	}
	cs := m.Changes()
	kinds := []diff.Kind{}
	for _, c := range cs {
		kinds = append(kinds, c.Hunk.Kind)
	}
	if want := []diff.Kind{diff.LeftOnly, diff.RightOnly, diff.Conflict}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("changes %v, want %v", kinds, want)
	}
	if ch, co := m.Counts(); ch != 3 || co != 1 {
		t.Fatalf("counts %d, %d, want 3 changes, 1 conflict", ch, co)
	}
	shadow := m.ResultLines()
	e, ok := m.MergeNonConflicting()
	if !ok {
		t.Fatal("MergeNonConflicting found nothing")
	}
	shadow = replay(shadow, e)
	if want := []string{"a", "B", "c", "D", "x", "e"}; !reflect.DeepEqual(m.ResultLines(), want) || !reflect.DeepEqual(shadow, want) {
		t.Fatalf("merged %q (replayed %q), want %q", m.ResultLines(), shadow, want)
	}
	if ch, co := m.Counts(); ch != 1 || co != 1 {
		t.Fatalf("after merging, counts %d, %d, want the conflict alone", ch, co)
	}
	if _, ok := m.MergeNonConflicting(); ok {
		t.Fatal("MergeNonConflicting again found more")
	}
	e, _ = m.TakeBoth(2)
	shadow = replay(shadow, e)
	if want := []string{"a", "B", "c", "D", "x", "E", "e!"}; !reflect.DeepEqual(m.ResultLines(), want) || !reflect.DeepEqual(shadow, want) {
		t.Fatalf("both taken: %q (replayed %q), want %q", m.ResultLines(), shadow, want)
	}
	if ch, co := m.Counts(); ch != 0 || co != 0 || !m.Touched() {
		t.Fatalf("counts %d, %d, touched %v; want none left, touched", ch, co, m.Touched())
	}
	if m.Changes()[2].State != TookBoth || m.Changes()[0].State != TookLeft || m.Changes()[1].State != TookRight {
		t.Fatalf("states %v", m.Changes())
	}
	check(t, m)
	if _, ok := m.Take(3, Left); ok {
		t.Fatal("Take of a fourth change answered ok")
	}
}

// An unresolved two-way conflict keeps the side the result replaces: ResultSide Right, the right's
// lines; Left, the left's; None, none.
func TestATwoWayConflictKeepsTheSideTheResultReplaces(t *testing.T) {
	left, right := text("a", "disk", "c"), text("a", "mine", "more", "c")
	for _, c := range []struct {
		name string
		side Side
		want []string
	}{
		{"right", Right, []string{"a", "mine", "more", "c"}},
		{"left", Left, []string{"a", "disk", "c"}},
		{"none", None, []string{"a", "c"}},
	} {
		m := NewModel(Input{Left: left, Right: right, LeftFrom: disk, RightFrom: buffer, ResultSide: c.side})
		check(t, m)
		if got := m.ResultLines(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: the result starts as %q, want %q", c.name, got, c.want)
		}
		if ch, co := m.Counts(); ch != 1 || co != 1 {
			t.Errorf("%s: counts %d, %d, want one conflict", c.name, ch, co)
		}
		if _, ok := m.MergeNonConflicting(); ok {
			t.Errorf("%s: a two-way merge had a change that is not a conflict", c.name)
		}
	}
}

// An unresolved three-way change keeps the side the result replaces too: a buffer merged with its
// file (the base the version it was opened at, the left the disk, the right the buffer) keeps the
// user's own lines in a conflict left unresolved, and in their own change; a result written to a
// third place (None) keeps the base. The side is named, never read from the origins: a ResultTo
// whose label and revision are not the right's still writes the right.
func TestAThreeWayConflictKeepsTheSideTheResultReplaces(t *testing.T) {
	in := Input{
		HasBase:  true,
		Base:     text("a", "b", "c", "d", "e"),
		Left:     text("a", "B-disk", "c", "d", "e-disk"),
		Right:    text("a", "b", "c", "D-mine", "e-mine"),
		BaseFrom: disk, LeftFrom: disk, RightFrom: buffer,
		ResultTo:   Origin{Kind: Buffer, Location: "notes.md", Label: "Result", Revision: "8"},
		ResultSide: Right,
	}
	m := NewModel(in)
	check(t, m)
	if got, want := m.Result(), text("a", "b", "c", "D-mine", "e-mine"); got != want {
		t.Fatalf("the buffer's merge, unresolved: %q, want the buffer's own lines %q", got, want)
	}
	if ch, co := m.Counts(); ch != 2 || co != 1 {
		t.Fatalf("counts %d, %d, want 2 changes (the disk's, the conflict), 1 conflict", ch, co)
	}
	m.MergeNonConflicting()
	if got, want := m.Result(), text("a", "B-disk", "c", "D-mine", "e-mine"); got != want {
		t.Fatalf("merged with the conflict left: %q, want %q (the conflict keeps the buffer's)", got, want)
	}
	in.ResultSide = None
	if got := NewModel(in).Result(); got != in.Base {
		t.Fatalf("ResultSide None: %q, want the base", got)
	}
}

// Lines typed where hunks meet go to the change that meets the place: into a change with no lines
// kept (an insert both sides made, unresolved to none), and after a change's last line. So a later
// Take replaces what was typed, never keeps it beside the taken lines.
func TestLinesTypedAtAChangeAreTheChanges(t *testing.T) {
	for _, c := range []struct {
		name string
		in   Input
	}{
		{"three-way, the base kept", Input{HasBase: true, Base: text("a", "c"), Left: text("a", "L", "c"), Right: text("a", "R", "c")}},
		{"two-way, none kept", Input{Left: text("a", "L", "c"), Right: text("a", "R", "c")}},
	} {
		m := NewModel(c.in)
		m.Settle(1)
		if got := m.Changes()[0].Result; got.Len() != 0 {
			t.Fatalf("%s: the conflict holds %v, want no lines", c.name, got)
		}
		m.Edited([]string{"a", "LR", "c"}, 2) // typed where the conflict is
		if cs := m.Changes(); cs[0].State != Edited || cs[0].Result != (diff.Span{From: 1, To: 2}) {
			t.Fatalf("%s: after typing at the conflict: %+v, want it Edited, holding the line", c.name, cs[0])
		}
		check(t, m)
		e, _ := m.Take(0, Left)
		if got, want := replay([]string{"a", "LR", "c"}, e), []string{"a", "L", "c"}; !reflect.DeepEqual(got, want) || !reflect.DeepEqual(m.ResultLines(), want) {
			t.Fatalf("%s: » after typing: %q (replayed %q), want %q", c.name, m.ResultLines(), got, want)
		}
	}
	// A line added after a taken change's last line is the change's: taking it again replaces it.
	m := NewModel(Input{HasBase: true, Base: text("a", "b", "c"), Left: text("a", "B", "c"), Right: text("a", "b", "c")})
	m.Take(0, Left)
	m.Settle(1)
	m.Edited([]string{"a", "B", "added", "c"}, 2)
	if cs := m.Changes(); cs[0].State != Edited || cs[0].Result != (diff.Span{From: 1, To: 3}) {
		t.Fatalf("a line added after the change: %+v, want it the change's", cs[0])
	}
	m.Take(0, Left)
	if got := m.ResultLines(); !reflect.DeepEqual(got, []string{"a", "B", "c"}) {
		t.Fatalf("taking the change again: %q, want the added line replaced", got)
	}
	check(t, m)
}

func TestAcceptingASideMakesTheResultThatSide(t *testing.T) {
	in := Input{HasBase: true, Base: text("a", "b"), Left: text("a", "L", "b"), Right: text("R", "b")}
	for _, s := range []Side{Left, Right} {
		m := NewModel(in)
		var e Edit
		want := m.LeftLines()
		if s == Left {
			e = m.AcceptLeft()
		} else {
			e, want = m.AcceptRight(), m.RightLines()
		}
		if !reflect.DeepEqual(m.ResultLines(), want) || !reflect.DeepEqual(replay(diff.Split(in.Base).Lines, e), want) {
			t.Fatalf("side %d: result %q, want %q", s, m.ResultLines(), want)
		}
		if ch, _ := m.Counts(); ch != 0 {
			t.Fatalf("side %d: %d changes left", s, ch)
		}
		check(t, m)
	}
}

// The result is written with the endings of the text it replaces.
func TestTheResultKeepsTheEndingsOfTheTextItReplaces(t *testing.T) {
	m := NewModel(Input{
		Left: "a\nb\n", Right: "a\r\nB\r\n",
		LeftFrom: disk, RightFrom: buffer, ResultTo: buffer, ResultSide: Right,
	})
	if got := m.Result(); got != "a\r\nB\r\n" {
		t.Fatalf("result %q, want the right's CRLF text", got)
	}
	m.Take(0, Left)
	if got := m.Result(); got != "a\r\nb\r\n" {
		t.Fatalf("result %q after taking the left, want CRLF", got)
	}
	m = NewModel(Input{Left: "x", Right: "y\n", LeftFrom: disk, RightFrom: buffer, ResultTo: disk, ResultSide: Left})
	if got := m.Result(); got != "x" {
		t.Fatalf("result %q, want the left's, with no final newline", got)
	}
}

// A hand edit marks the changes it reaches Edited and moves the spans after it; an undo back to a
// settled revision restores how each change was resolved then.
func TestAHandEditMarksWhatItReachesAndAnUndoRestoresIt(t *testing.T) {
	m := NewModel(Input{
		HasBase: true,
		Base:    text("a", "b", "c", "d", "e"),
		Left:    text("a", "B", "c", "d", "e"),
		Right:   text("a", "b", "c", "d", "E"),
	})
	m.Settle(1)
	e, _ := m.Take(0, Left)
	shadow := replay(diff.Split(m.Input().Base).Lines, e)
	m.Settle(2)
	// Typed into the unchanged "c": no change reached.
	typed := append([]string{}, shadow...)
	typed[2] = "c!"
	m.Edited(typed, 3)
	if cs := m.Changes(); cs[0].State != TookLeft || cs[1].State != Unresolved {
		t.Fatalf("after an edit of unchanged lines: %v", cs)
	}
	// The second change's line replaced by two: it is Edited, and its span holds them. (A line put
	// in beside an open change, not in it, is the unchanged lines': TestALineTypedBesideAnOpenConflict.)
	typed = append(append([]string{}, typed[:4]...), "x", "y")
	m.Edited(typed, 4)
	cs := m.Changes()
	if cs[1].State != Edited || cs[1].Result != (diff.Span{From: 4, To: 6}) {
		t.Fatalf("after an edit at the second change: %v", cs[1])
	}
	check(t, m)
	if ch, _ := m.Counts(); ch != 0 {
		t.Fatalf("%d changes unresolved, an edited one is resolved", ch)
	}
	// Undo to revision 2: the take stands, the edit is gone.
	m.Edited(shadow, 2)
	if cs := m.Changes(); cs[0].State != TookLeft || cs[1].State != Unresolved || cs[1].Result != (diff.Span{From: 4, To: 5}) {
		t.Fatalf("after the undo to 2: %v", cs)
	}
	// Undo to revision 1: nothing taken, untouched.
	m.Edited(diff.Split(m.Input().Base).Lines, 1)
	if cs := m.Changes(); cs[0].State != Unresolved || m.Touched() {
		t.Fatalf("after the undo to 1: %v, touched %v", cs, m.Touched())
	}
	check(t, m)
}

func TestDismissResolvesAChangeAsItIs(t *testing.T) {
	m := NewModel(Input{Left: text("a"), Right: text("b"), LeftFrom: disk, RightFrom: buffer, ResultTo: buffer, ResultSide: Right})
	m.Settle(1)
	if !m.Dismiss(0) || m.Dismiss(1) {
		t.Fatal("Dismiss answered wrong")
	}
	if ch, co := m.Counts(); ch != 0 || co != 0 || m.Changes()[0].State != Dismissed {
		t.Fatalf("after dismissing: %d, %d, %v", ch, co, m.Changes())
	}
	if got := m.ResultLines(); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("result %q, want it unchanged", got)
	}
	m.Edited([]string{"b"}, 1)
	if m.Changes()[0].State != Dismissed {
		t.Fatal("the dismiss was lost at its own revision")
	}
}

func TestAnOriginsTitleAndShortRevision(t *testing.T) {
	for _, c := range []struct {
		o            Origin
		title, short string
	}{
		{Origin{Kind: Buffer, Location: "notes.md", Label: "Your unsaved edits", Revision: "12"}, "Your unsaved edits", "12"},
		{Origin{Kind: File, Location: "/work/notes.md"}, "/work/notes.md", ""},
		{Origin{Kind: Remote, Location: "gs://bucket/notes.md", Revision: "1699999999123456"}, "gs://bucket/notes.md", "1699999999123456"},
		{Origin{Kind: Git, Revision: "a1b2c3d4e5f60718293a4b5c6d7e8f9012345678"}, "a1b2c3d", "a1b2c3d"},
		{Origin{Kind: Git, Location: "notes.md", Revision: "HEAD"}, "notes.md", "HEAD"},
	} {
		if c.o.Title() != c.title || c.o.ShortRevision() != c.short {
			t.Errorf("%+v: title %q, short %q; want %q, %q", c.o, c.o.Title(), c.o.ShortRevision(), c.title, c.short)
		}
	}
	if Buffer.String() != "buffer" || File.String() != "file" || Remote.String() != "remote" || Git.String() != "git" {
		t.Error("an OriginKind's word is wrong")
	}
}

// Any run of operations and hand edits: every Edit replays to the model's result, the spans
// partition it, the rows show every pane's lines once in order, and an undo to any settled revision
// restores that revision's states.
func TestAnyRunOfOperationsKeepsTheResultAndItsSpansWhole(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	random := func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "l%d\n", r.Intn(4))
		}
		return b.String()
	}
	for run := 0; run < 200; run++ {
		in := Input{Left: random(r.Intn(12)), Right: random(r.Intn(12)), LeftFrom: disk, RightFrom: buffer, ResultTo: buffer, ResultSide: Side(r.Intn(3))}
		if run%2 == 0 {
			in.HasBase, in.Base = true, random(r.Intn(12))
		}
		m := NewModel(in)
		shadow := append([]string{}, m.ResultLines()...)
		rev := uint64(1)
		m.Settle(rev)
		type kept struct {
			lines  []string
			states []Resolution
		}
		history := map[uint64]kept{rev: {shadow, append([]Resolution{}, m.states...)}}
		for step := 0; step < 12; step++ {
			n := len(m.Changes())
			var e Edit
			ok := true
			switch op := r.Intn(7); {
			case op == 0 && n > 0:
				e, ok = m.Take(r.Intn(n), Side(1+r.Intn(2)))
			case op == 1 && n > 0:
				e, ok = m.TakeBoth(r.Intn(n))
			case op == 2:
				e, ok = m.MergeNonConflicting()
			case op == 3:
				e = m.AcceptLeft()
			case op == 4:
				e = m.AcceptRight()
			case op == 5 && len(history) > 1:
				back := uint64(1 + r.Intn(int(rev)))
				if h, seen := history[back]; seen {
					m.Edited(h.lines, back)
					shadow = append([]string{}, h.lines...)
					if !reflect.DeepEqual(m.states, h.states) {
						t.Fatalf("run %d: the undo to %d restored %v, want %v", run, back, m.states, h.states)
					}
				}
				ok = false
			default: // a hand edit: a line put in, or one removed
				typed := append([]string{}, shadow...)
				at := r.Intn(len(typed) + 1)
				if at < len(typed) && r.Intn(2) == 0 {
					typed = append(typed[:at], typed[at+1:]...)
				} else {
					typed = append(typed[:at], append([]string{"typed"}, typed[at:]...)...)
				}
				rev++
				m.Edited(typed, rev)
				shadow = typed
				history[rev] = kept{append([]string{}, typed...), append([]Resolution{}, m.states...)}
				ok = false
			}
			if ok {
				shadow = replay(shadow, e)
				rev++
				m.Settle(rev)
				history[rev] = kept{append([]string{}, shadow...), append([]Resolution{}, m.states...)}
			}
			if !reflect.DeepEqual(shadow, m.ResultLines()) {
				t.Fatalf("run %d step %d: replayed %q, the model holds %q", run, step, shadow, m.ResultLines())
			}
			check(t, m)
		}
	}
}

// A line typed just above or just below a conflict still to resolve is not the conflict's: it stays
// unresolved, so the conflicts counted, and an Apply's question, stay right.
func TestALineTypedBesideAnOpenConflictLeavesItOpen(t *testing.T) {
	in := Input{HasBase: true, Base: text("a", "b", "c"), Left: text("a", "L", "c"), Right: text("a", "R", "c"),
		LeftFrom: disk, RightFrom: buffer, ResultTo: buffer, ResultSide: Right}
	for _, c := range []struct {
		name  string
		typed []string
		span  diff.Span
	}{
		{"above", []string{"a", "typed", "R", "c"}, diff.Span{From: 2, To: 3}},
		{"below", []string{"a", "R", "typed", "c"}, diff.Span{From: 1, To: 2}},
	} {
		m := NewModel(in)
		m.Settle(1)
		if cs := m.Changes(); len(cs) != 1 || !cs[0].Conflict() || cs[0].Result != (diff.Span{From: 1, To: 2}) {
			t.Fatalf("the fixture: %+v, want one conflict holding the buffer's R", cs)
		}
		m.Edited(c.typed, 2)
		if ch, co := m.Counts(); ch != 1 || co != 1 {
			t.Fatalf("a line typed %s the conflict: counts %d, %d, want it still open (1, 1)", c.name, ch, co)
		}
		if got := m.Changes()[0]; got.State != Unresolved || got.Result != c.span {
			t.Fatalf("a line typed %s the conflict: %+v, want it unresolved at %v", c.name, got, c.span)
		}
		check(t, m)
	}
}

// edgeConflicts is a merge whose first and last lines are conflicts, the buffer's side kept.
func edgeConflicts() Input {
	return Input{HasBase: true, Base: text("a", "x", "b"), Left: text("L1", "x", "L2"), Right: text("R1", "x", "R2"),
		LeftFrom: disk, RightFrom: buffer, ResultTo: buffer, ResultSide: Right}
}

// The model puts a Same hunk of no lines at an edge a change touches and between two changes next
// to each other, at the very place in base, left and right where its neighbours meet; Changes lists
// the changes alone.
func TestTheModelSeparatesChangesAtItsEdgesAndBetweenThem(t *testing.T) {
	m := NewModel(edgeConflicts())
	var kinds []diff.Kind
	for _, h := range m.Hunks() {
		kinds = append(kinds, h.Kind)
	}
	if want := []diff.Kind{diff.Same, diff.Conflict, diff.Same, diff.Conflict, diff.Same}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("hunks %v, want %v", kinds, want)
	}
	zero := func(n int) diff.Span { return diff.Span{From: n, To: n} }
	if h := m.Hunks()[0]; h.Base != zero(0) || h.Left != zero(0) || h.Right != zero(0) {
		t.Fatalf("the first separator %+v, want every side at 0", h)
	}
	if h := m.Hunks()[4]; h.Base != zero(3) || h.Left != zero(3) || h.Right != zero(3) {
		t.Fatalf("the last separator %+v, want every side at 3", h)
	}
	if len(m.Changes()) != 2 {
		t.Fatalf("changes %+v, want the two conflicts alone", m.Changes())
	}
	check(t, m)
	// Two changes next to each other get one between them, where the first ends.
	got := separated([]diff.Hunk{
		{Kind: diff.Conflict, Base: diff.Span{From: 0, To: 1}, Left: diff.Span{From: 0, To: 2}, Right: diff.Span{From: 0, To: 1}},
		{Kind: diff.LeftOnly, Base: diff.Span{From: 1, To: 2}, Left: diff.Span{From: 2, To: 3}, Right: diff.Span{From: 1, To: 2}},
	})
	if len(got) != 5 || got[2].Kind != diff.Same || got[2].Base != zero(1) || got[2].Left != zero(2) || got[2].Right != zero(1) {
		t.Fatalf("separated two changes next to each other: %+v", got)
	}
}

// A line typed above a conflict on the first line, below one on the last, or between two whose
// unchanged line between was deleted, goes to the unchanged lines beside them: both conflicts stay
// counted, and a later Take replaces the conflict's own lines alone, keeping the typed one.
func TestALineTypedWhereOnlyConflictsAreBesideLeavesThemOpen(t *testing.T) {
	for _, c := range []struct {
		name  string
		edits [][]string
		take  int
		want  []string
	}{
		{"above the first line's", [][]string{{"typed", "R1", "x", "R2"}}, 0, []string{"typed", "L1", "x", "R2"}},
		{"below the last line's", [][]string{{"R1", "x", "R2", "typed"}}, 1, []string{"R1", "x", "L2", "typed"}},
		{"between two, the line between deleted", [][]string{{"R1", "R2"}, {"R1", "typed", "R2"}}, 0, []string{"L1", "typed", "R2"}},
	} {
		m := NewModel(edgeConflicts())
		m.Settle(1)
		for k, e := range c.edits {
			m.Edited(e, uint64(2+k))
			check(t, m)
		}
		if ch, co := m.Counts(); ch != 2 || co != 2 {
			t.Fatalf("a line typed %s conflict: counts %d, %d, want both open (2, 2): %+v", c.name, ch, co, m.Changes())
		}
		m.Take(c.take, Left)
		if got := m.ResultLines(); !reflect.DeepEqual(got, c.want) {
			t.Fatalf("a line typed %s conflict, then a Take: %q, want %q (the typed line kept)", c.name, got, c.want)
		}
		check(t, m)
	}
}

// Typing into an empty open conflict still resolves it, the separator beside it notwithstanding;
// typing beside a conflict that holds lines does not: both in one merge.
func TestTypingIntoAnEmptyConflictResolvesItAndBesideAFullOneDoesNot(t *testing.T) {
	m := NewModel(Input{HasBase: true, // the base kept: the inserted conflict holds no lines, the last one b
		Base: text("a", "c", "x", "b"), Left: text("a", "L", "c", "x", "B1"), Right: text("a", "R", "c", "x", "B2")})
	m.Settle(1)
	if cs := m.Changes(); len(cs) != 2 || cs[0].Result.Len() != 0 || cs[1].Result.Len() != 1 {
		t.Fatalf("the fixture: %+v, want an empty conflict and one holding b", cs)
	}
	m.Edited([]string{"a", "typed", "c", "x", "b"}, 2)
	m.Edited([]string{"a", "typed", "c", "x", "b", "below"}, 3)
	cs := m.Changes()
	if cs[0].State != Edited || cs[1].State != Unresolved {
		t.Fatalf("states %v, %v: want the empty conflict Edited, the full one still open", cs[0].State, cs[1].State)
	}
	if ch, co := m.Counts(); ch != 1 || co != 1 {
		t.Fatalf("counts %d, %d, want the full conflict alone", ch, co)
	}
	check(t, m)
}
