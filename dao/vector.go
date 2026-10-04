package dao

import (
	"fmt"

	"github.com/yongjohnlee80/golib/errs"
)

// Metric is how a vector distance is measured.
type Metric int

// The metrics [Distance] orders by.
const (
	// Cosine is one minus the cosine similarity.
	Cosine Metric = iota
	// L2 is the Euclidean distance.
	L2
	// InnerProduct is the negated inner product, so nearer is smaller.
	InnerProduct
)

// String names the metric.
func (m Metric) String() string {
	switch m {
	case Cosine:
		return "cosine"
	case L2:
		return "L2"
	case InnerProduct:
		return "inner product"
	}
	return fmt.Sprintf("Metric(%d)", int(m))
}

// VectorDistancer is an optional [Dialect] capability: an ORDER BY expression,
// nearest first when ascending, between the vector column col and the vector
// bound at placeholder. [Distance] uses it.
type VectorDistancer interface {
	VectorDistance(col string, m Metric, placeholder string) string
}

// Distance orders rows nearest first (ascending) to a query vector bound when
// the statement renders, for [SortParam]:
//
//	dao.SortParam[…](ByNearest, dao.Distance(dao.T(TableEmbedding, EmbVector), dao.Cosine))
//	….OrderBy(dao.AscBy(ByNearest, postgres.Vector(q))).Limit(10)
//	// PostgreSQL: "embedding"."vector" <=> $1::vector
//
// The vector column, its type and dimensions, and any index on it are the
// consumer's DDL. An engine without [VectorDistancer] leaves the key failing its
// queries with [ErrUnsupported]. A zero col, or a metric other than the
// constants, panics at the call.
func Distance(col Expr, m Metric) ParamExpr {
	col.mustSet("Distance")
	switch m {
	case Cosine, L2, InnerProduct:
	default:
		panic(errs.Fatal{Op: "dao.Distance", Rule: "the metric must be Cosine, L2 or InnerProduct", Detail: m.String()})
	}
	return ParamExpr{
		arity: 1,
		render: func(d Dialect, ph func(any) string, args []any) string {
			return d.(VectorDistancer).VectorDistance(col.render(d), m, ph(args[0]))
		},
		needs: func(d Dialect) error {
			if err := col.check(d); err != nil {
				return err
			}
			if _, ok := d.(VectorDistancer); !ok {
				return fmt.Errorf("%w: vector distance on %s", ErrUnsupported, d.Name())
			}
			return nil
		},
	}
}
