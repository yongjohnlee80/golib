package postgres

import (
	"database/sql/driver"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

// VectorDistance implements dao.VectorDistancer with pgvector's operators:
// <=> for cosine, <-> for L2 and <#> for the negated inner product, each
// smaller for nearer, against the bound vector cast to vector.
func (PostgresDialect) VectorDistance(col string, m dao.Metric, placeholder string) string {
	return col + " " + distanceOp(m) + " " + placeholder + "::vector"
}

// distanceOp is pgvector's operator for m, smaller for nearer.
func distanceOp(m dao.Metric) string {
	switch m {
	case dao.L2:
		return "<->"
	case dao.InnerProduct:
		return "<#>"
	}
	return "<=>"
}

var (
	_ dao.VectorDistancer      = PostgresDialect{}
	_ dao.TypedVectorDistancer = PostgresDialect{}
)

// maxVectorDims is pgvector's limit on a vector's and a halfvec's dimensions as a type. An HNSW
// index has lower ones (HNSWElement).
const maxVectorDims = 16000

// CheckVectorType implements dao.TypedVectorDistancer: vector or halfvec, 1 to 16,000
// dimensions.
func (PostgresDialect) CheckVectorType(t dao.VectorType) error {
	switch t.Kind {
	case dao.VectorFloat32, dao.VectorFloat16:
	default:
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: no vector type for kind %s", t.Kind)
	}
	if t.Dims < 1 || t.Dims > maxVectorDims {
		return errs.Wrap(errs.ErrInvalidArgument, "postgres: a %s has 1 to %d dimensions, not %d", vectorTypeName(t.Kind), maxVectorDims, t.Dims)
	}
	return nil
}

// vectorTypeName is pgvector's type for k.
func vectorTypeName(k dao.VectorKind) string {
	if k == dao.VectorFloat16 {
		return "halfvec"
	}
	return "vector"
}

// vectorType is t as a pgvector type with its dimensions, e.g. vector(768). Dims is an integer,
// never text, so nothing but digits reaches the SQL.
func vectorType(t dao.VectorType) string {
	return vectorTypeName(t.Kind) + "(" + strconv.Itoa(t.Dims) + ")"
}

// VectorCast implements dao.TypedVectorDistancer: (col::vector(d)), or halfvec(d). It is the
// one rendering of the cast: TypedVectorDistance orders by it and HNSWElement indexes it.
func (PostgresDialect) VectorCast(col string, t dao.VectorType) string {
	return "(" + col + "::" + vectorType(t) + ")"
}

// TypedVectorDistance implements dao.TypedVectorDistancer: the cast column, the metric's
// operator, and the bound vector cast to the same type.
func (d PostgresDialect) TypedVectorDistance(col string, m dao.Metric, t dao.VectorType, placeholder string) string {
	return d.VectorCast(col, t) + " " + distanceOp(m) + " " + placeholder + "::" + vectorType(t)
}

// HNSW's dimension limits (pgvector 0.8): an index over vector takes up to 2,000, over halfvec
// up to 4,000.
const (
	maxHNSWVectorDims  = 2000
	maxHNSWHalfvecDims = 4000
)

// HNSWElement is the element of an HNSW index over col cast to t, with m's operator class:
// ((col::vector(4)) vector_cosine_ops). Its expression is VectorCast's, byte for byte, so an
// ORDER BY from dao.DistanceAs with the same column and type is the index's own expression. It
// refuses t outside HNSW's limits, or a metric outside the constants, with
// errs.ErrInvalidArgument.
func HNSWElement(col string, m dao.Metric, t dao.VectorType) (string, error) {
	var d PostgresDialect
	if err := d.CheckVectorType(t); err != nil {
		return "", err
	}
	limit := maxHNSWVectorDims
	if t.Kind == dao.VectorFloat16 {
		limit = maxHNSWHalfvecDims
	}
	if t.Dims > limit {
		return "", errs.Wrap(errs.ErrInvalidArgument, "postgres: an HNSW index over %s takes up to %d dimensions, not %d", vectorTypeName(t.Kind), limit, t.Dims)
	}
	var ops string
	switch m {
	case dao.Cosine:
		ops = "_cosine_ops"
	case dao.L2:
		ops = "_l2_ops"
	case dao.InnerProduct:
		ops = "_ip_ops"
	default:
		return "", errs.Wrap(errs.ErrInvalidArgument, "postgres: no operator class for metric %s", m)
	}
	return "(" + d.VectorCast(col, t) + " " + vectorTypeName(t.Kind) + ops + ")", nil
}

// Vector is a pgvector value, bound and scanned in pgvector's text form
// "[v1,v2,…]", so no pgvector module is needed. The extension, the column's
// type and dimensions belong to the consumer's migrations.
//
// A nil Vector is SQL NULL. A NaN or an infinity has no pgvector form and is
// refused when the value is bound. COPY sends binary rows, which this text
// form does not cover: write vectors with INSERT or an upsert.
type Vector []float32

// Value implements driver.Valuer.
func (v Vector) Value() (driver.Value, error) {
	if v == nil {
		return nil, nil
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range v {
		if math.IsNaN(float64(x)) || math.IsInf(float64(x), 0) {
			return nil, errs.Wrap(errs.ErrInvalidArgument, "postgres: a vector cannot hold %v (element %d)", x, i)
		}
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String(), nil
}

// Scan implements sql.Scanner, reading the text form back.
func (v *Vector) Scan(src any) error {
	var s string
	switch t := src.(type) {
	case nil:
		*v = nil
		return nil
	case string:
		s = t
	case []byte:
		s = string(t)
	default:
		return fmt.Errorf("postgres: cannot scan %T into a Vector (%w)", src, errs.ErrInvalidArgument)
	}
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return fmt.Errorf("postgres: %q is not a vector (%w)", s, errs.ErrInvalidArgument)
	}
	body := strings.TrimSpace(s[1 : len(s)-1])
	if body == "" {
		*v = Vector{}
		return nil
	}
	parts := strings.Split(body, ",")
	out := make(Vector, len(parts))
	for i, p := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return fmt.Errorf("postgres: vector element %d %q: %v (%w)", i, p, err, errs.ErrInvalidArgument)
		}
		out[i] = float32(f)
	}
	*v = out
	return nil
}
