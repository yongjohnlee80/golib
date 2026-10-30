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

// VectorKind is a vector's element type, for [VectorType].
type VectorKind int

// The kinds [DistanceAs] casts to.
const (
	// VectorFloat32 is a vector of 4-byte floats: pgvector's vector.
	VectorFloat32 VectorKind = iota
	// VectorFloat16 is a vector of 2-byte floats: pgvector's halfvec.
	VectorFloat16
)

// String names the kind.
func (k VectorKind) String() string {
	switch k {
	case VectorFloat32:
		return "float32"
	case VectorFloat16:
		return "float16"
	}
	return fmt.Sprintf("VectorKind(%d)", int(k))
}

// VectorType is a vector type and its dimension count, to which a column and its query operand
// are both cast, so an ORDER BY matches an expression index over the same cast.
type VectorType struct {
	Kind VectorKind
	Dims int
}

// TypedVectorDistancer is an optional [Dialect] capability: a vector distance with the column
// and the bound vector both cast to one [VectorType]. [DistanceAs] uses it.
type TypedVectorDistancer interface {
	// CheckVectorType refuses a type the engine has none for (a kind it lacks, dimensions
	// outside 1 to its maximum) with an error wrapping errs.ErrInvalidArgument.
	CheckVectorType(t VectorType) error
	// VectorCast is col cast to t, parenthesized: the expression an index over the cast is
	// defined on. It is the one rendering of the cast, for the index and the query alike.
	VectorCast(col string, t VectorType) string
	// TypedVectorDistance is VectorCast(col, t), m's operator, and the placeholder cast to t:
	// an ORDER BY expression, nearest first when ascending.
	TypedVectorDistance(col string, m Metric, t VectorType, placeholder string) string
}

// DistanceAs is [Distance] with the vector column and the bound vector both cast to t, so the
// ORDER BY is the expression an index over that cast is defined on, and a planner can use it:
//
//	dao.SortParam[…](ByNearest, dao.DistanceAs(dao.T(TableEmbedding, EmbVector), dao.Cosine,
//		dao.VectorType{Kind: dao.VectorFloat32, Dims: 768}))
//	// PostgreSQL: ("embedding"."vector"::vector(768)) <=> $1::vector(768)
//
// The type is the declared key's, fixed like the column: a consumer ordering vectors of several
// types declares a key for each. A zero col, a metric or kind other than the constants, or Dims
// below 1 panics at the call. An engine without [TypedVectorDistancer], or whose
// CheckVectorType refuses t, leaves the key failing its queries with [ErrUnsupported] (and, for
// a refused type, that refusal in the same chain).
func DistanceAs(col Expr, m Metric, t VectorType) ParamExpr {
	col.mustSet("DistanceAs")
	switch m {
	case Cosine, L2, InnerProduct:
	default:
		panic(errs.Fatal{Op: "dao.DistanceAs", Rule: "the metric must be Cosine, L2 or InnerProduct", Detail: m.String()})
	}
	switch t.Kind {
	case VectorFloat32, VectorFloat16:
	default:
		panic(errs.Fatal{Op: "dao.DistanceAs", Rule: "the kind must be VectorFloat32 or VectorFloat16", Detail: t.Kind.String()})
	}
	if t.Dims < 1 {
		panic(errs.Fatal{Op: "dao.DistanceAs", Rule: "a vector type has at least one dimension", Detail: fmt.Sprint(t.Dims)})
	}
	return ParamExpr{
		arity: 1,
		render: func(d Dialect, ph func(any) string, args []any) string {
			return d.(TypedVectorDistancer).TypedVectorDistance(col.render(d), m, t, ph(args[0]))
		},
		needs: func(d Dialect) error {
			if err := col.check(d); err != nil {
				return err
			}
			td, ok := d.(TypedVectorDistancer)
			if !ok {
				return fmt.Errorf("%w: typed vector distance on %s", ErrUnsupported, d.Name())
			}
			if err := td.CheckVectorType(t); err != nil {
				return fmt.Errorf("%w: %w", ErrUnsupported, err)
			}
			return nil
		},
	}
}
