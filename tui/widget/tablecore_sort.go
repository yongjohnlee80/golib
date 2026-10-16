package widget

import (
	"math/big"
	"slices"
	"strconv"
	"strings"
)

// THE SORT — the LOADED rows ordered by one or more fields, as a view of the source: the source
// keeps its order, and rows that arrive later are merged in by Changed. A source that knows each
// cell's kind (TableKinds) is ordered by those kinds; one that does not is ordered by compareCells.

// TableSortKey is one field of a sort and its direction.
type TableSortKey struct {
	Col  int
	Desc bool
}

// TableKinds is a TableSource that knows each cell's kind; Kind is read on the loop, for a source
// row and a field, as Cell is. Without it every cell is text, ordered by compareCells.
type TableKinds interface {
	Kind(row, col int) TableCellKind
}

// TableCellKind is what a cell holds, as one byte. A byte that is none of these is text.
type TableCellKind byte

const (
	TableCellText   TableCellKind = 't'
	TableCellNull   TableCellKind = 'z'
	TableCellNumber TableCellKind = 'n' // the cell's text is the number's literal
	TableCellBool   TableCellKind = 'b'
	TableCellJSON   TableCellKind = 'j'
)

// SortKeys orders the loaded rows by keys, the first deciding first; nil ends the sort, and keys on
// fields the source does not have are dropped. The cursor stays on its source row, revealed, and
// a pager is asked for nothing: a sort rearranges the rows loaded, wherever the cursor lands.
//
// With a source's kinds (TableKinds), a field orders as SQL's ORDER BY does with NULLS LAST:
//   - NULLs last in both directions; DESC reverses only the other cells;
//   - numbers as exact decimals, from their literal text (never through float64), so
//     12345678901234567890.123 sorts before 12345678901234567890.124; a decimal is what the WHERE
//     filter reads as one, and one whose exponent is out of its range (1e999999999) is unparsed;
//   - false before true; text and JSON by their bytes (strings.Compare);
//   - kinds mixed in one field by kind: numbers, then booleans, text, JSON;
//   - a number or boolean whose text does not parse (NaN, Infinity) above the ones that do, by
//     its text, as PostgreSQL orders NaN above every number.
//
// Without kinds each field orders by compareCells: two cells that are both decimals as numbers,
// any others as text. Either way the sort is stable: rows equal on
// every key keep the source's order.
func (c *TableCore) SortKeys(keys []TableSortKey) {
	keep := c.SourceRow(c.cur.Row)
	was := c.keys
	c.keys = c.validKeys(keys)
	c.arrange()
	c.cur.Row = c.displayRow(keep)
	c.show()
	if !slices.Equal(was, c.keys) {
		c.sortChanged()
	}
}

// SortedKeys is the sort, nil when there is none, and whether rows remain to load: the sort then
// covers only the rows loaded.
func (c *TableCore) SortedKeys() (keys []TableSortKey, partial bool) {
	if len(c.keys) == 0 {
		return nil, false
	}
	return slices.Clone(c.keys), c.pager != nil && c.pager.More()
}

// validKeys is keys without those on fields the source lacks, as a new slice; nil when none is left.
func (c *TableCore) validKeys(keys []TableSortKey) []TableSortKey {
	n := len(c.Fields())
	var out []TableSortKey
	for _, k := range keys {
		if k.Col >= 0 && k.Col < n {
			out = append(out, k)
		}
	}
	return out
}

func (c *TableCore) sortChanged() {
	if c.onSort != nil {
		c.onSort(slices.Clone(c.keys))
	}
}

// sorted is the order of every loaded row under the keys, before the filter.
func (c *TableCore) sorted() []int {
	order := make([]int, c.loaded())
	for i := range order {
		order[i] = i
	}
	// Each key's cells are read and parsed once, not once per comparison: a decimal's parse
	// allocates, and a sort compares each row many times.
	if c.kinds == nil {
		cols := make([][]plainCell, len(c.keys))
		for i, k := range c.keys {
			cols[i] = make([]plainCell, len(order))
			for r := range order {
				cols[i][r] = readPlainCell(c.src.Cell(r, k.Col))
			}
		}
		slices.SortStableFunc(order, func(a, b int) int {
			for i, k := range c.keys {
				r := comparePlain(cols[i][a], cols[i][b])
				if k.Desc {
					r = -r
				}
				if r != 0 {
					return r
				}
			}
			return 0
		})
		return order
	}
	cols := make([][]sortCell, len(c.keys))
	for i, k := range c.keys {
		cols[i] = make([]sortCell, len(order))
		for r := range order {
			cols[i][r] = readSortCell(c.kinds.Kind(r, k.Col), c.src.Cell(r, k.Col))
		}
	}
	slices.SortStableFunc(order, func(a, b int) int {
		for i, k := range c.keys {
			if r := compareSortCells(cols[i][a], cols[i][b], k.Desc); r != 0 {
				return r
			}
		}
		return 0
	})
	return order
}

// sortCell is one cell as a kinded sort reads it.
type sortCell struct {
	null bool
	rank int  // the kind's place among kinds: number, bool, text, json
	ok   bool // the number or boolean parsed
	num  *big.Rat
	b    bool
	text string
}

const (
	rankNumber = iota
	rankBool
	rankText
	rankJSON
)

func readSortCell(k TableCellKind, text string) sortCell {
	switch k {
	case TableCellNull:
		return sortCell{null: true}
	case TableCellNumber:
		n, ok := parseDecimal(strings.TrimSpace(text))
		return sortCell{rank: rankNumber, ok: ok, num: n, text: text}
	case TableCellBool:
		b, err := strconv.ParseBool(strings.TrimSpace(text))
		return sortCell{rank: rankBool, ok: err == nil, b: b, text: text}
	case TableCellJSON:
		return sortCell{rank: rankJSON, text: text}
	}
	return sortCell{rank: rankText, text: text}
}

// compareSortCells orders two cells of one field: NULLs last whatever the direction, desc
// reversing the rest.
func compareSortCells(a, b sortCell, desc bool) int {
	switch {
	case a.null && b.null:
		return 0
	case a.null:
		return 1
	case b.null:
		return -1
	}
	r := compareKinded(a, b)
	if desc {
		r = -r
	}
	return r
}

func compareKinded(a, b sortCell) int {
	if a.rank != b.rank {
		return a.rank - b.rank
	}
	if a.rank == rankNumber || a.rank == rankBool {
		switch {
		case a.ok && b.ok && a.rank == rankNumber:
			return a.num.Cmp(b.num)
		case a.ok && b.ok:
			return boolOrder(a.b) - boolOrder(b.b)
		case a.ok:
			return -1
		case b.ok:
			return 1
		}
	}
	return strings.Compare(a.text, b.text)
}

func boolOrder(b bool) int {
	if b {
		return 1
	}
	return 0
}
