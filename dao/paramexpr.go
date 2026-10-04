package dao

import (
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/errs"
)

// ParamExpr is an ORDER BY expression that binds values when the query
// renders: the sort-position sibling of a [Predicate]. Its placeholders are
// numbered with the WHERE's and LIMIT's, in the order the statement reads, so
// a vector or a search query is never inlined as text.
//
// Build one with [Distance] or [RankQuery], declare it with [SortParam], and
// order by it with [AscBy] or [DescBy]. The fields are unexported so a
// ParamExpr's arity always matches what its renderer binds.
type ParamExpr struct {
	// render writes the expression for d, binding through ph each value it
	// places in the statement. args are the values the caller gave, exactly
	// arity of them; an engine whose spelling needs none binds none.
	render func(d Dialect, ph func(any) string, args []any) string

	// arity is how many values a query ordering by it must give.
	arity int

	// needs is what the expression requires of an engine, or nil. An error
	// wrapping [ErrUnsupported] means this engine cannot render it; any other
	// error means the declaration is wrong everywhere.
	needs func(Dialect) error
}

func (e ParamExpr) isSet() bool { return e.render != nil }

// boundSort is a resolved [SortParam] key: its expression, and why this engine
// cannot render it, if it cannot.
type boundSort struct {
	expr ParamExpr
	err  error
}

// resolveSortParam checks e against d once, at New. A declaration that is
// wrong on every engine is a mistake in the code and panics; an engine without
// the capability is kept as the error a query ordering by the key returns.
func resolveSortParam(d Dialect, key string, e ParamExpr) boundSort {
	bs := boundSort{expr: e}
	if e.needs == nil {
		return bs
	}
	if err := e.needs(d); err != nil {
		if !errors.Is(err, ErrUnsupported) {
			panic(errs.Fatal{Op: "dao.New", Rule: "a bound sort key's declaration is invalid", Detail: fmt.Sprintf("%s: %v", key, err)})
		}
		bs.err = err
	}
	return bs
}

// order is the ORDER BY term for s on d: its values bound when the statement
// renders, or why it cannot be.
func (bs boundSort) order(d Dialect, s Sort) (orderClause, error) {
	if bs.err != nil {
		return orderClause{}, bs.err
	}
	args := s.values()
	if len(args) != bs.expr.arity {
		return orderClause{}, errs.Wrap(errs.ErrInvalidArgument,
			"dao: sort key %q takes %d value(s), got %d (order by it with AscBy or DescBy)", s.Key, bs.expr.arity, len(args))
	}
	vals := append([]any(nil), args...)
	render := bs.expr.render
	return orderClause{desc: s.Desc, bound: func(ph func(any) string) string {
		return render(d, ph, vals)
	}}, nil
}
