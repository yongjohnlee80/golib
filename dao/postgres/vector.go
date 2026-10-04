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
	op := "<=>"
	switch m {
	case dao.L2:
		op = "<->"
	case dao.InnerProduct:
		op = "<#>"
	}
	return col + " " + op + " " + placeholder + "::vector"
}

var _ dao.VectorDistancer = PostgresDialect{}

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
