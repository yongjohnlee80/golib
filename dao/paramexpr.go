package dao

import (
	"errors"
	"fmt"

	"github.com/yongjohnlee80/golib/errs"
)

// ParamExpr is an expression that binds values when the query renders: an
// ORDER BY term, the sort-position sibling of a [Predicate], or a selected
// field's. Its placeholders are numbered with the statement's others, in the
// order it reads, so a vector or a search query is never inlined as text.
//
// Build a sort key with [Distance] or [RankQuery], declare it with
// [SortParam], and order by it with [AscBy] or [DescBy]. Build a field with
// [SnippetQuery], declare it as a [Field]'s Param, and give its values with
// [Bind]. The fields are unexported so a ParamExpr's arity always matches
// what its renderer binds.
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

// boundField is a resolved [Field].Param: its expression, and why this engine
// cannot render it, if it cannot.
type boundField struct {
	expr ParamExpr
	err  error
}

// resolveFieldParam checks e against d once, at New. A declaration that is
// wrong on every engine panics there, as a [SortParam]'s does; an engine that
// cannot render it (without the capability, or refusing what the declaration
// gives it) is kept as the error a query selecting the field returns.
func resolveFieldParam(d Dialect, key string, e ParamExpr) boundField {
	bf := boundField{expr: e}
	if e.needs == nil {
		return bf
	}
	if err := e.needs(d); err != nil {
		if !errors.Is(err, ErrUnsupported) {
			panic(errs.Fatal{Op: "dao.New", Rule: "a bound field's declaration is invalid", Detail: fmt.Sprintf("%s: %v", key, err)})
		}
		bf.err = err
	}
	return bf
}

// column is the field's projection for d with args, the values a query bound
// it to: rendered when the statement renders, so its placeholders come first.
func (bf boundField) column(d Dialect, key string, args []any, bound bool) (selectCol, error) {
	if bf.err != nil {
		return selectCol{}, fmt.Errorf("dao: field %q: %w", key, bf.err)
	}
	if !bound {
		return selectCol{}, errs.Wrap(errs.ErrInvalidArgument,
			"dao: field %q binds %d value(s); give them with Bind before selecting it", key, bf.expr.arity)
	}
	if len(args) != bf.expr.arity {
		return selectCol{}, errs.Wrap(errs.ErrInvalidArgument,
			"dao: field %q takes %d value(s), got %d", key, bf.expr.arity, len(args))
	}
	vals := append([]any(nil), args...)
	render := bf.expr.render
	return selectCol{bound: func(ph func(any) string) string { return render(d, ph, vals) }}, nil
}

// selectCol is one projected column: its SQL, or, for a bound field, the
// renderer that binds its values through the builder's placeholder function.
type selectCol struct {
	sql   string
	bound func(ph func(any) string) string
}
