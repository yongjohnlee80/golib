package dao

import (
	"fmt"

	"github.com/yongjohnlee80/golib/errs"
)

// ArrayElem is the element type of one [KeyColumn]'s array.
type ArrayElem int

// The element types [KeysIn] binds.
const (
	ArrayInt64 ArrayElem = iota + 1 // bound as []int64
	ArrayInt32                      // bound as []int32
	ArrayText                       // bound as []string
)

// String names the element type.
func (e ArrayElem) String() string {
	switch e {
	case ArrayInt64:
		return "int64"
	case ArrayInt32:
		return "int32"
	case ArrayText:
		return "text"
	}
	return fmt.Sprintf("ArrayElem(%d)", int(e))
}

// KeyColumn is one column of a composite key for [KeysIn], and the element type its values are
// bound as.
type KeyColumn struct {
	Col  string // a raw column, as the other predicates take it
	Elem ArrayElem
}

// ArrayKeyer is an optional [Dialect] capability: the membership of a composite key in a set of
// keys bound as one array per column. [KeysIn] needs it.
type ArrayKeyer interface {
	// KeysInSQL is cols, together, matched against the arrays bound at placeholders, one per
	// column, of the element types elems.
	KeysInSQL(cols []string, elems []ArrayElem, placeholders []string) string
}

// KeysIn matches rows whose columns, together, equal one of keys (each key one value per column,
// in cols' order), with the keys bound as one array per column, whatever their number:
//
//	dao.KeysIn([]dao.KeyColumn{{Col: "c.doc", Elem: dao.ArrayInt64}, {Col: "c.ord", Elem: dao.ArrayInt32}}, keys)
//	// PostgreSQL: (c.doc, c.ord) IN (SELECT * FROM unnest($1::bigint[], $2::integer[]))
//
// Empty keys match nothing, with no parameter. A key of another width than cols, a value that is
// not its column's element type (any Go integer type within range, for the integer elements; a
// string for text), no columns, or an element type outside the constants fail the statement with
// errs.ErrInvalidArgument before anything is sent. An engine without [ArrayKeyer] fails it with
// [ErrUnsupported].
func KeysIn(cols []KeyColumn, keys [][]any) Predicate {
	p := &keysIn{cols: make([]string, len(cols)), elems: make([]ArrayElem, len(cols))}
	if len(cols) == 0 {
		p.err = errs.Wrap(errs.ErrInvalidArgument, "dao: KeysIn needs at least one column")
		return p
	}
	for i, c := range cols {
		p.cols[i], p.elems[i] = c.Col, c.Elem
		switch c.Elem {
		case ArrayInt64, ArrayInt32, ArrayText:
		default:
			p.err = errs.Wrap(errs.ErrInvalidArgument, "dao: KeysIn column %q has element type %s", c.Col, c.Elem)
			return p
		}
	}
	p.arrays, p.err = keyArrays(cols, keys)
	p.empty = len(keys) == 0
	return p
}

type keysIn struct {
	cols   []string
	elems  []ArrayElem
	arrays []any // one per column: []int64, []int32 or []string
	empty  bool
	err    error
}

func (p *keysIn) checkDialect(d Dialect) error {
	if p.err != nil {
		return p.err
	}
	if _, ok := d.(ArrayKeyer); !ok {
		return fmt.Errorf("%w: composite keys bound as arrays on %s", ErrUnsupported, d.Name())
	}
	return nil
}

func (p *keysIn) ToSQL(d Dialect, next *int) (string, []any) {
	if p.empty {
		return "1 = 0", nil
	}
	phs := make([]string, len(p.arrays))
	for i := range p.arrays {
		*next++
		phs[i] = d.Placeholder(*next)
	}
	return d.(ArrayKeyer).KeysInSQL(p.cols, p.elems, phs), append([]any(nil), p.arrays...)
}

// keyArrays transposes keys into one typed array per column.
func keyArrays(cols []KeyColumn, keys [][]any) ([]any, error) {
	out := make([]any, len(cols))
	for i, c := range cols {
		switch c.Elem {
		case ArrayInt64:
			out[i] = make([]int64, 0, len(keys))
		case ArrayInt32:
			out[i] = make([]int32, 0, len(keys))
		case ArrayText:
			out[i] = make([]string, 0, len(keys))
		}
	}
	for k, key := range keys {
		if len(key) != len(cols) {
			return nil, errs.Wrap(errs.ErrInvalidArgument, "dao: KeysIn key %d has %d values for %d columns", k, len(key), len(cols))
		}
		for i, v := range key {
			var ok bool
			switch c := cols[i]; c.Elem {
			case ArrayInt64:
				var n int64
				if n, ok = asInt64(v); ok {
					out[i] = append(out[i].([]int64), n)
				}
			case ArrayInt32:
				var n int64
				if n, ok = asInt64(v); ok && n >= -1<<31 && n <= 1<<31-1 {
					out[i] = append(out[i].([]int32), int32(n))
				} else {
					ok = false
				}
			case ArrayText:
				var s string
				if s, ok = v.(string); ok {
					out[i] = append(out[i].([]string), s)
				}
			}
			if !ok {
				return nil, errs.Wrap(errs.ErrInvalidArgument, "dao: KeysIn key %d: %T %v is not a %s for column %q", k, v, v, cols[i].Elem, cols[i].Col)
			}
		}
	}
	return out, nil
}

// asInt64 is v as an int64, for any Go integer type whose value fits.
func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case uint8:
		return int64(n), true
	case uint16:
		return int64(n), true
	case uint32:
		return int64(n), true
	case uint:
		if uint64(n) <= 1<<63-1 {
			return int64(n), true
		}
	case uint64:
		if n <= 1<<63-1 {
			return int64(n), true
		}
	}
	return 0, false
}
