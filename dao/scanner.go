package dao

import (
	"reflect"
	"sort"

	"github.com/yongjohnlee80/golib/errs"
)

// queryState is the mutable read/where/shape intent accumulated on a query-scoped
// DAO. Allocated lazily as intent methods are called.
type queryState struct {
	where       []Predicate
	order       []orderClause
	limit       *uint64
	offset      *uint64
	forcedJoins []JoinKey
}

// orderClause is a resolved ORDER BY term (SQL expression + direction).
type orderClause struct {
	expr string
	desc bool
}

// writeState is the mutable staged-value intent for INSERT/UPDATE/UPSERT.
type writeState struct {
	set   orderedSet
	rules map[string]resolvedRule // writeCol → resolved SetRules disposition (ADR-0010)
}

// orderedSet is a column->value map with deterministic (sorted-key) iteration, so
// generated SQL is byte-stable across runs (enables golden tests + statement
// reuse).
type orderedSet struct {
	m map[string]any
}

func (s *orderedSet) put(col string, v any) {
	if s.m == nil {
		s.m = make(map[string]any)
	}
	s.m[col] = v
}

func (s *orderedSet) del(col string) { delete(s.m, col) }

func (s orderedSet) empty() bool { return len(s.m) == 0 }

func (s orderedSet) sortedKeys() []string {
	keys := make([]string, 0, len(s.m))
	for k := range s.m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// rowScanner is the minimal scan surface, satisfied by [Rows].
type rowScanner interface {
	Scan(dest ...any) error
}

// scanRow allocates an R, builds the positional scan targets from the plan, and
// scans one row into it. The plan is the ordered slice of Field.Scan funcs from
// the same resolve pass that produced the projection, so projection and scan
// cannot drift.
func scanRow[R any](newR func() R, plan []func(R) any, row rowScanner) (R, error) {
	r := newR()
	targets := make([]any, len(plan))
	for i, get := range plan {
		targets[i] = get(r)
	}
	if err := row.Scan(targets...); err != nil {
		var zero R
		return zero, err
	}
	return r, nil
}

// scanScalar scans a single scalar from the first row of rows into dest, closing
// rows. It returns [ErrNoRows] when the result is empty.
func scanScalar(rows Rows, dest any) error {
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return ErrNoRows
	}
	if err := rows.Scan(dest); err != nil {
		return err
	}
	return rows.Err()
}

// rowsIterator implements [Iterator] over live driver rows.
type rowsIterator[R any] struct {
	rows      Rows
	plan      []func(R) any
	newR      func() R
	translate func(error) error
	cur       R
	err       error
}

func (it *rowsIterator[R]) Next() bool {
	if it.err != nil {
		return false
	}
	if !it.rows.Next() {
		it.err = it.translate(it.rows.Err())
		return false
	}
	r, err := scanRow(it.newR, it.plan, it.rows)
	if err != nil {
		it.err = it.translate(err)
		return false
	}
	it.cur = r
	return true
}

func (it *rowsIterator[R]) Value() R     { return it.cur }
func (it *rowsIterator[R]) Err() error   { return it.err }
func (it *rowsIterator[R]) Close() error { return it.rows.Close() }

// defaultNewRow returns a row allocator for R. For a pointer row type *T it
// returns a func building new(T); for other R it returns the zero value (a
// consumer with a non-pointer row should supply dao.NewRow).
//
// Note: ADR-0001 suggested a reflection-free default, which is not achievable for
// R = *T in Go generics without a second type parameter. reflect is stdlib, so
// the core's zero-external-dependency property is preserved.
func defaultNewRow[R any]() func() R {
	var zero R
	t := reflect.TypeOf(zero)
	if t == nil || t.Kind() != reflect.Pointer {
		return func() R { var r R; return r }
	}
	elem := t.Elem()
	return func() R { return reflect.New(elem).Interface().(R) }
}

// lastInsertID converts a driver LastInsertId (int64) into the ID type. It
// supports ID = int64; for other ID types use a dialect with RETURNING support
// (the Insert path prefers RETURNING and scans the ID generically).
func lastInsertID[ID any](id64 int64, err error) (ID, error) {
	var zero ID
	if err != nil {
		return zero, err
	}
	if v, ok := any(id64).(ID); ok {
		return v, nil
	}
	return zero, errs.Wrap(errs.ErrUnsupported, "dao: LastInsertId returned int64 but ID is %T; use a RETURNING dialect", zero)
}
