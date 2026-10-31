package postgres

import (
	"errors"
	"strings"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

// HNSWElement is the cast column with each metric's operator class, halfvec's when the type is, up
// to HNSW's limits inclusive, and refuses a type HNSW cannot index or pgvector has no type for, and a metric outside the
// constants: ErrInvalidArgument naming why.
func TestHNSWElementNamesTheOperatorClassOrRefuses(t *testing.T) {
	f32 := dao.VectorType{Kind: dao.VectorFloat32, Dims: 4}
	for m, want := range map[dao.Metric]string{
		dao.Cosine:       "((embedding::vector(4)) vector_cosine_ops)",
		dao.L2:           "((embedding::vector(4)) vector_l2_ops)",
		dao.InnerProduct: "((embedding::vector(4)) vector_ip_ops)",
	} {
		if got, err := HNSWElement("embedding", m, f32); err != nil || got != want {
			t.Errorf("%s: %q, %v; want %q", m, got, err, want)
		}
	}
	// HNSW's limits are inclusive: 2,000 dimensions of vector, 4,000 of halfvec, each indexed
	for _, c := range []struct {
		t    dao.VectorType
		want string
	}{
		{dao.VectorType{Kind: dao.VectorFloat32, Dims: 2000}, "((e::vector(2000)) vector_ip_ops)"},
		{dao.VectorType{Kind: dao.VectorFloat16, Dims: 3000}, "((e::halfvec(3000)) halfvec_ip_ops)"},
		{dao.VectorType{Kind: dao.VectorFloat16, Dims: 4000}, "((e::halfvec(4000)) halfvec_ip_ops)"},
	} {
		if got, err := HNSWElement("e", dao.InnerProduct, c.t); err != nil || got != c.want {
			t.Errorf("%v: %q, %v; want %q", c.t, got, err, c.want)
		}
	}
	for name, c := range map[string]struct {
		m    dao.Metric
		t    dao.VectorType
		says string
	}{
		"no type for the kind": {dao.Cosine, dao.VectorType{Kind: dao.VectorKind(9), Dims: 4}, "no vector type for kind"},
		"no dimensions":        {dao.Cosine, dao.VectorType{Kind: dao.VectorFloat32}, "1 to"},
		"vector over HNSW's":   {dao.Cosine, dao.VectorType{Kind: dao.VectorFloat32, Dims: 2001}, "up to 2000 dimensions, not 2001"},
		"halfvec over HNSW's":  {dao.Cosine, dao.VectorType{Kind: dao.VectorFloat16, Dims: 4001}, "up to 4000 dimensions, not 4001"},
		"a metric outside":     {dao.Metric(7), f32, "no operator class for metric"},
	} {
		got, err := HNSWElement("embedding", c.m, c.t)
		if got != "" || !errors.Is(err, errs.ErrInvalidArgument) || !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s: %q, %v; want ErrInvalidArgument saying %q", name, got, err, c.says)
		}
	}
}
