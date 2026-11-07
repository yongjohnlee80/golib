package widget

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/parse/markdown"
	mdhtml "github.com/yongjohnlee80/golib/parse/markdown/html"
	"github.com/yongjohnlee80/golib/tui"
)

// htmlview_tables_internal_test.go: a table wider than its room is boxed all the same, its columns
// narrowed (fitColumns) and its cells' text wrapped in them (layRow); one whose columns can't narrow
// to tableMinColumn is drawn at its own width and pans, its rows as one (Johno, 2026-11-07: wrap,
// pan as the fallback).

// TestFitColumnsNarrowsTheWidestFirst: the columns fit the room exactly when they are narrowed, the
// narrow ones keep their widths, the widest are narrowed to one cap (a cell more for the first ones
// the cap leaves room for), and columns that can't narrow to tableMinColumn get none.
func TestFitColumnsNarrowsTheWidestFirst(t *testing.T) {
	for _, tc := range []struct {
		natural []int
		avail   int
		want    []int
	}{
		{[]int{5, 24}, 13, []int{5, 8}},
		{[]int{3, 40, 40}, 30, []int{3, 14, 13}},
		{[]int{10, 10}, 20, []int{10, 10}},
		{[]int{2, 2, 2}, 3, nil},
		{[]int{30, 30, 30, 30}, 15, nil}, // four columns of tableMinColumn are 16
		{[]int{30, 30, 30, 30}, 16, []int{4, 4, 4, 4}},
		{nil, 10, nil},
	} {
		if got := fitColumns(tc.natural, tc.avail); !slices.Equal(got, tc.want) {
			t.Errorf("fitColumns(%v, %d) = %v, want %v", tc.natural, tc.avail, got, tc.want)
		}
	}
}

// layTable lays src's Markdown out in cells at width, as a terminal measures it.
func layTable(t *testing.T, src string, width int) *cellHTML {
	t.Helper()
	var html bytes.Buffer
	if err := mdhtml.Render(&html, markdown.Parse([]byte(src), markdown.WithGFM())); err != nil {
		t.Fatal(err)
	}
	c := newCellHTML(tui.StringWidth)
	c.SetSource(html.Bytes())
	c.SetWidth(float32(width))
	return c
}

// rowText is a laid-out row's clusters as text, their gaps filled.
func rowText(r hrow) string {
	var sb strings.Builder
	x := 0
	for _, k := range r.cls {
		for ; x < k.x; x++ {
			sb.WriteByte(' ')
		}
		sb.WriteString(k.text)
		x = k.x + k.w
	}
	return sb.String()
}

// TestAWideTableIsBoxedItsCellsWrapped: random tables at random widths are boxed: every row (its
// lines and the box's rules) puts its column rules in the same columns, and each cell's text reaches
// the page whole, in its order. A narrowed table's rows are no wider than the room; a table that
// can't narrow (panned) shows only between its room's mark columns, wherever it is panned to.
func TestAWideTableIsBoxedItsCellsWrapped(t *testing.T) {
	words := []string{"a", "index", "search_term", "migration", "`code`", "UNIQUE(search_term_id)", "東京", "이름", "x", "refresh"}
	boxedN, pannedN := 0, 0
	for seed := range uint64(300) {
		r := rand.New(rand.NewPCG(seed, 0x7ab1e))
		ncol, nrow, width := 1+r.IntN(5), 1+r.IntN(4), 20+r.IntN(80)
		cell := func() string {
			ws := make([]string, 1+r.IntN(8))
			for i := range ws {
				ws[i] = words[r.IntN(len(words))]
			}
			return strings.Join(ws, " ")
		}
		var src strings.Builder
		var cells [][]string
		for row := range nrow + 1 {
			var rc []string
			src.WriteString("|")
			for range ncol {
				c := cell()
				rc = append(rc, c)
				src.WriteString(" " + c + " |")
			}
			src.WriteString("\n")
			cells = append(cells, rc)
			if row == 0 {
				src.WriteString(strings.Repeat("| --- ", ncol) + "|\n")
			}
		}
		c := layTable(t, src.String(), width)
		var rules []int
		inBox := false
		text := map[[2]int]string{} // each (row block, cell)'s text as the page shows it
		panned := false
		for _, row := range c.rows {
			line := rowText(row)
			if row.pans {
				panned = true
				for _, off := range []int{0, 1 << 20} { // panned to its start, and to its end
					c.pan = map[int]int{c.panKey(row.block): off}
					for _, k := range row.cls {
						if x, shown := c.shownAt(&row, k); shown && (x < 1 || x+k.w > width-1) {
							t.Fatalf("seed %d, width %d: a panned table's cell %q shows at %d, outside the mark columns", seed, width, k.text, x)
						}
					}
				}
				c.pan = nil
			} else if w := tui.StringWidth(line); w > width {
				t.Fatalf("seed %d, width %d: a row %d wide: %q\n%s", seed, width, w, line, src.String())
			}
			var at []int
			for _, k := range row.cls {
				if k.deco && strings.ContainsAny(k.text, "│┌┬┐├┼┤└┴┘") {
					at = append(at, k.x)
				}
			}
			if len(at) > 0 {
				inBox = true
				if rules == nil {
					rules = at
				}
				if !slices.Equal(at, rules) {
					t.Fatalf("seed %d, width %d: a row's rules at %v, the box's at %v:\n%s", seed, width, at, rules, src.String())
				}
			}
			for _, k := range row.cls {
				if b := k.pos.Block; !k.deco && b >= 0 && b < len(c.doc) && c.doc[b].kind == hRow {
					ci := 0
					for ci+1 < len(c.doc[b].cellStarts) && c.doc[b].cellStarts[ci+1] <= k.pos.Span {
						ci++
					}
					text[[2]int{b, ci}] += k.text
				}
			}
		}
		if !inBox || len(rules) != ncol+1 {
			t.Fatalf("seed %d, width %d: not boxed, or %d rules for %d columns:\n%s", seed, width, len(rules), ncol, src.String())
		}
		if panned {
			pannedN++
		} else {
			boxedN++
		}
		// every cell's text reaches the page whole, in its order (a wrap drops only the space it
		// breaks at, and inline code its backticks)
		var rowBlocks []int
		for b := range c.doc {
			if c.doc[b].kind == hRow {
				rowBlocks = append(rowBlocks, b)
			}
		}
		if len(rowBlocks) != len(cells) {
			t.Fatalf("seed %d: %d rows laid out for %d", seed, len(rowBlocks), len(cells))
		}
		squash := func(s string) string { return strings.NewReplacer(" ", "", "`", "").Replace(s) }
		for ri, b := range rowBlocks {
			for ci, want := range cells[ri] {
				if got := text[[2]int{b, ci}]; squash(got) != squash(want) {
					t.Fatalf("seed %d, width %d: row %d's cell %d reads %q, want %q", seed, width, ri, ci, got, want)
				}
			}
		}
	}
	t.Logf("%d tables narrowed to the room, %d panned", boxedN, pannedN)
	if boxedN < 9*pannedN || pannedN == 0 {
		t.Errorf("%d tables narrowed, %d panned: want nearly all narrowed, and some panned (the cell's reach)", boxedN, pannedN)
	}
}

// TestAWrappedRowsFirstLineCopiesItsCellsAsTabs: the first line of a wrapped row keeps the row's own
// column separators, which copy as a tab, and its later lines' rules are the box's (no copy).
func TestAWrappedRowsFirstLineCopiesItsCellsAsTabs(t *testing.T) {
	c := layTable(t, "| Layer | Change |\n| --- | --- |\n| SQL | a change long enough to wrap in its column |\n", 30)
	var lines []hrow
	for _, r := range c.rows {
		if r.block >= 0 && c.doc[r.block].kind == hRow && !c.doc[r.block].head && len(r.cls) > 0 && !strings.HasPrefix(rowText(r), "└") {
			lines = append(lines, r)
		}
	}
	if len(lines) < 2 {
		t.Fatalf("the row isn't wrapped: %d lines\n%s", len(lines), fmt.Sprint(lines))
	}
	sep := func(r hrow) *hspan {
		for _, k := range r.cls {
			if k.text == "│" && k.x > 0 && k.sp != nil {
				return k.sp
			}
		}
		return nil
	}
	if sp := sep(lines[0]); sp == nil || sp.copy != "\t" {
		t.Errorf("the first line's column rule is %v, want the row's separator (copied as a tab)", sp)
	}
	for _, r := range lines[1:] {
		if sp := sep(r); sp != nil {
			t.Errorf("a later line's column rule is the row's separator %v, want the box's", sp)
		}
	}
}

// TestATableTooWideToNarrowPansAsOne: a table whose columns can't narrow to the room keeps its
// natural widths and pans: every row of it (and its box's) pans by the one offset, Pan moves them
// together, a » marks more of it to the right before it is panned and a « after, and panned to its
// end its box's right side shows.
func TestATableTooWideToNarrowPansAsOne(t *testing.T) {
	const width = 20
	c := layTable(t, "| one | two | three | four | five |\n| --- | --- | --- | --- | --- |\n| alpha | bravo | charlie | delta | echo |\n", width)
	var rows []int
	for ri, r := range c.rows {
		if r.pans {
			rows = append(rows, ri)
		}
	}
	if len(rows) != 5 { // its box's top, its header, the rule under it, its row, its box's bottom
		t.Fatalf("%d rows pan, want the table's 5", len(rows))
	}
	paint := func() *recordSurface {
		s := newRecordSurface(width, len(c.rows))
		c.PaintCells(s, 0, width, DocPos{}, DocPos{})
		return s
	}
	mid := (rows[0] + rows[len(rows)-1]) / 2
	if s := paint(); s.text[mid][width-1] != "»" || s.text[mid][0] == "«" {
		t.Fatalf("unpanned, the marks are %q … %q, want only a »:\n%v", s.text[mid][0], s.text[mid][width-1], s.text)
	}
	for c.Pan(0, len(c.rows), htmlPanStep) {
	}
	offs := map[int]bool{}
	for _, ri := range rows {
		offs[c.offset(c.rows[ri].block)] = true
	}
	if len(offs) != 1 {
		t.Fatalf("the table's rows pan by %v, want one offset", offs)
	}
	s := paint()
	if s.text[mid][0] != "«" || s.text[mid][width-1] == "»" {
		t.Fatalf("panned to its end, the marks are %q … %q, want only a «", s.text[mid][0], s.text[mid][width-1])
	}
	if got := s.text[rows[0]][width-2]; got != "┐" {
		t.Fatalf("panned to its end, its box's top ends in %q, want ┐: %q", got, strings.Join(s.text[rows[0]], ""))
	}
}
