package diff

import (
	"strings"
	"time"
	"unicode"
)

// Options are how lines compare, and the bounds past which a diff is coarse. The zero value
// compares exactly, within the default bounds.
type Options struct {
	// IgnoreWhitespace compares lines with their whitespace removed, IgnoreCase folded: only the
	// comparison changes, never the lines a diff or a merge keeps.
	IgnoreWhitespace, IgnoreCase bool
	// MaxLines bounds the lines compared (both sides together; 0: DefaultMaxLines). Past it the diff
	// of what differs is coarse.
	MaxLines int
	// Deadline bounds the time spent (0: DefaultDeadline). Past it the regions left are coarse.
	Deadline time.Duration
}

// The default bounds: a diff of more lines, or one that takes longer, is coarse.
const (
	DefaultMaxLines = 200_000
	DefaultDeadline = 2 * time.Second
)

// Span is a half-open range of lines, From up to To.
type Span struct{ From, To int }

// Len is the span's number of lines.
func (s Span) Len() int { return s.To - s.From }

// Op is what an edit does.
type Op uint8

const (
	// Equal lines are in both texts: A and B are as long.
	Equal Op = iota
	// Delete lines are in a only: B is empty, where they were.
	Delete
	// Insert lines are in b only: A is empty, where they go.
	Insert
)

// Edit is one run of equal, deleted or inserted lines, A in a and B in b.
type Edit struct {
	Op   Op
	A, B Span
}

// Diff is the edits that turn a into b, in order, covering both texts whole. A replaced block is a
// Delete then an Insert. Coarse is that a bound was met: some region is one block replaced by
// another, rather than the smallest difference.
type Diff struct {
	Edits  []Edit
	Coarse bool
}

// Lines is the diff of a and b.
func Lines(a, b []string, opts ...Options) Diff {
	o := options(opts)
	ids := intern(o, a, b)
	d := &differ{a: ids[0], b: ids[1], deadline: time.Now().Add(o.Deadline), now: time.Now}
	if len(a)+len(b) > o.MaxLines {
		d.coarse = true
		d.trimmed(0, len(a), 0, len(b), func(a0, a1, b0, b1 int) {}) // the common ends only
	} else {
		d.region(0, len(a), 0, len(b))
	}
	return Diff{Edits: edits(d.matches, len(a), len(b)), Coarse: d.coarse}
}

func options(opts []Options) Options {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.MaxLines <= 0 {
		o.MaxLines = DefaultMaxLines
	}
	if o.Deadline <= 0 {
		o.Deadline = DefaultDeadline
	}
	return o
}

// intern gives each distinct line of the texts, as compared, a number, one numbering for them all:
// a diff compares numbers.
func intern(o Options, texts ...[]string) [][]int {
	ids := map[string]int{}
	key := func(s string) string {
		if o.IgnoreWhitespace {
			s = strings.Map(func(r rune) rune {
				if unicode.IsSpace(r) {
					return -1
				}
				return r
			}, s)
		}
		if o.IgnoreCase {
			s = strings.ToLower(s)
		}
		return s
	}
	number := func(lines []string) []int {
		out := make([]int, len(lines))
		for i, l := range lines {
			k := key(l)
			id, ok := ids[k]
			if !ok {
				id = len(ids)
				ids[k] = id
			}
			out[i] = id
		}
		return out
	}
	out := make([][]int, len(texts))
	for i, t := range texts {
		out[i] = number(t)
	}
	return out
}

// differ finds the matched lines of a and b, in order.
type differ struct {
	a, b     []int
	matches  [][2]int // (i in a, j in b), increasing in both
	deadline time.Time
	now      func() time.Time // the clock the deadline is read on
	coarse   bool
	work     int // steps of the histogram search, for its bound
}

// maxChain is how often a line may occur in a region and still anchor it (git's histogram's
// limit): a region whose rarest common line is more common is diffed by Myers.
const maxChain = 64

// region matches a[a0:a1] against b[b0:b1].
func (d *differ) region(a0, a1, b0, b1 int) {
	d.trimmed(a0, a1, b0, b1, func(a0, a1, b0, b1 int) {
		if a0 == a1 || b0 == b1 {
			return
		}
		if d.late() {
			d.coarse = true // the region left is one block replaced
			return
		}
		s, t, n, ok, late := d.anchor(a0, a1, b0, b1)
		if late {
			return // coarse: the region left is one block replaced
		}
		if !ok {
			d.myers(a0, a1, b0, b1)
			return
		}
		d.region(a0, s, b0, t)
		for k := 0; k < n; k++ {
			d.matches = append(d.matches, [2]int{s + k, t + k})
		}
		d.region(s+n, a1, t+n, b1)
	})
}

// trimmed matches the region's common head and tail, and hands what is between to inner.
func (d *differ) trimmed(a0, a1, b0, b1 int, inner func(a0, a1, b0, b1 int)) {
	for a0 < a1 && b0 < b1 && d.a[a0] == d.b[b0] {
		d.matches = append(d.matches, [2]int{a0, b0})
		a0, b0 = a0+1, b0+1
	}
	tail := 0
	for a0 < a1-tail && b0 < b1-tail && d.a[a1-1-tail] == d.b[b1-1-tail] {
		tail++
	}
	inner(a0, a1-tail, b0, b1-tail)
	for k := tail; k > 0; k-- {
		d.matches = append(d.matches, [2]int{a1 - k, b1 - k})
	}
}

// anchor is the run of equal lines the region splits at, as git's histogram diff chooses: the one
// whose rarest line occurs least often in a's region (at most maxChain times), the longest of
// those. ok is false when no line of b's region occurs that rarely in a's; late, that the deadline
// passed while it looked, so the region is coarse.
//
// A run is expanded once: the lines of b it covers are passed over, so a long run of lines common
// to both costs its length, not its length squared.
func (d *differ) anchor(a0, a1, b0, b1 int) (s, t, n int, ok, late bool) {
	where := map[int][]int{}
	for i := a0; i < a1; i++ {
		where[d.a[i]] = append(where[d.a[i]], i)
		if d.tick() {
			return 0, 0, 0, false, true
		}
	}
	best := maxChain + 1
	for j := b0; j < b1; {
		next := j + 1
		if at := where[d.b[j]]; len(at) > 0 && len(at) <= best {
			for _, i := range at {
				rc := len(at) // the run's rarest line's count
				ss, tt := i, j
				for ss > a0 && tt > b0 && d.a[ss-1] == d.b[tt-1] {
					ss, tt = ss-1, tt-1
					rc = min(rc, len(where[d.a[ss]]))
					if d.tick() {
						return 0, 0, 0, false, true
					}
				}
				e, f := i+1, j+1
				for e < a1 && f < b1 && d.a[e] == d.b[f] {
					rc = min(rc, len(where[d.a[e]]))
					e, f = e+1, f+1
					if d.tick() {
						return 0, 0, 0, false, true
					}
				}
				if rc < best || (rc == best && e-ss > n) {
					s, t, n, best, ok = ss, tt, e-ss, rc, true
				}
				next = max(next, f)
			}
		}
		if d.tick() {
			return 0, 0, 0, false, true
		}
		j = next
	}
	return s, t, n, ok, false
}

// tickEvery is how many steps of the histogram search pass between two looks at the clock.
const tickEvery = 1024

// tick counts one step of the histogram search, and every tickEvery steps reports whether the
// deadline has passed (the diff is then coarse).
func (d *differ) tick() bool {
	d.work++
	if d.work%tickEvery == 0 && d.late() {
		d.coarse = true
		return true
	}
	return false
}

// late is whether the deadline has passed.
func (d *differ) late() bool { return d.now().After(d.deadline) }

// myersBudget bounds Myers' trace (its copies of v, in ints): past it the region is coarse, so a
// large region with no anchor never holds memory in proportion to its size times its distance.
const myersBudget = 4 << 20

// myers matches the region by Myers' O(ND) diff, a's and b's lines matched along its shortest
// edit script. Past the deadline or the trace's budget it stops, the region coarse.
func (d *differ) myers(a0, a1, b0, b1 int) {
	n, m := a1-a0, b1-b0
	max := n + m
	off := max
	v := make([]int, 2*max+2)
	var trace [][]int
	for e := 0; e <= max; e++ {
		if (e%64 == 0 && d.late()) || (e+1)*len(v) > myersBudget {
			d.coarse = true
			return
		}
		trace = append(trace, append([]int(nil), v...))
		for k := -e; k <= e; k += 2 {
			var x int
			if k == -e || (k != e && v[off+k-1] < v[off+k+1]) {
				x = v[off+k+1] // down: an insertion
			} else {
				x = v[off+k-1] + 1 // right: a deletion
			}
			y := x - k
			for x < n && y < m && d.a[a0+x] == d.b[b0+y] {
				x, y = x+1, y+1
			}
			v[off+k] = x
			if x >= n && y >= m {
				d.backtrack(trace, off, e, n, m, a0, b0)
				return
			}
		}
	}
}

// backtrack walks Myers' trace back from (n, m), recording the diagonal moves as matches in order.
func (d *differ) backtrack(trace [][]int, off, e, n, m, a0, b0 int) {
	var rev [][2]int
	x, y := n, m
	for ; e > 0; e-- {
		v := trace[e]
		k := x - y
		var pk int
		if k == -e || (k != e && v[off+k-1] < v[off+k+1]) {
			pk = k + 1
		} else {
			pk = k - 1
		}
		px := v[off+pk]
		py := px - pk
		for x > px && y > py && x > 0 && y > 0 {
			x, y = x-1, y-1
			rev = append(rev, [2]int{a0 + x, b0 + y})
		}
		x, y = px, py
	}
	for x > 0 && y > 0 {
		x, y = x-1, y-1
		rev = append(rev, [2]int{a0 + x, b0 + y})
	}
	for i := len(rev) - 1; i >= 0; i-- {
		d.matches = append(d.matches, rev[i])
	}
}

// edits are the matches as runs of equal lines, with the lines between deleted then inserted.
func edits(matches [][2]int, n, m int) []Edit {
	var out []Edit
	add := func(e Edit) {
		if e.A.Len() == 0 && e.B.Len() == 0 {
			return
		}
		if k := len(out) - 1; k >= 0 && out[k].Op == e.Op && out[k].A.To == e.A.From && out[k].B.To == e.B.From {
			out[k].A.To, out[k].B.To = e.A.To, e.B.To
			return
		}
		out = append(out, e)
	}
	i, j := 0, 0
	for _, mt := range matches {
		add(Edit{Op: Delete, A: Span{i, mt[0]}, B: Span{j, j}})
		add(Edit{Op: Insert, A: Span{mt[0], mt[0]}, B: Span{j, mt[1]}})
		add(Edit{Op: Equal, A: Span{mt[0], mt[0] + 1}, B: Span{mt[1], mt[1] + 1}})
		i, j = mt[0]+1, mt[1]+1
	}
	add(Edit{Op: Delete, A: Span{i, n}, B: Span{j, j}})
	add(Edit{Op: Insert, A: Span{n, n}, B: Span{j, m}})
	return out
}
