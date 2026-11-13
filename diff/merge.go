package diff

// Kind is what a merge hunk holds.
type Kind uint8

const (
	// Same lines are alike on every side.
	Same Kind = iota
	// LeftOnly is changed on the left; the right is as the base.
	LeftOnly
	// RightOnly is changed on the right; the left is as the base.
	RightOnly
	// Both is changed on both sides alike.
	Both
	// Conflict is changed on both sides, differently: someone decides.
	Conflict
)

// Hunk is one run of a merge, its lines in base, left and right. A two-way merge's Base spans are
// empty.
type Hunk struct {
	Kind              Kind
	Base, Left, Right Span
}

// Merge is the hunks of a merge, in order, covering every side whole. HasBase is that it was a
// three-way merge; Coarse that a bound was met in one of its diffs.
type Merge struct {
	Hunks   []Hunk
	HasBase bool
	Coarse  bool
}

// Merge3 is the three-way merge of left and right against base, diff3's way: a region where both
// sides keep the base's lines is Same; between two such regions, the sides' changes are LeftOnly,
// RightOnly, Both, or a Conflict. Changes on both sides with no unchanged line between them are one
// region, so they conflict, as diff3's do.
func Merge3(base, left, right []string, opts ...Options) Merge {
	dl, dr := Lines(base, left, opts...), Lines(base, right, opts...)
	ml, mr := matched(dl, len(base)), matched(dr, len(base))
	ids := intern(options(opts), base, left, right)
	ib, il, ir := ids[0], ids[1], ids[2]
	eq := func(x []int, xs Span, y []int, ys Span) bool {
		if xs.Len() != ys.Len() {
			return false
		}
		for k := 0; k < xs.Len(); k++ {
			if x[xs.From+k] != y[ys.From+k] {
				return false
			}
		}
		return true
	}
	m := Merge{HasBase: true, Coarse: dl.Coarse || dr.Coarse}
	add := func(h Hunk) {
		if h.Base.Len() == 0 && h.Left.Len() == 0 && h.Right.Len() == 0 {
			return
		}
		if k := len(m.Hunks) - 1; k >= 0 && h.Kind == Same && m.Hunks[k].Kind == Same {
			m.Hunks[k].Base.To, m.Hunks[k].Left.To, m.Hunks[k].Right.To = h.Base.To, h.Left.To, h.Right.To
			return
		}
		m.Hunks = append(m.Hunks, h)
	}
	unstable := func(b, l, r Span) {
		lc := !eq(ib, b, il, l)
		rc := !eq(ib, b, ir, r)
		h := Hunk{Base: b, Left: l, Right: r}
		switch {
		case !lc && !rc:
			h.Kind = Same
		case lc && !rc:
			h.Kind = LeftOnly
		case !lc && rc:
			h.Kind = RightOnly
		case eq(il, l, ir, r):
			h.Kind = Both
		default:
			h.Kind = Conflict
		}
		add(h)
	}
	bi, li, ri := 0, 0, 0
	for bi < len(base) {
		k := bi
		for k < len(base) && (ml[k] < 0 || mr[k] < 0) {
			k++
		}
		if k == len(base) {
			break
		}
		if k == bi && ml[k] == li && mr[k] == ri {
			add(Hunk{Kind: Same, Base: Span{bi, bi + 1}, Left: Span{li, li + 1}, Right: Span{ri, ri + 1}})
			bi, li, ri = bi+1, li+1, ri+1
			continue
		}
		unstable(Span{bi, k}, Span{li, ml[k]}, Span{ri, mr[k]})
		bi, li, ri = k, ml[k], mr[k]
	}
	unstable(Span{bi, len(base)}, Span{li, len(left)}, Span{ri, len(right)})
	return m
}

// matched is, for each line of a, the line of b the diff matched it to, or -1.
func matched(d Diff, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = -1
	}
	for _, e := range d.Edits {
		if e.Op != Equal {
			continue
		}
		for k := 0; k < e.A.Len(); k++ {
			out[e.A.From+k] = e.B.From + k
		}
	}
	return out
}

// Merge2 is the two-way merge of left and right, with no base: lines alike on both are Same, and
// every difference is a Conflict someone decides.
func Merge2(left, right []string, opts ...Options) Merge {
	d := Lines(left, right, opts...)
	m := Merge{Coarse: d.Coarse}
	for _, e := range d.Edits {
		if e.Op == Equal {
			m.Hunks = append(m.Hunks, Hunk{Kind: Same, Base: Span{}, Left: e.A, Right: e.B})
			continue
		}
		if k := len(m.Hunks) - 1; k >= 0 && m.Hunks[k].Kind == Conflict {
			h := &m.Hunks[k]
			if e.Op == Delete {
				h.Left.To = e.A.To
			} else {
				h.Right.To = e.B.To
			}
			continue
		}
		h := Hunk{Kind: Conflict, Left: Span{e.A.From, e.A.From}, Right: Span{e.B.From, e.B.From}}
		if e.Op == Delete {
			h.Left.To = e.A.To
		} else {
			h.Right.To = e.B.To
		}
		m.Hunks = append(m.Hunks, h)
	}
	return m
}

// Pick is the side an unresolved conflict keeps in a resolved text.
type Pick uint8

const (
	// PickBase keeps the base's lines (a two-way merge has none: the conflict keeps nothing).
	PickBase Pick = iota
	// PickLeft and PickRight keep that side's lines.
	PickLeft
	PickRight
	// PickNone keeps no lines: the conflict is an empty span, marked.
	PickNone
)

// Resolved is a merge resolved: its lines, each hunk's span in them (by the hunk's index), and the
// conflicts left.
type Resolved struct {
	Lines     []string
	Spans     []Span
	Conflicts int
}

// Resolve takes each hunk's changed side: Same and Both as they are (the base's for Same in a
// three-way merge, the left's otherwise), LeftOnly the left's, RightOnly the right's. A conflict is
// left unresolved, keeping the side unresolved picks, and is counted.
func Resolve(m Merge, base, left, right []string, unresolved Pick) Resolved {
	r := Resolved{Lines: []string{}, Spans: make([]Span, len(m.Hunks))}
	take := func(lines []string, s Span) []string { return lines[s.From:s.To] }
	for i, h := range m.Hunks {
		var lines []string
		switch h.Kind {
		case Same:
			if m.HasBase {
				lines = take(base, h.Base)
			} else {
				lines = take(left, h.Left)
			}
		case LeftOnly, Both:
			lines = take(left, h.Left)
		case RightOnly:
			lines = take(right, h.Right)
		case Conflict:
			r.Conflicts++
			switch unresolved {
			case PickBase:
				lines = take(base, h.Base)
			case PickLeft:
				lines = take(left, h.Left)
			case PickRight:
				lines = take(right, h.Right)
			}
		}
		from := len(r.Lines)
		r.Lines = append(r.Lines, lines...)
		r.Spans[i] = Span{from, len(r.Lines)}
	}
	return r
}
