package diff

import (
	"fmt"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"
)

func lines(s string) []string {
	if s == "" {
		return []string{}
	}
	return strings.Split(s, "|")
}

func TestSplitKeepsEndingsAndTheFinalNewline(t *testing.T) {
	for _, c := range []struct {
		in    string
		lines []string
		end   Ending
		final bool
	}{
		{"", []string{}, LF, false},
		{"a", []string{"a"}, LF, false},
		{"a\n", []string{"a"}, LF, true},
		{"\n", []string{""}, LF, true},
		{"a\nb", []string{"a", "b"}, LF, false},
		{"a\r\nb\r\n", []string{"a", "b"}, CRLF, true},
		{"a\rb\r", []string{"a", "b"}, CR, true},
		{"a\r\nb\nc", []string{"a", "b", "c"}, Mixed, false},
		{"\r\n\r\n", []string{"", ""}, CRLF, true},
	} {
		got := Split(c.in)
		if !reflect.DeepEqual(got.Lines, c.lines) || got.Ending != c.end || got.FinalNewline != c.final {
			t.Errorf("Split(%q) = %q %v %v, want %q %v %v", c.in, got.Lines, got.Ending, got.FinalNewline, c.lines, c.end, c.final)
		}
		if c.end != Mixed {
			if back := got.String(); back != c.in {
				t.Errorf("Split(%q).String() = %q", c.in, back)
			}
		}
	}
	// a Mixed text joins with the first ending it had
	if got := Split("a\r\nb\nc\n").Join([]string{"x", "y"}); got != "x\r\ny\r\n" {
		t.Errorf("a Mixed text's join: %q, want its first ending, CRLF", got)
	}
}

// apply is a's edits applied: Equal keeps a's lines, Insert takes b's, Delete drops a's. The spans
// must cover a and b whole, in order.
func apply(t *testing.T, d Diff, a, b []string) []string {
	t.Helper()
	out := []string{}
	ai, bi := 0, 0
	for _, e := range d.Edits {
		if e.A.From != ai || e.B.From != bi {
			t.Fatalf("edit %+v does not follow on from a %d, b %d", e, ai, bi)
		}
		switch e.Op {
		case Equal:
			if e.A.Len() != e.B.Len() {
				t.Fatalf("an Equal edit of unequal spans: %+v", e)
			}
			for k := 0; k < e.A.Len(); k++ {
				if a[e.A.From+k] != b[e.B.From+k] {
					t.Fatalf("an Equal edit over unequal lines: %+v", e)
				}
			}
			out = append(out, a[e.A.From:e.A.To]...)
		case Delete:
			if e.B.Len() != 0 {
				t.Fatalf("a Delete with b lines: %+v", e)
			}
		case Insert:
			if e.A.Len() != 0 {
				t.Fatalf("an Insert with a lines: %+v", e)
			}
			out = append(out, b[e.B.From:e.B.To]...)
		}
		ai, bi = e.A.To, e.B.To
	}
	if ai != len(a) || bi != len(b) {
		t.Fatalf("the edits end at a %d, b %d; the texts at %d, %d", ai, bi, len(a), len(b))
	}
	return out
}

// randomText is n lines from a small alphabet, so lines repeat as code's do.
func randomText(r *rand.Rand, n, alphabet int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("l%d", r.Intn(alphabet))
	}
	return out
}

// mutate is a with random lines deleted, inserted and changed.
func mutate(r *rand.Rand, a []string, alphabet int) []string {
	out := []string{}
	for _, l := range a {
		switch r.Intn(10) {
		case 0: // deleted
		case 1:
			out = append(out, fmt.Sprintf("n%d", r.Intn(alphabet)), l)
		case 2:
			out = append(out, fmt.Sprintf("c%d", r.Intn(alphabet)))
		default:
			out = append(out, l)
		}
	}
	if r.Intn(3) == 0 {
		out = append(out, fmt.Sprintf("t%d", r.Intn(alphabet)))
	}
	return out
}

// Applying Lines' edits to a gives b, over random texts with repeated lines.
func TestLinesEditsTurnAIntoB(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		alphabet := 2 + r.Intn(20)
		a := randomText(r, r.Intn(40), alphabet)
		b := mutate(r, a, alphabet)
		if r.Intn(5) == 0 {
			b = randomText(r, r.Intn(40), alphabet)
		}
		d := Lines(a, b)
		if d.Coarse {
			t.Fatalf("a small diff is coarse: %q / %q", a, b)
		}
		if got := apply(t, d, a, b); !reflect.DeepEqual(got, b) {
			t.Fatalf("Lines(%q, %q) applied gives %q", a, b, got)
		}
	}
}

// The histogram heuristic anchors on rare lines: a function inserted before another that shares its
// braces and blank lines comes out as one insertion, not the closing lines of one and the opening
// of the other interleaved.
func TestLinesAnchorsOnRareLines(t *testing.T) {
	a := lines("func A() {|\treturn 1|}|")
	b := lines("func B() {|\treturn 2|}||func A() {|\treturn 1|}|")
	d := Lines(a, b)
	var ins []Span
	for _, e := range d.Edits {
		switch e.Op {
		case Insert:
			ins = append(ins, e.B)
		case Delete:
			t.Fatalf("an insertion before A deletes A's lines: %+v", d.Edits)
		}
	}
	if len(ins) != 1 || ins[0] != (Span{0, 4}) {
		t.Fatalf("the insertion is %v, want B's four lines at the start as one: %+v", ins, d.Edits)
	}
}

// IgnoreWhitespace and IgnoreCase change the comparison only.
func TestLinesComparesAsOptionsSay(t *testing.T) {
	a, b := lines("Alpha|  beta"), lines("alpha|beta  ")
	if d := Lines(a, b); len(d.Edits) == 1 && d.Edits[0].Op == Equal {
		t.Fatal("exact comparison found unequal lines equal")
	}
	d := Lines(a, b, Options{IgnoreWhitespace: true, IgnoreCase: true})
	if len(d.Edits) != 1 || d.Edits[0].Op != Equal {
		t.Fatalf("lines alike but for case and whitespace: %+v", d.Edits)
	}
}

// Past MaxLines the diff is coarse, and still correct: the common ends kept, the rest replaced.
func TestLinesPastItsBoundIsCoarse(t *testing.T) {
	a := lines("h|x1|x2|x3|t")
	b := lines("h|y1|x2|y3|t")
	d := Lines(a, b, Options{MaxLines: 4})
	if !d.Coarse {
		t.Fatal("a diff past MaxLines is not coarse")
	}
	if got := apply(t, d, a, b); !reflect.DeepEqual(got, b) {
		t.Fatalf("the coarse diff applied gives %q", got)
	}
	if d.Edits[0] != (Edit{Op: Equal, A: Span{0, 1}, B: Span{0, 1}}) || d.Edits[len(d.Edits)-1] != (Edit{Op: Equal, A: Span{4, 5}, B: Span{4, 5}}) {
		t.Fatalf("the coarse diff lost the common ends: %+v", d.Edits)
	}
	if d := Lines(a, b); d.Coarse {
		t.Fatal("within its bound the diff is coarse")
	}
}

// diff3's cases: each hunk kind, adjacent and overlapping changes, empty sides, inserts at either
// end.
func TestMerge3Cases(t *testing.T) {
	type h struct {
		k       Kind
		b, l, r string
	}
	for _, c := range []struct {
		name              string
		base, left, right string
		want              []h
	}{
		{"unchanged", "a|b|c", "a|b|c", "a|b|c", []h{{Same, "a|b|c", "a|b|c", "a|b|c"}}},
		{"left only", "a|b|c", "a|B|c", "a|b|c", []h{{Same, "a", "a", "a"}, {LeftOnly, "b", "B", "b"}, {Same, "c", "c", "c"}}},
		{"right only", "a|b|c", "a|b|c", "a|b|C", []h{{Same, "a|b", "a|b", "a|b"}, {RightOnly, "c", "c", "C"}}},
		{"both alike", "a|b|c", "a|X|c", "a|X|c", []h{{Same, "a", "a", "a"}, {Both, "b", "X", "X"}, {Same, "c", "c", "c"}}},
		{"conflict", "a|b|c", "a|L|c", "a|R|c", []h{{Same, "a", "a", "a"}, {Conflict, "b", "L", "R"}, {Same, "c", "c", "c"}}},
		{"apart", "a|b|c|d|e", "a|B|c|d|e", "a|b|c|D|e", []h{{Same, "a", "a", "a"}, {LeftOnly, "b", "B", "b"}, {Same, "c", "c", "c"}, {RightOnly, "d", "d", "D"}, {Same, "e", "e", "e"}}},
		{"adjacent conflict", "a|b|c|d", "a|B|c|d", "a|b|C|d", []h{{Same, "a", "a", "a"}, {Conflict, "b|c", "B|c", "b|C"}, {Same, "d", "d", "d"}}},
		{"delete against change", "a|b|c", "a|c", "a|X|c", []h{{Same, "a", "a", "a"}, {Conflict, "b", "", "X"}, {Same, "c", "c", "c"}}},
		{"both delete", "a|b|c", "a|c", "a|c", []h{{Same, "a", "a", "a"}, {Both, "b", "", ""}, {Same, "c", "c", "c"}}},
		{"empty base", "", "x", "y", []h{{Conflict, "", "x", "y"}}},
		{"empty base, one side", "", "x", "", []h{{LeftOnly, "", "x", ""}}},
		{"empty sides", "a", "", "", []h{{Both, "a", "", ""}}},
		{"insert at start and end", "m", "s|m", "m|e", []h{{LeftOnly, "", "s", ""}, {Same, "m", "m", "m"}, {RightOnly, "", "", "e"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			base, left, right := lines(c.base), lines(c.left), lines(c.right)
			m := Merge3(base, left, right)
			var got []h
			for _, x := range m.Hunks {
				got = append(got, h{x.Kind, strings.Join(base[x.Base.From:x.Base.To], "|"), strings.Join(left[x.Left.From:x.Left.To], "|"), strings.Join(right[x.Right.From:x.Right.To], "|")})
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("Merge3 hunks\n got %+v\nwant %+v", got, c.want)
			}
		})
	}
}

// Merge3's hunks cover base, left and right whole, in order; and a merge of changes on one side
// only, or alike on both, resolves to that side with no conflict.
func TestMerge3CoversEverySideAndResolvesOneSidedChanges(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	cover := func(spans []Span, n int, what string) {
		t.Helper()
		at := 0
		for _, s := range spans {
			if s.From != at || s.To < s.From {
				t.Fatalf("%s: span %v does not follow on from %d", what, s, at)
			}
			at = s.To
		}
		if at != n {
			t.Fatalf("%s: the spans end at %d, the text at %d", what, at, n)
		}
	}
	for i := 0; i < 1500; i++ {
		alphabet := 2 + r.Intn(15)
		base := randomText(r, r.Intn(30), alphabet)
		left, right := mutate(r, base, alphabet), mutate(r, base, alphabet)
		m := Merge3(base, left, right)
		var bs, ls, rs []Span
		for _, h := range m.Hunks {
			bs, ls, rs = append(bs, h.Base), append(ls, h.Left), append(rs, h.Right)
		}
		cover(bs, len(base), "base")
		cover(ls, len(left), "left")
		cover(rs, len(right), "right")

		for _, c := range []struct {
			l, r, want []string
		}{{left, base, left}, {base, right, right}, {left, left, left}} {
			res := Resolve(Merge3(base, c.l, c.r), base, c.l, c.r, PickBase)
			if res.Conflicts != 0 || !reflect.DeepEqual(res.Lines, c.want) {
				t.Fatalf("base %q left %q right %q: resolved %q with %d conflicts, want %q", base, c.l, c.r, res.Lines, res.Conflicts, c.want)
			}
		}
	}
}

// Merge2 has no base: every difference is a conflict, the lines alike on both are Same.
func TestMerge2MakesEveryDifferenceAConflict(t *testing.T) {
	left, right := lines("a|b|c|d"), lines("a|B|c|d|e")
	m := Merge2(left, right)
	if m.HasBase {
		t.Fatal("a two-way merge says it has a base")
	}
	var kinds []Kind
	for _, h := range m.Hunks {
		kinds = append(kinds, h.Kind)
		if h.Base.Len() != 0 {
			t.Fatalf("a two-way hunk with base lines: %+v", h)
		}
	}
	if want := []Kind{Same, Conflict, Same, Conflict}; !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds %v, want %v (%+v)", kinds, want, m.Hunks)
	}
	if res := Resolve(m, nil, left, right, PickRight); res.Conflicts != 2 || strings.Join(res.Lines, "|") != "a|B|c|d|e" {
		t.Fatalf("resolved keeping the right: %q, %d conflicts", res.Lines, res.Conflicts)
	}
	if res := Resolve(m, nil, left, right, PickLeft); strings.Join(res.Lines, "|") != "a|b|c|d" {
		t.Fatalf("resolved keeping the left: %q", res.Lines)
	}
}

// Resolve keeps what each pick says for a conflict, and gives each hunk's span in its lines.
func TestResolveKeepsTheUnresolvedPickAndSpansEachHunk(t *testing.T) {
	base, left, right := lines("a|b|c"), lines("a|L|c"), lines("a|R|c")
	m := Merge3(base, left, right)
	for pick, want := range map[Pick]string{PickBase: "a|b|c", PickLeft: "a|L|c", PickRight: "a|R|c", PickNone: "a|c"} {
		res := Resolve(m, base, left, right, pick)
		if strings.Join(res.Lines, "|") != want || res.Conflicts != 1 {
			t.Errorf("pick %d: %q (%d conflicts), want %q", pick, res.Lines, res.Conflicts, want)
		}
		if len(res.Spans) != len(m.Hunks) || res.Spans[len(res.Spans)-1].To != len(res.Lines) {
			t.Errorf("pick %d: spans %v do not cover %d lines", pick, res.Spans, len(res.Lines))
		}
	}
}

// lcs is the length of a and b's longest common subsequence, by the table.
func lcs(a, b []string) int {
	prev := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		for j := 1; j <= len(b); j++ {
			switch {
			case a[i-1] == b[j-1]:
				cur[j] = prev[j-1] + 1
			case prev[j] >= cur[j-1]:
				cur[j] = prev[j]
			default:
				cur[j] = cur[j-1]
			}
		}
		prev = cur
	}
	return prev[len(b)]
}

// Myers' edits are the shortest: as many lines matched as the texts' longest common subsequence,
// in order, each pair alike. (Lines anchors where a line is rare, which need not be shortest; a
// region with no such line is Myers', so Myers is diffed here on its own.)
func TestMyersMatchesALongestCommonSubsequence(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for i := 0; i < 60; i++ {
		a := randomText(r, 140+r.Intn(60), 2+i%3)
		b := randomText(r, 140+r.Intn(60), 2+i%3)
		ids := intern(Options{}, a, b)
		d := &differ{a: ids[0], b: ids[1], deadline: time.Now().Add(time.Minute)}
		d.myers(0, len(a), 0, len(b))
		if d.coarse {
			t.Fatalf("case %d: Myers stopped coarse within its bounds", i)
		}
		for k, mt := range d.matches {
			if a[mt[0]] != b[mt[1]] {
				t.Fatalf("case %d: a[%d] %q matched to b[%d] %q", i, mt[0], a[mt[0]], mt[1], b[mt[1]])
			}
			if k > 0 && (mt[0] <= d.matches[k-1][0] || mt[1] <= d.matches[k-1][1]) {
				t.Fatalf("case %d: match %v follows %v", i, mt, d.matches[k-1])
			}
		}
		if got := apply(t, Diff{Edits: edits(d.matches, len(a), len(b))}, a, b); !reflect.DeepEqual(got, b) {
			t.Fatalf("case %d: applied, the edits give %d lines unlike b", i, len(got))
		}
		if want := lcs(a, b); len(d.matches) != want {
			t.Fatalf("case %d: %d lines matched, the longest common subsequence is %d", i, len(d.matches), want)
		}
	}
}
