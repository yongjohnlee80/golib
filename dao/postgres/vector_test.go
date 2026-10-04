package postgres

import (
	"errors"
	"math"
	"reflect"
	"testing"

	"github.com/yongjohnlee80/golib/dao"
	"github.com/yongjohnlee80/golib/errs"
)

func TestVector_TextForm(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		v    Vector
		want any
	}{
		{nil, nil},
		{Vector{}, "[]"},
		{Vector{1, -2.5, 0.1}, "[1,-2.5,0.1]"},
		{Vector{1e-7, 3.4028235e38}, "[1e-07,3.4028235e+38]"},
	} {
		got, err := tc.v.Value()
		if err != nil || got != tc.want {
			t.Errorf("%v: %v %v, want %v", tc.v, got, err, tc.want)
		}
	}
	for _, bad := range []Vector{{float32(math.NaN())}, {1, float32(math.Inf(1))}, {float32(math.Inf(-1))}} {
		if _, err := bad.Value(); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("%v: %v, want ErrInvalidArgument", bad, err)
		}
	}
}

func TestVector_ScanRoundTrips(t *testing.T) {
	t.Parallel()
	for _, src := range []any{"[1,-2.5,0.1]", []byte("[1, -2.5, 0.1]"), " [1,-2.5,0.1] "} {
		var v Vector
		if err := v.Scan(src); err != nil || !reflect.DeepEqual(v, Vector{1, -2.5, 0.1}) {
			t.Errorf("%q: %v %v", src, v, err)
		}
	}
	var v Vector = Vector{9}
	if err := v.Scan(nil); err != nil || v != nil {
		t.Errorf("nil: %v %v", v, err)
	}
	if err := v.Scan("[]"); err != nil || len(v) != 0 || v == nil {
		t.Errorf("empty: %#v %v", v, err)
	}
	for _, bad := range []any{"1,2", "[1,x]", 42, "[1,2"} {
		if err := v.Scan(bad); !errors.Is(err, errs.ErrInvalidArgument) {
			t.Errorf("%v: %v, want ErrInvalidArgument", bad, err)
		}
	}
}

func TestVectorDistance_Operators(t *testing.T) {
	t.Parallel()
	d := PostgresDialect{}
	for m, want := range map[dao.Metric]string{
		dao.Cosine:       `"t"."emb" <=> $2::vector`,
		dao.L2:           `"t"."emb" <-> $2::vector`,
		dao.InnerProduct: `"t"."emb" <#> $2::vector`,
	} {
		if got := d.VectorDistance(`"t"."emb"`, m, "$2"); got != want {
			t.Errorf("%s: %s, want %s", m, got, want)
		}
	}
}
