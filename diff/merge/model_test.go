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

// check is what holds after every operation: the spans partition the result, in order, and the
// rows show each pane's lines once, in order.
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
	next := [3]int{}
	for _, r := range m.Rows() {
		for k, v := range [3]int{r.Left, r.Result, r.Right} {
			if v < 0 {
				continue
			}
			if v != next[k] {
				t.Fatalf("pane %d's row shows line %d, want %d", k, v, next[k])
			}
			next[k]++
		}
	}
	if want := [3]int{len(m.LeftLines()), len(m.ResultLines()), len(m.RightLines())}; next != want {
		t.Fatalf("the rows show %v lines, the panes have %v", next, want)
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

// An unresolved two-way conflict keeps the side the result replaces: ResultTo naming the right,
// the right's lines; the left, the left's; neither, none.
func TestATwoWayConflictKeepsTheSideTheResultReplaces(t *testing.T) {
	left, right := text("a", "disk", "c"), text("a", "mine", "more", "c")
	for _, c := range []struct {
		name string
		to   Origin
		want []string
	}{
		{"right", buffer, []string{"a", "mine", "more", "c"}},
		{"left", disk, []string{"a", "disk", "c"}},
		{"neither", ours, []string{"a", "c"}},
	} {
		m := NewModel(Input{Left: left, Right: right, LeftFrom: disk, RightFrom: buffer, ResultTo: c.to})
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
		LeftFrom: disk, RightFrom: buffer, ResultTo: buffer,
	})
	if got := m.Result(); got != "a\r\nB\r\n" {
		t.Fatalf("result %q, want the right's CRLF text", got)
	}
	m.Take(0, Left)
	if got := m.Result(); got != "a\r\nb\r\n" {
		t.Fatalf("result %q after taking the left, want CRLF", got)
	}
	m = NewModel(Input{Left: "x", Right: "y\n", LeftFrom: disk, RightFrom: buffer, ResultTo: disk})
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
	// Two lines put in the second change's line: it is Edited, and the spans move.
	typed = append(append(append([]string{}, typed[:4]...), "x", "y"), typed[4:]...)
	m.Edited(typed, 4)
	cs := m.Changes()
	if cs[1].State != Edited || cs[1].Result != (diff.Span{From: 4, To: 7}) {
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
	m := NewModel(Input{Left: text("a"), Right: text("b"), LeftFrom: disk, RightFrom: buffer, ResultTo: buffer})
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
		in := Input{Left: random(r.Intn(12)), Right: random(r.Intn(12)), LeftFrom: disk, RightFrom: buffer, ResultTo: buffer}
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
				e, ok = m.Take(r.Intn(n), Side(r.Intn(2)))
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
